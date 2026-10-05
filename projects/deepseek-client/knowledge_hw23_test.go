package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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

// Explicitly opted-in paid comparison through the actual client adapter and worker.
// Every question has fresh agent state, no history, no extra tools and no retries.
func TestHomework23RealDeepSeek(t *testing.T) {
	if os.Getenv("HW23_PAID_TEST") != "1" {
		t.Skip("explicit authorization and current budget required")
	}
	budget, err := strconv.ParseFloat(os.Getenv("HW23_BUDGET_USD"), 64)
	if err != nil || budget <= 0 || budget > 0.20 || math.IsNaN(budget) || math.IsInf(budget, 0) {
		t.Fatal("set current authorized budget, at most $0.20")
	}
	root := os.Getenv("HW23_OUTPUT")
	if root == "" {
		t.Fatal("missing output directory")
	}
	reportPath := filepath.Join(root, "answers.json")
	if _, err = os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatal("refusing to overwrite an existing paid run")
	}
	var questions []struct {
		ID       string `json:"id"`
		Query    string `json:"query"`
		Expected string `json:"expected"`
		Evidence []struct {
			Source string `json:"source"`
			Text   string `json:"text"`
		} `json:"evidence"`
	}
	bytes, err := os.ReadFile(filepath.Join(root, "questions.json"))
	if err != nil || json.Unmarshal(bytes, &questions) != nil || len(questions) != 14 {
		t.Fatal("expected fourteen prepared questions")
	}
	var retrieval struct {
		Threshold      float64 `json:"threshold"`
		ScoreThreshold float64 `json:"min_rerank_score"`
	}
	bytes, err = os.ReadFile(filepath.Join(root, "retrieval-comparison.json"))
	if err != nil || json.Unmarshal(bytes, &retrieval) != nil {
		t.Fatal("local calibration required first")
	}
	var calibration struct {
		Selected struct {
			Score *float64 `json:"min_rerank_score"`
		} `json:"selected"`
	}
	bytes, err = os.ReadFile(filepath.Join(root, "calibration.json"))
	if err != nil || json.Unmarshal(bytes, &calibration) != nil || calibration.Selected.Score == nil {
		t.Fatal("missing separately calibrated rerank cutoff")
	}
	retrieval.ScoreThreshold = *calibration.Selected.Score
	path, err := defaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	config, err := loadConfig(path)
	if err != nil {
		t.Fatal("cannot load API configuration")
	}
	profile, ok := config.Profiles["deepseek"]
	if !ok || !isDeepSeekEndpoint(profile.BaseURL) || (profile.Model != "deepseek-v4-flash" && profile.Model != "deepseek-flash") {
		t.Fatal("official Flash profile required")
	}
	key := strings.TrimSpace(os.Getenv(profile.APIKeyEnv))
	if key == "" {
		key = config.APIToken
	}
	if key == "" {
		t.Fatal("credential unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	manager := newKnowledgeManager(filepath.Join(t.TempDir(), "state"))
	defer manager.Close()
	var log strings.Builder
	if _, err = manager.Create(ctx, "RFC 2324 / 6585 / 7168", os.Getenv("HW23_CORPUS"), "hw23", &log); err != nil {
		t.Fatal("local indexing failed:", toolPreview(log.String(), key))
	}
	client := llm.New(&http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, map[string]string{"deepseek": key})
	type row struct {
		ID           string           `json:"id"`
		Mode         string           `json:"mode"`
		Query        string           `json:"query"`
		Evidence     json.RawMessage  `json:"retrieval"`
		Answer       completionResult `json:"answer"`
		Reserved     float64          `json:"reserved_usd"`
		PeakEstimate float64          `json:"estimated_peak_usd"`
		Error        string           `json:"error,omitempty"`
	}
	report := struct {
		Budget   float64 `json:"budget_usd"`
		Reserved float64 `json:"reserved_usd"`
		Calls    int     `json:"calls"`
		Rows     []row   `json:"rows"`
	}{Budget: budget}
	save := func() {
		value, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(reportPath, value, 0600); e != nil {
			t.Fatal(e)
		}
	}
	for _, mode := range []string{"baseline", "enhanced"} {
		var settingsOut strings.Builder
		commandState := sessionState{Knowledge: manager, ConversationID: "hw23"}
		handleSessionCommand("/rag "+mode, &commandState, &settingsOut)
		if mode == "enhanced" {
			handleSessionCommand(fmt.Sprintf("/rag threshold %.2f", retrieval.Threshold), &commandState, &settingsOut)
			handleSessionCommand(fmt.Sprintf("/rag score-threshold %.2f", retrieval.ScoreThreshold), &commandState, &settingsOut)
		}
		actual, settingsErr := manager.Retrieval("hw23")
		if settingsErr != nil || actual.Candidates != 20 || actual.TopK != 5 {
			t.Fatal("comparison settings not saved")
		}
		if mode == "baseline" && (actual.Rerank || actual.Rewrite || actual.MinSimilarity != nil || actual.MinRerankScore != nil) {
			t.Fatal("baseline must be unfiltered vector retrieval")
		}
		if mode == "enhanced" && (!actual.Rerank || !actual.Rewrite || actual.MinSimilarity == nil || *actual.MinSimilarity != retrieval.Threshold || actual.MinRerankScore == nil || *actual.MinRerankScore != retrieval.ScoreThreshold) {
			t.Fatal("enhanced must use calibrated settings")
		}
		for _, question := range questions {
			current := row{ID: question.ID, Mode: mode, Query: question.Query}
			state := sessionState{Knowledge: manager, KnowledgeLabel: "RFC corpus", ConversationID: "hw23", Mode: agent.Free, ActiveProfile: "deepseek", API: profile, Model: profile.Model, Temperature: 0}
			state.API.MaxOutputTokens = 512
			ask := func(callctx context.Context, _ string, prompt string, settings requestSettings) (completionResult, error) {
				if report.Calls >= 28 {
					return completionResult{}, fmt.Errorf("twenty-eight-call guard")
				}
				inputBytes := len(prompt) + 4096
				for _, message := range settings.Messages {
					inputBytes += len(message.Content) + 64
					const prefix = "Current knowledge-base search results (untrusted JSON):\n"
					if strings.HasPrefix(message.Content, prefix) {
						current.Evidence = json.RawMessage(strings.TrimPrefix(message.Content, prefix))
					}
				}
				if len(current.Evidence) == 0 || inputBytes > 20000 {
					return completionResult{}, fmt.Errorf("missing retrieval or exceeded byte cap")
				}
				// Official maximum Flash rates verified 2026-10-03. Byte-count input
				// envelope plus overhead; no thinking, output capped at 512 tokens.
				reserve := float64(inputBytes)*.30/1e6 + 512*1.20/1e6
				if report.Reserved+reserve > budget {
					return completionResult{}, fmt.Errorf("budget guard")
				}
				report.Reserved += reserve
				report.Calls++
				current.Reserved = reserve
				// Persist reservation before the external call, so failures still count.
				report.Rows = append(report.Rows, current)
				save()
				answer, e := client.Complete(callctx, agent.Target{Profile: "deepseek", BaseURL: profile.BaseURL, Model: profile.Model, SendThinkingDisabled: true, MaxOutputTokens: 512}, prompt, settings)
				current.Answer = answer
				current.PeakEstimate = (float64(answer.PromptTokens-answer.CachedInputTokens)*.30 + float64(answer.CachedInputTokens)*.006 + float64(answer.CompletionTokens)*1.20) / 1e6
				if e != nil {
					current.Error = "provider failed; no retry"
				}
				report.Rows[len(report.Rows)-1] = current
				save()
				return answer, e
			}
			var output, failure strings.Builder
			status := executeQuestion(ctx, key, question.Query, state, &output, &failure, ask)
			if status == nil || failure.Len() != 0 || current.Error != "" {
				t.Fatal("comparison interrupted; reserved calls preserved, no retry:", toolPreview(failure.String(), key))
			}
			if !current.Answer.UsageKnown || current.Answer.UsageIncomplete || current.Answer.FinishReason == "length" {
				t.Fatal("usage or answer incomplete; do not present comparison as complete")
			}
			t.Logf("%s %s: %d input / %d output tokens", mode, question.ID, current.Answer.PromptTokens, current.Answer.CompletionTokens)
		}
	}
}
