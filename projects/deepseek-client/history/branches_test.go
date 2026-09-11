package history_test

import (
	"context"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func TestBranchesDivergeAndSurviveRestart(t *testing.T) {
	ctx := context.Background()
	store := &history.JSON{Dir: t.TempDir()}
	base := []agent.Message{{Role: "user", Content: "shared"}, {Role: "assistant", Content: "base"}}
	if err := store.Save(ctx, "test", base); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateCheckpoint(ctx, "test", "fork"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta"} {
		if err := store.CreateBranch(ctx, "test", name, "fork"); err != nil {
			t.Fatal(err)
		}
	}
	alpha, _ := store.SwitchBranch(ctx, "test", "alpha")
	alpha = append(alpha, agent.Message{Role: "user", Content: "alpha choice"}, agent.Message{Role: "assistant", Content: "alpha answer"})
	if err := store.SaveTurn(ctx, "test", alpha, agent.TurnUsage{UsageKnown: true, Total: 3}); err != nil {
		t.Fatal(err)
	}
	beta, _ := store.SwitchBranch(ctx, "test", "beta")
	if len(beta) != 2 {
		t.Fatal("alpha leaked into beta")
	}
	beta = append(beta, agent.Message{Role: "user", Content: "beta choice"}, agent.Message{Role: "assistant", Content: "beta answer"})
	if err := store.SaveTurn(ctx, "test", beta, agent.TurnUsage{UsageKnown: true, Total: 5}); err != nil {
		t.Fatal(err)
	}
	reopened := &history.JSON{Dir: store.Dir}
	alpha, err := reopened.SwitchBranch(ctx, "test", "alpha")
	if err != nil || len(alpha) != 4 || alpha[2].Content != "alpha choice" {
		t.Fatalf("alpha=%+v err=%v", alpha, err)
	}
	u, _ := reopened.Usage(ctx, "test")
	if u.Turns != 2 || u.Total != 3 {
		t.Fatalf("alpha usage=%+v", u)
	}
	main, _ := reopened.SwitchBranch(ctx, "test", "main")
	if len(main) != 2 {
		t.Fatal("branch changes leaked into main")
	}
	branches, checkpoints, err := reopened.Branches(ctx, "test")
	if err != nil || len(branches) != 3 || len(checkpoints) != 1 || checkpoints[0] != "fork" {
		t.Fatalf("branches=%+v checkpoints=%+v err=%v", branches, checkpoints, err)
	}
}
