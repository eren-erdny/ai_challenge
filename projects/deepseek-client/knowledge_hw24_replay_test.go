package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

// Revalidate the saved real outputs against the final code without another API
// run. This is replay, not ten new model generations.
func TestHomework24SavedAnswers(t *testing.T) {
	if os.Getenv("HW24_REPLAY_TEST") != "1" {
		t.Skip("saved public homework artifacts required")
	}
	root := os.Getenv("HW24_OUTPUT")
	var saved struct {
		Rows []struct {
			ID        string                `json:"id"`
			Query     string                `json:"query"`
			Evidence  json.RawMessage       `json:"retrieval"`
			Raw       agent.Completion      `json:"raw_answer"`
			Grounding *agent.GroundedAnswer `json:"grounding"`
		} `json:"rows"`
	}
	data, err := os.ReadFile(filepath.Join(root, "answers.json"))
	if err != nil || json.Unmarshal(data, &saved) != nil || len(saved.Rows) != 10 {
		t.Fatal("ten saved public answers required")
	}
	facts, unknown, fixtures := 0, 0, 0
	for _, row := range saved.Rows {
		calls := 0
		runner := agent.New(agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
			calls++
			fixtures++
			if row.Grounding.Local {
				t.Fatal("local abstention called fixture")
			}
			return row.Raw, nil
		}))
		r, err := runner.Run(context.Background(), agent.Request{Prompt: row.Query, Evidence: string(row.Evidence), Target: agent.Target{Model: "saved-deepseek-output"}})
		if err != nil || len(r.Responses) != 1 || r.Responses[0].Grounding == nil {
			t.Fatal(row.ID, err)
		}
		g := r.Responses[0].Grounding
		if g.Status != row.Grounding.Status || !reflect.DeepEqual(g.Claims, row.Grounding.Claims) || !reflect.DeepEqual(g.Quotes, row.Grounding.Quotes) || !reflect.DeepEqual(g.Sources, row.Grounding.Sources) {
			t.Fatal(row.ID, "changed provenance")
		}
		if g.Status == "answered" {
			facts++
		} else {
			unknown++
		}
		if row.Grounding.Local && calls != 0 {
			t.Fatal("unexpected call")
		}
	}
	if facts != 8 || unknown != 2 || fixtures != 9 {
		t.Fatal(facts, unknown, fixtures)
	}
	report := map[string]any{"kind": "offline replay of previously saved real DeepSeek outputs; no new model generations", "questions": 10, "factual_answers": facts, "unknown_answers": unknown, "fixture_calls": fixtures, "provider_calls": 0, "result": "PASS"}
	b, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "offline-replay.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}
