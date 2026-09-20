package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type invariantReaderStub struct{ values []agent.Invariant }

func (s invariantReaderStub) LoadInvariants(context.Context) ([]agent.Invariant, error) {
	return append([]agent.Invariant(nil), s.values...), nil
}

func testInvariant() agent.Invariant {
	return agent.Invariant{ID: 1, Kind: agent.InvariantStack, Text: "Использовать только Go", Updated: time.Now().UTC()}
}

func TestInvariantConflictReplacesViolatingAnswerWithExplanation(t *testing.T) {
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
		calls++
		joined := ""
		for _, message := range settings.Messages {
			joined += message.Content
		}
		if calls == 1 {
			if !strings.Contains(joined, "mandatory constraints") || !strings.Contains(joined, "Использовать только Go") {
				t.Fatalf("invariants missing from main request: %s", joined)
			}
			return agent.Completion{Content: "Перепишем сервис на Python."}, nil
		}
		if !strings.Contains(joined, "fail-closed invariant compliance checker") || !strings.Contains(prompt, "Перепишем сервис на Python") {
			t.Fatalf("invalid audit request: prompt=%s messages=%s", prompt, joined)
		}
		return agent.Completion{Content: `{"compliant":false,"violations":[1],"explanation":"Python противоречит выбранному стеку Go."}`}, nil
	})
	result, err := agent.New(client).WithInvariants(invariantReaderStub{[]agent.Invariant{testInvariant()}}).Run(context.Background(), agent.Request{Prompt: "Перепиши на Python", Mode: agent.Free, Target: agent.Target{Model: "test"}})
	if err != nil || calls != 2 || len(result.Responses) != 1 {
		t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
	}
	response := result.Responses[0]
	if response.Invariant == nil || !response.Invariant.Refused || !strings.Contains(response.Answer.Content, "[1, stack]") || !strings.Contains(response.Answer.Content, "Python противоречит") || strings.Contains(response.Answer.Content, "Перепишем сервис") {
		t.Fatalf("response=%+v", response)
	}
}

func TestInvariantCompliantAnswerPassesAndMalformedAuditFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		verdict   string
		wantError bool
	}{
		{name: "compliant", verdict: `{"compliant":true,"violations":[],"explanation":""}`},
		{name: "malformed", verdict: `{"compliant":true}`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
				calls++
				if calls == 1 {
					return agent.Completion{Content: "Реализуем на Go."}, nil
				}
				return agent.Completion{Content: tc.verdict}, nil
			})
			result, err := agent.New(client).WithInvariants(invariantReaderStub{[]agent.Invariant{testInvariant()}}).Run(context.Background(), agent.Request{Prompt: "Добавь endpoint", Mode: agent.Free, Target: agent.Target{Model: "test"}})
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "failed closed") || len(result.Responses) != 0 {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				return
			}
			if err != nil || result.Responses[0].Answer.Content != "Реализуем на Go." || result.Responses[0].Invariant == nil || !result.Responses[0].Invariant.Compliant {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestInvariantRefusalDoesNotAdvanceTaskState(t *testing.T) {
	current := agent.TaskState{Goal: "Build API", Stage: agent.TaskPlanning, CurrentStep: "Choose stack", ExpectedAction: "Approve", Updated: time.Now().UTC()}
	tasks := &taskStoreStub{state: current}
	calls := 0
	client := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		calls++
		if calls == 1 {
			return agent.Completion{Content: `{"answer":"Перепишем на Python","task_state":{"goal":"Build API","stage":"execution","current_step":"Rewrite","expected_action":"Deploy","paused":false,"pause_reason":""},"transition":{"action":"advance","from":"planning","to":"execution","gate":"plan_approved","reason":"План утверждён","evidence":"Пользователь утвердил план"}}`}, nil
		}
		return agent.Completion{Content: `{"compliant":false,"violations":[1],"explanation":"Python нарушает ограничение стека."}`}, nil
	})
	result, err := agent.New(client).
		WithTaskStates(tasks).
		WithInvariants(invariantReaderStub{[]agent.Invariant{testInvariant()}}).
		Run(context.Background(), agent.Request{ConversationID: "test", Prompt: "Утверждаю план, используй Python", Mode: agent.Task, Target: agent.Target{Model: "test"}})
	if err != nil || tasks.state != current || result.TaskState == nil || *result.TaskState != current || !result.Responses[0].Invariant.Refused {
		t.Fatalf("result=%+v state=%+v err=%v", result, tasks.state, err)
	}
}
