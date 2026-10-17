package main

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type hw30Step int
type hw30Checks struct {
	Lines  []string
	Passed bool
}
type hw30Demo struct {
	inner  tuiModel
	phase  int
	signal string
	output string
	lines  []string
	answer string
	failed string
}

func (d hw30Demo) Init() tea.Cmd {
	return tea.Batch(d.inner.Init(), tea.Tick(time.Second, func(time.Time) tea.Msg { return hw30Step(0) }))
}
func hw30Later(n int, seconds int) tea.Cmd {
	return tea.Tick(time.Duration(seconds)*time.Second, func(time.Time) tea.Msg { return hw30Step(n) })
}
func (d hw30Demo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		d.inner.resize(v.Width, v.Height-12)
		return d, nil
	case hw30Step:
		switch int(v) {
		case 0:
			if _, err := os.Stat(d.signal); err != nil {
				return d, hw30Later(0, 1)
			}
			d.phase = 1
			d.lines = []string{"1/4. Локальная модель на Ubuntu VPS; доступ по HTTP через SSH-ключ.", "Ollama и API работают как systemd-сервисы. Публичного порта модели нет."}
			return d, hw30Later(1, 7)
		case 1:
			d.inner.textarea.SetValue("Hello! Say one short greeting.")
			return d, hw30Later(2, 3)
		case 2:
			next, cmd := d.inner.submit()
			d.inner = next.(tuiModel)
			return d, cmd
		case 3:
			d.phase = 2
			d.lines = []string{"2/4. Стабильность: отправляем 2 запроса одновременно и ещё 2 последовательно.", "Генерации выполняются по очереди; ответы получаем от настоящей модели на VPS."}
			return d, func() tea.Msg { return hw30RunChecks() }
		case 4:
			d.phase = 3
			d.lines = append([]string{}, "3/4. Лимиты проверены реальными HTTP-запросами:", "Контекст 8192 > 512 -> HTTP 400; ответ 129 > 128 -> HTTP 400.", "Шестой запрос за минуту -> HTTP 429 (rate limit = 5/min).")
			return d, hw30Later(5, 12)
		case 5:
			d.phase = 4
			d.lines = []string{"4/4. Все требования проверены: VPS + HTTP API + чат + доступ по сети.", "Стабильность: 5 успешных запросов, включая 2 одновременно отправленных.", "Приватность: SSH; ограничения: 5/min, контекст 512, ответ до 128 токенов.", "Проверка короткая; длительный uptime и высокая нагрузка не заявляются."}
			return d, hw30Later(6, 10)
		case 6:
			return d, tea.Quit
		}
	case answerMessage:
		next, cmd := d.inner.Update(v)
		d.inner = next.(tuiModel)
		if v.lastStatus == nil || strings.TrimSpace(v.lastStatus.Result.Content) == "" {
			d.failed = "Chat request failed: " + v.text
			return d, hw30Later(6, 4)
		}
		d.answer = v.lastStatus.Result.Content
		d.lines = append(d.lines, "Чат: настоящий ответ получен с VPS. HTTP API доступен с Windows.")
		return d, tea.Batch(cmd, hw30Later(3, 8))
	case hw30Checks:
		d.lines = append(d.lines, v.Lines[:3]...)
		data, _ := json.MarshalIndent(map[string]any{"chat_answer": d.answer, "checks": v.Lines, "passed": v.Passed, "transport": "Windows SSH tunnel to VPS", "scripted_input": true, "real_TUI": true, "cloud_calls": 0}, "", "  ")
		_ = os.WriteFile(filepath.Join(d.output, "recording-results.json"), data, 0600)
		if !v.Passed {
			d.failed = "HTTP verification failed"
			return d, hw30Later(6, 8)
		}
		return d, hw30Later(4, 10)
	}
	next, cmd := d.inner.Update(msg)
	d.inner = next.(tuiModel)
	return d, cmd
}
func (d hw30Demo) View() tea.View {
	heading := "ДЕНЬ 30 | ПРИВАТНАЯ LLM НА VPS | smollm2:135m\nWindows -> SSH-туннель -> HTTP API -> Ollama на Ubuntu\nНастоящий DeepSeek Client. Автоввод; панель проверок делает прямые HTTP-запросы."
	stage := "Ожидание начала записи"
	if len(d.lines) > 0 {
		stage = strings.Join(d.lines, "\n")
	}
	if d.failed != "" {
		stage = "ОШИБКА: " + d.failed
	}
	for strings.Count(stage, "\n") < 5 {
		stage += "\n"
	}
	view := d.inner.View()
	view.Content = heading + "\n\n" + stage + "\n\n" + view.Content
	if view.Cursor != nil {
		view.Cursor.Y += 12
	}
	return view
}
func hw30HTTP(body string) (int, error) {
	c := &http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Post("http://127.0.0.1:11439/api/chat", "application/json", bytes.NewBufferString(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 {
		var answer struct{ Message struct{ Content string } }
		if json.Unmarshal(raw, &answer) != nil || strings.TrimSpace(answer.Message.Content) == "" {
			return 0, fmt.Errorf("empty model answer")
		}
	}
	return resp.StatusCode, nil
}
func hw30RunChecks() hw30Checks {
	result := hw30Checks{Passed: true}
	body := `{"messages":[{"role":"user","content":"Say hello briefly."}],"options":{"num_predict":16}}`
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) { defer wg.Done(); codes[i], _ = hw30HTTP(body) }(i)
	}
	wg.Wait()
	result.Lines = append(result.Lines, fmt.Sprintf("Одновременные запросы: HTTP %d и HTTP %d.", codes[0], codes[1]))
	if codes[0] != 200 || codes[1] != 200 {
		result.Passed = false
	}
	for i := 0; i < 2; i++ {
		code, err := hw30HTTP(body)
		result.Lines = append(result.Lines, fmt.Sprintf("Последовательный запрос %d: HTTP %d.", i+1, code))
		if code != 200 || err != nil {
			result.Passed = false
		}
	}
	for _, tc := range []struct {
		body string
		want int
		name string
	}{
		{`{"messages":[{"role":"user","content":"hi"}],"options":{"num_ctx":8192}}`, 400, "Контекст"},
		{`{"messages":[{"role":"user","content":"hi"}],"max_tokens":129}`, 400, "Ответ"},
		{body, 429, "Rate limit"},
	} {
		code, err := hw30HTTP(tc.body)
		result.Lines = append(result.Lines, fmt.Sprintf("%s: HTTP %d.", tc.name, code))
		if code != tc.want || err != nil {
			result.Passed = false
		}
	}
	return result
}
func TestHomework30PrivateServiceRecording(t *testing.T) {
	if os.Getenv("HW30_RECORD") != "1" {
		t.Skip("opt-in real VPS recording")
	}
	output := os.Getenv("HW30_OUTPUT")
	if output == "" {
		t.Fatal("fresh output directory required")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(filepath.Join("..", "..", "examples", "private-llm", "client.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := c.Profiles[c.ActiveProfile]
	p.BaseURL = "http://127.0.0.1:11439/v1"
	p.MaxOutputTokens = 32
	c.Profiles[c.ActiveProfile] = p
	if err = c.normalizeAndValidateProfiles(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	m := newTUIModel(c, askDeepSeek)
	m.ctx = ctx
	m.resize(128, 35)
	demo := hw30Demo{inner: m, signal: os.Getenv("HW30_SIGNAL"), output: output}
	final, err := tea.NewProgram(demo, tea.WithContext(ctx)).Run()
	if err != nil {
		t.Fatal(err)
	}
	d := final.(hw30Demo)
	if d.failed != "" {
		t.Fatal(d.failed)
	}
	if d.phase != 4 {
		t.Fatal("recording did not finish")
	}
	_ = os.WriteFile(filepath.Join(output, "done"), []byte("passed"), 0600)
}
