package mcppipeline

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/gitmcp"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/mcptools"
)

type trace struct {
	agent.ToolExecutor
	names, args, results []string
	fail                 string
}

func (t *trace) Execute(ctx context.Context, name, args string) (string, error) {
	t.names = append(t.names, name)
	t.args = append(t.args, args)
	if name == t.fail {
		return "", errors.New("injected failure")
	}
	r, e := t.ToolExecutor.Execute(ctx, name, args)
	t.results = append(t.results, r)
	return r, e
}

func setup(t *testing.T) (*mcptools.Client, string) {
	t.Helper()
	repo, data := t.TempDir(), t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %s %v", args, out, e)
		}
	}
	git("init", "-b", "main")
	for _, subject := range []string{"feat: add search", "fix: correct results", "fix: исправить UTF-8"} {
		git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "core.hooksPath=", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", subject)
	}
	server, e := gitmcp.NewServer(repo)
	if e != nil {
		t.Fatal(e)
	}
	p, e := gitmcp.OpenPipelineTools(repo, data)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close() })
	p.Register(server)
	scheduler, e := gitmcp.OpenScheduler(repo, data, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { scheduler.Close() })
	scheduler.Register(server)
	const token = "test-pipeline-token-not-secret-123456789"
	handler, e := gitmcp.HTTPHandler(server, token)
	if e != nil {
		t.Fatal(e)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, e := mcptools.ConnectHTTP(ctx, httpServer.URL+"/mcp", token, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { client.Close() })
	if len(client.Definitions()) != 11 {
		t.Fatalf("tools=%d want 11", len(client.Definitions()))
	}
	return client, data
}

func TestPipelineTransfersExactResultsAndSaves(t *testing.T) {
	client, data := setup(t)
	rec := &trace{ToolExecutor: client}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, e := Run(ctx, rec, gitmcp.SearchInput{Query: "fix", Limit: 10})
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"git_search_commits", "git_summarize_commits", "git_save_report"}
	if !reflect.DeepEqual(rec.names, want) || !reflect.DeepEqual(r.Steps, want) {
		t.Fatalf("steps=%v", rec.names)
	}
	if rec.args[1] != rec.results[0] || rec.args[2] != rec.results[1] {
		t.Fatal("intermediate data was modified")
	}
	if r.Summary.CommitCount != 2 || r.Summary.Categories["fix"] != 2 || !strings.Contains(r.Summary.Markdown, "исправить") {
		t.Fatalf("summary=%+v", r.Summary)
	}
	if filepath.Dir(r.Saved.File) != filepath.Join(data, "reports") {
		t.Fatalf("file escaped output directory: %s", r.Saved.File)
	}
	content, e := os.ReadFile(r.Saved.File)
	if e != nil {
		t.Fatal(e)
	}
	if string(content) != r.Summary.Markdown || len(content) != r.Saved.Bytes {
		t.Fatal("saved data mismatch")
	}
	r2, e := Run(ctx, client, gitmcp.SearchInput{Query: "fix"})
	if e != nil {
		t.Fatal(e)
	}
	if r2.Saved.File == r.Saved.File {
		t.Fatal("report overwritten")
	}
	content, e = os.ReadFile(r.Saved.File)
	if e != nil || string(content) != r.Summary.Markdown {
		t.Fatal("first report changed")
	}
}

func TestPipelineEmptyAndFailure(t *testing.T) {
	client, data := setup(t)
	ctx := context.Background()
	r, e := Run(ctx, client, gitmcp.SearchInput{Query: "no-such-commit"})
	if e != nil || r.Summary.CommitCount != 0 || !strings.Contains(r.Summary.Markdown, "No matching") {
		t.Fatalf("empty result: %+v %v", r, e)
	}
	for _, step := range []string{"git_search_commits", "git_summarize_commits", "git_save_report"} {
		rec := &trace{ToolExecutor: client, fail: step}
		_, e = Run(ctx, rec, gitmcp.SearchInput{Query: "fix"})
		if e == nil || !strings.Contains(e.Error(), step) {
			t.Fatalf("missing step error: %v", e)
		}
		if rec.names[len(rec.names)-1] != step {
			t.Fatalf("continued after failure: %v", rec.names)
		}
	}
	files, e := os.ReadDir(filepath.Join(data, "reports"))
	if e != nil || len(files) != 1 {
		t.Fatalf("unexpected report on failure: %v %v", files, e)
	}
	for _, args := range []string{`{"query":"fix","limit":51}`, `{"query":"bad\nquery"}`} {
		if _, e = client.Execute(ctx, "git_search_commits", args); e == nil {
			t.Fatal("invalid search accepted")
		}
	}
	if _, e = client.Execute(ctx, "git_summarize_commits", `{"query":"fix","commits":[{"hash":"bad","subject":"fix"}]}`); e == nil {
		t.Fatal("malformed data accepted")
	}
	if _, e = client.Execute(ctx, "git_save_report", `{"query":"","commit_count":0,"categories":{},"markdown":""}`); e == nil {
		t.Fatal("empty report accepted")
	}
}

func TestAgentChainsMCPTools(t *testing.T) {
	client, _ := setup(t)
	rec := &trace{ToolExecutor: client}
	calls := 0
	model := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		calls++
		name, args := "git_search_commits", `{"query":"fix","limit":10}`
		if calls > 1 {
			last := s.Messages[len(s.Messages)-1]
			if last.Role != "tool" {
				t.Fatal("missing tool result")
			}
			args = last.Content
		}
		switch calls {
		case 2:
			name = "git_summarize_commits"
		case 3:
			name = "git_save_report"
		case 4:
			var saved gitmcp.SavedReport
			if e := json.Unmarshal([]byte(args), &saved); e != nil {
				t.Fatal(e)
			}
			return agent.Completion{Content: "Saved: " + saved.File}, nil
		}
		return agent.Completion{ToolCalls: []agent.ToolCall{{ID: name, Type: "function", Function: agent.FunctionCall{Name: name, Arguments: args}}}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, e := agent.New(model).WithTools(rec).Run(ctx, agent.Request{Prompt: "Find fix commits, summarize and save", Mode: agent.Free, Target: agent.Target{Model: "test"}})
	if e != nil || calls != 4 || !strings.HasPrefix(r.Last().Answer.Content, "Saved: ") {
		t.Fatalf("result=%+v error=%v calls=%d", r, e, calls)
	}
	if len(rec.args) != 3 || rec.args[1] != rec.results[0] || rec.args[2] != rec.results[1] {
		t.Fatal("agent chain broke data transfer")
	}
}
