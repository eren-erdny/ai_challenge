package invariants_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/invariants"
)

func TestInvariantsPersistSeparatelyAndKeepStableIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory", "invariants.json")
	store := &invariants.JSON{Path: path}
	first, err := store.AddInvariant(context.Background(), agent.InvariantStack, "Use Go only")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AddInvariant(context.Background(), agent.InvariantBusiness, "Orders require payment")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("ids=%d,%d", first.ID, second.ID)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	reopened := &invariants.JSON{Path: path}
	values, err := reopened.LoadInvariants(context.Background())
	if err != nil || len(values) != 2 || values[0].Text != "Use Go only" {
		t.Fatalf("values=%+v err=%v", values, err)
	}
	deleted, err := reopened.DeleteInvariant(context.Background(), 2)
	if err != nil || !deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
	third, err := reopened.AddInvariant(context.Background(), agent.InvariantDecision, "Use PostgreSQL")
	if err != nil || third.ID != 3 {
		t.Fatalf("third=%+v err=%v", third, err)
	}
}

func TestInvariantValidationRejectsBadInput(t *testing.T) {
	store := &invariants.JSON{Path: filepath.Join(t.TempDir(), "invariants.json")}
	if _, err := store.AddInvariant(context.Background(), "other", "value"); err == nil {
		t.Fatal("invalid kind accepted")
	}
	if _, err := store.AddInvariant(context.Background(), agent.InvariantStack, "   "); err == nil {
		t.Fatal("blank invariant accepted")
	}
	if deleted, err := store.DeleteInvariant(context.Background(), 99); err != nil || deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
}
