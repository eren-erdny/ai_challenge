package mcppipeline

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/gitmcp"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/mcptools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Two real HTTP MCP connections; only the model and metadata backend are fixtures.
func TestRoutesLongFlowAcrossServersWithScriptedModel(t *testing.T) {
	gitClient, _ := setup(t)
	metadata := mcp.NewServer(&mcp.Implementation{Name: "metadata-fixture", Version: "test"}, nil)
	type input struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
	}
	type output struct {
		Query string `json:"query"`
	}
	metadataCalls := 0
	mcp.AddTool(metadata, &mcp.Tool{Name: "github_get_repository", Description: "Get repository metadata"}, func(_ context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, output, error) {
		metadataCalls++
		if in.Owner != "acme" || in.Repo != "demo" {
			t.Error("wrong metadata arguments")
		}
		return nil, output{Query: "fix"}, nil
	})
	const token = "multi-test-token-not-secret-123456789"
	handler, e := gitmcp.HTTPHandler(metadata, token)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	registry, e := mcptools.ConnectMany(ctx, []mcptools.Registration{
		{Name: "github", Connect: func(ctx context.Context) (mcptools.Session, error) {
			c, e := mcptools.ConnectHTTP(ctx, server.URL+"/mcp", token, nil)
			if e != nil {
				return nil, e
			}
			return c, nil
		}},
		{Name: "git", Connect: func(context.Context) (mcptools.Session, error) { return gitClient, nil }},
	}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer registry.Close()
	rec := &trace{ToolExecutor: registry}
	names := []string{"github__github_get_repository", "git__git_status", "git__git_branches", "git__git_search_commits", "git__git_summarize_commits", "git__git_save_report"}
	calls := 0
	query := ""
	savedPath := ""
	model := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		if len(s.Tools) != 12 {
			t.Fatalf("definitions=%d", len(s.Tools))
		}
		if calls > 0 {
			last := s.Messages[len(s.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != names[calls-1] {
				t.Fatal("wrong tool correlation")
			}
			if last.Content != rec.results[calls-1] {
				t.Fatal("lost tool result")
			}
		}
		args := "{}"
		switch calls {
		case 0:
			args = `{"owner":"acme","repo":"demo"}`
		case 1:
			var result output
			if e := json.Unmarshal([]byte(rec.results[0]), &result); e != nil {
				t.Fatal(e)
			}
			query = result.Query
		case 3:
			b, _ := json.Marshal(gitmcp.SearchInput{Query: query, Limit: 10})
			args = string(b)
		case 4, 5:
			args = rec.results[calls-1]
		case 6:
			var saved gitmcp.SavedReport
			if e := json.Unmarshal([]byte(rec.results[5]), &saved); e != nil {
				t.Fatal(e)
			}
			savedPath = saved.File
			calls++
			return agent.Completion{Content: "Saved: " + saved.File}, nil
		}
		name := names[calls]
		calls++
		return agent.Completion{ToolCalls: []agent.ToolCall{{ID: name, Type: "function", Function: agent.FunctionCall{Name: name, Arguments: args}}}}, nil
	})
	result, e := agent.New(model).WithTools(rec).Run(ctx, agent.Request{Prompt: "Get metadata, inspect Git status and branches, search matching commits, summarize and save.", Mode: agent.Free, Target: agent.Target{Model: "test"}})
	if e != nil {
		t.Fatal(e)
	}
	if calls != 7 || metadataCalls != 1 || !reflect.DeepEqual(rec.names, names) {
		t.Fatalf("calls=%d metadata=%d order=%v", calls, metadataCalls, rec.names)
	}
	if rec.args[4] != rec.results[3] || rec.args[5] != rec.results[4] {
		t.Fatal("data changed between tools")
	}
	var summary gitmcp.CommitSummary
	if e := json.Unmarshal([]byte(rec.results[4]), &summary); e != nil {
		t.Fatal(e)
	}
	content, e := os.ReadFile(savedPath)
	if e != nil || string(content) != summary.Markdown || summary.CommitCount != 2 || summary.Query != query {
		t.Fatalf("saved report mismatch: %v", e)
	}
	if result.Last().Answer.Content != "Saved: "+savedPath {
		t.Fatal("final answer lost saved result")
	}
}
