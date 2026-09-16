package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func TestDifferentProfilesChangeSyntheticAnswers(t *testing.T) {
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		answer := "neutral"
		for _, message := range settings.Messages {
			if strings.Contains(message.Content, `"name":"formal"`) && strings.Contains(message.Content, `"style":"formal"`) {
				answer = "formal answer"
			}
			if strings.Contains(message.Content, `"name":"teacher"`) && strings.Contains(message.Content, `"format":"step-by-step"`) {
				answer = "teaching answer"
			}
		}
		return agent.Completion{Content: answer}, nil
	})
	formal, err := agent.New(client).Run(context.Background(), agent.Request{Prompt: "Explain", Target: agent.Target{Model: "test"}, UserProfile: agent.UserProfile{Name: "formal", Style: "formal"}})
	if err != nil || formal.Last().Answer.Content != "formal answer" {
		t.Fatalf("formal=%+v err=%v", formal, err)
	}
	teacher, err := agent.New(client).Run(context.Background(), agent.Request{Prompt: "Explain", Target: agent.Target{Model: "test"}, UserProfile: agent.UserProfile{Name: "teacher", Format: "step-by-step"}})
	if err != nil || teacher.Last().Answer.Content != "teaching answer" {
		t.Fatalf("teacher=%+v err=%v", teacher, err)
	}
}

func TestProfilePrecedesExplicitMemoryAndPrompt(t *testing.T) {
	store := &history.JSON{Dir: t.TempDir()}
	layers := layerStoreStub{working: map[string]agent.MemoryEntry{"goal": {Value: "ship"}}}
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		joined := ""
		for _, message := range settings.Messages {
			joined += message.Content + "\n"
		}
		profileAt := strings.Index(joined, `"name":"concise"`)
		memoryAt := strings.Index(joined, `"goal":"ship"`)
		promptAt := strings.LastIndex(joined, "new question")
		if profileAt < 0 || memoryAt < profileAt || promptAt < memoryAt {
			t.Fatalf("unexpected order: %s", joined)
		}
		return agent.Completion{Content: "answer"}, nil
	})
	request := agent.Request{ConversationID: "test", Prompt: "new question", Target: agent.Target{Model: "test"}, UserProfile: agent.UserProfile{Name: "concise", Style: "brief"}}
	if _, err := agent.NewWithHistory(client, store).WithMemoryLayers(layers).Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}

func TestProfileIsAttachedToEveryCompareCall(t *testing.T) {
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		calls++
		joined := ""
		for _, message := range settings.Messages {
			joined += message.Content
		}
		if !strings.Contains(joined, `"name":"brief"`) || !strings.Contains(joined, `"constraints":["maximum 3 bullets"]`) {
			t.Fatalf("profile missing from call %d: %s", calls, joined)
		}
		return agent.Completion{Content: "answer"}, nil
	})
	request := agent.Request{Prompt: "Explain", Mode: agent.Compare, Target: agent.Target{Model: "test"}, UserProfile: agent.UserProfile{Name: "brief", Constraints: []string{"maximum 3 bullets"}}, Control: agent.ControlConfig{Format: "plain_text", MaxWords: 20, MaxTokens: 100}}
	if _, err := agent.New(client).Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
