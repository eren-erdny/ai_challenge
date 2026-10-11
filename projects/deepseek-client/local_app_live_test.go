package main

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHomework27LocalApplication(t *testing.T) {
	if os.Getenv("HW27_LOCAL_TEST") != "1" {
		t.Skip("opt-in local application integration")
	}
	output := os.Getenv("HW27_OUTPUT")
	if output == "" {
		t.Fatal("fresh HW27_OUTPUT required")
	}
	if e := os.Mkdir(output, 0700); e != nil {
		t.Fatal(e)
	}
	source := os.Getenv("HW27_REQUESTS")
	if source == "" {
		source = filepath.Join("..", "..", "examples", "local-app", "requests.json")
	}
	raw, e := os.ReadFile(source)
	if e != nil {
		t.Fatal(e)
	}
	var questions []localScenario
	if e = json.Unmarshal(raw, &questions); e != nil || len(questions) != 3 {
		t.Fatal("three scenarios required", e)
	}
	stateDir := t.TempDir()
	c, e := homework27ProfileConfig(stateDir)
	if e != nil {
		t.Fatal(e)
	}
	ask := askDeepSeek
	report := &localReport{Endpoint: "http://127.0.0.1:11434/v1", Model: "batiai/gemma4-12b:iq3", ReasoningEffort: "none", Started: time.Now().UTC().Format(time.RFC3339)}
	save := func() {
		b, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(output, "answers.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	meta := map[string]any{"production_entrypoint": "ordinary client with ollama_local profile", "cloud_generation_calls": 0, "history_restored_messages": 0}
	saveMeta := func() {
		b, _ := json.MarshalIndent(meta, "", "  ")
		if e := os.WriteFile(filepath.Join(output, "application.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	snapshot := func() {
		response, e := http.Get("http://127.0.0.1:11434/api/ps")
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		if e = json.NewDecoder(response.Body).Decode(&report.Running); e != nil {
			t.Fatal(e)
		}
		save()
	}
	snapshot()
	saveMeta()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	m := newTUIModel(c, ask)
	m.ctx = ctx
	m.resize(124, 42)
	m.textarea.SetValue("/profile ollama_local")
	next, _ := m.submit()
	m = next.(tuiModel)
	before := func(i int, m *tuiModel) {
		if i != 2 {
			return
		}
		reopened, e := homework27ProfileConfig(stateDir)
		if e != nil {
			t.Fatal(e)
		}
		if len(reopened.InitialMessages) != 4 {
			t.Fatal("four messages not persisted")
		}
		meta["history_restored_messages"] = len(reopened.InitialMessages)
		saveMeta()
		w, h := m.width, m.height
		*m = newTUIModel(reopened, ask)
		m.ctx = ctx
		m.resize(w, h)
		m.history = []string{"Повторно открыт JSON store: восстановлены 4 сообщения. Контекст третьего запроса читается с диска."}
		m.refreshHistory()
	}
	if os.Getenv("HW27_RECORD") == "1" {
		demo := localDemo{inner: m, questions: questions, report: report, save: save, signal: os.Getenv("HW27_START_SIGNAL"), banner: "ДЕНЬ 27 · DEEPSEEK CLIENT → OLLAMA · профиль ollama_local", before: before}
		final, e := tea.NewProgram(demo, tea.WithContext(ctx)).Run()
		if e != nil {
			t.Fatal(e)
		}
		f, ok := final.(localDemo)
		if !ok || f.failed != "" {
			t.Fatal("recording failed", f.failed)
		}
	} else {
		for i, q := range questions {
			before(i, &m)
			m.textarea.SetValue(q.Prompt)
			next, cmd := m.submit()
			m = next.(tuiModel)
			batch := cmd().(tea.BatchMsg)
			a := batch[0]().(answerMessage)
			if a.lastStatus == nil || a.lastStatus.Result.FinishReason != "stop" || strings.TrimSpace(a.lastStatus.Result.Content) == "" {
				t.Fatal("incomplete actual answer", a.text)
			}
			report.Answers = append(report.Answers, localAnswer{ID: q.ID, Prompt: q.Prompt, Answer: a.lastStatus.Result, Visible: a.text})
			save()
			next, _ = m.Update(a)
			m = next.(tuiModel)
		}
	}
	snapshot()
	if len(report.Answers) != 3 {
		t.Fatal("three actual responses required")
	}
	messages, e := c.History.Load(ctx, c.ConversationID)
	if e != nil || len(messages) != 6 {
		t.Fatal("six persisted messages required", e)
	}
	b, _ := json.MarshalIndent(messages, "", "  ")
	if e = os.WriteFile(filepath.Join(output, "history.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	meta["final_history_messages"] = len(messages)
	saveMeta()
}

// Same configuration loader, TUI, persistence and completion function as the app.
func homework27ProfileConfig(stateDir string) (appConfig, error) {
	c, e := loadConfig(filepath.Join("..", "..", "examples", "local-llm", "profile.example.json"))
	if e != nil {
		return c, e
	}
	store := &history.JSON{Dir: filepath.Join(stateDir, "conversations")}
	c.History = store
	c.ConversationID, c.InitialMessages, e = store.Active(context.Background())
	return c, e
}
