package gitmcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/mcptools"
)

const testToken = "local-test-token-not-a-secret-123456789"

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %v %s", args, e, b)
		}
	}
	git("init", "-b", "main")
	if e := os.WriteFile(filepath.Join(dir, "example.txt"), []byte("first\n"), 0600); e != nil {
		t.Fatal(e)
	}
	git("add", "example.txt")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=", "commit", "-m", "fixture commit")
	if e := os.WriteFile(filepath.Join(dir, "example.txt"), []byte("second\n"), 0600); e != nil {
		t.Fatal(e)
	}
	return dir
}

func TestHTTPAgentAndGitTools(t *testing.T) {
	dir := fixture(t)
	server, e := NewServer(dir)
	if e != nil {
		t.Fatal(e)
	}
	handler, e := HTTPHandler(server, testToken)
	if e != nil {
		t.Fatal(e)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, e := mcptools.ConnectHTTP(ctx, httpServer.URL+"/mcp", testToken, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	if len(client.Definitions()) != 4 {
		t.Fatalf("tools: %v", client.Definitions())
	}
	for _, test := range []struct{ name, args, want string }{
		{"git_status", `{}`, "example.txt"}, {"git_log", `{"limit":1}`, "fixture commit"}, {"git_diff", `{}`, "+second"}, {"git_branches", `{}`, "main"},
	} {
		result, e := client.Execute(ctx, test.name, test.args)
		if e != nil || !strings.Contains(result, test.want) {
			t.Fatalf("%s: %s %v", test.name, result, e)
		}
	}
	if _, e := client.Execute(ctx, "git_log", `{"limit":51}`); e == nil {
		t.Fatal("unbounded log accepted")
	}
	if _, e := client.Execute(ctx, "git_push", `{}`); e == nil {
		t.Fatal("write tool accepted")
	}
	turns := 0
	model := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		turns++
		if turns == 1 {
			return agent.Completion{ToolCalls: []agent.ToolCall{{ID: "git-call", Type: "function", Function: agent.FunctionCall{Name: "git_status", Arguments: `{}`}}}}, nil
		}
		last := s.Messages[len(s.Messages)-1]
		if last.Role != "tool" || last.ToolCallID != "git-call" {
			t.Fatalf("missing result: %+v", last)
		}
		var out output
		if e := json.Unmarshal([]byte(last.Content), &out); e != nil {
			t.Fatal(e)
		}
		return agent.Completion{Content: "Server repository status: " + out.Text}, nil
	})
	result, e := agent.New(model).WithTools(client).Run(ctx, agent.Request{Prompt: "Check the remote clone", Mode: agent.Free, Target: agent.Target{Model: "test"}})
	if e != nil || turns != 2 || !strings.Contains(result.Last().Answer.Content, "example.txt") {
		t.Fatalf("result=%+v err=%v", result, e)
	}
	status, e := run(ctx, dir, "status", "--short")
	if e != nil || !strings.Contains(status.Text, " M example.txt") {
		t.Fatalf("worktree changed: %+v %v", status, e)
	}
}

func TestHTTPAuthorization(t *testing.T) {
	server, e := NewServer(fixture(t))
	if e != nil {
		t.Fatal(e)
	}
	if _, e := HTTPHandler(server, ""); e == nil {
		t.Fatal("empty token allowed")
	}
	handler, e := HTTPHandler(server, testToken)
	if e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct {
		token, origin string
		code          int
	}{
		{"", "", 401}, {"wrong", "", 401}, {testToken, "https://untrusted.example", 403},
	} {
		req := httptest.NewRequest("POST", "http://127.0.0.1/mcp", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+test.token)
		req.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != test.code {
			t.Fatalf("got %d want %d", w.Code, test.code)
		}
	}
	api := httptest.NewServer(handler)
	defer api.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if c, e := mcptools.ConnectHTTP(ctx, api.URL+"/mcp", strings.Repeat("x", 32), nil); e == nil {
		c.Close()
		t.Fatal("wrong token connected")
	}
}

func TestRepositoryAndOutputBounds(t *testing.T) {
	if _, e := NewServer(t.TempDir()); e == nil {
		t.Fatal("non-repository accepted")
	}
	dir := fixture(t)
	nested := filepath.Join(dir, "nested")
	if e := os.Mkdir(nested, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := NewServer(nested); e == nil {
		t.Fatal("nested path accepted as root")
	}
	var b boundedBuffer
	_, _ = b.Write([]byte(strings.Repeat("x", 300<<10)))
	if !b.truncated || len(b.data) != 256<<10 {
		t.Fatal("output bound failed")
	}
}

func TestRemoteURLValidation(t *testing.T) {
	for _, endpoint := range []string{"http://example.com/mcp", "https://user:pass@example.com/mcp", "https://example.com/mcp?token=secret", "file:///repo"} {
		if _, e := mcptools.ConnectHTTP(context.Background(), endpoint, testToken, nil); e == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
}
