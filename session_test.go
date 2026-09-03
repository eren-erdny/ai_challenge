package main

import (
	"bufio"
	"fmt"
	"strings"
	"testing"
)

func TestInteractiveSessionSwitchesModesAndFormats(t *testing.T) {
	input := bufio.NewReader(strings.NewReader(strings.Join([]string{
		"/mode free",
		"Первый вопрос",
		"/mode controlled",
		"/format json",
		"Второй вопрос",
		"/mode compare",
		"Третий вопрос",
		"/settings",
		"/exit",
		"",
	}, "\n")))
	config := defaultAppConfig()
	config.APIToken = "test-token"

	type call struct {
		Prompt  string
		Control *responseControl
	}
	var calls []call
	fakeAsk := func(_ string, prompt string, control *responseControl) (completionResult, error) {
		calls = append(calls, call{Prompt: prompt, Control: control})
		content := "Свободный ответ"
		if control != nil && control.Format == "json" {
			content = `{"summary":"Ответ","points":["Один","Два"]}`
		}
		return completionResult{Content: content, CompletionTokens: 20, FinishReason: "stop"}, nil
	}

	var output strings.Builder
	var errorOutput strings.Builder
	exitCode := runInteractiveSession(input, &output, &errorOutput, config, fakeAsk)

	if exitCode != 0 {
		t.Fatalf("runInteractiveSession() exit code = %d, stderr = %q", exitCode, errorOutput.String())
	}
	if len(calls) != 4 {
		t.Fatalf("API calls = %d, want 4: %#v", len(calls), calls)
	}
	if calls[0].Prompt != "Первый вопрос" || calls[0].Control != nil {
		t.Fatalf("free call = %#v", calls[0])
	}
	if calls[1].Prompt != "Второй вопрос" || calls[1].Control == nil || calls[1].Control.Format != "json" {
		t.Fatalf("controlled call = %#v", calls[1])
	}
	if calls[2].Prompt != "Третий вопрос" || calls[2].Control != nil {
		t.Fatalf("compare free call = %#v", calls[2])
	}
	if calls[3].Prompt != "Третий вопрос" || calls[3].Control == nil || calls[3].Control.Format != "json" {
		t.Fatalf("compare controlled call = %#v", calls[3])
	}

	for _, expected := range []string{
		"Режим изменён: free",
		"Режим изменён: controlled",
		"Формат изменён: json",
		"ЛОКАЛЬНЫЙ СУДЬЯ",
		"СРАВНЕНИЕ",
		"Сеанс завершён",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output.String())
		}
	}
}

func TestInteractiveSessionContinuesAfterAPIError(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("Первый вопрос\nВторой вопрос\n/exit\n"))
	config := defaultAppConfig()
	config.APIToken = "test-token"
	calls := 0
	fakeAsk := func(_ string, _ string, _ *responseControl) (completionResult, error) {
		calls++
		if calls == 1 {
			return completionResult{}, fmt.Errorf("temporary error")
		}
		return completionResult{
			Content:          "## Краткий ответ\n\nОтвет.\n\n## Ключевые пункты\n\n- Один\n- Два\n- Три",
			CompletionTokens: 20,
			FinishReason:     "stop",
		}, nil
	}

	var output strings.Builder
	var errorOutput strings.Builder
	exitCode := runInteractiveSession(input, &output, &errorOutput, config, fakeAsk)

	if exitCode != 0 || calls != 2 {
		t.Fatalf("exit code = %d, calls = %d", exitCode, calls)
	}
	if !strings.Contains(errorOutput.String(), "temporary error") {
		t.Fatalf("stderr does not contain API error: %q", errorOutput.String())
	}
	if !strings.Contains(output.String(), "ОТВЕТ С ОГРАНИЧЕНИЯМИ") {
		t.Fatalf("second answer was not printed: %q", output.String())
	}
}
