package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestNormalizePastedText(t *testing.T) {
	input := "first\n\nsecond&#xA0;line"
	want := "first\n\nsecond line"
	if got := normalizePastedText(input); got != want {
		t.Fatalf("normalizePastedText() = %q, want %q", got, want)
	}
}

func TestTUIPastePreservesMultipleLines(t *testing.T) {
	config := defaultAppConfig()
	model := newTUIModel(config, nil)
	updated, _ := model.Update(tea.PasteMsg{Content: "first\n\nsecond"})
	got := updated.(tuiModel).textarea.Value()
	if got != "first\n\nsecond" {
		t.Fatalf("pasted value = %q", got)
	}
}

func TestTUIHistoryHeightDoesNotMoveInput(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.resize(100, 50)
	initialHeight := model.viewport.Height()
	model.history = append(model.history, strings.Repeat("message\n", 50))
	model.refreshHistory()
	if model.viewport.Height() != initialHeight {
		t.Fatalf("viewport height changed from %d to %d", initialHeight, model.viewport.Height())
	}
	if initialHeight != historyViewportHeight {
		t.Fatalf("viewport height = %d, want %d", initialHeight, historyViewportHeight)
	}
}

func TestTUIShowsHorizontalDialogAndComposerPanels(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.resize(120, 35)
	view := model.View()
	if !strings.Contains(view.Content, "Диалог") || !strings.Contains(view.Content, "Ввод и настройки") ||
		!strings.Contains(view.Content, "model=deepseek-v4-flash") || !strings.Contains(view.Content, "temperature=0.7") {
		t.Fatalf("layout does not contain horizontal panels: %q", view.Content)
	}
	if got := lipgloss.Height(view.Content); got > 35 {
		t.Fatalf("view height = %d, exceeds terminal height 35", got)
	}
}

func TestMouseWheelScrollsOnlyInsideDialog(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.resize(120, 35)
	model.history = []string{strings.Repeat("line\n", 40)}
	model.refreshHistory()
	initialOffset := model.viewport.YOffset()
	if initialOffset == 0 {
		t.Fatal("test requires scrollable history")
	}

	inside := tea.MouseWheelMsg(tea.Mouse{X: 5, Y: 5, Button: tea.MouseWheelUp})
	updated, _ := model.Update(inside)
	model = updated.(tuiModel)
	if model.viewport.YOffset() >= initialOffset {
		t.Fatalf("wheel inside dialog did not scroll up: before=%d after=%d", initialOffset, model.viewport.YOffset())
	}

	offsetAfterInsideScroll := model.viewport.YOffset()
	outside := tea.MouseWheelMsg(tea.Mouse{
		X:      5,
		Y:      model.viewport.Height() + 2,
		Button: tea.MouseWheelUp,
	})
	updated, _ = model.Update(outside)
	model = updated.(tuiModel)
	if model.viewport.YOffset() != offsetAfterInsideScroll {
		t.Fatalf("wheel outside dialog changed offset: before=%d after=%d",
			offsetAfterInsideScroll, model.viewport.YOffset())
	}
}

func TestTUIAutocompleteCompletesCommandAndValue(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.textarea.SetValue("/tem")

	updated, _ := model.Update(keyPress(tea.KeyTab))
	model = updated.(tuiModel)
	if got := model.textarea.Value(); got != "/temperature " {
		t.Fatalf("command completion = %q, want /temperature ", got)
	}

	updated, _ = model.Update(keyPress(tea.KeyTab))
	model = updated.(tuiModel)
	if got := model.textarea.Value(); got != "/temperature 0" {
		t.Fatalf("value completion = %q, want /temperature 0", got)
	}
}

func TestTUIAutocompleteSelectionUsesArrowKeys(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.textarea.SetValue("/mode ")

	updated, _ := model.Update(keyPress(tea.KeyDown))
	model = updated.(tuiModel)
	updated, _ = model.Update(keyPress(tea.KeyTab))
	model = updated.(tuiModel)

	if got := model.textarea.Value(); got != "/mode controlled" {
		t.Fatalf("selected completion = %q, want /mode controlled", got)
	}
}

func TestTUIAutocompleteIncludesBenchmarkMode(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.textarea.SetValue("/mode t")

	updated, _ := model.Update(keyPress(tea.KeyTab))
	model = updated.(tuiModel)
	if got := model.textarea.Value(); got != "/mode temperature_benchmark" {
		t.Fatalf("benchmark completion = %q, want /mode temperature_benchmark", got)
	}
}

func TestTUIAutocompleteIncludesReload(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.textarea.SetValue("/rel")

	updated, _ := model.Update(keyPress(tea.KeyTab))
	model = updated.(tuiModel)
	if got := model.textarea.Value(); got != "/reload" {
		t.Fatalf("reload completion = %q, want /reload", got)
	}
}

func TestTUIEnterExecutesExactCompletedCommand(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.textarea.SetValue("/mode free")

	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = updated.(tuiModel)

	if model.state.Mode != modeFree {
		t.Fatalf("mode = %q, want free", model.state.Mode)
	}
	if model.textarea.Value() != "" {
		t.Fatalf("textarea was not cleared: %q", model.textarea.Value())
	}
}

func TestTUIAutocompleteAppearsInStableHelpLine(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.textarea.SetValue("/str")

	help := model.helpLine()
	if !strings.Contains(help, "/strategy ") || !strings.Contains(help, "Tab/Enter") {
		t.Fatalf("autocomplete help is incomplete: %q", help)
	}
}

func keyPress(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}
