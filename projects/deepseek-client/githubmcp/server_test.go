package githubmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/filetools"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/mcptools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAgentUsesGitHubMCPResult(t *testing.T) {
	requests := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/repos/acme/demo" || r.Method != "GET" || r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request: %v", r)
		}
		fmt.Fprint(w, `{"full_name":"acme/demo","html_url":"https://github.com/acme/demo","description":"Example","language":"Go","stargazers_count":42,"open_issues_count":3}`)
	}))
	defer api.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.Command(os.Args[0], "-test.run=^TestGitHubMCPProcess$")
	command.Env = append(os.Environ(), "GO_MCP_TEST_SERVER="+api.URL)
	tools, err := mcptools.Connect(ctx, &mcp.CommandTransport{Command: command}, &filetools.Documents{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	if len(tools.Definitions()) != 8 {
		t.Fatalf("missing combined tools: %v", tools.Definitions())
	}
	if _, err := tools.Execute(ctx, "list_files", `{}`); err != nil {
		t.Fatalf("file fallback: %v", err)
	}
	calls := 0
	model := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		calls++
		if calls == 1 {
			found := false
			for _, tool := range settings.Tools {
				if tool.Function.Name == ToolName {
					found = true
					if !strings.Contains(string(tool.Function.Parameters), `"owner"`) || !strings.Contains(string(tool.Function.Parameters), `"required"`) {
						t.Fatal("missing parameter schema")
					}
				}
			}
			if !found {
				t.Fatal("MCP tool not advertised to model")
			}
			return agent.Completion{ToolCalls: []agent.ToolCall{{ID: "github-1", Type: "function", Function: agent.FunctionCall{Name: ToolName, Arguments: `{"owner":"acme","repo":"demo"}`}}}}, nil
		}
		last := settings.Messages[len(settings.Messages)-1]
		if last.Role != "tool" || last.ToolCallID != "github-1" {
			t.Fatalf("invalid tool message: %+v", last)
		}
		var repo Repository
		if err := json.Unmarshal([]byte(last.Content), &repo); err != nil {
			t.Fatal(err)
		}
		return agent.Completion{Content: fmt.Sprintf("%s: %d stars", repo.FullName, repo.Stars)}, nil
	})
	result, err := agent.New(model).WithTools(tools).Run(ctx, agent.Request{Prompt: "Describe acme/demo", Mode: agent.Free, Target: agent.Target{Model: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || requests != 1 || result.Last().Answer.Content != "acme/demo: 42 stars" {
		t.Fatalf("calls=%d requests=%d result=%+v", calls, requests, result)
	}
}

// The subprocess uses real MCP stdio; only its upstream HTTP API is a fixture.
func TestGitHubMCPProcess(t *testing.T) {
	baseURL := os.Getenv("GO_MCP_TEST_SERVER")
	if baseURL == "" {
		return
	}
	if err := newServer(&http.Client{Timeout: time.Second}, baseURL).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestMCPFailures(t *testing.T) {
	for _, tc := range []struct {
		name, args, body, want string
		status                 int
		requests               int
	}{
		{"not-found", `{"owner":"acme","repo":"missing"}`, `{}`, "404", 404, 1},
		{"rate-limit", `{"owner":"acme","repo":"demo"}`, `{}`, "rate limit", 429, 1},
		{"bad-json", `{"owner":"acme","repo":"demo"}`, `broken`, "decode", 200, 1},
		{"path", `{"owner":"../acme","repo":"demo"}`, `{}`, "names", 200, 0},
		{"missing-param", `{"owner":"acme"}`, `{}`, "", 200, 0},
		{"malformed-args", `{`, `{}`, "JSON object", 200, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				count++
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer api.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			st, ct := mcp.NewInMemoryTransports()
			ss, err := newServer(api.Client(), api.URL).Connect(ctx, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			client, err := mcptools.Connect(ctx, ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_, err = client.Execute(ctx, ToolName, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) || count != tc.requests {
				t.Fatalf("error=%v requests=%d", err, count)
			}
		})
	}
}
