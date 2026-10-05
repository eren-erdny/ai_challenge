package llm_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
)

func TestJSONOutputIsOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		r := llm.BuildChatRequest("policy?", agent.Settings{Model: "test", JSONOutput: enabled, MaxOutputTokens: 1024})
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if enabled && (r.ResponseFormat == nil || r.ResponseFormat.Type != "json_object") {
			t.Fatal("missing JSON output")
		}
		if !enabled && strings.Contains(string(b), "response_format") {
			t.Fatal("changed ordinary requests")
		}
		if r.MaxTokens != 1024 {
			t.Fatal("lost generation bound")
		}
	}
}
