package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
)

type localScenario struct{ ID, Difficulty, Prompt, Expected string }
type localAnswer struct {
	ID      string           `json:"id"`
	Prompt  string           `json:"prompt"`
	Answer  agent.Completion `json:"completion"`
	Visible string           `json:"visible"`
}
type localReport struct {
	Endpoint        string          `json:"endpoint"`
	Model           string          `json:"model"`
	ReasoningEffort string          `json:"reasoning_effort"`
	Started         string          `json:"started"`
	Answers         []localAnswer   `json:"answers"`
	Running         json.RawMessage `json:"running_models"`
}

// This opt-in integration uses the actual TUI, agent and local HTTP provider.
// It can never send requests to an external endpoint or follow a redirect.
func TestHomework26LocalLLM(t *testing.T) {
	if os.Getenv("HW26_LOCAL_TEST") != "1" {
		t.Skip("opt-in local Ollama integration")
	}
	output := os.Getenv("HW26_OUTPUT")
	if output == "" {
		t.Fatal("HW26_OUTPUT must name a fresh evidence directory")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	scenarioPath := filepath.Join("..", "..", "examples", "local-llm", "requests.json")
	if p := os.Getenv("HW26_REQUESTS"); p != "" {
		scenarioPath = p
	}
	data, err := os.ReadFile(scenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	var questions []localScenario
	if err = json.Unmarshal(data, &questions); err != nil || len(questions) != 3 {
		t.Fatal("three scenarios required", err)
	}
	c := defaultAppConfig()
	c.ActiveProfile = "ollama_local"
	c.Profiles = map[string]apiProfile{"ollama_local": {BaseURL: "http://127.0.0.1:11434/v1", Model: "batiai/gemma4-12b:iq3", ReasoningEffort: "none", ContextWindow: 4096, MaxOutputTokens: 512}}
	c.ResponseControl.Enabled = false
	c.Generation.Temperature = 0
	c.HistoryPolicy.Strategy = agent.MemoryFull
	no := false
	c.HistoryPolicy.AutoCompress = &no
	if err = c.normalizeAndValidateProfiles(); err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	provider := llm.New(httpClient, nil)
	report := &localReport{Endpoint: c.Profiles[c.ActiveProfile].BaseURL, Model: c.Generation.Model, ReasoningEffort: "none", Started: time.Now().UTC().Format(time.RFC3339)}
	save := func() {
		b, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(output, "answers.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	response, e := httpClient.Get("http://127.0.0.1:11434/api/ps")
	if e != nil {
		t.Fatal(e)
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		t.Fatal(response.Status)
	}
	if e = json.NewDecoder(response.Body).Decode(&report.Running); e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	save()

	ask := func(ctx context.Context, token, prompt string, s requestSettings) (completionResult, error) {
		if token != "" || s.BaseURL != report.Endpoint || s.Model != report.Model || s.ReasoningEffort != "none" || len(s.Tools) != 0 {
			return completionResult{}, fmt.Errorf("local request boundary violated")
		}
		return provider.Complete(ctx, agent.Target{BaseURL: s.BaseURL, Model: s.Model, ReasoningEffort: s.ReasoningEffort}, prompt, s)
	}
	m := newTUIModel(c, ask)
	m.ctx = context.Background()
	m.state.ToolsDisabled = true
	m.resize(110, 38)
	demo := localDemo{inner: m, questions: questions, report: report, save: save, record: os.Getenv("HW26_RECORD") == "1", signal: os.Getenv("HW26_START_SIGNAL")}
	if demo.record {
		final, e := tea.NewProgram(demo).Run()
		if e != nil {
			t.Fatal(e)
		}
		if f, ok := final.(localDemo); !ok || f.failed != "" {
			t.Fatalf("demo failed: %+v", f)
		}
	} else {
		for _, q := range questions {
			m.textarea.SetValue(q.Prompt)
			next, cmd := m.submit()
			m = next.(tuiModel)
			batch, ok := cmd().(tea.BatchMsg)
			if !ok || len(batch) == 0 {
				t.Fatal("no real TUI request")
			}
			a, ok := batch[0]().(answerMessage)
			if !ok {
				t.Fatal("no answer event")
			}
			if a.lastStatus == nil || a.lastStatus.Result.FinishReason != "stop" || strings.TrimSpace(a.lastStatus.Result.Content) == "" {
				t.Fatal("incomplete answer", a.text)
			}
			report.Answers = append(report.Answers, localAnswer{ID: q.ID, Prompt: q.Prompt, Answer: a.lastStatus.Result, Visible: a.text})
			save()
			next, _ = m.Update(a)
			m = next.(tuiModel)
		}
	}
	response, e = httpClient.Get("http://127.0.0.1:11434/api/ps")
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	if e = json.NewDecoder(response.Body).Decode(&report.Running); e != nil {
		t.Fatal(e)
	}
	save()
	if len(report.Answers) != 3 {
		t.Fatal("expected three successful responses")
	}
}

type localNext struct{}
type localPreview struct{}
type localBegin struct{}
type localEnd struct{}
type localDemo struct {
	banner    string
	before    func(int, *tuiModel)
	inner     tuiModel
	questions []localScenario
	report    *localReport
	save      func()
	record    bool
	signal    string
	index     int
	failed    string
	finished  bool
}

func (d localDemo) Init() tea.Cmd {
	return tea.Batch(d.inner.Init(), tea.Tick(time.Second, func(time.Time) tea.Msg { return localBegin{} }))
}
func (d localDemo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		d.inner.resize(v.Width, v.Height-6)
		return d, nil
	case localBegin:
		if d.signal != "" {
			if _, e := os.Stat(d.signal); e != nil {
				return d, tea.Tick(time.Second, func(time.Time) tea.Msg { return localBegin{} })
			}
		}
		return d, tea.Tick(9*time.Second, func(time.Time) tea.Msg { return localPreview{} })
	case localPreview:
		if d.index >= len(d.questions) {
			d.finished = true
			return d, tea.Tick(14*time.Second, func(time.Time) tea.Msg { return localEnd{} })
		}
		d.inner.history = nil
		if d.before != nil {
			d.before(d.index, &d.inner)
		}
		d.inner.refreshHistory()
		d.inner.textarea.SetValue(d.questions[d.index].Prompt)
		return d, tea.Tick(6*time.Second, func(time.Time) tea.Msg { return localNext{} })
	case localNext:
		next, cmd := d.inner.submit()
		d.inner = next.(tuiModel)
		return d, cmd
	case answerMessage:
		next, cmd := d.inner.Update(v)
		d.inner = next.(tuiModel)
		if v.lastStatus == nil || v.lastStatus.Result.FinishReason != "stop" || strings.TrimSpace(v.lastStatus.Result.Content) == "" {
			d.failed = "incomplete real answer: " + v.text
			return d, tea.Tick(4*time.Second, func(time.Time) tea.Msg { return localEnd{} })
		}
		q := d.questions[d.index]
		d.report.Answers = append(d.report.Answers, localAnswer{ID: q.ID, Prompt: q.Prompt, Answer: v.lastStatus.Result, Visible: v.text})
		d.save()
		d.index++
		return d, tea.Batch(cmd, tea.Tick(23*time.Second, func(time.Time) tea.Msg { return localPreview{} }))
	case localEnd:
		return d, tea.Quit
	}
	next, cmd := d.inner.Update(msg)
	d.inner = next.(tuiModel)
	return d, cmd
}
func (d localDemo) View() tea.View {
	heading := "ДЕНЬ 26 · ЛОКАЛЬНАЯ LLM · Ollama + Gemma 4 12B IQ3\nHTTP: 127.0.0.1:11434/v1 · thinking: none · облачных вызовов: 0\nНастоящий TUI; автоматический ввод. Временная оболочка показывает этапы."
	var ps struct {
		Models []struct {
			Name    string `json:"name"`
			Size    int64  `json:"size"`
			VRAM    int64  `json:"size_vram"`
			Context int    `json:"context_length"`
		} `json:"models"`
	}
	json.Unmarshal(d.report.Running, &ps)
	for _, model := range ps.Models {
		if model.Name == d.report.Model {
			heading += fmt.Sprintf("\n/api/ps: %s · GPU: %d / %d MB · runtime context: %d", model.Name, model.VRAM/1000000, model.Size/1000000, model.Context)
		}
	}
	if d.banner != "" {
		heading = d.banner + "\nHTTP: " + d.report.Endpoint + " · локальная модель: " + d.report.Model + "\nНастоящий TUI; автоматический ввод; временная оболочка этапов. Без облачных моделей.\nОбычный DeepSeek Client · профиль ollama_local · история хранится локально."
	}
	stage := "Ожидание начала записи"
	if d.inner.textarea.Value() != "" || d.inner.busy || d.index > 0 {
		i := d.index
		if d.inner.textarea.Value() == "" && !d.inner.busy && i > 0 {
			i--
		}
		if i >= len(d.questions) {
			i = len(d.questions) - 1
		}
		stage = d.questions[i].Difficulty
	}
	if d.finished {
		stage = fmt.Sprintf("Завершено: %d/3 настоящих локальных ответов. Результаты сохранены в answers.json", len(d.report.Answers))
	}
	if d.failed != "" {
		stage = "ОШИБКА: " + d.failed
	}
	view := d.inner.View()
	view.Content = heading + "\n" + stage + "\n\n" + view.Content
	if view.Cursor != nil {
		view.Cursor.Y += 6
	}
	return view
}

func TestReasoningEffortThroughTUI(t *testing.T) {
	c := defaultAppConfig()
	c.Profiles[c.ActiveProfile] = apiProfile{BaseURL: "http://localhost:11434/v1", Model: "local", ReasoningEffort: "none"}
	c.ResponseControl.Enabled = false
	if e := c.normalizeAndValidateProfiles(); e != nil {
		t.Fatal(e)
	}
	called := false
	m := newTUIModel(c, func(ctx context.Context, token, prompt string, s requestSettings) (completionResult, error) {
		called = true
		if s.ReasoningEffort != "none" {
			t.Errorf("reasoning_effort lost: %+v", s)
		}
		return completionResult{Content: "ok", FinishReason: "stop"}, nil
	})
	m.ctx = context.Background()
	m.textarea.SetValue("test")
	_, cmd := m.submit()
	batch := cmd().(tea.BatchMsg)
	batch[0]()
	if !called {
		t.Fatal("provider not called")
	}
}
