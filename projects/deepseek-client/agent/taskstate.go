package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type TaskStage string

const (
	TaskPlanning   TaskStage = "planning"
	TaskExecution  TaskStage = "execution"
	TaskValidation TaskStage = "validation"
	TaskDone       TaskStage = "done"
)

type TaskState struct {
	Goal           string    `json:"goal"`
	Stage          TaskStage `json:"stage"`
	CurrentStep    string    `json:"current_step"`
	ExpectedAction string    `json:"expected_action"`
	Paused         bool      `json:"paused"`
	PauseReason    string    `json:"pause_reason,omitempty"`
	Updated        time.Time `json:"updated"`
}

type TaskStateStore interface {
	LoadTaskState(context.Context, string) (TaskState, error)
	SaveTaskState(context.Context, string, TaskState, TaskState) error
}

type taskEnvelope struct {
	Answer    string       `json:"answer"`
	TaskState taskProposal `json:"task_state"`
}

type taskProposal struct {
	Goal           string    `json:"goal"`
	Stage          TaskStage `json:"stage"`
	CurrentStep    string    `json:"current_step"`
	ExpectedAction string    `json:"expected_action"`
	Paused         bool      `json:"paused"`
	PauseReason    string    `json:"pause_reason"`
}

func ValidTaskStage(stage TaskStage) bool {
	return stage == TaskPlanning || stage == TaskExecution || stage == TaskValidation || stage == TaskDone
}

func ValidateTaskState(state TaskState) error {
	if state.Stage == "" {
		if state.Goal == "" && state.CurrentStep == "" && state.ExpectedAction == "" && !state.Paused && state.PauseReason == "" && state.Updated.IsZero() {
			return nil
		}
		return errors.New("incomplete task state")
	}
	if !ValidTaskStage(state.Stage) || strings.TrimSpace(state.Goal) == "" || strings.TrimSpace(state.CurrentStep) == "" || len(state.Goal) > 4000 || len(state.CurrentStep) > 2000 || len(state.ExpectedAction) > 2000 || len(state.PauseReason) > 2000 || state.Updated.IsZero() {
		return errors.New("invalid task state")
	}
	if state.Stage != TaskDone && strings.TrimSpace(state.ExpectedAction) == "" {
		return errors.New("expected action is required before done")
	}
	if !state.Paused && state.PauseReason != "" {
		return errors.New("pause reason requires paused state")
	}
	return nil
}

func nextTaskStage(stage TaskStage) TaskStage {
	switch stage {
	case TaskPlanning:
		return TaskExecution
	case TaskExecution:
		return TaskValidation
	case TaskValidation:
		return TaskDone
	default:
		return ""
	}
}

func ValidateTaskTransition(previous, next TaskState) error {
	if err := ValidateTaskState(next); err != nil {
		return err
	}
	if previous.Stage == "" {
		if next.Stage != TaskPlanning {
			return errors.New("new task must start at planning")
		}
		return nil
	}
	if err := ValidateTaskState(previous); err != nil {
		return fmt.Errorf("previous state: %w", err)
	}
	if previous.Paused && next.Stage != previous.Stage {
		return errors.New("paused task must resume before changing stage")
	}
	if previous.Stage == TaskDone {
		if next.Stage == TaskPlanning && next.Goal != previous.Goal {
			return nil
		}
		if next.Stage != TaskDone {
			return errors.New("done task can only start a different goal at planning")
		}
	}
	if next.Goal != previous.Goal {
		return errors.New("task goal cannot change before done")
	}
	if next.Stage != previous.Stage && next.Stage != nextTaskStage(previous.Stage) {
		return fmt.Errorf("invalid task transition %s -> %s", previous.Stage, next.Stage)
	}
	return nil
}

func TaskModeMessages(mode Mode, state TaskState) ([]Message, error) {
	if mode != Task {
		return nil, nil
	}
	if err := ValidateTaskState(state); err != nil {
		return nil, err
	}
	current := "null"
	if state.Stage != "" {
		data, err := json.Marshal(state)
		if err != nil {
			return nil, err
		}
		current = string(data)
	}
	instruction := `You are operating a persistent task state machine. Return ONLY one JSON object with exactly this schema: {"answer":"user-facing answer","task_state":{"goal":"stable task goal","stage":"planning|execution|validation|done","current_step":"specific current step","expected_action":"what should happen next, empty only when done","paused":false,"pause_reason":"empty unless paused"}}. For a new task, infer the goal from the user's request and start at planning. For an existing task, continue it when the user says Continue or equivalent without asking them to repeat context. The stage may stay unchanged or advance by exactly one step: planning to execution, execution to validation, validation to done. Never advance a paused task in the same response that resumes it. Interpret an ordinary-language request to pause or resume the task. When the current task is done and the user gives a different task, create a new planning state with the new goal; Continue on a done task keeps it done. The user-facing answer must not expose the JSON protocol.`
	return []Message{{Role: "system", Content: instruction + " Current persisted task state: " + current}}, nil
}

func DecodeTaskCompletion(content string, now time.Time) (string, TaskState, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```json") && strings.HasSuffix(content, "```") {
		content = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content, "```json"), "```"))
	} else if strings.HasPrefix(content, "```") && strings.HasSuffix(content, "```") {
		content = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content, "```"), "```"))
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	var envelope taskEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return "", TaskState{}, fmt.Errorf("invalid task-mode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", TaskState{}, errors.New("invalid task-mode JSON: trailing content")
	}
	if strings.TrimSpace(envelope.Answer) == "" {
		return "", TaskState{}, errors.New("task-mode answer is empty")
	}
	next := TaskState{Goal: envelope.TaskState.Goal, Stage: envelope.TaskState.Stage, CurrentStep: envelope.TaskState.CurrentStep, ExpectedAction: envelope.TaskState.ExpectedAction, Paused: envelope.TaskState.Paused, PauseReason: envelope.TaskState.PauseReason, Updated: now.UTC()}
	if err := ValidateTaskState(next); err != nil {
		return "", TaskState{}, err
	}
	return envelope.Answer, next, nil
}
