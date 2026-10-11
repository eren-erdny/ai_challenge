package main

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type hw29Question struct {
	ID    string `json:"id"`
	Query string `json:"query"`
}
type hw29Row struct {
	Model        string                `json:"model"`
	Variant      string                `json:"variant"`
	Runtime      json.RawMessage       `json:"runtime,omitempty"`
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
type hw29Report struct {
	Model          string          `json:"model"`
	Endpoint       string          `json:"endpoint"`
	Started        string          `json:"started"`
	Manifest       json.RawMessage `json:"manifest"`
	StartupSeconds float64         `json:"startup_seconds"`
	CloudCalls     int             `json:"cloud_generation_calls"`
	Rows           []hw29Row       `json:"rows"`
}

// Opt-in: real existing Qdrant, local embeddings/reranker and real Ollama.
// The injected HTTP client only permits loopback and disables proxies/redirects.
func TestHomework29Optimization(t *testing.T) {
	if os.Getenv("HW29_LOCAL_TEST") != "1" {
		t.Skip("opt-in local RAG integration")
	}
	output := os.Getenv("HW29_OUTPUT")
	if output == "" {
		t.Fatal("fresh HW29_OUTPUT required")
	}
	if e := os.Mkdir(output, 0700); e != nil {
		t.Fatal(e)
	}
	manifest, e := filepath.Abs(filepath.Join("..", "..", "artifacts", "hw21", "index.json"))
	if e != nil {
		t.Fatal(e)
	}
	if path := os.Getenv("HW29_INDEX"); path != "" {
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
	var questions []hw29Question
	if e = json.Unmarshal(questionsData, &questions); e != nil {
		t.Fatal(e)
	}
	record := os.Getenv("HW29_RECORD") == "1"
	if record {
		questions = []hw29Question{questions[2], questions[2], questions[6], questions[8]}
	}
	initialVariant := os.Getenv("HW29_VARIANT")
	if record {
		initialVariant = "before"
	}
	c, e := loadConfig(filepath.Join("..", "..", "examples", "local-optimization", initialVariant+".json"))
	if e != nil {
		t.Fatal(e)
	}
	c.ConversationID = "hw29"
	manager := newKnowledgeManager(t.TempDir())
	defer manager.Close()
	if manager.Python == "" || manager.ModelCache == "" {
		t.Fatal("existing RAG Python and model cache required")
	}
	c.Knowledge = manager
	profile := c.Profiles[c.ActiveProfile]
	report := &hw29Report{Model: profile.Model, Endpoint: "http://127.0.0.1:11434/v1", Started: time.Now().UTC().Format(time.RFC3339), Manifest: data}
	if record {
		report.Model = "mixed: before + " + os.Getenv("HW29_VARIANT")
	}
	save := func() {
		b, _ := json.MarshalIndent(report, "", "  ")
		if e := os.WriteFile(filepath.Join(output, "answers.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	client := llm.New(&http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil)
	var current *hw29Row
	ask := func(ctx context.Context, token, prompt string, s requestSettings) (completionResult, error) {
		if s.BaseURL != report.Endpoint || s.Model != profile.Model || token != "" || !s.JSONOutput || len(s.Tools) != 0 || s.ReasoningEffort != "none" {
			return completionResult{}, fmt.Errorf("local JSON-only request required")
		}
		current.Calls++
		b, _ := json.MarshalIndent(s, "", "  ")
		os.WriteFile(filepath.Join(output, fmt.Sprintf("request-%s-%d-%s.json", current.Variant, current.Run, current.ID)), b, 0600)
		for _, m := range s.Messages {
			if strings.HasPrefix(m.Content, "Current knowledge-base search results (untrusted JSON):\n") {
				current.Retrieval = json.RawMessage(strings.TrimPrefix(m.Content, "Current knowledge-base search results (untrusted JSON):\n"))
			}
		}
		a, e := client.Complete(ctx, agent.Target{Profile: "ollama_optimized", BaseURL: s.BaseURL, Model: s.Model, ReasoningEffort: s.ReasoningEffort}, prompt, s)
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
	base, e := manager.Selected("hw29")
	if e != nil || base.IndexDir != filepath.Dir(manifest) {
		t.Fatal("existing index not attached", e)
	}
	started := time.Now()
	if _, e = manager.Search(ctx, "hw29", questions[0].Query); e != nil {
		t.Fatal(e)
	}
	report.StartupSeconds = time.Since(started).Seconds()
	save()
	recordIndex := 0
	begin := func(run int, q hw29Question, m *tuiModel) {
		variant := os.Getenv("HW29_VARIANT")
		if record {
			if recordIndex == 0 {
				variant = "before"
			}
			recordIndex++
			c, e = loadConfig(filepath.Join("..", "..", "examples", "local-optimization", variant+".json"))
			if e != nil {
				t.Fatal(e)
			}
			c.ConversationID = "hw29"
			c.Knowledge = manager
			profile = c.Profiles[c.ActiveProfile]
		}
		*m = newTUIModel(c, ask)
		m.ctx = ctx
		m.resize(124, 42)
		evidence, e := manager.Search(ctx, "hw29", q.Query)
		if e != nil {
			t.Fatal(e)
		}
		// Preflight snapshot covers zero-call abstentions; real request overwrites it.
		current = &hw29Row{Model: profile.Model, Variant: variant, ID: q.ID, Run: run, Query: q.Query, Retrieval: json.RawMessage(evidence)}
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
			var fullPS json.RawMessage
			decoder := json.NewDecoder(response.Body)
			if e = decoder.Decode(&fullPS); e != nil {
				t.Fatal(e)
			}
			current.Runtime = fullPS
			var ps struct {
				Models []struct {
					Name    string `json:"name"`
					Context int    `json:"context_length"`
				} `json:"models"`
			}
			e = json.Unmarshal(fullPS, &ps)
			response.Body.Close()
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, model := range ps.Models {
				if model.Name == profile.Model {
					found = model.Context == profile.ContextWindow
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
		demo := hw29Demo{inner: m, questions: questions, signal: os.Getenv("HW29_START_SIGNAL"), begin: begin, finish: finish, summary: os.Getenv("HW29_SUMMARY")}
		restoreConsole := enableHW29Console()
		defer restoreConsole()
		final, e := tea.NewProgram(demo, tea.WithContext(ctx), tea.WithColorProfile(colorprofile.TrueColor), tea.WithoutRenderer(), tea.WithWindowSize(124, 48)).Run()
		if e != nil {
			t.Fatal(e)
		}
		if final.(hw29Demo).failed != "" {
			t.Fatal(final.(hw29Demo).failed)
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

type hw29Start struct{}
type hw29Preview struct{}
type hw29Submit struct{}
type hw29Next struct{}
type hw29Quit struct{}
type hw29Scroll struct{}
type hw29Repaint struct{}
type hw29Demo struct {
	inner                   tuiModel
	questions               []hw29Question
	signal, summary, failed string
	index                   int
	started                 time.Time
	begin                   func(int, hw29Question, *tuiModel)
	finish                  func(answerMessage, float64)
	done                    bool
}

func (d hw29Demo) Init() tea.Cmd {
	return tea.Batch(d.inner.Init(), tea.Tick(time.Second, func(time.Time) tea.Msg { return hw29Start{} }), hw29RepaintTick())
}
func hw29RepaintTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return hw29Repaint{} })
}

func (d hw29Demo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case hw29Repaint:
		hw29FullFrame(d.View(), d.inner.width, d.inner.height+8)
		return d, hw29RepaintTick()
	case tea.WindowSizeMsg:
		d.inner.resize(v.Width, v.Height-8)
		return d, nil
	case hw29Start:
		if _, e := os.Stat(d.signal); e != nil {
			return d, tea.Tick(time.Second, func(time.Time) tea.Msg { return hw29Start{} })
		}
		return d, tea.Tick(8*time.Second, func(time.Time) tea.Msg { return hw29Preview{} })
	case hw29Preview:
		if d.index >= len(d.questions) {
			d.done = true
			d.inner.history = []string{d.summary}
			d.inner.refreshHistory()
			return d, tea.Tick(24*time.Second, func(time.Time) tea.Msg { return hw29Quit{} })
		}
		w, h := d.inner.width, d.inner.height
		d.begin(1, d.questions[d.index], &d.inner)
		d.inner.resize(w, h)
		d.inner.textarea.SetValue(d.questions[d.index].Query)
		return d, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return hw29Submit{} })
	case hw29Submit:
		d.started = time.Now()
		next, cmd := d.inner.submit()
		d.inner = next.(tuiModel)
		return d, cmd
	case answerMessage:
		d.finish(v, time.Since(d.started).Seconds())
		next, cmd := d.inner.Update(v)
		d.inner = next.(tuiModel)
		// Rejected answers are retained and displayed as real comparison evidence.
		d.index++
		d.inner.viewport.GotoTop()
		return d, tea.Batch(cmd, tea.Tick(9*time.Second, func(time.Time) tea.Msg { return hw29Scroll{} }), tea.Tick(18*time.Second, func(time.Time) tea.Msg { return hw29Preview{} }))
	case hw29Scroll:
		d.inner.viewport.GotoBottom()
		return d, nil
	case hw29Quit:
		fmt.Print("\x1b[?1049l\x1b[?25h")
		return d, tea.Quit
	}
	next, cmd := d.inner.Update(msg)
	d.inner = next.(tuiModel)
	return d, cmd
}
func (d hw29Demo) View() tea.View {
	stage := "Готовый индекс подключён через F2 → 7. Переиндексации нет."
	if d.index > 0 || d.inner.busy {
		stage = fmt.Sprintf("Показаны результаты: %d/4 · вопросы, цитаты и источники — настоящий TUI", d.index)
	}
	if d.done {
		stage = "Сохранённое сравнение до/после: одинаковые вопросы и индекс"
	}
	v := d.inner.View()
	v.Content = "ДЕНЬ 29 · ОПТИМИЗАЦИЯ ЛОКАЛЬНОЙ LLM\n" + fmt.Sprintf("Модель: %s · ctx=%d · max=%d · T=%.1f\n", d.inner.state.Model, d.inner.state.API.ContextWindow, d.inner.state.API.MaxOutputTokens, d.inner.state.Temperature) + "Индекс: artifacts/hw21 · Qdrant Local · nomic embeddings · CPU reranker\nГенерация и retrieval локальные. Облачных вызовов: 0.\nАвтоввод; полный вывод TUI для OBS; настоящие запросы. Q4 / Q8 — локально.\n" + stage + "\n\n" + v.Content
	if v.Cursor != nil {
		v.Cursor.Y += 7
	}
	v.Content = lipgloss.NewStyle().Width(d.inner.width).Render(ansi.Strip(v.Content))
	return v
}

// Recording-only painter: the real BubbleTea event loop and ordinary TUI View
// remain in use. Printing the complete View avoids native-console dirty-region
// capture artifacts. No response or application state is substituted.
func hw29FullFrame(view tea.View, width, height int) {
	if width < 1 || height < 1 {
		return
	}
	lines := strings.Split("\n"+ansi.Strip(view.Content), "\n")
	var b strings.Builder
	b.WriteString("\x1b[?25l\x1b[H")
	for i := 0; i < height; i++ {
		if i > 0 {
			b.WriteString("\r\n")
		}
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], width-1, "")
		}
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", width-1-ansi.StringWidth(line)))
	}
	fmt.Print(b.String())
}
