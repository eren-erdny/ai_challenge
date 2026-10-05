package ragtools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/ragtools"
)

func TestAgentRetrievesEvidenceBeforeAnswer(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/search" || r.Method != "POST" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var value map[string]any
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			t.Error(err)
		}
		if value["query"] != "retention?" || value["top_k"] != float64(5) {
			t.Errorf("bad request %v", value)
		}
		fmt.Fprint(w, `{"results":[{"source":"operations.md","chunk_id":"abc","text":"Backups are retained for 14 days.","vector_score":0.8}]}`)
	}))
	defer server.Close()
	tools, err := ragtools.New(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	model := agent.ClientFunc(func(ctx context.Context, target agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
		calls++
		if calls == 1 {
			if len(settings.Tools) != 1 || settings.Tools[0].Function.Name != "rag_search" {
				t.Fatal("RAG tool not advertised")
			}
			return agent.Completion{ToolCalls: []agent.ToolCall{{ID: "r1", Type: "function", Function: agent.FunctionCall{Name: "rag_search", Arguments: `{"query":"retention?"}`}}}}, nil
		}
		found := false
		for _, m := range settings.Messages {
			if m.Role == "tool" && strings.Contains(m.Content, "14 days") && strings.Contains(m.Content, "operations.md") {
				found = true
			}
		}
		if !found {
			t.Fatal("retrieved evidence missing from model input")
		}
		return agent.Completion{Content: "14 days [operations.md#abc]"}, nil
	})
	result, err := agent.New(model).WithTools(tools).Run(context.Background(), agent.Request{Prompt: "Find backup retention in my documents", Target: agent.Target{Model: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || requests != 1 || !strings.Contains(result.Last().FileContext, "14 days") || result.Last().Answer.Content != "14 days [operations.md#abc]" {
		t.Fatalf("invalid RAG flow: %+v", result)
	}
}

func TestRejectInvalidRequestsAndRemoteOrigins(t *testing.T) {
	for _, origin := range []string{"https://localhost:8765", "http://example.org", "http://localhost:8765/search", "http://localhost:8765?x=1"} {
		if _, err := ragtools.New(origin, nil); err == nil {
			t.Errorf("accepted %s", origin)
		}
	}
	tools, _ := ragtools.New("http://127.0.0.1:1", nil)
	for _, args := range []string{`{"query":" "}`, `{"query":"x","top_k":21}`, `{"query":"x","candidates":1}`, `{"query":"x","strategy":"unknown"}`, `{"query":"x","source":"C:/secret"}`, `{"query":"x"} {}`} {
		if _, err := tools.Execute(context.Background(), "rag_search", args); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
}

func TestErrorsAndRedirectsDoNotBecomeEvidence(t *testing.T) {
	for _, status := range []int{302, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "http://example.org")
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":"index unavailable"}`)
		}))
		tools, _ := ragtools.New(server.URL, nil)
		if _, err := tools.Execute(context.Background(), "rag_search", `{"query":"x"}`); err == nil {
			t.Errorf("accepted status %d", status)
		}
		server.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tools, _ := ragtools.New("http://127.0.0.1:1", nil)
	if _, err := tools.Status(ctx); err == nil {
		t.Fatal("ignored cancellation")
	}
}
