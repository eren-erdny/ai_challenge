package main

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestTUIReceivesAgentResultAndCancelsOnExit(t *testing.T) {
	config := defaultAppConfig()
	config.ResponseControl.Enabled = false
	model := newTUIModel(config, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model.ctx, model.cancel = ctx, cancel
	command := executeQuestionCommand(ctx, "test-token", "question", model.state,
		func(_ context.Context, token, prompt string, settings requestSettings) (completionResult, error) {
			if token != "test-token" || prompt != "question" || len(settings.Messages) != 1 {
				t.Fatal("request was not prepared by agent")
			}
			return completionResult{Content: "answer", CompletionTokens: 7}, nil
		})
	updated, _ := model.Update(command())
	model = updated.(tuiModel)
	if model.state.LastRequest == nil || model.state.LastRequest.Result.CompletionTokens != 7 || !strings.Contains(strings.Join(model.history, "\n"), "answer") {
		t.Fatal("agent response did not reach TUI and status")
	}
	_, _ = model.Update(keyPress(tea.KeyEscape))
	if ctx.Err() != context.Canceled {
		t.Fatal("exit did not cancel active work")
	}
}

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

func TestTUIAutocompleteIncludesModelBenchmarkMode(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.textarea.SetValue("/mode m")

	updated, _ := model.Update(keyPress(tea.KeyTab))
	model = updated.(tuiModel)
	if got := model.textarea.Value(); got != "/mode model_benchmark" {
		t.Fatalf("model benchmark completion = %q, want /mode model_benchmark", got)
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

func TestTUIAutocompleteIncludesStatus(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.textarea.SetValue("/sta")

	updated, _ := model.Update(keyPress(tea.KeyTab))
	model = updated.(tuiModel)
	if got := model.textarea.Value(); got != "/status" {
		t.Fatalf("status completion = %q, want /status", got)
	}
}

func TestTUICompactSettingsHideAPIEndpoint(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	settings := model.compactSettings("готов")
	if strings.Contains(settings, "api=") || strings.Contains(settings, "api.deepseek.com") {
		t.Fatalf("compact settings expose API endpoint: %q", settings)
	}
}

func TestTUICompactSettingsShowDisabledResponseControl(t *testing.T) {
	config := defaultAppConfig()
	config.ResponseControl.Enabled = false
	model := newTUIModel(config, nil)
	settings := model.compactSettings("готов")
	if !strings.Contains(settings, "format=off") {
		t.Fatalf("disabled response control is not visible: %q", settings)
	}
}

func TestTUIActivityIndicatorAnimatesWhileBusy(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.busy = true
	model.activityGen = 7
	before := model.activityText()

	updated, command := model.Update(activityTickMessage{generation: 7})
	model = updated.(tuiModel)
	after := model.activityText()

	if before == after || !strings.Contains(after, "обрабатываю запрос.") {
		t.Fatalf("activity did not animate: before=%q after=%q", before, after)
	}
	if command == nil {
		t.Fatal("busy activity must schedule the next frame")
	}
}

func TestTUIActivityIndicatorIgnoresStaleTicks(t *testing.T) {
	model := newTUIModel(defaultAppConfig(), nil)
	model.busy = true
	model.activityGen = 2

	updated, command := model.Update(activityTickMessage{generation: 1})
	model = updated.(tuiModel)
	if model.activityFrame != 0 || command != nil {
		t.Fatalf("stale tick changed activity: frame=%d command=%v", model.activityFrame, command)
	}
}

func TestTUIAutocompleteIncludesProfiles(t *testing.T) {
	config := defaultAppConfig()
	config.Profiles["local"] = apiProfile{BaseURL: "http://127.0.0.1:1234/v1", Model: "local-model"}
	model := newTUIModel(config, nil)
	model.textarea.SetValue("/profile l")

	updated, _ := model.Update(keyPress(tea.KeyTab))
	model = updated.(tuiModel)
	if got := model.textarea.Value(); got != "/profile local" {
		t.Fatalf("profile completion = %q, want /profile local", got)
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
