package history_test

import (
	"context"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func TestTurnAccountingIsAtomicAndNotDuplicated(t *testing.T) {
	s := &history.JSON{Dir: t.TempDir()}
	ctx := context.Background()
	messages := []agent.Message{{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"}}
	u := agent.TurnUsage{UsageKnown: true, Input: 10, Output: 2, Total: 12}
	if err := s.SaveTurn(ctx, "test", messages, u); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTurn(ctx, "test", messages, u); err == nil {
		t.Fatal("duplicate turn accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	next := append(append([]agent.Message(nil), messages...), messages...)
	if err := s.SaveTurn(cancelled, "test", next, u); err == nil {
		t.Fatal("cancel ignored")
	}
	got, err := s.Usage(ctx, "test")
	if err != nil || got.Turns != 1 || got.Total != 12 {
		t.Fatalf("totals changed: %+v %v", got, err)
	}
	loaded, _ := s.Load(ctx, "test")
	if len(loaded) != 2 {
		t.Fatal("messages changed without accounting")
	}
}
