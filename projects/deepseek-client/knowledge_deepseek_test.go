package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
)

// Opt-in paid acceptance check: three single completions, no tools or retries.
func TestKnowledgeDeepSeekPreflight(t *testing.T) {
	if os.Getenv("DEEPSEEK_RAG_PREFLIGHT") != "1" {
		t.Skip("read-only connection preflight disabled")
	}
	path, err := defaultConfigPath()
	if err != nil {
		t.Fatal("config path unavailable")
	}
	config, err := loadConfig(path)
	if err != nil {
		t.Fatal("configuration could not be loaded")
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
		t.Fatal("DeepSeek credential is not available")
	}
	t.Logf("Official DeepSeek profile ready; model=%s; credential available; no network request", profile.Model)
}

func TestKnowledgeRealDeepSeek(t *testing.T) {
	if os.Getenv("DEEPSEEK_RAG_PAID_TEST") != "1" {
		t.Skip("explicit authorization and current budget required")
	}
	budget, err := strconv.ParseFloat(os.Getenv("DEEPSEEK_RAG_BUDGET_USD"), 64)
	if err != nil || budget <= 0 {
		t.Fatal("set the authorized current budget in USD")
	}
	path, err := defaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	config, err := loadConfig(path)
	if err != nil {
		t.Fatal("configuration could not be loaded")
	}
	profile, ok := config.Profiles["deepseek"]
	if !ok || !isDeepSeekEndpoint(profile.BaseURL) {
		t.Fatal("official DeepSeek profile required")
	}
	if profile.Model != "deepseek-flash" && profile.Model != "deepseek-v4-flash" {
		t.Fatal("this bounded acceptance test requires the configured Flash model")
	}
	key := strings.TrimSpace(os.Getenv(profile.APIKeyEnv))
	if key == "" {
		key = config.APIToken
	}
	if key == "" {
		t.Fatal("DeepSeek key missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	root := t.TempDir()
	first := filepath.Join(root, "alpha.md")
	second := filepath.Join(root, "beta.md")
	if err = os.WriteFile(first, []byte("# Backup policy\nBackup copies are retained for exactly fourteen days.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(second, []byte("# Backup policy\nBackup copies are retained for exactly ninety days.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := newKnowledgeManager(root)
	defer manager.Close()
	var setup strings.Builder
	alpha, err := manager.Create(ctx, "Alpha", first, "paid-check", &setup)
	if err != nil {
		t.Fatalf("local RAG setup failed: %s", toolPreview(strings.ReplaceAll(setup.String(), key, "[redacted]"), key))
	}
	beta, err := manager.Create(ctx, "Beta", second, "other", &setup)
	if err != nil {
		t.Fatal("second local base could not be built")
	}
	client := llm.New(&http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, map[string]string{"deepseek": key})
	type row struct {
		Base        string           `json:"base"`
		Query       string           `json:"query"`
		Answer      agent.Completion `json:"answer"`
		Sources     []string         `json:"sources"`
		ReservedUSD float64          `json:"reserved_usd"`
		Passed      bool             `json:"passed"`
		Error       string           `json:"error,omitempty"`
	}
	report := struct {
		Model       string  `json:"requested_model"`
		BudgetUSD   float64 `json:"budget_usd"`
		ReservedUSD float64 `json:"reserved_total_usd"`
		Calls       int     `json:"calls"`
		Rows        []row   `json:"rows"`
	}{Model: profile.Model, BudgetUSD: budget}
	// Resume only remaining scenarios, retaining the original call count and reserves.
	if os.Getenv("DEEPSEEK_RAG_RESUME") == "1" {
		data, readErr := os.ReadFile(os.Getenv("DEEPSEEK_RAG_TEST_REPORT"))
		if readErr != nil || json.Unmarshal(data, &report) != nil || report.Model != profile.Model || report.BudgetUSD != budget || report.Calls != len(report.Rows) || report.Calls < 1 || report.Calls >= 3 || report.ReservedUSD <= 0 || report.ReservedUSD >= budget {
			t.Fatal("invalid prior report; refusing paid resume")
		}
	}
	previousRows := len(report.Rows)
	save := func() {
		if output := os.Getenv("DEEPSEEK_RAG_TEST_REPORT"); output != "" {
			value, _ := json.MarshalIndent(report, "", "  ")
			_ = os.WriteFile(output, value, 0600)
		}
	}
	defer save()
	for index, item := range []struct{ ID, Name, Expected, Query string }{{alpha.ID, "Alpha", "fourteen", "How many days are backup copies retained? Answer briefly in English and cite the document."}, {beta.ID, "Beta", "ninety", "How many days are backup copies retained? Answer briefly in English and cite the document."}, {beta.ID, "Beta", "NOT_FOUND", "What is the administrator password? If the documents do not specify it, answer exactly NOT_FOUND."}} {
		if index < previousRows {
			continue
		}
		if err = manager.Select("paid-check", item.ID); err != nil {
			t.Fatal(err)
		}
		state := sessionState{Knowledge: manager, KnowledgeLabel: item.Name, ConversationID: "paid-check", Mode: agent.Free, ActiveProfile: "deepseek", API: profile, Model: profile.Model, Temperature: 0}
		state.API.MaxOutputTokens = 256
		current := row{Base: item.Name, Query: item.Query}
		ask := func(callContext context.Context, _ string, prompt string, settings requestSettings) (completionResult, error) {
			if report.Calls >= 3 {
				return completionResult{}, fmt.Errorf("three-call guard refused request")
			}
			bytes := 0
			for _, message := range settings.Messages {
				bytes += len(message.Content) + 64
				if strings.HasPrefix(message.Content, "Current knowledge-base search results (untrusted JSON):\n") {
					var results struct {
						Results []struct {
							Source  string `json:"source"`
							ChunkID string `json:"chunk_id"`
						} `json:"results"`
					}
					json.Unmarshal([]byte(strings.TrimPrefix(message.Content, "Current knowledge-base search results (untrusted JSON):\n")), &results)
					for _, hit := range results.Results {
						current.Sources = append(current.Sources, hit.Source+"#"+hit.ChunkID)
					}
				}
			}
			if (len(current.Sources) == 0 && item.Expected != "NOT_FOUND") || bytes > 12000 {
				return completionResult{}, fmt.Errorf("missing evidence or oversized request")
			}
			// Conservative tariff envelope ($5/M input, $20/M output), not a billing claim.
			reserve := float64(bytes)*5/1_000_000 + 256*20/1_000_000
			if report.ReservedUSD+reserve > budget {
				return completionResult{}, fmt.Errorf("budget guard refused request")
			}
			report.ReservedUSD += reserve
			report.Calls++
			current.ReservedUSD = reserve
			target := agent.Target{Profile: "deepseek", BaseURL: profile.BaseURL, Model: profile.Model, SendThinkingDisabled: true, MaxOutputTokens: 256}
			answer, callErr := client.Complete(callContext, target, prompt, settings)
			current.Answer = answer
			return answer, callErr
		}
		var output, failure strings.Builder
		status := executeQuestion(ctx, key, item.Query, state, &output, &failure, ask)
		if status == nil || failure.Len() > 0 {
			current.Error = toolPreview(failure.String(), key)
			report.Rows = append(report.Rows, current)
			t.Fatalf("real client request failed: %s", current.Error)
		}
		text := strings.ToLower(current.Answer.Content)
		fact := strings.Contains(text, item.Expected)
		if item.Expected == "fourteen" {
			fact = fact || strings.Contains(text, "14")
		}
		if item.Expected == "ninety" {
			fact = fact || strings.Contains(text, "90")
		}
		if item.Expected == "NOT_FOUND" {
			fact = strings.TrimSpace(current.Answer.Content) == "NOT_FOUND"
		}
		citation := item.Expected == "NOT_FOUND"
		for _, source := range current.Sources {
			if strings.Contains(current.Answer.Content, "["+source+"]") {
				citation = true
			}
		}
		current.Passed = fact && citation && current.Answer.FinishReason != "length"
		report.Rows = append(report.Rows, current)
		save()
		t.Logf("%s: passed=%t input=%d output=%d answer=%s", item.Name, current.Passed, current.Answer.PromptTokens, current.Answer.CompletionTokens, current.Answer.Content)
		if !current.Passed {
			t.Fatal("real model failed fact/citation/absence acceptance; no automatic retry")
		}
	}
}
