package fileassistant

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

// Opt-in deterministic replay of a saved real trace; this is not a new model run.
func TestRecordedToolReplay(t *testing.T) {
	prior := os.Getenv("FILE_ASSISTANT_REPLAY")
	if prior == "" {
		t.Skip("set FILE_ASSISTANT_REPLAY to a saved run directory")
	}
	data, err := os.ReadFile(filepath.Join(prior, "tools.json"))
	if os.IsNotExist(err) {
		data, err = os.ReadFile(filepath.Join(prior, "tool-trace.json"))
	}
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	if err = json.Unmarshal(data, &events); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "replay"), filepath.Join(dir, "budget.json"), .20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, event := range events {
		_, err = s.Execute(context.Background(), event.Tool, string(event.Arguments))
		if (err != nil) != (event.Error != "") {
			t.Fatalf("replay outcome differs for %s", event.Tool)
		}
	}
	if err = s.Finish(); err != nil {
		t.Fatal(err)
	}
	if err = s.Verify(); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(prior, "workspace")
	if _, e := os.Stat(workspace); os.IsNotExist(e) {
		workspace = filepath.Join(prior, "result")
	}
	expected, err := snapshot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(expected) != len(s.After) {
		t.Fatal("replay file count differs")
	}
	for name, content := range expected {
		if s.After[name] != content {
			t.Fatalf("replay bytes differ: %s", name)
		}
	}
	if s.Ledger.Calls != 0 {
		t.Fatal("offline replay consumed model budget")
	}
}

func TestBudgetSurvivesFailureAndRestart(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "budget.json")
	s, err := Open(filepath.Join(dir, "first"), ledger, .20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Open(filepath.Join(dir, "locked"), ledger, .20); err == nil {
		t.Fatal("concurrent process accepted")
	}
	called := 0
	base := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		called++
		return agent.Completion{}, os.ErrPermission
	})
	target := agent.Target{BaseURL: "https://api.deepseek.com", Model: "deepseek-flash"}
	_, err = s.Client(base).Complete(context.Background(), target, "goal", agent.Settings{})
	if err == nil || called != 1 || s.Ledger.Reserved <= 0 || s.Ledger.Calls != 1 {
		t.Fatal("failed call not reserved")
	}
	reserved := s.Ledger.Reserved
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Open(filepath.Join(dir, "second"), ledger, .20)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.Ledger.Reserved != reserved || next.Ledger.Calls != 1 {
		t.Fatal("restart reset spending")
	}
	next.Ledger.Reserved = .20
	_, err = next.Client(base).Complete(context.Background(), target, "goal", agent.Settings{})
	if err == nil || called != 1 {
		t.Fatal("exhausted budget dispatched")
	}
}

func TestRealExecutorPolicyDiffAndFreshWorkspace(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "run"), filepath.Join(dir, "budget.json"), .20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, call := range []struct{ name, args string }{
		{"read_file", `{"path":"../private.md"}`},
		{"write_file", `{"path":"src/export.js","content":"broken","overwrite":true}`},
		{"write_file", `{"path":"docs/unrequested.md","content":"unrequested"}`},
	} {
		if _, err = s.Execute(ctx, call.name, call.args); err == nil {
			t.Fatal("forbidden operation accepted")
		}
	}
	_, err = s.Execute(ctx, "edit_file", `{"path":"README.md","old_text":"Free supports CSV and PDF. Pro supports CSV only.","new_text":"Free supports CSV. Pro supports CSV and PDF."}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Execute(ctx, "write_file", `{"path":"docs/export-api-map.md","content":"# Local test output\n"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(); err != nil {
		t.Fatal(err)
	}
	diff, err := os.ReadFile(filepath.Join(s.Dir, "changes.diff"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Changes) != 2 || !strings.Contains(string(diff), "+++ b/README.md") || !strings.Contains(string(diff), "--- /dev/null") {
		t.Fatal("disk changes missing from diff")
	}
	if s.After["src/export.js"] != s.Before["src/export.js"] {
		t.Fatal("source changed")
	}
	if s.Verify() == nil {
		t.Fatal("incomplete task passed")
	}
}

func TestExistingAgentRejectsBadParametersBeforeExecutor(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "run"), filepath.Join(dir, "budget.json"), .20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		calls++
		if calls == 1 {
			return agent.Completion{ToolCalls: []agent.ToolCall{{ID: "bad", Type: "function", Function: agent.FunctionCall{Name: "read_file", Arguments: `{"path":"README.md","user_override":true}`}}}}, nil
		}
		return agent.Completion{Content: "Denied invalid parameter", FinishReason: "stop"}, nil
	})
	_, err = agent.New(client).WithTools(s).Run(context.Background(), agent.Request{Prompt: "Inspect project", Mode: agent.Free, Target: agent.Target{Model: "fixture-not-DeepSeek"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 0 {
		t.Fatal("invalid request reached executor")
	}
}
