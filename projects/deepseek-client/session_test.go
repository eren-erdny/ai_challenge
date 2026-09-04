package main

import (
	"bufio"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInteractiveSessionSwitchesModesAndFormats(t *testing.T) {
	input := bufio.NewReader(strings.NewReader(strings.Join([]string{
		"/mode free",
		"Первый вопрос",
		"/mode controlled",
		"/format json",
		"/strategy experts",
		"/temperature 1.2",
		"/models",
		"/model deepseek-v4-pro",
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
		Prompt   string
		Settings requestSettings
	}
	var calls []call
	fakeAsk := func(_ string, prompt string, settings requestSettings) (completionResult, error) {
		calls = append(calls, call{Prompt: prompt, Settings: settings})
		content := "Свободный ответ"
		if settings.Control != nil && settings.Control.Format == "json" {
			content = `{"summary":"Ответ","points":["Один","Два"]}`
		}
		return completionResult{Content: content, Model: settings.Model, CompletionTokens: 20, Duration: time.Second, FinishReason: "stop"}, nil
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
	if calls[0].Prompt != "Первый вопрос" || calls[0].Settings.Control != nil {
		t.Fatalf("free call = %#v", calls[0])
	}
	if calls[1].Prompt != "Второй вопрос" || calls[1].Settings.Control == nil || calls[1].Settings.Control.Format != "json" {
		t.Fatalf("controlled call = %#v", calls[1])
	}
	if calls[1].Settings.Temperature != 1.2 || calls[1].Settings.Strategy != strategyExperts {
		t.Fatalf("generation settings were not applied: %#v", calls[1].Settings)
	}
	if calls[1].Settings.Model != "deepseek-v4-pro" {
		t.Fatalf("selected model was not applied: %#v", calls[1].Settings)
	}
	if calls[2].Prompt != "Третий вопрос" || calls[2].Settings.Control != nil {
		t.Fatalf("compare free call = %#v", calls[2])
	}
	if calls[3].Prompt != "Третий вопрос" || calls[3].Settings.Control == nil || calls[3].Settings.Control.Format != "json" {
		t.Fatalf("compare controlled call = %#v", calls[3])
	}

	for _, expected := range []string{
		"Режим изменён: free",
		"Режим изменён: controlled",
		"Формат изменён: json",
		"Стратегия изменена: experts",
		"Temperature изменена: 1.2",
		"Модель изменена: deepseek-v4-pro",
		"Проверка ограничений: OK",
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
	fakeAsk := func(_ string, _ string, _ requestSettings) (completionResult, error) {
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
	if !strings.Contains(output.String(), "Метрики:") {
		t.Fatalf("second answer was not printed: %q", output.String())
	}
}

func TestTemperatureBenchmarkUsesOnlyRequestedTemperatures(t *testing.T) {
	config := defaultAppConfig()
	state := sessionState{
		Mode:        modeTemperatureBenchmark,
		Model:       config.Generation.Model,
		Temperature: config.Generation.Temperature,
		Strategy:    config.Generation.Strategy,
		Control:     config.ResponseControl,
	}

	var calls []requestSettings
	fakeAsk := func(_ string, prompt string, settings requestSettings) (completionResult, error) {
		calls = append(calls, settings)
		if settings.Model != defaultModelName || settings.Strategy != strategyStandard {
			t.Fatalf("benchmark changed non-temperature settings: %#v", settings)
		}
		if len(calls) <= 3 {
			if prompt != "Один и тот же запрос" || settings.Control == nil ||
				settings.Control.Format != "temperature_benchmark_answer" || settings.Control.MaxTokens != 300 ||
				settings.Control.MaxWords != 120 || len(settings.Control.Stop) != 0 {
				t.Fatalf("compact benchmark call: prompt=%q settings=%#v", prompt, settings)
			}
		}
		if len(calls) == 4 {
			if settings.Control == nil || !strings.Contains(prompt, "ИСХОДНЫЙ ЗАПРОС") ||
				!strings.Contains(prompt, "ОТВЕТ ПРИ TEMPERATURE=2") {
				t.Fatalf("analysis call is incomplete: prompt=%q settings=%#v", prompt, settings)
			}
		}
		return completionResult{
			Content:          fmt.Sprintf("Ответ или анализ для %.1f с разными словами", settings.Temperature),
			Model:            settings.Model,
			CompletionTokens: 30,
			Duration:         2 * time.Second,
			FinishReason:     "stop",
		}, nil
	}

	var output strings.Builder
	var errorOutput strings.Builder
	executeQuestion("test-token", "Один и тот же запрос", state, &output, &errorOutput, fakeAsk)

	if errorOutput.Len() != 0 {
		t.Fatalf("benchmark stderr = %q", errorOutput.String())
	}
	if len(calls) != 4 {
		t.Fatalf("API calls = %d, want 4", len(calls))
	}
	if got := []float64{calls[0].Temperature, calls[1].Temperature, calls[2].Temperature}; fmt.Sprint(got) != "[0 1.2 2]" {
		t.Fatalf("benchmark temperatures = %v, want [0 1.2 2]", got)
	}
	if calls[3].Temperature != 0 || calls[3].Control.Format != "benchmark_analysis" ||
		calls[3].Control.MaxTokens != 500 || calls[3].Control.MaxWords != 250 {
		t.Fatalf("analysis settings = %#v", calls[3])
	}
	for _, expected := range []string{"TEMPERATURE = 0", "TEMPERATURE = 1.2", "TEMPERATURE = 2", "СВОДКА БЕНЧМАРКА", "АНАЛИЗ МОДЕЛИ", "15.0 ток/с"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("benchmark output does not contain %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "Проверка ограничений") {
		t.Fatalf("benchmark answers unexpectedly use response control:\n%s", output.String())
	}
}

func TestLexicalDiversity(t *testing.T) {
	if got := lexicalDiversity("Go go Rust"); got != 2.0/3.0 {
		t.Fatalf("lexicalDiversity() = %g, want %g", got, 2.0/3.0)
	}
}

func TestLegacyBenchmarkModeUsesCanonicalName(t *testing.T) {
	for _, alias := range []string{"benchmark", "benchmark_temperature"} {
		state := sessionState{Mode: modeControlled}
		var output strings.Builder
		handleSessionCommand("/mode "+alias, &state, &output)

		if state.Mode != modeTemperatureBenchmark {
			t.Fatalf("alias %q produced mode %q, want %q", alias, state.Mode, modeTemperatureBenchmark)
		}
		if !strings.Contains(output.String(), "Режим изменён: temperature_benchmark") {
			t.Fatalf("canonical mode name is missing for alias %q: %q", alias, output.String())
		}
	}
}

func TestReloadSessionConfigAppliesSettingsWithoutExposingToken(t *testing.T) {
	configRoot := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", configRoot)
	} else {
		t.Setenv("XDG_CONFIG_HOME", configRoot)
	}
	t.Setenv("DEEPSEEK_API_KEY", "")

	configPath, err := defaultConfigPath()
	if err != nil {
		t.Fatalf("defaultConfigPath() returned error: %v", err)
	}
	config := defaultAppConfig()
	config.APIToken = ""
	config.Generation.Model = "deepseek-v4-pro"
	config.Generation.Temperature = 1.2
	config.Generation.Strategy = strategyExperts
	config.ResponseControl.Enabled = false
	if err := saveConfig(configPath, config); err != nil {
		t.Fatalf("saveConfig() returned error: %v", err)
	}

	state := sessionState{Mode: modeTemperatureBenchmark, Model: defaultModelName}
	token, loadedPath, err := reloadSessionConfig("current-token", &state)
	if err != nil {
		t.Fatalf("reloadSessionConfig() returned error: %v", err)
	}
	if loadedPath != configPath || token != "current-token" {
		t.Fatalf("reload result path=%q token=%q", loadedPath, token)
	}
	if state.Mode != modeFree || state.Model != "deepseek-v4-pro" || state.Temperature != 1.2 || state.Strategy != strategyExperts {
		t.Fatalf("reloaded state = %#v", state)
	}
}
