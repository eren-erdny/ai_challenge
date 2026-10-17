package llm_test

import (
	"context"
	"encoding/json"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReasoningEffortHTTP(t *testing.T) {
	for _, effort := range []string{"", "none"} {
		t.Run("effort="+effort, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Fatal(e)
				}
				got, exists := body["reasoning_effort"]
				if effort == "" && exists {
					t.Error("default must omit reasoning_effort")
				}
				if effort != "" && string(got) != `"none"` {
					t.Errorf("wrong effort: %s", got)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()
			client := llm.New(server.Client(), nil)
			_, e := client.Complete(context.Background(), agent.Target{BaseURL: server.URL, Model: "local", ReasoningEffort: effort}, "test", agent.Settings{})
			if e != nil {
				t.Fatal(e)
			}
		})
	}
}
