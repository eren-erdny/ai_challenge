package memorylayers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/memorylayers"
)

func TestLayersAreSeparatedByScopeAndFiles(t *testing.T) {
	ctx := context.Background()
	store := &memorylayers.JSON{Dir: t.TempDir()}
	if err := store.SetMemory(ctx, "task-a", agent.MemoryWorking, "deadline", "Friday"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMemory(ctx, "task-a", agent.MemoryLongTerm, "preferred_language", "Russian"); err != nil {
		t.Fatal(err)
	}
	workingA, _ := store.LoadMemory(ctx, "task-a", agent.MemoryWorking)
	workingB, _ := store.LoadMemory(ctx, "task-b", agent.MemoryWorking)
	longB, _ := store.LoadMemory(ctx, "task-b", agent.MemoryLongTerm)
	if workingA["deadline"].Value != "Friday" || len(workingB) != 0 || longB["preferred_language"].Value != "Russian" {
		t.Fatalf("workingA=%+v workingB=%+v longB=%+v", workingA, workingB, longB)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "working", "task-a.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "long-term.json")); err != nil {
		t.Fatal(err)
	}
	longTermFile, err := os.ReadFile(filepath.Join(store.Dir, "long-term.json"))
	if err != nil || strings.Contains(string(longTermFile), "conversation_id") {
		t.Fatalf("global file=%s err=%v", longTermFile, err)
	}
	reopened := &memorylayers.JSON{Dir: store.Dir}
	if got, err := reopened.LoadMemory(ctx, "task-a", agent.MemoryWorking); err != nil || got["deadline"].Value != "Friday" {
		t.Fatalf("reopened=%+v err=%v", got, err)
	}
}

func TestLayerUpdateDeleteAndValidation(t *testing.T) {
	ctx := context.Background()
	store := &memorylayers.JSON{Dir: t.TempDir()}
	if err := store.SetMemory(ctx, "task", agent.MemoryWorking, "goal", "first"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMemory(ctx, "task", agent.MemoryWorking, "goal", "corrected"); err != nil {
		t.Fatal(err)
	}
	entries, _ := store.LoadMemory(ctx, "task", agent.MemoryWorking)
	if len(entries) != 1 || entries["goal"].Value != "corrected" || entries["goal"].Updated.IsZero() {
		t.Fatal(entries)
	}
	deleted, err := store.DeleteMemory(ctx, "task", agent.MemoryWorking, "goal")
	if err != nil || !deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
	deleted, err = store.DeleteMemory(ctx, "task", agent.MemoryWorking, "goal")
	if err != nil || deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
	if err := store.SetMemory(ctx, "task", agent.MemoryWorking, "bad key", "value"); err == nil {
		t.Fatal("invalid key accepted")
	}
	if err := store.SetMemory(ctx, "task", "short-term", "key", "value"); err == nil {
		t.Fatal("invalid layer accepted")
	}
}
