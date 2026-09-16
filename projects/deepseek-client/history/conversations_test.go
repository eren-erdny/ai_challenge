package history_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func TestConversationSelectionAndMigration(t *testing.T) {
	ctx := context.Background()
	s := &history.JSON{Dir: t.TempDir()}
	old := []agent.Message{{Role: "user", Content: "old"}, {Role: "assistant", Content: "answer"}}
	if err := s.Save(ctx, "default", old); err != nil {
		t.Fatal(err)
	}
	id, messages, err := s.Active(ctx)
	if err != nil || id != "default" || len(messages) != 2 {
		t.Fatalf("migration: %s %v %v", id, messages, err)
	}
	id, err = s.NewConversation(ctx)
	if err != nil || id == "default" || id == "" {
		t.Fatalf("new: %s %v", id, err)
	}
	reopened := &history.JSON{Dir: s.Dir}
	active, messages, err := reopened.Active(ctx)
	if err != nil || active != id || len(messages) != 0 {
		t.Fatalf("restart: %s %v %v", active, messages, err)
	}
	for _, invalid := range []string{"missing", "../default", ".active"} {
		if _, err := reopened.Select(ctx, invalid); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	active, _, _ = reopened.Active(ctx)
	if active != id {
		t.Fatal("failed selection changed active ID")
	}
	messages, err = reopened.Select(ctx, "default")
	if err != nil || len(messages) != 2 {
		t.Fatalf("old history: %v %v", messages, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := reopened.NewConversation(cancelled); err == nil {
		t.Fatal("cancel ignored")
	}
	active, _, _ = reopened.Active(ctx)
	if active != "default" {
		t.Fatal("cancel changed selection")
	}
}

func TestSelectionFailurePreservesActive(t *testing.T) {
	s := &history.JSON{Dir: t.TempDir()}
	ctx := context.Background()
	if _, _, err := s.Active(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "broken.json"), []byte("bad json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Select(ctx, "broken"); err == nil {
		t.Fatal("corruption accepted")
	}
	id, _, err := s.Active(ctx)
	if err != nil || id != "default" {
		t.Fatalf("active lost: %s %v", id, err)
	}
}
