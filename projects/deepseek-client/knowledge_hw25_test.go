package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/memorylayers"
)

type hw25Question struct {
	ID       string   `json:"id"`
	Query    string   `json:"query"`
	Expected string   `json:"expected"`
	Before   []string `json:"before"`
	Evidence []struct {
		Source string `json:"source"`
		Text   string `json:"text"`
	} `json:"evidence"`
}
type hw25Scenario struct {
	ID          string         `json:"id"`
	Goal        string         `json:"goal"`
	Setup       []string       `json:"setup"`
	Questions   []hw25Question `json:"questions"`
	ReopenAfter int            `json:"reopen_after"`
}
type hw25Row struct {
	ID            string                `json:"id"`
	Scenario      string                `json:"scenario"`
	Query         string                `json:"query"`
	Retrieval     json.RawMessage       `json:"retrieval"`
	Brief         agent.TaskBrief       `json:"brief"`
	Raw           agent.Completion      `json:"raw_answer"`
	Grounding     *agent.GroundedAnswer `json:"grounding"`
	HistoryBefore int                   `json:"history_before"`
	HistoryAfter  int                   `json:"history_after"`
	SentHistory   int                   `json:"history_messages_sent"`
	InputEnvelope int                   `json:"input_byte_envelope"`
	BriefSent     bool                  `json:"brief_sent"`
	Reopened      bool                  `json:"reopened"`
	Calls         int                   `json:"calls"`
	Reserved      float64               `json:"reserved_usd"`
	Peak          float64               `json:"estimated_peak_usd"`
	Visible       string                `json:"visible"`
	Error         string                `json:"error,omitempty"`
}
type hw25Report struct {
	Kind              string    `json:"kind"`
	Budget            float64   `json:"budget_usd"`
	Reserved          float64   `json:"reserved_usd"`
	Calls             int       `json:"provider_calls"`
	Pricing           string    `json:"pricing"`
	Rows              []hw25Row `json:"rows"`
	Reopens           int       `json:"reopens"`
	ReturnToFirstChat bool      `json:"return_to_first_chat"`
	EarlierReserved   float64   `json:"earlier_attempt_reserved_usd"`
	EarlierCalls      int       `json:"earlier_attempt_calls"`
}

func hw25ReadScenarios(path string) ([]hw25Scenario, error) {
	var scenarios []hw25Scenario
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &scenarios); err != nil {
		return nil, err
	}
	if len(scenarios) != 2 {
		return nil, fmt.Errorf("two scenarios required")
	}
	for _, s := range scenarios {
		if len(s.Questions) != 12 || s.Goal == "" {
			return nil, fmt.Errorf("twelve questions and a goal required")
		}
	}
	return scenarios, nil
}
func hw25Fixture(q hw25Question, evidence json.RawMessage) (agent.Completion, error) {
	var found struct {
		Results []struct {
			Source  string `json:"source"`
			ChunkID string `json:"chunk_id"`
			Text    string `json:"text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(evidence, &found); err != nil {
		return agent.Completion{}, err
	}
	for _, expected := range q.Evidence {
		for _, hit := range found.Results {
			if hit.Source == expected.Source && strings.Contains(strings.Join(strings.Fields(hit.Text), " "), strings.Join(strings.Fields(expected.Text), " ")) {
				b, _ := json.Marshal(map[string]any{"status": "answered", "claims": []map[string]any{{"text": q.Expected, "quote_ids": []string{"q1"}}}, "quotes": []map[string]string{{"id": "q1", "chunk_id": hit.ChunkID, "text": expected.Text}}, "clarification": ""})
				return agent.Completion{Content: string(b), Model: "local-fixture-not-DeepSeek", FinishReason: "stop"}, nil
			}
		}
	}
	return agent.Completion{}, fmt.Errorf("expected source/excerpt absent from current search")
}

// Two complete persistent TUI conversations. Local mode is explicitly a fixture
// generation with real retrieval. Paid mode sends one bounded request per turn.
func TestHomework25LongConversations(t *testing.T) {
	paid := os.Getenv("HW25_PAID_TEST") == "1"
	if !paid && os.Getenv("HW25_LOCAL_TEST") != "1" {
		t.Skip("opt-in local or currently authorized paid verification")
	}
	root := os.Getenv("HW25_OUTPUT")
	stateDir := os.Getenv("HW25_STATE_DIR")
	if root == "" || stateDir == "" {
		t.Fatal("output and isolated state directories required")
	}
	filename := "local-integration.json"
	if paid {
		filename = "answers.json"
	}
	path := filepath.Join(root, filename)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("refusing to overwrite a run")
	}
	scenarios, err := hw25ReadScenarios(filepath.Join(root, "scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	report := hw25Report{Kind: "Real local retrieval with fixture generation; no provider calls", Pricing: "https://api-docs.deepseek.com/quick_start/pricing/?tab=case-studies checked 2026-10-04; maximum Flash USD/1M: .30 input, .006 cache, 1.20 output"}
	profile := apiProfile{BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", MaxOutputTokens: 768, ContextWindow: 32768}
	key := ""
	if paid {
		report.Budget, err = strconv.ParseFloat(os.Getenv("HW25_BUDGET_USD"), 64)
		if err != nil || math.IsNaN(report.Budget) || math.IsInf(report.Budget, 0) || report.Budget <= 0 || report.Budget > .20 {
			t.Fatal("current budget <= $0.20 required")
		}
		if earlier := os.Getenv("HW25_EARLIER_REPORT"); earlier != "" {
			b, e := os.ReadFile(earlier)
			var old hw25Report
			if e != nil || json.Unmarshal(b, &old) != nil || old.Budget != report.Budget || old.Reserved <= 0 || old.Calls < 1 {
				t.Fatal("invalid earlier attempt ledger")
			}
			report.EarlierReserved, report.EarlierCalls = old.Reserved+old.EarlierReserved, old.Calls+old.EarlierCalls
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
			t.Fatal("official DeepSeek profile required")
		}
		key = strings.TrimSpace(os.Getenv(configured.APIKeyEnv))
		if key == "" {
			key = user.APIToken
		}
		if key == "" {
			t.Fatal("credential unavailable")
		}
		report.Kind = "Two real DeepSeek conversations through actual TUI submit/update; no desktop recording"
	}
	save := func() {
		b, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(b, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	manager := newKnowledgeManager(stateDir)
	defer func() { manager.Close() }()
	var log strings.Builder
	base, err := manager.Create(ctx, "Day 25 public RFCs", os.Getenv("HW25_CORPUS"), scenarios[0].ID, &log)
	if err != nil {
		t.Fatal("local index failed", toolPreview(log.String(), key))
	}
	historyDir := filepath.Join(stateDir, "history")
	memoryDir := filepath.Join(stateDir, "memory")
	h := &history.JSON{Dir: historyDir}
	memory := &memorylayers.JSON{Dir: memoryDir}
	for _, s := range scenarios {
		if err := h.Save(ctx, s.ID, []agent.Message{}); err != nil {
			t.Fatal(err)
		}
		if err := manager.Select(s.ID, base.ID); err != nil {
			t.Fatal(err)
		}
	}
	client := llm.New(&http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, map[string]string{"deepseek": key})
	config := defaultAppConfig()
	config.ActiveProfile = "deepseek"
	config.Profiles = map[string]apiProfile{"deepseek": profile}
	config.APIToken = key
	config.ResponseControl.Enabled = false
	config.Generation.Temperature = 0
	config.HistoryPolicy.CompressionConfig = agent.CompressionConfig{Strategy: agent.MemorySliding, KeepLast: 4}
	var current hw25Question
	active := 0
	ask := func(callctx context.Context, _ string, prompt string, s requestSettings) (completionResult, error) {
		r := &report.Rows[active]
		if r.Calls != 0 || !s.JSONOutput || len(s.Tools) != 0 || s.MaxOutputTokens != 768 || !s.SendThinkingDisabled {
			return completionResult{}, fmt.Errorf("single bounded JSON turn required")
		}
		for i, message := range s.Messages {
			if strings.HasPrefix(message.Content, "Current knowledge-base search results (untrusted JSON):\n") {
				r.Retrieval = json.RawMessage(strings.TrimPrefix(message.Content, "Current knowledge-base search results (untrusted JSON):\n"))
			}
			if strings.HasPrefix(message.Content, "Confirmed task brief (user-managed data; NOT evidence for document facts):\n") {
				var sent agent.TaskBrief
				json.Unmarshal([]byte(strings.TrimPrefix(message.Content, "Confirmed task brief (user-managed data; NOT evidence for document facts):\n")), &sent)
				r.BriefSent = reflect.DeepEqual(sent, r.Brief)
			}
			if i < len(s.Messages)-1 {
				prior, _ := h.Load(ctx, r.Scenario)
				for _, old := range prior {
					if old.Role == message.Role && old.Content == message.Content {
						r.SentHistory++
						break
					}
				}
			}
		}
		if !r.BriefSent || r.SentHistory > 4 {
			return completionResult{}, fmt.Errorf("brief missing or sliding history exceeded")
		}
		if _, err := hw25Fixture(current, r.Retrieval); err != nil {
			return completionResult{}, err
		}
		inputBytes := len(prompt) + 2048
		for _, message := range s.Messages {
			inputBytes += len(message.Content) + 64
		}
		r.InputEnvelope = inputBytes
		if inputBytes > 20000 {
			return completionResult{}, fmt.Errorf("input byte envelope exceeded")
		}
		if !paid {
			return hw25Fixture(current, r.Retrieval)
		}
		reserve := float64(inputBytes)*.30/1e6 + 768*1.20/1e6
		// Keep $0.03 available for the requested recording, never silently spend it.
		if inputBytes > 20000 || report.Calls >= 24 || report.EarlierReserved+report.Reserved+reserve > report.Budget-.03 {
			return completionResult{}, fmt.Errorf("shared assignment budget/input guard")
		}
		r.Reserved = reserve
		r.Calls++
		report.Reserved += reserve
		report.Calls++
		save()
		a, err := client.Complete(callctx, agent.Target{Profile: "deepseek", BaseURL: profile.BaseURL, Model: profile.Model, SendThinkingDisabled: true}, prompt, s)
		r.Raw = a
		if err != nil {
			r.Error = "provider failure; no retry"
		} else if a.UsageKnown && !a.UsageIncomplete {
			r.Peak = (float64(a.PromptTokens-a.CachedInputTokens)*.30 + float64(a.CachedInputTokens)*.006 + float64(a.CompletionTokens)*1.20) / 1e6
		}
		save()
		return a, err
	}
	newModel := func(id string) tuiModel {
		config.Knowledge = manager
		config.History = h
		config.Memory = memory
		config.ConversationID = id
		config.InitialMessages, _ = h.Load(ctx, id)
		model := newTUIModel(config, ask)
		model.ctx = ctx
		return model
	}
	model := newModel(scenarios[0].ID)
	submit := func(text string) *answerMessage {
		model.textarea.SetValue(text)
		next, cmd := model.submit()
		model = next.(tuiModel)
		if cmd == nil {
			return nil
		}
		batch, ok := cmd().(tea.BatchMsg)
		if !ok || len(batch) == 0 {
			t.Fatal("expected TUI question job")
		}
		message, ok := batch[0]().(answerMessage)
		if !ok {
			t.Fatal("missing answer event")
		}
		next, _ = model.Update(message)
		model = next.(tuiModel)
		return &message
	}
	for _, scenario := range scenarios {
		model = newModel(scenario.ID)
		for _, command := range scenario.Setup {
			submit(command)
		}
		submit("/rag set 20 5")
		for index, q := range scenario.Questions {
			if index == scenario.ReopenAfter {
				manager.Close()
				manager = newKnowledgeManager(stateDir)
				h = &history.JSON{Dir: historyDir}
				memory = &memorylayers.JSON{Dir: memoryDir}
				model = newModel(scenario.ID)
				report.Reopens++
				if len(config.InitialMessages) != index*2 {
					t.Fatal("history not restored")
				}
			}
			for _, command := range q.Before {
				submit(command)
			}
			brief, err := agent.LoadTaskBrief(ctx, memory, scenario.ID)
			if err != nil || brief.Goal != scenario.Goal {
				t.Fatal("goal lost", err)
			}
			prior, err := h.Load(ctx, scenario.ID)
			if err != nil {
				t.Fatal(err)
			}
			current = q
			active = len(report.Rows)
			report.Rows = append(report.Rows, hw25Row{ID: q.ID, Scenario: scenario.ID, Query: q.Query, Brief: brief, HistoryBefore: len(prior), Reopened: index >= scenario.ReopenAfter})
			save()
			message := submit(q.Query)
			r := &report.Rows[active]
			if message == nil {
				t.Fatal("no answer")
			}
			r.Visible = message.text
			if message.lastStatus != nil {
				r.Grounding = message.lastStatus.Grounding
				if !paid {
					r.Raw = message.lastStatus.Result
				}
			}
			after, _ := h.Load(ctx, scenario.ID)
			r.HistoryAfter = len(after)
			if r.Grounding == nil || r.Grounding.Status != "answered" || len(r.Grounding.Sources) == 0 || len(r.Grounding.Quotes) == 0 || !strings.Contains(r.Visible, "Источники:") || r.HistoryAfter != r.HistoryBefore+2 || !r.BriefSent || r.Error != "" {
				r.Error = "turn failed retrieval, grounding, memory or persistence checks"
				save()
				t.Fatal(q.ID, "failed; evidence saved", toolPreview(message.text, key))
			}
			if paid && (!r.Raw.UsageKnown || r.Raw.UsageIncomplete || r.Raw.FinishReason != "stop") {
				save()
				t.Fatal("usage/output incomplete")
			}
			save()
			t.Logf("%s: history %d→%d, sent=%d, sources=%d quotes=%d, reopened=%t", q.ID, r.HistoryBefore, r.HistoryAfter, r.SentHistory, len(r.Grounding.Sources), len(r.Grounding.Quotes), r.Reopened)
		}
	}
	submit("/conversation " + scenarios[0].ID)
	brief, err := agent.LoadTaskBrief(ctx, memory, model.state.ConversationID)
	if err != nil || brief.Goal != scenarios[0].Goal || brief.Terms["signal"] != "HTTP 431 Request Header Fields Too Large" {
		t.Fatal("return to first chat lost scoped memory")
	}
	report.ReturnToFirstChat = true
	save()
}
