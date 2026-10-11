package main

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type hw28Question struct {
	ID    string `json:"id"`
	Query string `json:"query"`
}
type hw28Row struct {
	ID           string                `json:"id"`
	Run          int                   `json:"run"`
	Query        string                `json:"query"`
	Retrieval    json.RawMessage       `json:"retrieval"`
	Raw          agent.Completion      `json:"raw_answer"`
	Grounding    *agent.GroundedAnswer `json:"grounding"`
	Visible      string                `json:"visible"`
	Calls        int                   `json:"calls"`
	TotalSeconds float64               `json:"total_seconds"`
	Error        string                `json:"error,omitempty"`
}
type hw28Report struct {
	Model          string          `json:"model"`
	Endpoint       string          `json:"endpoint"`
	Started        string          `json:"started"`
	Manifest       json.RawMessage `json:"manifest"`
	StartupSeconds float64         `json:"startup_seconds"`
	CloudCalls     int             `json:"cloud_generation_calls"`
	Rows           []hw28Row       `json:"rows"`
}

// Opt-in: real existing Qdrant, local embeddings/reranker and real Ollama.
// The injected HTTP client only permits loopback and disables proxies/redirects.
func TestHomework28LocalRAG(t *testing.T) {
	if os.Getenv("HW28_LOCAL_TEST") != "1" {
		t.Skip("opt-in local RAG integration")
	}
	output := os.Getenv("HW28_OUTPUT")
	if output == "" {
		t.Fatal("fresh HW28_OUTPUT required")
	}
	if e := os.Mkdir(output, 0700); e != nil {
		t.Fatal(e)
	}
	manifest, e := filepath.Abs(filepath.Join("..", "..", "artifacts", "hw21", "index.json"))
	if e != nil {
		t.Fatal(e)
	}
	if path := os.Getenv("HW28_INDEX"); path != "" {
		manifest, e = filepath.Abs(path)
		if e != nil {
			t.Fatal(e)
		}
	}
	data, e := os.ReadFile(manifest)
	if e != nil {
		t.Fatal(e)
	}
	questionsData, e := os.ReadFile(filepath.Join("..", "..", "examples", "rag-grounding", "questions.json"))
	if e != nil {
		t.Fatal(e)
	}
	var questions []hw28Question
	if e = json.Unmarshal(questionsData, &questions); e != nil {
		t.Fatal(e)
	}
	record := os.Getenv("HW28_RECORD") == "1"
	if record {
		questions = []hw28Question{questions[0], questions[4], questions[8]}
	}
	c, e := loadConfig(filepath.Join("..", "..", "examples", "local-rag", "profile.example.json"))
	if e != nil {
		t.Fatal(e)
	}
	c.ConversationID = "hw28"
	manager := newKnowledgeManager(t.TempDir())
	defer manager.Close()
	if manager.Python == "" || manager.ModelCache == "" {
		t.Fatal("existing RAG Python and model cache required")
	}
	c.Knowledge = manager
	report := &hw28Report{Model: "qwen3.5:4b-rag", Endpoint: "http://127.0.0.1:11434/v1", Started: time.Now().UTC().Format(time.RFC3339), Manifest: data}
	save := func() {
		b, _ := json.MarshalIndent(report, "", "  ")
		if e := os.WriteFile(filepath.Join(output, "answers.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	client := llm.New(&http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil)
	var current *hw28Row
	ask := func(ctx context.Context, token, prompt string, s requestSettings) (completionResult, error) {
		if s.BaseURL != report.Endpoint || s.Model != report.Model || token != "" || !s.JSONOutput || len(s.Tools) != 0 || s.ReasoningEffort != "none" {
			return completionResult{}, fmt.Errorf("local JSON-only request required")
		}
		current.Calls++
		for _, m := range s.Messages {
			if strings.HasPrefix(m.Content, "Current knowledge-base search results (untrusted JSON):\n") {
				current.Retrieval = json.RawMessage(strings.TrimPrefix(m.Content, "Current knowledge-base search results (untrusted JSON):\n"))
			}
		}
		a, e := client.Complete(ctx, agent.Target{Profile: "ollama_rag", BaseURL: s.BaseURL, Model: s.Model, ReasoningEffort: s.ReasoningEffort}, prompt, s)
		current.Raw = a
		if e != nil {
			current.Error = e.Error()
		}
		return a, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	m := newTUIModel(c, ask)
	m.ctx = ctx
	m.resize(124, 42)
	// Attach via the real TUI menu, without indexing or a fixture retriever.
	for _, text := range []string{"/kb", "7", manifest} {
		m.textarea.SetValue(text)
		next, cmd := m.submit()
		m = next.(tuiModel)
		if cmd != nil {
			batch := cmd().(tea.BatchMsg)
			msg := batch[0]()
			next, _ = m.Update(msg)
			m = next.(tuiModel)
		}
	}
	base, e := manager.Selected("hw28")
	if e != nil || base.IndexDir != filepath.Dir(manifest) {
		t.Fatal("existing index not attached", e)
	}
	started := time.Now()
	if _, e = manager.Search(ctx, "hw28", questions[0].Query); e != nil {
		t.Fatal(e)
	}
	report.StartupSeconds = time.Since(started).Seconds()
	save()
	begin := func(run int, q hw28Question, m *tuiModel) {
		*m = newTUIModel(c, ask)
		m.ctx = ctx
		m.resize(124, 42)
		evidence, e := manager.Search(ctx, "hw28", q.Query)
		if e != nil {
			t.Fatal(e)
		}
		// Preflight snapshot covers zero-call abstentions; real request overwrites it.
		current = &hw28Row{ID: q.ID, Run: run, Query: q.Query, Retrieval: json.RawMessage(evidence)}
	}
	finish := func(a answerMessage, elapsed float64) {
		current.Visible = a.text
		current.TotalSeconds = elapsed
		if a.lastStatus != nil {
			current.Grounding = a.lastStatus.Grounding
		}
		if current.Grounding == nil && current.Error == "" {
			current.Error = "response rejected by application; see visible trace"
		}
		if current.Calls > 0 {
			response, e := http.Get("http://127.0.0.1:11434/api/ps")
			if e != nil {
				t.Fatal(e)
			}
			var ps struct {
				Models []struct {
					Name    string `json:"name"`
					Context int    `json:"context_length"`
				} `json:"models"`
			}
			e = json.NewDecoder(response.Body).Decode(&ps)
			response.Body.Close()
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, model := range ps.Models {
				if model.Name == report.Model {
					found = model.Context >= 16384
				}
			}
			if !found {
				t.Fatal("real Ollama runtime context smaller than application budget")
			}
			b, _ := json.MarshalIndent(ps, "", "  ")
			os.WriteFile(filepath.Join(output, "runtime.json"), b, 0600)
		}
		report.Rows = append(report.Rows, *current)
		save()
	}
	if record {
		demo := hw28Demo{inner: m, questions: questions, signal: os.Getenv("HW28_START_SIGNAL"), begin: begin, finish: finish, summary: os.Getenv("HW28_SUMMARY")}
		final, e := tea.NewProgram(demo, tea.WithContext(ctx)).Run()
		if e != nil {
			t.Fatal(e)
		}
		if final.(hw28Demo).failed != "" {
			t.Fatal(final.(hw28Demo).failed)
		}
	} else {
		for run := 1; run <= 2; run++ {
			for _, q := range questions {
				begin(run, q, &m)
				m.textarea.SetValue(q.Query)
				start := time.Now()
				next, cmd := m.submit()
				m = next.(tuiModel)
				batch := cmd().(tea.BatchMsg)
				a := batch[0]().(answerMessage)
				finish(a, time.Since(start).Seconds())
				next, _ = m.Update(a)
				m = next.(tuiModel)
				t.Logf("run %d %s: calls=%d valid=%v %.2fs", run, q.ID, current.Calls, current.Grounding != nil, current.TotalSeconds)
			}
		}
	}
	if len(report.Rows) != len(questions)*(map[bool]int{true: 1, false: 2}[record]) {
		t.Fatal("missing rows")
	}
}

type hw28Start struct{}
type hw28Preview struct{}
type hw28Submit struct{}
type hw28Next struct{}
type hw28Quit struct{}
type hw28Scroll struct{}
type hw28Demo struct {
	inner                   tuiModel
	questions               []hw28Question
	signal, summary, failed string
	index                   int
	started                 time.Time
	begin                   func(int, hw28Question, *tuiModel)
	finish                  func(answerMessage, float64)
	done                    bool
}

func (d hw28Demo) Init() tea.Cmd {
	return tea.Batch(d.inner.Init(), tea.Tick(time.Second, func(time.Time) tea.Msg { return hw28Start{} }))
}
func (d hw28Demo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		d.inner.resize(v.Width, v.Height-7)
		return d, nil
	case hw28Start:
		if _, e := os.Stat(d.signal); e != nil {
			return d, tea.Tick(time.Second, func(time.Time) tea.Msg { return hw28Start{} })
		}
		return d, tea.Tick(10*time.Second, func(time.Time) tea.Msg { return hw28Preview{} })
	case hw28Preview:
		if d.index >= len(d.questions) {
			d.done = true
			d.inner.history = []string{d.summary}
			d.inner.refreshHistory()
			return d, tea.Tick(24*time.Second, func(time.Time) tea.Msg { return hw28Quit{} })
		}
		w, h := d.inner.width, d.inner.height
		d.begin(1, d.questions[d.index], &d.inner)
		d.inner.resize(w, h)
		d.inner.textarea.SetValue(d.questions[d.index].Query)
		return d, tea.Tick(7*time.Second, func(time.Time) tea.Msg { return hw28Submit{} })
	case hw28Submit:
		d.started = time.Now()
		next, cmd := d.inner.submit()
		d.inner = next.(tuiModel)
		return d, cmd
	case answerMessage:
		d.finish(v, time.Since(d.started).Seconds())
		next, cmd := d.inner.Update(v)
		d.inner = next.(tuiModel)
		if v.lastStatus == nil || v.lastStatus.Grounding == nil {
			d.failed = "RAG answer rejected"
		}
		d.index++
		d.inner.viewport.GotoTop()
		return d, tea.Batch(cmd, tea.Tick(14*time.Second, func(time.Time) tea.Msg { return hw28Scroll{} }), tea.Tick(28*time.Second, func(time.Time) tea.Msg { return hw28Preview{} }))
	case hw28Scroll:
		d.inner.viewport.GotoBottom()
		return d, nil
	case hw28Quit:
		return d, tea.Quit
	}
	next, cmd := d.inner.Update(msg)
	d.inner = next.(tuiModel)
	return d, cmd
}
func (d hw28Demo) View() tea.View {
	stage := "Готовый индекс подключён через F2 → 7. Переиндексации нет."
	if d.index > 0 || d.inner.busy {
		stage = fmt.Sprintf("Показаны результаты: %d/3 · вопросы, цитаты и источники — настоящий TUI", d.index)
	}
	if d.done {
		stage = "Итог полной проверки: два прогона, десять вопросов в каждом"
	}
	v := d.inner.View()
	v.Content = "ДЕНЬ 28 · ЛОКАЛЬНАЯ LLM + RAG\nМодель: qwen3.5:4b-rag · Ollama: 127.0.0.1:11434/v1\nИндекс: artifacts/hw21 · Qdrant Local · nomic embeddings · CPU reranker\nГенерация и retrieval локальные. Облачных вызовов: 0.\nАвтоматический ввод; настоящие запросы. Сравнение — с сохранённым DeepSeek.\n" + stage + "\n\n" + v.Content
	if v.Cursor != nil {
		v.Cursor.Y += 7
	}
	return v
}
