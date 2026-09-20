package agent_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/filetools"
)

type failingTools struct{ calls int }

func (*failingTools) Definitions() []agent.ToolDefinition {
	return []agent.ToolDefinition{{Type: "function", Function: agent.ToolFunction{Name: "unknown", Parameters: []byte(`{"type":"object"}`)}}}
}
func (f *failingTools) Execute(context.Context, string, string) (string, error) {
	f.calls++
	return "", errors.New("unknown tool")
}

func TestToolLoopBoundedAndErrorsReturned(t *testing.T) {
	tools := &failingTools{}
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		calls++
		if len(s.Tools) == 0 {
			last := s.Messages[len(s.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, "not executed") {
				t.Fatalf("missing exhausted-budget result: %+v", last)
			}
			return agent.Completion{ToolCalls: []agent.ToolCall{{ID: "after-limit", Type: "function", Function: agent.FunctionCall{Name: "unknown", Arguments: "{}"}}}}, nil
		}
		if calls > 1 {
			last := s.Messages[len(s.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "call" || !strings.Contains(last.Content, "unknown tool") {
				t.Fatalf("missing tool error: %+v", last)
			}
		}
		return agent.Completion{ToolCalls: []agent.ToolCall{{ID: "call", Type: "function", Function: agent.FunctionCall{Name: "unknown", Arguments: "{}"}}}}, nil
	})
	_, err := agent.New(client).WithTools(tools).Run(context.Background(), agent.Request{Prompt: "read", Target: agent.Target{Model: "test"}})
	if err == nil || !strings.Contains(err.Error(), "after the tool budget") || calls != 9 || tools.calls != 7 {
		t.Fatalf("calls=%d executions=%d error=%v", calls, tools.calls, err)
	}
}

func TestToolLimitReturnsFinalStatusWithoutExecutingPendingCall(t *testing.T) {
	tools := &failingTools{}
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		calls++
		if len(settings.Tools) == 0 {
			last := settings.Messages[len(settings.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, "not executed") {
				t.Fatalf("missing pending-call failure: %+v", last)
			}
			return agent.Completion{Content: "Выполнено частично; остался один шаг.", PromptTokens: 3, CompletionTokens: 4, UsageKnown: true}, nil
		}
		return agent.Completion{ToolCalls: []agent.ToolCall{{ID: fmt.Sprintf("call-%d", calls), Type: "function", Function: agent.FunctionCall{Name: "unknown", Arguments: "{}"}}}, PromptTokens: 1, CompletionTokens: 2, UsageKnown: true}, nil
	})
	result, err := agent.New(client).WithTools(tools).Run(context.Background(), agent.Request{Prompt: "work", Target: agent.Target{Model: "test"}})
	answer := result.Last().Answer
	if err != nil || calls != 9 || tools.calls != 7 || answer.Content != "Выполнено частично; остался один шаг." || answer.PromptTokens != 11 || answer.CompletionTokens != 20 {
		t.Fatalf("result=%+v calls=%d executions=%d error=%v", result, calls, tools.calls, err)
	}
}

func TestToolBatchCanContainMoreThanEightCalls(t *testing.T) {
	tools := &failingTools{}
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		calls++
		if calls == 1 {
			toolCalls := make([]agent.ToolCall, 9)
			for i := range toolCalls {
				toolCalls[i] = agent.ToolCall{ID: fmt.Sprintf("batch-%d", i), Type: "function", Function: agent.FunctionCall{Name: "unknown", Arguments: "{}"}}
			}
			return agent.Completion{ToolCalls: toolCalls}, nil
		}
		toolResults := 0
		for _, message := range settings.Messages {
			if message.Role == "tool" {
				toolResults++
			}
		}
		if toolResults != 9 {
			t.Fatalf("tool results=%d", toolResults)
		}
		return agent.Completion{Content: "batch processed"}, nil
	})
	result, err := agent.New(client).WithTools(tools).Run(context.Background(), agent.Request{Prompt: "create files", Target: agent.Target{Model: "test"}})
	if err != nil || calls != 2 || tools.calls != 9 || result.Last().Answer.Content != "batch processed" {
		t.Fatalf("result=%+v calls=%d executions=%d error=%v", result, calls, tools.calls, err)
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

func TestToolLoopCreatesKotlinClass(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		calls++
		if calls == 1 {
			if len(settings.Tools) != 7 || !strings.Contains(settings.Messages[0].Content, "edit_file") {
				t.Fatalf("workspace tools not advertised: %+v", settings)
			}
			return agent.Completion{ToolCalls: []agent.ToolCall{
				{ID: "mkdir", Type: "function", Function: agent.FunctionCall{Name: "create_directory", Arguments: `{"path":"src/main/kotlin/com/example"}`}},
				{ID: "write", Type: "function", Function: agent.FunctionCall{Name: "write_file", Arguments: `{"path":"src/main/kotlin/com/example/User.kt","content":"package com.example\n\ndata class User(val id: Long)\n"}`}},
			}}, nil
		}
		if calls != 2 {
			t.Fatalf("unexpected call %d", calls)
		}
		messages := settings.Messages
		if len(messages) < 3 || messages[len(messages)-2].ToolCallID != "mkdir" || messages[len(messages)-1].ToolCallID != "write" || !strings.Contains(messages[len(messages)-1].Content, `"bytes"`) {
			t.Fatalf("tool results missing: %+v", messages)
		}
		return agent.Completion{Content: "Класс User создан."}, nil
	})
	result, err := agent.New(client).WithTools(&filetools.Documents{Dir: dir}).Run(context.Background(), agent.Request{Prompt: "Создай Kotlin-класс User", Target: agent.Target{Model: "test"}})
	if err != nil || calls != 2 || result.Last().Answer.Content != "Класс User создан." || result.Last().FileContext != "" {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "src", "main", "kotlin", "com", "example", "User.kt"))
	if err != nil || !strings.Contains(string(data), "data class User") {
		t.Fatalf("file=%q err=%v", data, err)
	}
}
