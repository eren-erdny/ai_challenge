package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type taskStoreStub struct {
	state agent.TaskState
	saves int
}

func (s *taskStoreStub) LoadTaskState(context.Context, string) (agent.TaskState, error) {
	return s.state, nil
}

func (s *taskStoreStub) SaveTaskState(_ context.Context, _ string, previous, next agent.TaskState) error {
	if previous != s.state {
		return errors.New("unexpected previous state")
	}
	if err := agent.ValidateTaskTransition(previous, next); err != nil {
		return err
	}
	s.state = next
	s.saves++
	return nil
}

func TestTaskModeCreatesStateAndHidesProtocol(t *testing.T) {
	store := &taskStoreStub{}
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		joined := ""
		for _, message := range settings.Messages {
			joined += message.Content
		}
		if !strings.Contains(joined, "persistent task state machine") || !strings.Contains(joined, "Current persisted task state: null") {
			t.Fatalf("task instruction missing: %s", joined)
		}
		return agent.Completion{Content: `{"answer":"План готов","task_state":{"goal":"Создать API","stage":"planning","current_step":"Согласовать контракт","expected_action":"Подтвердить поля","paused":false,"pause_reason":""}}`}, nil
	})
	result, err := agent.New(client).WithTaskStates(store).Run(context.Background(), agent.Request{ConversationID: "test", Prompt: "Создай API", Mode: agent.Task, Target: agent.Target{Model: "test"}})
	if err != nil || result.Last().Answer.Content != "План готов" || result.TaskState == nil || result.TaskState.Stage != agent.TaskPlanning || store.saves != 1 {
		t.Fatalf("result=%+v state=%+v err=%v", result, store.state, err)
	}
}

func TestTaskModeContinuesWithoutRepeatedExplanation(t *testing.T) {
	store := &taskStoreStub{state: agent.TaskState{Goal: "Publish API", Stage: agent.TaskExecution, CurrentStep: "Implement handler", ExpectedAction: "Run integration tests", Updated: time.Now().UTC()}}
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
		if prompt != "Продолжай" {
			t.Fatalf("prompt=%q", prompt)
		}
		joined := ""
		for _, message := range settings.Messages {
			joined += message.Content
		}
		for _, want := range []string{`"goal":"Publish API"`, `"stage":"execution"`, `"current_step":"Implement handler"`} {
			if !strings.Contains(joined, want) {
				t.Fatalf("missing %s in %s", want, joined)
			}
		}
		return agent.Completion{Content: `{"answer":"Handler готов, проверяю интеграцию","task_state":{"goal":"Publish API","stage":"validation","current_step":"Run integration tests","expected_action":"Проверить результаты","paused":false,"pause_reason":""}}`}, nil
	})
	result, err := agent.New(client).WithTaskStates(store).Run(context.Background(), agent.Request{ConversationID: "test", Prompt: "Продолжай", Mode: agent.Task, Target: agent.Target{Model: "test"}})
	if err != nil || result.TaskState == nil || result.TaskState.Stage != agent.TaskValidation || store.state.Stage != agent.TaskValidation {
		t.Fatalf("result=%+v state=%+v err=%v", result, store.state, err)
	}
}

func TestTaskTransitionValidation(t *testing.T) {
	now := time.Now().UTC()
	planning := agent.TaskState{Goal: "Goal", Stage: agent.TaskPlanning, CurrentStep: "Plan", ExpectedAction: "Approve", Updated: now}
	paused := planning
	paused.Paused, paused.PauseReason = true, "wait"
	execution := agent.TaskState{Goal: "Goal", Stage: agent.TaskExecution, CurrentStep: "Build", ExpectedAction: "Run", Updated: now}
	if err := agent.ValidateTaskTransition(planning, execution); err != nil {
		t.Fatal(err)
	}
	if err := agent.ValidateTaskTransition(paused, execution); err == nil {
		t.Fatal("paused task advanced")
	}
	if err := agent.ValidateTaskTransition(agent.TaskState{}, execution); err == nil {
		t.Fatal("new task skipped planning")
	}
	done := agent.TaskState{Goal: "Goal", Stage: agent.TaskDone, CurrentStep: "Done", Updated: now}
	newPlanning := agent.TaskState{Goal: "Another goal", Stage: agent.TaskPlanning, CurrentStep: "Plan", ExpectedAction: "Approve", Updated: now}
	if err := agent.ValidateTaskTransition(done, newPlanning); err != nil {
		t.Fatalf("new task after done: %v", err)
	}
	changedGoal := planning
	changedGoal.Goal = "Changed"
	if err := agent.ValidateTaskTransition(planning, changedGoal); err == nil {
		t.Fatal("active task goal changed")
	}
	pausedDone := done
	pausedDone.Paused, pausedDone.PauseReason = true, "wait"
	if err := agent.ValidateTaskTransition(pausedDone, newPlanning); err == nil {
		t.Fatal("paused done task started another goal")
	}
}

func TestTaskCompletionRejectsTrailingContent(t *testing.T) {
	content := `{"answer":"ok","task_state":{"goal":"Goal","stage":"planning","current_step":"Plan","expected_action":"Approve","paused":false,"pause_reason":""}} trailing`
	if _, _, err := agent.DecodeTaskCompletion(content, time.Now()); err == nil {
		t.Fatal("trailing content accepted")
	}
}
