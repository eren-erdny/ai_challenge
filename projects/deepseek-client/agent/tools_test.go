package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type failingTools struct{ calls int }

func (*failingTools) Definitions() []agent.ToolDefinition { return nil }
func (f *failingTools) Execute(context.Context, string, string) (string, error) {
	f.calls++
	return "", errors.New("unknown tool")
}

func TestToolLoopBoundedAndErrorsReturned(t *testing.T) {
	tools := &failingTools{}
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		calls++
		if calls > 1 {
			last := s.Messages[len(s.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "call" || !strings.Contains(last.Content, "unknown tool") {
				t.Fatalf("missing tool error: %+v", last)
			}
		}
		return agent.Completion{ToolCalls: []agent.ToolCall{{ID: "call", Type: "function", Function: agent.FunctionCall{Name: "unknown", Arguments: "{}"}}}}, nil
	})
	_, err := agent.New(client).WithTools(tools).Run(context.Background(), agent.Request{Prompt: "read", Target: agent.Target{Model: "test"}})
	if err == nil || !strings.Contains(err.Error(), "tool call limit") || calls != 4 || tools.calls != 3 {
		t.Fatalf("calls=%d executions=%d error=%v", calls, tools.calls, err)
	}
}

func TestToolCycleMissingUsageIsIncomplete(t *testing.T) {
	calls := 0
	client := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		calls++
		if calls == 1 {
			return agent.Completion{ToolCalls: []agent.ToolCall{{ID: "call", Type: "function", Function: agent.FunctionCall{Name: "unknown", Arguments: "{}"}}}}, nil
		}
		return agent.Completion{Content: "Cannot read", PromptTokens: 30, CompletionTokens: 5}, nil
	})
	result, err := agent.New(client).WithTools(&failingTools{}).Run(context.Background(), agent.Request{Prompt: "read", Target: agent.Target{Model: "deepseek-v4-flash", BaseURL: "https://api.deepseek.com"}})
	if err != nil {
		t.Fatal(err)
	}
	r := result.Responses[0]
	if r.Tokens.UsageKnown || r.CostUSD != nil || r.Answer.PromptTokens != 30 {
		t.Fatalf("partial usage reported as complete: %+v", r)
	}
}
