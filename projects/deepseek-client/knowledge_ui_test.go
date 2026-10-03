package main

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/knowledge"
	"strings"
	"testing"
)

func TestKnowledgeMenuAndWizardDoNotSendInputsToModel(t *testing.T) {
	config := defaultAppConfig()
	config.Knowledge = &knowledge.Manager{Dir: t.TempDir()}
	calls := 0
	model := newTUIModel(config, func(context.Context, string, string, requestSettings) (completionResult, error) {
		calls++
		return completionResult{}, nil
	})
	result, _ := model.Update(keyPress(tea.KeyF2))
	model = result.(tuiModel)
	if model.knowledgeStage != "menu" || !strings.Contains(strings.Join(model.history, "\n"), "Создать базу") {
		t.Fatal("missing knowledge menu")
	}
	model.textarea.SetValue("1")
	result, _ = model.Update(keyPress(tea.KeyEnter))
	model = result.(tuiModel)
	model.textarea.SetValue("Работа")
	result, _ = model.Update(keyPress(tea.KeyEnter))
	model = result.(tuiModel)
	if model.knowledgeStage != "path" || model.knowledgeName != "Работа" {
		t.Fatal("wizard did not request source")
	}
	model.textarea.SetValue("/cancel")
	result, _ = model.Update(keyPress(tea.KeyEnter))
	model = result.(tuiModel)
	if model.knowledgeStage != "" || calls != 0 {
		t.Fatal("wizard called the LLM")
	}
}
func TestKnowledgeEvidenceIsUntrustedAndCountedBeforeLLM(t *testing.T) {
	evidence := `{"results":[{"source":"manual.md","chunk_id":"abc","text":"Retention is fourteen days"}]}`
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
		calls++
		found, guard := false, false
		for _, m := range settings.Messages {
			if m.Role == "user" && strings.Contains(m.Content, evidence) {
				found = true
			}
			if m.Role == "system" && strings.Contains(m.Content, "untrusted evidence") {
				guard = true
			}
		}
		if !found || !guard || prompt != "Retention?" {
			t.Fatal("knowledge context missing or promoted to instructions")
		}
		return agent.Completion{Content: "Fourteen days [manual.md#abc]"}, nil
	})
	result, err := agent.New(client).Run(context.Background(), agent.Request{Prompt: "Retention?", Evidence: evidence, Target: agent.Target{Model: "fixture", ContextWindow: 10000}})
	if err != nil || calls != 1 || result.Last().Tokens.InputEstimate == 0 {
		t.Fatalf("bad evidence request %v", err)
	}
	calls = 0
	_, err = agent.New(client).Run(context.Background(), agent.Request{Prompt: "Retention?", Evidence: strings.Repeat(evidence, 100), Target: agent.Target{Model: "fixture", ContextWindow: 30}})
	if err == nil || calls != 0 {
		t.Fatal("evidence bypassed context budget")
	}
}
