package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/filetools"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/mcptools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type projectVerificationRow struct {
	Question  string                `json:"question"`
	Evidence  json.RawMessage       `json:"evidence"`
	Answers   []agent.Completion    `json:"raw_answers"`
	Grounding *agent.GroundedAnswer `json:"grounding"`
	Visible   string                `json:"visible"`
	Error     string                `json:"error,omitempty"`
}
type projectBudgetLedger struct {
	Budget        float64 `json:"budget_usd"`
	Reserved      float64 `json:"reserved_usd"`
	EstimatedPeak float64 `json:"estimated_peak_usd"`
	Calls         int     `json:"calls"`
}

func projectWriteJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}
func projectEvidence(s requestSettings) string {
	for _, m := range s.Messages {
		if strings.HasPrefix(m.Content, "Current knowledge-base search results (untrusted JSON):\n") {
			return strings.TrimPrefix(m.Content, "Current knowledge-base search results (untrusted JSON):\n")
		}
	}
	return ""
}
func projectFixture(evidence, quote, claim string) (completionResult, error) {
	if quote == "" {
		return completionResult{Content: `{"status":"unknown","claims":[],"quotes":[],"clarification":"Добавьте документ с описанием PostgreSQL."}`}, nil
	}
	var wire struct {
		Results []struct {
			Text string `json:"text"`
			ID   string `json:"chunk_id"`
		} `json:"results"`
		Context []struct {
			Text string `json:"text"`
			ID   string `json:"chunk_id"`
		} `json:"context_results"`
	}
	if json.Unmarshal([]byte(evidence), &wire) != nil {
		return completionResult{}, fmt.Errorf("missing evidence")
	}
	for _, hit := range append(wire.Results, wire.Context...) {
		if strings.Contains(hit.Text, quote) {
			b, _ := json.Marshal(map[string]any{"status": "answered", "claims": []any{map[string]any{"text": claim, "quote_ids": []string{"q1"}}}, "quotes": []any{map[string]string{"id": "q1", "chunk_id": hit.ID, "text": quote}}, "clarification": ""})
			return completionResult{Content: string(b), Model: "fixture-not-DeepSeek"}, nil
		}
	}
	return completionResult{}, fmt.Errorf("expected excerpt not retrieved: %s", quote)
}

// Opt-in: real local retrieval + real subprocess MCP + actual TUI submit/update.
// Local mode uses fixture generation; paid mode uses bounded official DeepSeek.
func TestProjectAssistantTUI(t *testing.T) {
	paid := os.Getenv("PROJECT_ASSISTANT_PAID") == "1"
	if !paid && os.Getenv("PROJECT_ASSISTANT_LOCAL") != "1" {
		t.Skip("opt-in project integration")
	}
	binary := os.Getenv("PROJECT_ASSISTANT_BINARY")
	manifest := os.Getenv("PROJECT_ASSISTANT_INDEX")
	output := os.Getenv("PROJECT_ASSISTANT_OUTPUT")
	if binary == "" || manifest == "" || output == "" {
		t.Fatal("binary, index and unique output directory required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(output, "answers.json")
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatal("refusing to overwrite prior run")
	}
	project, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoCmd := exec.Command("git", "rev-parse", "--show-toplevel")
	repoBytes, err := repoCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	repo := strings.TrimSpace(string(repoBytes))
	branchBytes, err := exec.Command("git", "branch", "--show-current").Output()
	if err != nil {
		t.Fatal(err)
	}
	branch := strings.TrimSpace(string(branchBytes))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	tools, err := mcptools.ConnectMany(ctx, []mcptools.Registration{{Name: "git", Connect: func(ctx context.Context) (mcptools.Session, error) {
		return mcptools.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(binary, "--mcp-git-server", repo)}, nil)
	}}}, &filetools.Documents{Dir: project})
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	manager := newKnowledgeManager(filepath.Join(output, "state"))
	defer manager.Close()
	_, err = manager.AttachIndex("DeepSeek project documentation", manifest, "project-verification")
	if err != nil {
		t.Fatal(err)
	}
	profile := apiProfile{BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", ContextWindow: 32768, MaxOutputTokens: 768}
	config := defaultAppConfig()
	config.Profiles = map[string]apiProfile{"deepseek": profile}
	config.ActiveProfile = "deepseek"
	config.ProjectRoot = project
	config.Knowledge = manager
	config.DocumentsDirectory = project
	config.Tools = tools
	config.ConversationID = "project-verification"
	config.History = &history.JSON{Dir: filepath.Join(output, "history")}
	config.HistoryPolicy.KeepLast = 2
	config.HistoryPolicy.Strategy = agent.MemorySliding
	config.ResponseControl.Enabled = false
	config.Generation.Temperature = 0
	config.Generation.Strategy = strategyStandard
	ledger := projectBudgetLedger{Budget: .20}
	key := ""
	ledgerPath := os.Getenv("PROJECT_ASSISTANT_LEDGER")
	if paid {
		if os.Getenv("PROJECT_ASSISTANT_BUDGET_USD") != "0.20" || ledgerPath == "" {
			t.Fatal("explicit current $0.20 budget and ledger required")
		}
		lock, err := os.OpenFile(ledgerPath+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal("budget ledger locked")
		}
		lock.Close()
		defer os.Remove(ledgerPath + ".lock")
		if b, err := os.ReadFile(ledgerPath); err == nil {
			if json.Unmarshal(b, &ledger) != nil || ledger.Budget != .20 || ledger.Reserved < 0 || ledger.Reserved > .20 {
				t.Fatal("invalid shared ledger")
			}
		} else if !os.IsNotExist(err) {
			t.Fatal("ledger unreadable")
		}
		configPath, err := defaultConfigPath()
		if err != nil {
			t.Fatal("config unavailable")
		}
		user, err := loadConfig(configPath)
		if err != nil {
			t.Fatal("config unavailable")
		}
		configured, ok := user.Profiles["deepseek"]
		if !ok || !isDeepSeekEndpoint(configured.BaseURL) {
			t.Fatal("official DeepSeek configuration required")
		}
		key = strings.TrimSpace(os.Getenv(configured.APIKeyEnv))
		if key == "" {
			key = user.APIToken
		}
		if key == "" {
			t.Fatal("credential unavailable")
		}
	}
	report := struct {
		Kind    string                   `json:"kind"`
		Pricing string                   `json:"pricing"`
		Branch  string                   `json:"branch"`
		Ledger  projectBudgetLedger      `json:"ledger"`
		Rows    []projectVerificationRow `json:"rows"`
	}{Kind: "Real RAG + subprocess MCP + scripted TUI; fixture generation", Branch: branch,
		Pricing: "https://api-docs.deepseek.com/quick_start/pricing/?tab=case-studies checked 2026-10-16; Flash peak USD per 1M: input 0.30, cache 0.006, output 1.20"}
	if paid {
		report.Kind = "Real DeepSeek + real RAG + subprocess MCP + scripted TUI; no recording"
	}
	save := func() {
		report.Ledger = ledger
		if err := projectWriteJSON(reportPath, report); err != nil {
			t.Fatal(err)
		}
	}
	defer save()
	questions := []struct{ Query, Quote, Claim, Contains string }{
		{"Как устроен проект? За что отвечает пакет agent?", "Пакет agent отвечает за подготовку запросов, цикл вызова инструментов, проверку ответов, инварианты и учёт токенов.", "Пакет agent подготавливает запросы, вызывает инструменты и проверяет ответы.", "agent"},
		{"Где добавлять новый Git MCP-инструмент?", "Регистрация Git MCP-инструментов находится в gitmcp/server.go.", "Git MCP-инструменты регистрируются в gitmcp/server.go.", "gitmcp/server.go"},
		{"На какой git-ветке мы сейчас работаем?", "Current Git status: ## " + branch, "Текущая ветка: " + branch, branch},
		{"Какие поля содержит доказательный ответ модели?", "Доказательный ответ модели содержит status, claims, quotes и clarification.", "Ответ содержит status, claims, quotes и clarification.", "clarification"},
		{"Какой порт использует PostgreSQL-база нашего проекта?", "", "", ""},
	}
	client := llm.New(&http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, map[string]string{"deepseek": key})
	var current int
	perQuestionCalls := 0
	ask := func(callCtx context.Context, _ string, prompt string, s requestSettings) (completionResult, error) {
		row := &report.Rows[current]
		evidence := projectEvidence(s)
		row.Evidence = json.RawMessage(evidence)
		q := questions[current]
		if _, err := projectFixture(evidence, q.Quote, q.Claim); err != nil {
			return completionResult{}, err
		}
		if !paid {
			return projectFixture(evidence, q.Quote, q.Claim)
		}
		if !s.JSONOutput || s.MaxOutputTokens != 768 || !s.SendThinkingDisabled {
			return completionResult{}, fmt.Errorf("bounded non-thinking JSON request required")
		}
		b, _ := json.Marshal(s)
		envelope := len(b) + len(prompt) + 4096
		reserve := float64(envelope)*.30/1e6 + 768*1.20/1e6
		if envelope > 60000 || ledger.Calls >= 10 || perQuestionCalls >= 2 || ledger.Reserved+reserve > ledger.Budget {
			return completionResult{}, fmt.Errorf("shared budget or request bound reached")
		}
		ledger.Reserved += reserve
		ledger.Calls++
		perQuestionCalls++
		if err := projectWriteJSON(ledgerPath, ledger); err != nil {
			return completionResult{}, err
		}
		save()
		answer, err := client.Complete(callCtx, agent.Target{Profile: "deepseek", BaseURL: profile.BaseURL, Model: profile.Model, SendThinkingDisabled: true}, prompt, s)
		row.Answers = append(row.Answers, answer)
		if answer.UsageKnown && !answer.UsageIncomplete {
			ledger.EstimatedPeak += (float64(answer.PromptTokens-answer.CachedInputTokens)*.30 + float64(answer.CachedInputTokens)*.006 + float64(answer.CompletionTokens)*1.20) / 1e6
		}
		if e := projectWriteJSON(ledgerPath, ledger); e != nil {
			return answer, e
		}
		save()
		if err != nil {
			return answer, fmt.Errorf("provider failed; reservation retained")
		}
		return answer, nil
	}
	model := newTUIModel(config, ask)
	model.ctx = ctx
	for i, q := range questions {
		current = i
		perQuestionCalls = 0
		report.Rows = append(report.Rows, projectVerificationRow{Question: q.Query})
		model.textarea.SetValue("/help " + q.Query)
		next, cmd := model.submit()
		model = next.(tuiModel)
		if cmd == nil {
			t.Fatal("TUI did not submit help question")
		}
		batch, ok := cmd().(tea.BatchMsg)
		if !ok || len(batch) == 0 {
			t.Fatal("TUI question job absent")
		}
		message, ok := batch[0]().(answerMessage)
		if !ok {
			t.Fatal("TUI answer absent")
		}
		next, _ = model.Update(message)
		model = next.(tuiModel)
		row := &report.Rows[i]
		row.Visible = message.text
		if message.lastStatus == nil {
			row.Error = message.text
			save()
			t.Fatalf("question %d failed: see saved report", i)
		}
		if message.lastStatus == nil || message.lastStatus.Grounding == nil {
			save()
			t.Fatal("grounded answer missing")
		}
		row.Grounding = message.lastStatus.Grounding
		save()
		if q.Contains != "" && (row.Grounding.Status != "answered" || !strings.Contains(row.Grounding.Answer, q.Contains)) {
			t.Fatalf("question %d did not meet expected answer; see saved report", i)
		}
		if q.Contains == "" && row.Grounding.Status != "unknown" {
			t.Fatal("unsupported question was answered")
		}
	}
}
