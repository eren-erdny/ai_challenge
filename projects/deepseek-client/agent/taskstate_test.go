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
		return agent.Completion{Content: `{"answer":"План готов","task_state":{"goal":"Создать API","stage":"planning","current_step":"Согласовать контракт","expected_action":"Подтвердить поля","paused":false,"pause_reason":""},"transition":{"action":"start","from":"","to":"planning","gate":"none","reason":"Задача создана","evidence":""}}`}, nil
	})
	result, err := agent.New(client).WithTaskStates(store).Run(context.Background(), agent.Request{ConversationID: "test", Prompt: "Создай API", Mode: agent.Task, Target: agent.Target{Model: "test"}})
	if err != nil || result.Last().Answer.Content != "План готов" || result.TaskState == nil || result.TaskState.Stage != agent.TaskPlanning || store.saves != 1 {
		t.Fatalf("result=%+v state=%+v err=%v", result, store.state, err)
	}
}

func TestTaskModeRepairsPlainTextEnvelope(t *testing.T) {
	store := &taskStoreStub{}
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, target agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
		calls++
		if calls == 1 {
			return agent.Completion{Content: "План готов. Сначала согласуем контракт.", UsageKnown: true, PromptTokens: 10, CompletionTokens: 8, TotalTokens: 18}, nil
		}
		if settings.Temperature != 0 || settings.MaxOutputTokens != 1024 || len(settings.Tools) != 0 {
			t.Fatalf("repair settings=%+v", settings)
		}
		joined := ""
		for _, message := range settings.Messages {
			joined += message.Content
		}
		if !strings.Contains(joined, "repair malformed output") || !strings.Contains(prompt, `"malformed_candidate":"План готов`) || strings.Contains(joined, "Current persisted task state") {
			t.Fatalf("unexpected repair request: prompt=%s messages=%s", prompt, joined)
		}
		return agent.Completion{Content: `{"answer":"План готов. Сначала согласуем контракт.","task_state":{"goal":"Создать API","stage":"planning","current_step":"Согласовать контракт","expected_action":"Утвердить план","paused":false,"pause_reason":""},"transition":{"action":"start","from":"","to":"planning","gate":"none","reason":"Задача создана","evidence":""}}`, UsageKnown: true, PromptTokens: 20, CompletionTokens: 30, TotalTokens: 50}, nil
	})
	result, err := agent.New(client).WithTaskStates(store).Run(context.Background(), agent.Request{ConversationID: "test", Prompt: "Создай API", Mode: agent.Task, Target: agent.Target{Model: "test", MaxOutputTokens: 2048}})
	if err != nil || calls != 2 || !result.TaskRepaired || store.saves != 1 || result.TaskState == nil || result.TaskState.Stage != agent.TaskPlanning {
		t.Fatalf("result=%+v calls=%d state=%+v err=%v", result, calls, store.state, err)
	}
	last := result.Last()
	if last.Answer.Content != "План готов. Сначала согласуем контракт." || !last.Tokens.UsageKnown || last.Answer.TotalTokens != 68 {
		t.Fatalf("answer=%q tokens=%+v completion=%+v", last.Answer.Content, last.Tokens, last.Answer)
	}
}

func TestTaskModeFailsClosedWhenFormatRepairIsInvalid(t *testing.T) {
	store := &taskStoreStub{}
	calls := 0
	client := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		calls++
		if calls == 1 {
			return agent.Completion{Content: "План без конверта"}, nil
		}
		return agent.Completion{Content: "Повторно не JSON"}, nil
	})
	result, err := agent.New(client).WithTaskStates(store).Run(context.Background(), agent.Request{ConversationID: "test", Prompt: "Создай API", Mode: agent.Task, Target: agent.Target{Model: "test"}})
	if err == nil || !strings.Contains(err.Error(), "format recovery returned invalid JSON") || calls != 2 || store.saves != 0 || result.TaskState != nil || result.TaskRepaired {
		t.Fatalf("result=%+v calls=%d saves=%d err=%v", result, calls, store.saves, err)
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
		return agent.Completion{Content: `{"answer":"Handler готов, проверяю интеграцию","task_state":{"goal":"Publish API","stage":"validation","current_step":"Run integration tests","expected_action":"Проверить результаты","paused":false,"pause_reason":""},"transition":{"action":"advance","from":"execution","to":"validation","gate":"implementation_complete","reason":"Реализация завершена","evidence":"Handler реализован"}}`}, nil
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
	content := `{"answer":"ok","task_state":{"goal":"Goal","stage":"planning","current_step":"Plan","expected_action":"Approve","paused":false,"pause_reason":""},"transition":{"action":"start","from":"","to":"planning","gate":"none","reason":"start","evidence":""}} trailing`
	if _, _, _, err := agent.DecodeTaskCompletion(content, time.Now()); err == nil {
		t.Fatal("trailing content accepted")
	}
}

func TestTaskPlanningRequiresExplicitApproval(t *testing.T) {
	now := time.Now().UTC()
	planning := agent.TaskState{Goal: "Build API", Stage: agent.TaskPlanning, CurrentStep: "Review plan", ExpectedAction: "Approve plan", Updated: now}
	execution := agent.TaskState{Goal: "Build API", Stage: agent.TaskExecution, CurrentStep: "Implement API", ExpectedAction: "Complete implementation", Updated: now}
	proposal := agent.TaskTransitionProposal{Action: agent.TaskTransitionAdvance, From: agent.TaskPlanning, To: agent.TaskExecution, Gate: "plan_approved", Reason: "Start work", Evidence: "User said continue"}

	transition, err := agent.ValidateExplicitTaskTransition(planning, execution, proposal, "Продолжай")
	if err == nil || transition.Allowed || !strings.Contains(err.Error(), "explicitly approves") {
		t.Fatalf("transition=%+v err=%v", transition, err)
	}
	transition, err = agent.ValidateExplicitTaskTransition(planning, execution, proposal, "Утверждаю план, начинай реализацию")
	if err != nil || !transition.Allowed {
		t.Fatalf("approved transition=%+v err=%v", transition, err)
	}
}

func TestTaskLifecycleRejectsSkippedStageAndDoneWithoutEvidence(t *testing.T) {
	now := time.Now().UTC()
	planning := agent.TaskState{Goal: "Goal", Stage: agent.TaskPlanning, CurrentStep: "Plan", ExpectedAction: "Approve", Updated: now}
	validation := agent.TaskState{Goal: "Goal", Stage: agent.TaskValidation, CurrentStep: "Test", ExpectedAction: "Validate", Updated: now}
	if transition, err := agent.ValidateExplicitTaskTransition(planning, validation, agent.TaskTransitionProposal{Action: agent.TaskTransitionAdvance, From: agent.TaskPlanning, To: agent.TaskValidation, Gate: "plan_approved", Reason: "skip", Evidence: "approved"}, "Утверждаю план"); err == nil || transition.Allowed {
		t.Fatalf("skipped stage accepted: transition=%+v err=%v", transition, err)
	}
	done := agent.TaskState{Goal: "Goal", Stage: agent.TaskDone, CurrentStep: "Done", Updated: now}
	proposal := agent.TaskTransitionProposal{Action: agent.TaskTransitionAdvance, From: agent.TaskValidation, To: agent.TaskDone, Gate: "validation_passed", Reason: "finish"}
	if transition, err := agent.ValidateExplicitTaskTransition(validation, done, proposal, "Продолжай"); err == nil || transition.Allowed {
		t.Fatalf("done without evidence accepted: transition=%+v err=%v", transition, err)
	}
}

func TestTaskModeExplainsRejectedTransitionAndPreservesState(t *testing.T) {
	current := agent.TaskState{Goal: "Build API", Stage: agent.TaskPlanning, CurrentStep: "Review plan", ExpectedAction: "Approve plan", Updated: time.Now().UTC()}
	store := &taskStoreStub{state: current}
	client := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		return agent.Completion{Content: `{"answer":"Начинаю писать код","task_state":{"goal":"Build API","stage":"execution","current_step":"Implement API","expected_action":"Run tests","paused":false,"pause_reason":""},"transition":{"action":"advance","from":"planning","to":"execution","gate":"plan_approved","reason":"Пользователь попросил продолжить","evidence":"Продолжай"}}`}, nil
	})
	result, err := agent.New(client).WithTaskStates(store).Run(context.Background(), agent.Request{ConversationID: "test", Prompt: "Продолжай", Mode: agent.Task, Target: agent.Target{Model: "test"}})
	if err != nil || store.state != current || store.saves != 0 || result.TaskState == nil || *result.TaskState != current {
		t.Fatalf("result=%+v state=%+v saves=%d err=%v", result, store.state, store.saves, err)
	}
	if result.TaskTransition == nil || result.TaskTransition.Allowed || !strings.Contains(result.Last().Answer.Content, "Утверждаю план") || strings.Contains(result.Last().Answer.Content, "писать код") {
		t.Fatalf("transition=%+v answer=%q", result.TaskTransition, result.Last().Answer.Content)
	}
}

func TestTaskPauseResumesAtSameStage(t *testing.T) {
	now := time.Now().UTC()
	execution := agent.TaskState{Goal: "Goal", Stage: agent.TaskExecution, CurrentStep: "Build", ExpectedAction: "Continue", Updated: now}
	paused := execution
	paused.Paused, paused.PauseReason = true, "wait"
	pause := agent.TaskTransitionProposal{Action: agent.TaskTransitionPause, From: agent.TaskExecution, To: agent.TaskExecution, Gate: "none", Reason: "User requested pause"}
	if transition, err := agent.ValidateExplicitTaskTransition(execution, paused, pause, "Пауза"); err != nil || !transition.Allowed {
		t.Fatalf("pause transition=%+v err=%v", transition, err)
	}
	resume := agent.TaskTransitionProposal{Action: agent.TaskTransitionResume, From: agent.TaskExecution, To: agent.TaskExecution, Gate: "none", Reason: "User resumed"}
	if transition, err := agent.ValidateExplicitTaskTransition(paused, execution, resume, "Продолжай"); err != nil || !transition.Allowed {
		t.Fatalf("resume transition=%+v err=%v", transition, err)
	}
}
