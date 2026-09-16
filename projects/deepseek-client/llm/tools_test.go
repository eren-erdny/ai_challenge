package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/filetools"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
)

func TestToolCallingHTTPAndRestoredContext(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "article.txt"), []byte("Code 7392"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload llm.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if calls == 1 {
			if len(payload.Tools) != 2 {
				t.Error("tools not sent")
			}
			w.Write([]byte(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"read-1","type":"function","function":{"name":"read_file","arguments":"{\"name\":\"article.txt\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`))
			return
		}
		if calls == 2 {
			last := payload.Messages[len(payload.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "read-1" || !strings.Contains(last.Content, "7392") {
				t.Errorf("tool result missing: %+v", last)
			}
			if payload.Messages[len(payload.Messages)-2].ToolCalls == nil {
				t.Error("assistant call missing")
			}
		} else {
			found := false
			for _, m := range payload.Messages {
				if strings.Contains(m.Content, "7392") {
					found = true
				}
				if m.FileContext != "" {
					t.Error("internal field leaked")
				}
			}
			if !found {
				t.Error("file context not restored")
			}
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"The code is 7392"},"finish_reason":"stop"}],"usage":{"prompt_tokens":30,"completion_tokens":5}}`))
	}))
	defer server.Close()
	store := &history.JSON{Dir: t.TempDir()}
	client := llm.New(server.Client(), nil)
	runner := agent.NewWithHistory(client, store).WithTools(&filetools.Documents{Dir: dir})
	request := agent.Request{ConversationID: "test", Prompt: "Read article.txt", Target: agent.Target{Model: "test", BaseURL: server.URL}}
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || result.Responses[0].Answer.PromptTokens != 40 || result.Responses[0].Answer.CompletionTokens != 9 {
		t.Fatalf("usage: %+v", result)
	}
	messages, err := store.Load(context.Background(), "test")
	if err != nil || len(messages) != 2 || !strings.Contains(messages[0].FileContext, "7392") {
		t.Fatal("file not persisted")
	}
	request.Prompt = "Recall code"
	if _, err := agent.NewWithHistory(client, &history.JSON{Dir: store.Dir}).Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}
