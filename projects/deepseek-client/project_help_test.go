package main

import (
	"bufio"
	"context"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type helpTestTools struct{ calls []string }

func (*helpTestTools) Definitions() []agent.ToolDefinition {
	return []agent.ToolDefinition{
		{Type: "function", Function: agent.ToolFunction{Name: "read_file", Parameters: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)}},
		{Type: "function", Function: agent.ToolFunction{Name: "write_file", Parameters: []byte(`{"type":"object"}`)}},
	}
}
func (e *helpTestTools) Execute(_ context.Context, name, _ string) (string, error) {
	e.calls = append(e.calls, name)
	return "project docs", nil
}

func TestProjectHelpAdapterEnforcesReadOnly(t *testing.T) {
	config := defaultAppConfig()
	profile, _ := config.activeAPIProfile()
	tools := &helpTestTools{}
	state := sessionState{Mode: modeTask, Strategy: strategyStandard, API: profile, Model: profile.Model, Tools: tools}
	rounds := 0
	ask := func(_ context.Context, _ string, prompt string, s requestSettings) (completionResult, error) {
		rounds++
		if prompt != "Как устроен проект?" {
			t.Fatalf("unparsed question: %q", prompt)
		}
		for _, d := range s.Tools {
			if d.Function.Name == "write_file" {
				t.Fatal("write advertised")
			}
		}
		if rounds == 1 {
			return completionResult{ToolCalls: []agent.ToolCall{
				{ID: "bad", Type: "function", Function: agent.FunctionCall{Name: "write_file", Arguments: "{}"}},
				{ID: "good", Type: "function", Function: agent.FunctionCall{Name: "read_file", Arguments: `{"path":"README.md"}`}},
			}}, nil
		}
		return completionResult{Content: "Структура проекта"}, nil
	}
	var out, errOut strings.Builder
	status := executeQuestion(context.Background(), "fake", "/help Как устроен проект?", state, &out, &errOut, ask)
	if status == nil || errOut.Len() != 0 || strings.Join(tools.calls, ",") != "read_file" {
		t.Fatalf("status=%v errors=%s calls=%v", status, errOut.String(), tools.calls)
	}
	if state.Mode != modeTask {
		t.Fatal("help changed saved session mode")
	}
}
func TestProjectHelpDisabledToolsMakesNoCall(t *testing.T) {
	var out, errOut strings.Builder
	status := executeQuestion(context.Background(), "fake", "/help structure", sessionState{ToolsDisabled: true}, &out, &errOut,
		func(context.Context, string, string, requestSettings) (completionResult, error) {
			t.Fatal("unexpected model call")
			return completionResult{}, nil
		})
	if status != nil || !strings.Contains(errOut.String(), "/tools on") {
		t.Fatalf("status=%v error=%s", status, errOut.String())
	}
}
func TestProjectHelpCLIAndBareHelp(t *testing.T) {
	config := defaultAppConfig()
	config.APIToken = "fake"
	calls := 0
	var out, errOut strings.Builder
	code := runInteractiveSession(bufio.NewReader(strings.NewReader("/help\n/help structure\n/exit\n")), &out, &errOut, config,
		func(_ context.Context, _ string, prompt string, _ requestSettings) (completionResult, error) {
			calls++
			if prompt != "structure" {
				t.Fatalf("prompt=%q", prompt)
			}
			return completionResult{Content: "project structure"}, nil
		})
	if code != 0 || calls != 1 || errOut.Len() != 0 || !strings.Contains(out.String(), "/help ВОПРОС") {
		t.Fatalf("code=%d calls=%d error=%s", code, calls, errOut.String())
	}
}
func TestProjectHelpTUISubmitsQuestion(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.textarea.SetValue("/help structure")
	updated, command := model.submit()
	next := updated.(tuiModel)
	if command == nil || !next.busy {
		t.Fatal("project help was handled as static command")
	}
	bare := newTUIModel(defaultAppConfig(), nil)
	bare.textarea.SetValue("/help")
	updated, command = bare.submit()
	if command != nil || updated.(tuiModel).busy {
		t.Fatal("bare help must remain local")
	}
}
