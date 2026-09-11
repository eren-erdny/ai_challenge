package main

import (
	"bufio"
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPartialBenchmarkIsRenderedBeforeError(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "test-ollama-key")
	config := defaultAppConfig()
	profile, _ := config.activeAPIProfile()
	state := sessionState{Mode: modeModelBenchmark, ActiveProfile: config.ActiveProfile, Profiles: config.Profiles, API: profile, Model: profile.Model, Strategy: strategyStandard}
	calls := 0
	var output, errorOutput strings.Builder
	status := executeQuestion(context.Background(), "test-secret", "question", state, &output, &errorOutput,
		func(context.Context, string, string, requestSettings) (completionResult, error) {
			calls++
			if calls == 2 {
				return completionResult{}, fmt.Errorf("failed with test-secret")
			}
			return completionResult{Content: "preserved first answer"}, nil
		})
	if status == nil || !strings.Contains(output.String(), "preserved first answer") || !strings.Contains(errorOutput.String(), "failed") || strings.Contains(errorOutput.String(), "test-secret") {
		t.Fatalf("partial result or sanitized error missing: output=%q err=%q", output.String(), errorOutput.String())
	}
}

func TestInteractiveSessionSwitchesModesAndFormats(t *testing.T) {
	input := bufio.NewReader(strings.NewReader(strings.Join([]string{
		"/mode free",
		"Первый вопрос",
		"/mode controlled",
		"/format json",
		"/strategy experts",
		"/temperature 1.2",
		"/profiles",
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
	fakeAsk := func(_ context.Context, _ string, prompt string, settings requestSettings) (completionResult, error) {
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
	fakeAsk := func(_ context.Context, _ string, _ string, _ requestSettings) (completionResult, error) {
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
	if !strings.Contains(output.String(), "Краткий ответ") {
		t.Fatalf("second answer was not printed: %q", output.String())
	}
}

func TestTemperatureBenchmarkUsesOnlyRequestedTemperatures(t *testing.T) {
	config := defaultAppConfig()
	profile, _ := config.activeAPIProfile()
	state := sessionState{
		Mode:          modeTemperatureBenchmark,
		ActiveProfile: config.ActiveProfile,
		Profiles:      config.Profiles,
		API:           profile,
		Model:         config.Generation.Model,
		Temperature:   config.Generation.Temperature,
		Strategy:      config.Generation.Strategy,
		Control:       config.ResponseControl,
	}

	var calls []requestSettings
	fakeAsk := func(_ context.Context, _ string, prompt string, settings requestSettings) (completionResult, error) {
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
	executeQuestion(context.Background(), "test-token", "Один и тот же запрос", state, &output, &errorOutput, fakeAsk)

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

func TestModelBenchmarkComparesThreeModelsAndRunsJudge(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "ollama-token")
	config := defaultAppConfig()
	profile, _ := config.activeAPIProfile()
	state := sessionState{
		Mode: modeModelBenchmark, ActiveProfile: config.ActiveProfile, Profiles: config.Profiles,
		API: profile, APIToken: "deepseek-token", Model: profile.Model,
		Temperature: 0.7, Strategy: strategyStandard, Control: config.ResponseControl,
	}

	type call struct {
		Token    string
		Prompt   string
		Settings requestSettings
	}
	var calls []call
	fakeAsk := func(_ context.Context, token string, prompt string, settings requestSettings) (completionResult, error) {
		calls = append(calls, call{Token: token, Prompt: prompt, Settings: settings})
		return completionResult{
			Content: fmt.Sprintf("Ответ %s", settings.Model), Model: settings.Model,
			PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30,
			Duration: time.Second, FinishReason: "stop",
		}, nil
	}

	var output strings.Builder
	var errorOutput strings.Builder
	status := executeQuestion(context.Background(), "deepseek-token", "Одинаковая задача", state, &output, &errorOutput, fakeAsk)

	if errorOutput.Len() != 0 {
		t.Fatalf("model benchmark stderr = %q", errorOutput.String())
	}
	if len(calls) != 4 {
		t.Fatalf("API calls = %d, want 4", len(calls))
	}
	wantModels := []string{"deepseek-v4-flash", "deepseek-v4-pro", "gpt-oss:120b", "deepseek-v4-pro"}
	for index, want := range wantModels {
		if calls[index].Settings.Model != want {
			t.Fatalf("call %d model = %q, want %q", index, calls[index].Settings.Model, want)
		}
	}
	for index := 0; index < 3; index++ {
		if calls[index].Prompt != "Одинаковая задача" {
			t.Fatalf("call %d prompt = %q", index, calls[index].Prompt)
		}
		if calls[index].Settings.Control == nil || calls[index].Settings.Control.Format != defaultFormatName ||
			calls[index].Settings.Temperature != 0.7 || calls[index].Settings.Strategy != strategyStandard {
			t.Fatalf("call %d did not inherit session settings: %#v", index, calls[index].Settings)
		}
	}
	if calls[0].Token != "deepseek-token" || calls[1].Token != "deepseek-token" || calls[2].Token != "ollama-token" {
		t.Fatalf("provider tokens were not selected correctly: %#v", calls)
	}
	if calls[3].Settings.Temperature != 0 || !strings.Contains(calls[3].Prompt, "МОДЕЛЬ gpt-oss:120b") {
		t.Fatalf("judge call is incomplete: %#v", calls[3])
	}
	if status == nil || status.Result.Model != "deepseek-v4-pro" {
		t.Fatalf("last request status = %#v", status)
	}
	for _, expected := range []string{"deepseek-v4-flash:", "deepseek-v4-pro:", "gpt-oss:120b:", "СВОДКА ПО МОДЕЛЯМ", "ИТОГОВЫЙ АНАЛИЗ"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("benchmark output does not contain %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "Одинаковая задача") || strings.Contains(output.String(), "ЗАПРОС") {
		t.Fatalf("benchmark must not repeat the prompt:\n%s", output.String())
	}
}

func TestModelBenchmarkRunsModelRequestsWithoutControlsAfterFreeMode(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "ollama-token")
	config := defaultAppConfig()
	profile, _ := config.activeAPIProfile()
	state := sessionState{
		Mode: modeControlled, ActiveProfile: config.ActiveProfile, Profiles: config.Profiles,
		API: profile, APIToken: "deepseek-token", Model: profile.Model,
		Temperature: 1.2, Strategy: strategyExperts, Control: config.ResponseControl,
	}
	var commandOutput strings.Builder
	handleSessionCommand("/mode free", &state, &commandOutput)
	handleSessionCommand("/mode model_benchmark", &state, &commandOutput)

	var calls []requestSettings
	fakeAsk := func(_ context.Context, _ string, _ string, settings requestSettings) (completionResult, error) {
		calls = append(calls, settings)
		return completionResult{Content: "Ответ", Model: settings.Model}, nil
	}
	var output strings.Builder
	var errorOutput strings.Builder
	executeQuestion(context.Background(), "deepseek-token", "Задача", state, &output, &errorOutput, fakeAsk)

	if errorOutput.Len() != 0 || len(calls) != 4 {
		t.Fatalf("stderr=%q calls=%d", errorOutput.String(), len(calls))
	}
	for index := 0; index < 3; index++ {
		if calls[index].Control != nil || calls[index].Temperature != 1.2 || calls[index].Strategy != strategyExperts {
			t.Fatalf("free benchmark call %d settings = %#v", index, calls[index])
		}
	}
	if calls[3].Control == nil {
		t.Fatal("judge must keep its analysis instruction")
	}
}

func TestModelBenchmarkRequiresOllamaToken(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "")
	config := defaultAppConfig()
	profile, _ := config.activeAPIProfile()
	state := sessionState{
		Mode: modeModelBenchmark, ActiveProfile: config.ActiveProfile,
		Profiles: config.Profiles, API: profile, APIToken: "deepseek-token",
	}
	var output strings.Builder
	var errorOutput strings.Builder
	executeQuestion(context.Background(), "deepseek-token", "Задача", state, &output, &errorOutput, func(context.Context, string, string, requestSettings) (completionResult, error) {
		t.Fatal("API must not be called without all required tokens")
		return completionResult{}, nil
	})
	if !strings.Contains(errorOutput.String(), "OLLAMA_API_KEY") {
		t.Fatalf("missing token error = %q", errorOutput.String())
	}
}

func TestLexicalDiversity(t *testing.T) {
	if got := lexicalDiversity("Go go Rust"); got != 2.0/3.0 {
		t.Fatalf("lexicalDiversity() = %g, want %g", got, 2.0/3.0)
	}
}

func TestStatusShowsLastRequestUsageCostAndSpeed(t *testing.T) {
	state := sessionState{LastRequest: &requestStatus{
		Profile: "ollama_cloud",
		BaseURL: "https://ollama.com/v1",
		Result: completionResult{
			Model: "gpt-oss:120b", PromptTokens: 1000, CachedInputTokens: 200,
			CompletionTokens: 500, TotalTokens: 1500, Duration: 2 * time.Second,
		},
	}}
	var output strings.Builder
	handleSessionCommand("/status last", &state, &output)

	for _, expected := range []string{
		"Последний API-запрос",
		"Токены: вход=1000 (кэш=200), выход=500, всего=1500",
		"Скорость: 250.0 token/sec",
		"Оценка стоимости: $0.00042280 USD",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("status does not contain %q:\n%s", expected, output.String())
		}
	}
}

func TestStatusBeforeFirstRequest(t *testing.T) {
	var output strings.Builder
	handleSessionCommand("/status last", &sessionState{}, &output)
	if !strings.Contains(output.String(), "сначала отправьте запрос") {
		t.Fatalf("empty status output = %q", output.String())
	}
}

func TestSettingsDoNotExposeAPIConfiguration(t *testing.T) {
	state := sessionState{
		ActiveProfile: "deepseek",
		API:           apiProfile{BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_API_KEY"},
		Model:         "deepseek-v4-flash",
	}
	var output strings.Builder
	printSessionSettings(&output, state)
	if strings.Contains(output.String(), "api.deepseek.com") || strings.Contains(output.String(), "DEEPSEEK_API_KEY") {
		t.Fatalf("settings expose API configuration: %q", output.String())
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

func TestModelCommandAcceptsOpenAICompatibleModelName(t *testing.T) {
	config := defaultAppConfig()
	profile, _ := config.activeAPIProfile()
	state := sessionState{ActiveProfile: config.ActiveProfile, Profiles: config.Profiles, API: profile, Model: defaultModelName}
	var output strings.Builder
	handleSessionCommand("/model qwen2.5-coder:7b", &state, &output)

	if state.Model != "qwen2.5-coder:7b" {
		t.Fatalf("model = %q", state.Model)
	}
	if !strings.Contains(output.String(), "Модель изменена") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestProfileCommandSwitchesToLocalProfileWithoutToken(t *testing.T) {
	config := defaultAppConfig()
	config.Profiles["local"] = apiProfile{BaseURL: "http://127.0.0.1:1234/v1", Model: "local-model"}
	profile, _ := config.activeAPIProfile()
	state := sessionState{
		ActiveProfile: config.ActiveProfile, Profiles: config.Profiles, API: profile,
		APIToken: "deepseek-secret", Model: profile.Model,
	}
	var output strings.Builder
	handleSessionCommand("/profile local", &state, &output)

	if state.ActiveProfile != "local" || state.Model != "local-model" || state.APIToken != "" {
		t.Fatalf("state after profile switch = %#v", state)
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
	profile := config.Profiles[config.ActiveProfile]
	profile.Model = "deepseek-v4-pro"
	config.Profiles[config.ActiveProfile] = profile
	config.Generation.Model = "deepseek-v4-pro"
	config.Generation.Temperature = 1.2
	config.Generation.Strategy = strategyExperts
	config.ResponseControl.Enabled = false
	if err := saveConfig(configPath, config); err != nil {
		t.Fatalf("saveConfig() returned error: %v", err)
	}

	defaultConfig := defaultAppConfig()
	defaultProfile, _ := defaultConfig.activeAPIProfile()
	state := sessionState{
		Mode: modeTemperatureBenchmark, ActiveProfile: defaultConfig.ActiveProfile,
		Profiles: defaultConfig.Profiles, Model: defaultModelName, API: defaultProfile,
	}
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
