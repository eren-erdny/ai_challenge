package agent_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func requestFor(mode agent.Mode) agent.Request {
	target := agent.Target{Profile: "test", Model: "model-a"}
	return agent.Request{ConversationID: "conversation-1", Prompt: "Solve this task", Mode: mode, Target: target, Temperature: 0.7, Strategy: agent.Standard,
		Control: agent.ControlConfig{Enabled: true, Format: "json", MaxWords: 80, MaxTokens: 200, StopSequences: []string{"<END>"}},
		Targets: []agent.Target{target, {Profile: "test", Model: "model-b"}, {Profile: "other", Model: "model-c"}},
	}
}

func TestAgentScenarios(t *testing.T) {
	for _, tt := range []struct {
		mode          agent.Mode
		calls, checks int
		judge         bool
	}{
		{agent.Free, 1, 0, false}, {agent.Controlled, 1, 1, false}, {agent.Compare, 2, 1, false},
		{agent.TemperatureBenchmark, 4, 0, true}, {agent.ModelBenchmark, 4, 3, true},
	} {
		t.Run(string(tt.mode), func(t *testing.T) {
			request := requestFor(tt.mode)
			var calls []agent.Settings
			runner := agent.New(agent.ClientFunc(func(ctx context.Context, target agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
				if ctx == nil {
					t.Fatal("missing context")
				}
				calls = append(calls, settings)
				isJudge := tt.judge && len(calls) == tt.calls
				if !isJudge && prompt != request.Prompt {
					t.Fatalf("input changed: %q", prompt)
				}
				if settings.Messages[len(settings.Messages)-1].Content != prompt {
					t.Fatal("messages do not contain prompt")
				}
				if isJudge && (!strings.Contains(prompt, "answer-1") || !strings.Contains(prompt, "answer-3")) {
					t.Fatal("judge is missing previous answers")
				}
				return agent.Completion{Content: fmt.Sprintf(`{"summary":"answer-%d","points":["ok"]}`, len(calls)), PromptTokens: 3, CompletionTokens: 7, FinishReason: "stop"}, nil
			}))
			result, err := runner.Run(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != tt.calls || (result.Analysis != nil) != tt.judge {
				t.Fatalf("calls=%d analysis=%v", len(calls), result.Analysis)
			}
			if result.ConversationID != request.ConversationID {
				t.Fatal("conversation id lost")
			}
			checks := 0
			for _, r := range result.Responses {
				if r.Answer.TotalTokens != 10 || r.Answer.Model != r.Target.Model {
					t.Fatal("missing normalized usage or model")
				}
				if r.Validation != nil {
					checks++
					if !r.Validation.Passed {
						t.Fatalf("validation failed: %+v", r.Validation)
					}
				}
			}
			if checks != tt.checks {
				t.Fatalf("checks=%d", checks)
			}
			if tt.mode == agent.Free && (calls[0].Control != nil || len(calls[0].Messages) != 1) {
				t.Fatal("free mode added instructions")
			}
			if tt.mode == agent.TemperatureBenchmark {
				if got := []float64{calls[0].Temperature, calls[1].Temperature, calls[2].Temperature}; !reflect.DeepEqual(got, []float64{0, 1.2, 2}) {
					t.Fatal(got)
				}
			}
			if tt.judge && calls[3].Temperature != 0 {
				t.Fatal("judge temperature changed")
			}
		})
	}
}

func TestPoliciesAndSnapshotAreOwnedByAgent(t *testing.T) {
	request := requestFor(agent.ModelBenchmark)
	request.Strategy = agent.Experts
	var messages []agent.Message
	calls := 0
	runner := agent.New(agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		calls++
		if calls <= 3 {
			if settings.Control.Format != "json" || settings.Control.Stop[0] != "<END>" || settings.Control.MaxTokens != 200 {
				t.Fatal("settings changed between models")
			}
			if !strings.Contains(settings.Messages[0].Content, "Аналитик") {
				t.Fatal("strategy not applied")
			}
			if calls == 1 {
				messages = append([]agent.Message(nil), settings.Messages...)
			} else if !reflect.DeepEqual(messages, settings.Messages) {
				t.Fatal("models received different messages")
			}
			// A dependency changing its arguments cannot change the next call or validation.
			settings.Control.Stop[0] = "changed"
			settings.Control.Format = "plain_text"
			settings.Messages[0].Content = "changed"
			request.Control.StopSequences[0] = "changed"
			request.Targets[1].Model = "changed"
		}
		return agent.Completion{Content: "invalid JSON", FinishReason: "stop"}, nil
	}))
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Responses[1].Target.Model != "model-b" {
		t.Fatal("targets were not captured")
	}
	for _, r := range result.Responses {
		if r.Validation == nil || r.Validation.Passed {
			t.Fatal("output policy did not detect invalid JSON")
		}
	}
}

func TestPartialResultsSurviveModelAndJudgeErrors(t *testing.T) {
	for _, failAt := range []int{2, 4} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			failure := errors.New("provider unavailable")
			calls := 0
			runner := agent.New(agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
				calls++
				if calls == failAt {
					return agent.Completion{}, failure
				}
				return agent.Completion{Content: "completed answer"}, nil
			}))
			result, err := runner.Run(context.Background(), requestFor(agent.ModelBenchmark))
			if !errors.Is(err, failure) || len(result.Responses) != failAt-1 || result.Analysis != nil {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if result.Last() == nil {
				t.Fatal("last successful call lost")
			}
		})
	}
}

func TestValidationPreventsRequests(t *testing.T) {
	for _, change := range []func(*agent.Request){
		func(r *agent.Request) { r.Prompt = " \n" }, func(r *agent.Request) { r.Mode = "invalid" },
		func(r *agent.Request) { r.Temperature = math.NaN() }, func(r *agent.Request) { r.Strategy = "invalid" },
		func(r *agent.Request) { r.Control.Format = "invalid" }, func(r *agent.Request) { r.Targets[2].Model = "" },
	} {
		r := requestFor(agent.ModelBenchmark)
		change(&r)
		runner := agent.New(agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
			t.Fatal("invalid request reached client")
			return agent.Completion{}, nil
		}))
		if _, err := runner.Run(context.Background(), r); err == nil {
			t.Fatal("expected input error")
		}
	}
}

func TestCancellationStopsScenario(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	runner := agent.New(agent.ClientFunc(func(ctx context.Context, _ agent.Target, _ string, _ agent.Settings) (agent.Completion, error) {
		calls++
		if calls == 2 {
			cancel()
			return agent.Completion{}, ctx.Err()
		}
		return agent.Completion{Content: "first answer"}, nil
	}))
	result, err := runner.Run(ctx, requestFor(agent.ModelBenchmark))
	if !errors.Is(err, context.Canceled) || calls != 2 || len(result.Responses) != 1 {
		t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
	}
	_, err = runner.Run(ctx, requestFor(agent.Free))
	if !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatal("cancelled context reached client")
	}
}

func TestAgentDoesNotRetainConversationHistory(t *testing.T) {
	runner := agent.New(agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		if len(settings.Messages) != 1 {
			t.Fatal("unexpected history")
		}
		return agent.Completion{Content: "response"}, nil
	}))
	for _, id := range []string{"one", "two", "one"} {
		r := requestFor(agent.Free)
		r.ConversationID = id
		if _, err := runner.Run(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
}
