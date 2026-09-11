package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func TestTokenEstimates(t *testing.T) {
	c := agent.ApproximateCounter{}
	for text, want := range map[string]int{"": 0, "abcd": 1, "abcde": 2, "Привет": 6, "🙂": 1} {
		if got := c.Count(text); got != want {
			t.Fatalf("%q: %d != %d", text, got, want)
		}
	}
}

func TestContextBudgetAndActualUsage(t *testing.T) {
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		calls++
		if s.MaxOutputTokens != 5 {
			t.Fatalf("reserve not passed: %d", s.MaxOutputTokens)
		}
		return agent.Completion{Content: "answer", PromptTokens: 42, CompletionTokens: 9, UsageKnown: true}, nil
	})
	r := agent.Request{Mode: agent.Free, Prompt: "abcd", Target: agent.Target{Model: "test", ContextWindow: 13, MaxOutputTokens: 5}}
	result, err := agent.New(client).Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	report := result.Responses[0].Tokens
	if report.LastUserMessageEstimate != 1 || report.InputEstimate != 8 || report.InputActual != 42 || report.OutputActual != 9 || report.TotalActual != 51 || !report.UsageKnown {
		t.Fatalf("report: %+v", report)
	}
	r.Target.ContextWindow = 12
	result, err = agent.New(client).Run(context.Background(), r)
	var overflow *agent.ContextLimitError
	if !errors.As(err, &overflow) || calls != 1 || result.Failed == nil || result.Failed.Tokens.OutputReserve != 5 {
		t.Fatalf("overflow: %+v %v calls=%d", result, err, calls)
	}
}

func TestMissingUsageIsNotFree(t *testing.T) {
	c := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		return agent.Completion{Content: "text"}, nil
	})
	r, err := agent.New(c).Run(context.Background(), agent.Request{Prompt: "hi", Target: agent.Target{Model: "deepseek-v4-flash", BaseURL: "https://api.deepseek.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Responses[0].Tokens.UsageKnown || r.Responses[0].CostUSD != nil {
		t.Fatal("missing usage treated as exact zero")
	}
}
