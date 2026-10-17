package main

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"github.com/charmbracelet/colorprofile"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in terminal rehearsal: public saved metrics only, never a model call.
func TestHomework29Render(t *testing.T) {
	if os.Getenv("HW29_RENDER_TEST") != "1" {
		t.Skip("visible terminal rehearsal")
	}
	c, e := loadConfig(filepath.Join("..", "..", "examples", "local-optimization", "q8.json"))
	if e != nil {
		t.Fatal(e)
	}
	text, e := os.ReadFile(filepath.Join("..", "..", "examples", "local-optimization", "video-summary.txt"))
	if e != nil {
		t.Fatal(e)
	}
	m := newTUIModel(c, func(context.Context, string, string, requestSettings) (completionResult, error) {
		t.Fatal("rehearsal must never call a model")
		return completionResult{}, nil
	})
	m.resize(124, 41)
	restoreConsole := enableHW29Console()
	defer restoreConsole()
	_, e = tea.NewProgram(hw29PaintCheck{hw29Demo: hw29Demo{inner: m}, text: string(text)}, tea.WithColorProfile(colorprofile.TrueColor), tea.WithoutRenderer(), tea.WithWindowSize(124, 48)).Run()
	if e != nil {
		t.Fatal(e)
	}
}

type hw29PaintCheck struct {
	hw29Demo
	text  string
	phase int
}
type hw29PaintTick struct{}

func paintTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return hw29PaintTick{} })
}
func (d hw29PaintCheck) Init() tea.Cmd {
	return tea.Batch(d.inner.Init(), paintTick(), hw29RepaintTick())
}
func (d hw29PaintCheck) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case hw29Repaint:
		hw29FullFrame(d.View(), d.inner.width, d.inner.height+8)
		return d, hw29RepaintTick()
	case tea.WindowSizeMsg:
		d.inner.resize(v.Width, v.Height-8)
		return d, nil
	case hw29PaintTick:
		d.phase++
		if d.phase == 20 {
			fmt.Print("\x1b[?1049l\x1b[?25h")
			return d, tea.Quit
		}
		d.inner.history = []string{fmt.Sprintf("ПРОВЕРКА ПЕРЕРИСОВКИ %d: запросов к модели НЕТ. Это сохранённая таблица.\n\n%s", d.phase, d.text)}
		d.inner.refreshHistory()
		d.inner.viewport.GotoTop()
		d.inner.textarea.SetValue(fmt.Sprintf("Тест Unicode и изменения экрана, фаза %d", d.phase))
		return d, paintTick()
	}
	next, cmd := d.inner.Update(msg)
	d.inner = next.(tuiModel)
	return d, cmd
}
