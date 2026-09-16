package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
)

func TestProviderContextOverflowPreservesHistory(t *testing.T) {
	ctx := context.Background()
	store := &history.JSON{Dir: t.TempDir()}
	messages := []agent.Message{{Role: "user", Content: strings.Repeat("fact ", 200)}, {Role: "assistant", Content: "remembered"}}
	if err := store.Save(ctx, "test", messages); err != nil {
		t.Fatal(err)
	}
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		var body llm.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Messages) != 3 || body.MaxTokens != 16 {
			t.Errorf("request: %+v", body)
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"type":"context_length_exceeded","message":"input exceeds context window"}}`))
	}))
	defer server.Close()
	runner := agent.NewWithHistory(llm.New(server.Client(), nil), store)
	result, err := runner.Run(ctx, agent.Request{ConversationID: "test", Prompt: "recall", Target: agent.Target{Model: "test", BaseURL: server.URL, MaxOutputTokens: 16}})
	if err == nil || !called || !strings.Contains(err.Error(), "status=400") || result.Failed == nil || result.Failed.Tokens.HistoryMessages != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	loaded, err := store.Load(ctx, "test")
	if err != nil || len(loaded) != 2 || loaded[0] != messages[0] {
		t.Fatal("history changed after failure")
	}
}

func TestUsageAndTruncation(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		known       bool
	}{
		{"missing", "", false},
		{"known", `,"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}]` + tc.usage + `}`))
			}))
			defer server.Close()
			result, err := agent.New(llm.New(server.Client(), nil)).Run(context.Background(), agent.Request{Prompt: "hi", Target: agent.Target{Model: "test", BaseURL: server.URL}})
			if err != nil {
				t.Fatal(err)
			}
			r := result.Responses[0]
			if r.Tokens.UsageKnown != tc.known || r.Answer.FinishReason != "length" {
				t.Fatalf("response: %+v", r)
			}
			if tc.known && (r.Tokens.InputActual != 100 || r.Tokens.OutputActual != 10) {
				t.Fatal("usage lost")
			}
		})
	}
}
