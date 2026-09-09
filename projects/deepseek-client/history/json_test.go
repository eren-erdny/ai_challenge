package history_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func TestJSONValidationAndCancellation(t *testing.T) {
	s := &history.JSON{Dir: t.TempDir()}
	ctx := context.Background()
	if _, err := s.Load(ctx, "../escape"); err == nil {
		t.Fatal("path traversal accepted")
	}
	turn := []agent.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}
	if err := s.Save(ctx, "default", turn); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Save(cancelled, "default", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Save(ctx, "default", turn[:1]); err == nil {
		t.Fatal("incomplete turn accepted")
	}
	messages, err := s.Load(ctx, "default")
	if err != nil || len(messages) != 2 {
		t.Fatalf("previous data lost: %v", err)
	}
	path := filepath.Join(s.Dir, "default.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(ctx, "default"); err == nil {
		t.Fatal("corrupt history accepted")
	}
}

func TestAgentHistoryPolicies(t *testing.T) {
	store := &history.JSON{Dir: t.TempDir()}
	ctx := context.Background()
	request := agent.Request{ConversationID: "default", Mode: agent.Free, Prompt: "first", Target: agent.Target{Model: "test"}}
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		return agent.Completion{Content: "answer"}, nil
	})
	if _, err := agent.NewWithHistory(client, store).Run(ctx, request); err != nil {
		t.Fatal(err)
	}
	request.Mode = agent.Controlled
	request.Control = agent.ControlConfig{Format: "text", MaxWords: 100, MaxTokens: 200}
	request.Strategy = agent.StepByStep
	request.Prompt = "second"
	failing := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		if len(settings.Messages) != 4 || settings.Messages[0].Role != "system" || settings.Messages[1].Content != "first" || settings.Messages[3].Content != "second" {
			t.Fatalf("messages: %#v", settings.Messages)
		}
		return agent.Completion{}, errors.New("offline")
	})
	if _, err := agent.NewWithHistory(failing, store).Run(ctx, request); err == nil {
		t.Fatal("expected failure")
	}
	messages, _ := store.Load(ctx, "default")
	if len(messages) != 2 {
		t.Fatal("failed turn persisted")
	}
	request.Mode = agent.TemperatureBenchmark
	isolated := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		if len(settings.Messages) != 2 {
			t.Fatal("benchmark received chat history")
		}
		return agent.Completion{Content: "result"}, nil
	})
	if _, err := agent.NewWithHistory(isolated, store).Run(ctx, request); err != nil {
		t.Fatal(err)
	}
	messages, _ = store.Load(ctx, "default")
	if len(messages) != 2 {
		t.Fatal("benchmark persisted in chat")
	}
}
