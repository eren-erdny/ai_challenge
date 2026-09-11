package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
)

// Explicitly opt-in: three paid Flash calls, isolated history, no retries.
func TestLiveTokenExperiment(t *testing.T) {
	if os.Getenv("DEEPSEEK_LIVE_TOKEN_TEST") != "1" {
		t.Skip("paid test disabled")
	}
	path, err := defaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	config, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	profile, ok := config.Profiles["deepseek"]
	if !ok || !isDeepSeekEndpoint(profile.BaseURL) {
		t.Fatal("official DeepSeek profile required")
	}
	key := strings.TrimSpace(os.Getenv(profile.APIKeyEnv))
	if key == "" {
		key = config.APIToken
	}
	if key == "" {
		t.Fatal("DeepSeek key missing")
	}
	client := llm.New(&http.Client{Timeout: 3 * time.Minute}, map[string]string{"deepseek": key})
	store := &history.JSON{Dir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	runner := agent.NewWithHistory(client, store)
	target := agent.Target{Profile: "deepseek", BaseURL: profile.BaseURL, Model: "deepseek-flash", SendThinkingDisabled: true, MaxOutputTokens: 64}
	request := agent.Request{ConversationID: "experiment", Mode: agent.Free, Temperature: 0, Target: target, Prompt: "What is the project name and budget mentioned earlier? Answer in one short sentence."}
	type row struct {
		Scenario         string            `json:"scenario"`
		HistoryMessages  int               `json:"history_messages"`
		HistoryBytes     int               `json:"history_bytes"`
		Tokens           agent.TokenReport `json:"tokens"`
		Answer           agent.Completion  `json:"answer"`
		Error            string            `json:"error,omitempty"`
		CostUpperUSD     float64           `json:"cost_upper_usd"`
		HistoryUnchanged bool              `json:"history_unchanged_on_failure"`
	}
	report := struct {
		UTC       time.Time `json:"utc"`
		Model     string    `json:"model"`
		BudgetUSD float64   `json:"budget_usd"`
		Rows      []row     `json:"rows"`
	}{UTC: time.Now().UTC(), Model: target.Model, BudgetUSD: 1}
	messages := []agent.Message{{Role: "user", Content: "My project is North. Its budget is 75000 rubles."}, {Role: "assistant", Content: "Noted."}}
	spentBound := 0.0
	for i, scenario := range []string{"short", "long", "overflow"} {
		if i > 0 {
			repeats := 2000
			if i == 2 {
				repeats = 1_100_000
			}
			messages = append(messages, agent.Message{Role: "user", Content: "Background data (ignore):" + strings.Repeat(" x", repeats)}, agent.Message{Role: "assistant", Content: "Noted."})
		}
		if err := store.Save(ctx, request.ConversationID, messages); err != nil {
			t.Fatal(err)
		}
		bytes := len(request.Prompt)
		for _, m := range messages {
			bytes += len(m.Content) + 32
		}
		// Conservative byte-based charge reserve at published peak Flash rates.
		bound := float64(bytes)*0.30/1_000_000 + 64*1.20/1_000_000
		if spentBound+bound > report.BudgetUSD {
			t.Fatal("budget guard refused request")
		}
		spentBound += bound
		t.Logf("Calling %s: messages=%d, bytes=%d, cumulative worst-case reserve=$%.6f", scenario, len(messages), bytes, spentBound)
		result, callErr := runner.Run(ctx, request)
		r := row{Scenario: scenario, HistoryMessages: len(messages), HistoryBytes: bytes - len(request.Prompt)}
		if callErr != nil {
			r.Error = strings.ReplaceAll(callErr.Error(), key, "[redacted]")
			if result.Failed != nil {
				r.Tokens = result.Failed.Tokens
			}
			loaded, err := store.Load(ctx, request.ConversationID)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(messages)
			after, _ := json.Marshal(loaded)
			r.HistoryUnchanged = string(before) == string(after)
		} else {
			r.Answer = result.Responses[0].Answer
			r.Tokens = result.Responses[0].Tokens
			cached := min(r.Answer.CachedInputTokens, r.Answer.PromptTokens)
			r.CostUpperUSD = (float64(r.Answer.PromptTokens-cached)*.30 + float64(cached)*.006 + float64(r.Answer.CompletionTokens)*1.20) / 1_000_000
			messages, err = store.Load(ctx, request.ConversationID)
			if err != nil {
				t.Fatal(err)
			}
		}
		report.Rows = append(report.Rows, r)
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile("live-token-results.json", data, 0600); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%s: input=%d output=%d cost<=%.8f duration=%s error=%s\n", scenario, r.Answer.PromptTokens, r.Answer.CompletionTokens, r.CostUpperUSD, r.Answer.Duration, r.Error)
		if callErr != nil && i < 2 {
			t.Fatal("early API error; see live-token-results.json (no retries)")
		}
		if i < 2 && (!r.Tokens.UsageKnown || !strings.Contains(r.Answer.Content, "North")) {
			t.Fatal("expected factual answer and usage; see live-token-results.json")
		}
		if i == 2 && (callErr == nil || !strings.Contains(r.Error, "maximum context length") || !r.HistoryUnchanged) {
			t.Fatal("expected context overflow and unchanged history; see live-token-results.json")
		}
	}
}
