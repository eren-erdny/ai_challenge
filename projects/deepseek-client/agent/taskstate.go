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

type TaskTransitionAction string

const (
	TaskTransitionStart   TaskTransitionAction = "start"
	TaskTransitionStay    TaskTransitionAction = "stay"
	TaskTransitionAdvance TaskTransitionAction = "advance"
	TaskTransitionPause   TaskTransitionAction = "pause"
	TaskTransitionResume  TaskTransitionAction = "resume"
	TaskTransitionRestart TaskTransitionAction = "restart"
)

type TaskTransition struct {
	Action    TaskTransitionAction `json:"action"`
	From      TaskStage            `json:"from"`
	To        TaskStage            `json:"to"`
	Gate      string               `json:"gate"`
	Reason    string               `json:"reason"`
	Evidence  string               `json:"evidence,omitempty"`
	Allowed   bool                 `json:"allowed"`
	Applied   bool                 `json:"applied"`
	Rejection string               `json:"rejection,omitempty"`
}

type TaskStateStore interface {
	LoadTaskState(context.Context, string) (TaskState, error)
	SaveTaskState(context.Context, string, TaskState, TaskState) error
}

type taskEnvelope struct {
	Answer     string                 `json:"answer"`
	TaskState  taskProposal           `json:"task_state"`
	Transition TaskTransitionProposal `json:"transition"`
}

type taskProposal struct {
	Goal           string    `json:"goal"`
	Stage          TaskStage `json:"stage"`
	CurrentStep    string    `json:"current_step"`
	ExpectedAction string    `json:"expected_action"`
	Paused         bool      `json:"paused"`
	PauseReason    string    `json:"pause_reason"`
}

type TaskTransitionProposal struct {
	Action   TaskTransitionAction `json:"action"`
	From     TaskStage            `json:"from"`
	To       TaskStage            `json:"to"`
	Gate     string               `json:"gate"`
	Reason   string               `json:"reason"`
	Evidence string               `json:"evidence"`
}

type TaskTransitionError struct {
	Code    string
	Message string
}

func (e *TaskTransitionError) Error() string { return e.Message }

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

func validTaskTransitionAction(action TaskTransitionAction) bool {
	switch action {
	case TaskTransitionStart, TaskTransitionStay, TaskTransitionAdvance, TaskTransitionPause, TaskTransitionResume, TaskTransitionRestart:
		return true
	default:
		return false
	}
}

func transitionError(code, message string) error {
	return &TaskTransitionError{Code: code, Message: message}
}

func explicitPlanApproval(prompt string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(prompt, "ё", "е"))
	replacer := strings.NewReplacer(".", " ", ",", " ", "!", " ", "?", " ", ":", " ", ";", " ", "-", " ", "_", " ", "\n", " ", "\r", " ", "\t", " ")
	normalized = strings.Join(strings.Fields(replacer.Replace(normalized)), " ")
	phrases := []string{
		"утверждаю план", "план утвержден", "согласен с планом", "согласна с планом",
		"план согласован", "одобряю план", "принимаю план", "начинай реализацию",
		"начинайте реализацию", "приступай к реализации", "приступайте к реализации",
		"approve the plan", "plan approved", "i approve the plan", "start implementation",
	}
	for _, phrase := range phrases {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

func ValidateExplicitTaskTransition(previous, next TaskState, proposed TaskTransitionProposal, prompt string) (TaskTransition, error) {
	transition := TaskTransition{
		Action:   proposed.Action,
		From:     proposed.From,
		To:       proposed.To,
		Gate:     strings.TrimSpace(proposed.Gate),
		Reason:   strings.TrimSpace(proposed.Reason),
		Evidence: strings.TrimSpace(proposed.Evidence),
	}
	reject := func(code, message string) (TaskTransition, error) {
		transition.Rejection = message
		return transition, transitionError(code, message)
	}
	if !validTaskTransitionAction(transition.Action) {
		return reject("invalid_action", "unknown task transition action")
	}
	if transition.From != previous.Stage || transition.To != next.Stage {
		return reject("state_mismatch", "transition endpoints do not match task states")
	}
	if transition.Reason == "" {
		return reject("missing_reason", "task transition reason is required")
	}
	if err := ValidateTaskTransition(previous, next); err != nil {
		return reject("invalid_path", err.Error())
	}

	expectedAction := TaskTransitionStay
	expectedGate := "none"
	if previous.Stage == "" {
		expectedAction = TaskTransitionStart
	} else if previous.Stage == TaskDone && next.Stage == TaskPlanning && next.Goal != previous.Goal {
		expectedAction = TaskTransitionRestart
	} else if previous.Stage == next.Stage {
		switch {
		case !previous.Paused && next.Paused:
			expectedAction = TaskTransitionPause
		case previous.Paused && !next.Paused:
			expectedAction = TaskTransitionResume
		}
	} else {
		expectedAction = TaskTransitionAdvance
		switch previous.Stage {
		case TaskPlanning:
			expectedGate = "plan_approved"
		case TaskExecution:
			expectedGate = "implementation_complete"
		case TaskValidation:
			expectedGate = "validation_passed"
		}
	}
	if transition.Action != expectedAction {
		return reject("action_mismatch", fmt.Sprintf("transition action must be %s", expectedAction))
	}
	if transition.Gate != expectedGate {
		return reject("gate_mismatch", fmt.Sprintf("transition gate must be %s", expectedGate))
	}
	if expectedGate != "none" && transition.Evidence == "" {
		return reject("missing_evidence", fmt.Sprintf("transition %s -> %s requires evidence", previous.Stage, next.Stage))
	}
	if expectedGate == "plan_approved" && !explicitPlanApproval(prompt) {
		return reject("plan_not_approved", "planning cannot advance until the user explicitly approves the plan")
	}
	transition.Allowed = true
	return transition, nil
}

func TaskTransitionRefusal(previous TaskState, proposed TaskTransition, err error) string {
	current := string(previous.Stage)
	if current == "" {
		current = "not_started"
	}
	message := "Переход состояния отклонён: " + err.Error() + "."
	var transitionErr *TaskTransitionError
	if errors.As(err, &transitionErr) {
		switch transitionErr.Code {
		case "plan_not_approved":
			message = "Переход planning → execution отклонён: сначала явно утвердите предложенный план, например сообщением «Утверждаю план»."
		case "invalid_path":
			message = fmt.Sprintf("Переход %s → %s отклонён: этапы задачи нельзя пропускать.", current, proposed.To)
		case "missing_evidence":
			message = fmt.Sprintf("Переход %s → %s отклонён: требуется зафиксировать результат текущего этапа.", current, proposed.To)
		case "gate_mismatch":
			message = fmt.Sprintf("Переход %s → %s отклонён: не выполнено обязательное условие этого перехода.", current, proposed.To)
		case "action_mismatch", "state_mismatch", "invalid_action":
			message = fmt.Sprintf("Переход %s → %s отклонён: описание перехода не соответствует текущему состоянию задачи.", current, proposed.To)
		case "missing_reason":
			message = fmt.Sprintf("Переход %s → %s отклонён: причина перехода не указана.", current, proposed.To)
		}
	}
	return message + " Текущее состояние сохранено: " + current + "."
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
	instruction := `You are operating a persistent task state machine. Return ONLY one JSON object with exactly this schema: {"answer":"user-facing answer","task_state":{"goal":"stable task goal","stage":"planning|execution|validation|done","current_step":"specific current step","expected_action":"what should happen next, empty only when done","paused":false,"pause_reason":"empty unless paused"},"transition":{"action":"start|stay|advance|pause|resume|restart","from":"previous stage or empty","to":"next stage","gate":"none|plan_approved|implementation_complete|validation_passed","reason":"short explanation","evidence":"concrete evidence for a satisfied gate, otherwise empty"}}. Allowed stage path is planning -> execution -> validation -> done; a stage may also stay unchanged. A new task starts at planning. Do not perform implementation while planning. Advance planning to execution only when the current user message explicitly approves the plan; Continue alone is not approval. Advance execution to validation only after implementation is complete and cite concrete evidence. Advance validation to done only after validation passed and cite the result. Failed validation stays at validation. A paused task may only stay paused or resume at the same stage; resuming and advancing require separate responses. Use action start for the first planning state, advance for the next stage, stay without stage/pause changes, pause or resume for pause changes, and restart only for a different goal after done. Use gate none except for advance, where the gates in order are plan_approved, implementation_complete, validation_passed. Continue an existing task without asking the user to repeat context. Interpret ordinary-language pause and resume requests. Continue on a done task keeps it done. If the user requests a forbidden transition, keep the current state and explain which prerequisite is missing. The user-facing answer must not expose the JSON protocol.`
	return []Message{{Role: "system", Content: instruction + " Current persisted task state: " + current}}, nil
}

func DecodeTaskCompletion(content string, now time.Time) (string, TaskState, TaskTransitionProposal, error) {
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
		return "", TaskState{}, TaskTransitionProposal{}, fmt.Errorf("invalid task-mode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", TaskState{}, TaskTransitionProposal{}, errors.New("invalid task-mode JSON: trailing content")
	}
	if strings.TrimSpace(envelope.Answer) == "" {
		return "", TaskState{}, TaskTransitionProposal{}, errors.New("task-mode answer is empty")
	}
	next := TaskState{Goal: envelope.TaskState.Goal, Stage: envelope.TaskState.Stage, CurrentStep: envelope.TaskState.CurrentStep, ExpectedAction: envelope.TaskState.ExpectedAction, Paused: envelope.TaskState.Paused, PauseReason: envelope.TaskState.PauseReason, Updated: now.UTC()}
	if err := ValidateTaskState(next); err != nil {
		return "", TaskState{}, TaskTransitionProposal{}, err
	}
	return envelope.Answer, next, envelope.Transition, nil
}
