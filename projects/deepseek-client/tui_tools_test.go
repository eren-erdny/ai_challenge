package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type tuiTraceExecutor struct {
	err error
}

func (e tuiTraceExecutor) Definitions() []agent.ToolDefinition {
	return []agent.ToolDefinition{{Type: "function", Function: agent.ToolFunction{Name: "github__github_get_repository", Parameters: []byte(`{"type":"object"}`)}}}
}

func (e tuiTraceExecutor) Execute(_ context.Context, name, arguments string) (string, error) {
	if name != "github__github_get_repository" || arguments != `{"owner":"golang","repo":"go"}` {
		return "", errors.New("unexpected tool call")
	}
	return `{"full_name":"golang/go","language":"Go"}`, e.err
}

func TestTUIShowsActualToolResultAndFeedsItToModel(t *testing.T) {
	for _, failed := range []bool{false, true} {
		config := defaultAppConfig()
		config.ResponseControl.Enabled = false
		executor := tuiTraceExecutor{}
		if failed {
			executor.err = errors.New("API unavailable")
		}
		config.Tools = executor
		model := newTUIModel(config, nil)
		calls := 0
		ask := func(_ context.Context, _, _ string, settings requestSettings) (completionResult, error) {
			calls++
			if calls == 1 {
				return completionResult{ToolCalls: []agent.ToolCall{{ID: "demo", Type: "function", Function: agent.FunctionCall{Name: "github__github_get_repository", Arguments: `{"owner":"golang","repo":"go"}`}}}}, nil
			}
			last := settings.Messages[len(settings.Messages)-1]
			want := `{"full_name":"golang/go","language":"Go"}`
			if failed {
				want = "API unavailable"
			}
			if last.Role != "tool" || !strings.Contains(last.Content, want) {
				t.Fatalf("model did not receive actual tool outcome: %+v", last)
			}
			return completionResult{Content: "Final answer"}, nil
		}
		answer := executeQuestionCommand(context.Background(), "", "Describe golang/go", model.state, ask)().(answerMessage)
		want := `Result: {"full_name":"golang/go","language":"Go"}`
		if failed {
			want = "Error: API unavailable"
		}
		if calls != 2 || !strings.Contains(answer.text, "[tool] github__github_get_repository") || !strings.Contains(answer.text, want) || !strings.Contains(answer.text, "Final answer") {
			t.Fatalf("incomplete TUI trace: calls=%d text=%s", calls, answer.text)
		}
	}
}

func TestToolPreviewRedactsAndBoundsUntrustedOutput(t *testing.T) {
	got := toolPreview("secret\x1b[2J"+strings.Repeat("я", 1700), "secret")
	if strings.Contains(got, "secret") || strings.ContainsRune(got, '\x1b') || !strings.Contains(got, "[redacted]") || !strings.HasSuffix(got, "[preview truncated]") {
		t.Fatalf("unsafe or unbounded preview: %q", got)
	}
}
