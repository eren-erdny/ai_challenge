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

// Ten fresh TUI turns on public RFCs. Opt in only with a current authorized
// budget. No tools, history, memory, audits, thinking or automatic retries.
func TestHomework24RealDeepSeek(t *testing.T) {
	if os.Getenv("HW24_PAID_TEST") != "1" {
		t.Skip("explicit authorization and current budget required")
	}
	budget, err := strconv.ParseFloat(os.Getenv("HW24_BUDGET_USD"), 64)
	if err != nil || budget <= 0 || budget > .20 || math.IsNaN(budget) || math.IsInf(budget, 0) {
		t.Fatal("current authorized budget must be <= $0.20")
	}
	root := os.Getenv("HW24_OUTPUT")
	if root == "" {
		t.Fatal("missing output directory")
	}
	reportPath := filepath.Join(root, "answers.json")
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatal("refusing to overwrite a paid run")
	}
	var questions []struct {
		ID       string `json:"id"`
		Query    string `json:"query"`
		Expected string `json:"expected"`
		Evidence []struct {
			Source  string `json:"source"`
			Section string `json:"section"`
			Text    string `json:"text"`
		} `json:"evidence"`
	}
	data, err := os.ReadFile(filepath.Join(root, "questions.json"))
	if err != nil || json.Unmarshal(data, &questions) != nil || len(questions) != 10 {
		t.Fatal("ten prepared questions required")
	}
	configPath, err := defaultConfigPath()
	if err != nil {
		t.Fatal("configuration path unavailable")
	}
	userConfig, err := loadConfig(configPath)
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	profile, ok := userConfig.Profiles["deepseek"]
	if !ok || !isDeepSeekEndpoint(profile.BaseURL) || (profile.Model != "deepseek-v4-flash" && profile.Model != "deepseek-flash") {
		t.Fatal("official Flash profile required")
	}
	key := strings.TrimSpace(os.Getenv(profile.APIKeyEnv))
	if key == "" {
		key = userConfig.APIToken
	}
	if key == "" {
		t.Fatal("credential unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	manager := newKnowledgeManager(filepath.Join(t.TempDir(), "state"))
	defer manager.Close()
	var setup strings.Builder
	if _, err := manager.Create(ctx, "RFC corpus", os.Getenv("HW24_CORPUS"), "hw24", &setup); err != nil {
		t.Fatal("local corpus indexing failed:", toolPreview(setup.String(), key))
	}
	profile.MaxOutputTokens = 1024
	client := llm.New(&http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, map[string]string{"deepseek": key})
	type row struct {
		ID           string                `json:"id"`
		Query        string                `json:"query"`
		Retrieval    json.RawMessage       `json:"retrieval"`
		RawAnswer    agent.Completion      `json:"raw_answer"`
		Grounding    *agent.GroundedAnswer `json:"grounding"`
		Visible      string                `json:"visible"`
		Calls        int                   `json:"calls"`
		Reserved     float64               `json:"reserved_usd"`
		PeakEstimate float64               `json:"estimated_peak_usd"`
		Error        string                `json:"error,omitempty"`
	}
	report := struct {
		Budget   float64 `json:"budget_usd"`
		Reserved float64 `json:"reserved_usd"`
		Calls    int     `json:"calls"`
		Pricing  string  `json:"pricing"`
		Rows     []row   `json:"rows"`
	}{Budget: budget, Pricing: "https://api-docs.deepseek.com/quick_start/pricing/ verified 2026-10-03; maximum Flash USD/1M: input .30, cache .006, output 1.20"}
	save := func() {
		b, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(reportPath, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range questions {
		current := row{ID: q.ID, Query: q.Query}
		retrieved, e := manager.Search(ctx, "hw24", q.Query)
		if e != nil {
			t.Fatal("local search failed")
		}
		current.Retrieval = json.RawMessage(retrieved)
		ask := func(callctx context.Context, _ string, prompt string, s requestSettings) (completionResult, error) {
			if report.Calls >= 10 || current.Calls != 0 || !s.JSONOutput || len(s.Tools) != 0 || s.MaxOutputTokens != 1024 || !s.SendThinkingDisabled {
				return completionResult{}, fmt.Errorf("bounded one-call JSON request required")
			}
			inputBytes := len(prompt) + 4096
			for _, m := range s.Messages {
				inputBytes += len(m.Content) + 64
			}
			if inputBytes > 24000 {
				return completionResult{}, fmt.Errorf("input byte envelope exceeded")
			}
			reserve := float64(inputBytes)*.30/1e6 + 1024*1.20/1e6
			if report.Reserved+reserve > budget {
				return completionResult{}, fmt.Errorf("budget guard")
			}
			report.Reserved += reserve
			report.Calls++
			current.Calls++
			current.Reserved = reserve
			report.Rows = append(report.Rows, current)
			save()
			a, e := client.Complete(callctx, agent.Target{Profile: "deepseek", BaseURL: profile.BaseURL, Model: profile.Model, SendThinkingDisabled: true}, prompt, s)
			current.RawAnswer = a
			if e != nil {
				current.Error = "provider request failed; no retry"
			} else if a.UsageKnown && !a.UsageIncomplete {
				current.PeakEstimate = (float64(a.PromptTokens-a.CachedInputTokens)*.30 + float64(a.CachedInputTokens)*.006 + float64(a.CompletionTokens)*1.20) / 1e6
			}
			report.Rows[len(report.Rows)-1] = current
			save()
			return a, e
		}
		// Use the actual TUI command and result update. This is automated TUI
		// verification, not a desktop recording or a fixture model.
		config := defaultAppConfig()
		config.ResponseControl.Enabled = false
		config.ActiveProfile = "deepseek"
		config.Profiles = map[string]apiProfile{"deepseek": profile}
		config.APIToken = key
		config.ConversationID = "hw24"
		config.Knowledge = manager
		config.Generation.Temperature = 0
		model := newTUIModel(config, ask)
		model.ctx = ctx
		msg := executeQuestionCommand(ctx, key, q.Query, model.state, ask)().(answerMessage)
		updated, _ := model.Update(msg)
		model = updated.(tuiModel)
		current.Visible = msg.text
		if msg.lastStatus != nil {
			current.Grounding = msg.lastStatus.Grounding
		}
		if current.Calls == 0 {
			report.Rows = append(report.Rows, current)
		} else {
			report.Rows[len(report.Rows)-1] = current
		}
		if msg.lastStatus == nil || current.Grounding == nil || current.Error != "" {
			current.Error = "response failed validation or transport; no retry"
			report.Rows[len(report.Rows)-1] = current
			save()
			t.Fatal("grounding run interrupted; evidence retained")
		}
		if current.Calls > 0 && (!current.RawAnswer.UsageKnown || current.RawAnswer.UsageIncomplete || current.RawAnswer.FinishReason != "stop") {
			save()
			t.Fatal("provider usage/output incomplete")
		}
		if len(q.Evidence) > 0 && (current.Grounding.Status != "answered" || len(current.Grounding.Sources) == 0 || len(current.Grounding.Quotes) == 0) {
			save()
			t.Fatal("factual question abstained; evaluate before claiming success", q.ID)
		}
		if len(q.Evidence) == 0 && (current.Grounding.Status != "unknown" || current.Grounding.Clarification == "" || len(current.Grounding.Quotes) != 0 || len(current.Grounding.Sources) != 0) {
			save()
			t.Fatal("unsupported question not safely handled", q.ID)
		}
		if q.ID == "Q09" && (current.Calls != 0 || !current.Grounding.Local) {
			save()
			t.Fatal("empty context must use zero LLM calls")
		}
		if !strings.Contains(strings.Join(model.history, "\n"), current.Grounding.Answer) {
			save()
			t.Fatal("validated answer missing from TUI")
		}
		save()
		t.Logf("%s: %s, sources=%d quotes=%d calls=%d", q.ID, current.Grounding.Status, len(current.Grounding.Sources), len(current.Grounding.Quotes), current.Calls)
	}
}
