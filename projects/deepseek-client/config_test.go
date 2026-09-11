package main

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func TestSaveAndLoadConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "deepseek-client", "config.json")
	want := defaultAppConfig()
	want.ResponseControl.Format = "json"
	want.ResponseControl.MaxWords = 120
	want.Generation.Temperature = 1.2
	want.Generation.Strategy = strategyExperts

	if err := saveConfig(configPath, want); err != nil {
		t.Fatalf("saveConfig() returned error: %v", err)
	}

	got, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig() returned error: %v", err)
	}
	if got.ResponseControl.Format != "json" || got.ResponseControl.MaxWords != 120 {
		t.Fatalf("loaded response control = %#v", got.ResponseControl)
	}
	if got.Generation != want.Generation {
		t.Fatalf("loaded generation = %#v, want %#v", got.Generation, want.Generation)
	}
	if got.ActiveProfile != want.ActiveProfile || len(got.Profiles) != len(want.Profiles) {
		t.Fatalf("loaded profiles = %#v, want %#v", got.Profiles, want.Profiles)
	}
	if got.HistoryPolicy.Memory() != agent.MemorySummary || got.HistoryPolicy.Keep() != 10 {
		t.Fatalf("loaded memory policy = %#v", got.HistoryPolicy)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(data), "api_token") || strings.Contains(string(data), "test-token") {
		t.Fatalf("saved config must not contain a token: %s", data)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(configPath)
		if err != nil {
			t.Fatalf("stat config: %v", err)
		}
		if gotMode := info.Mode().Perm(); gotMode != 0o600 {
			t.Fatalf("config permissions = %o, want 600", gotMode)
		}
	}
}

func TestLoadConfigAppliesDefaultsToLegacyFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"deepseek_api_token":"test-token"}`), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	config, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig() returned error: %v", err)
	}
	if config.ResponseControl.Format != defaultFormatName {
		t.Fatalf("default format = %q, want %q", config.ResponseControl.Format, defaultFormatName)
	}
	if config.ResponseControl.MaxTokens != 200 {
		t.Fatalf("default max_tokens = %d, want 200", config.ResponseControl.MaxTokens)
	}
	if config.Generation.Model != defaultModelName || config.Generation.Temperature != 0.7 || config.Generation.Strategy != strategyStandard {
		t.Fatalf("default generation = %#v", config.Generation)
	}
}

func TestLoadConfigForReloadPreservesOrOverridesToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	config := defaultAppConfig()
	config.APIToken = ""
	if err := saveConfig(configPath, config); err != nil {
		t.Fatalf("saveConfig() returned error: %v", err)
	}

	baseURL := config.Profiles[config.ActiveProfile].BaseURL
	preserved, err := loadConfigForReload(configPath, "current-token", baseURL)
	if err != nil {
		t.Fatalf("loadConfigForReload() returned error: %v", err)
	}
	if preserved.APIToken != "current-token" {
		t.Fatalf("preserved token = %q", preserved.APIToken)
	}

	t.Setenv("DEEPSEEK_API_KEY", "environment-token")
	overridden, err := loadConfigForReload(configPath, "current-token", baseURL)
	if err != nil {
		t.Fatalf("loadConfigForReload() returned error: %v", err)
	}
	if overridden.APIToken != "environment-token" {
		t.Fatalf("overridden token = %q", overridden.APIToken)
	}
}

func TestValidateGeneration(t *testing.T) {
	for _, temperature := range []float64{-0.1, 2.1, math.NaN(), math.Inf(1)} {
		if err := validateGeneration(generationConfig{Model: defaultModelName, Temperature: temperature, Strategy: strategyStandard}); err == nil {
			t.Fatalf("temperature %g must be rejected", temperature)
		}
	}
	if err := validateGeneration(generationConfig{Model: defaultModelName, Temperature: 0.7, Strategy: "unknown"}); err == nil {
		t.Fatal("unknown strategy must be rejected")
	}
	if err := validateGeneration(generationConfig{Model: "local/model-name", Temperature: 0.7, Strategy: strategyStandard}); err != nil {
		t.Fatalf("custom OpenAI-compatible model must be accepted: %v", err)
	}
	if err := validateGeneration(generationConfig{Model: "", Temperature: 0.7, Strategy: strategyStandard}); err == nil {
		t.Fatal("empty model must be rejected")
	}
}

func TestLoadLocalAPIConfigWithoutToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{
  "active_profile": "local",
  "profiles": {
    "local": {
      "base_url": "http://127.0.0.1:1234/v1",
      "api_key_env": "",
      "model": "local-model"
    }
  },
  "generation": {"temperature": 0.7, "strategy": "standard"}
}`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig() returned error: %v", err)
	}
	profile, ok := config.activeAPIProfile()
	if !ok || profile.APIKeyEnv != "" || config.Generation.Model != "local-model" {
		t.Fatalf("local config = %#v", config)
	}
}

func TestReloadDoesNotSendDeepSeekTokenToDifferentAPI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-secret")
	configPath := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{
  "active_profile":"local",
  "profiles":{"local":{"base_url":"http://127.0.0.1:1234/v1","api_key_env":"","model":"local-model"}},
  "generation":{"temperature":0.7,"strategy":"standard"}
}`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := loadConfigForReload(configPath, "current-deepseek-token", defaultAPIBaseURL)
	if err != nil {
		t.Fatalf("loadConfigForReload() returned error: %v", err)
	}
	if config.APIToken != "" {
		t.Fatal("token from a different endpoint must not be reused")
	}
}

func TestValidateAPIRejectsInvalidOrCredentialedURL(t *testing.T) {
	for _, baseURL := range []string{"localhost:1234/v1", "ftp://localhost/v1", "http://user:pass@localhost/v1"} {
		if err := validateAPIProfile(apiProfile{BaseURL: baseURL, Model: "test-model"}); err == nil {
			t.Fatalf("base URL %q must be rejected", baseURL)
		}
	}
}

func TestValidateResponseControlRejectsUnknownFormat(t *testing.T) {
	config := defaultAppConfig().ResponseControl
	config.Format = "unknown"

	if err := validateResponseControl(config); err == nil {
		t.Fatal("validateResponseControl() must reject an unknown format")
	}
}

func TestBuildResponseControlUsesCatalog(t *testing.T) {
	config := defaultAppConfig().ResponseControl
	config.Format = "bullet_list"
	config.MaxWords = 50
	config.StopSequences = []string{"<STOP>"}

	control, err := buildResponseControl(config)
	if err != nil {
		t.Fatalf("buildResponseControl() returned error: %v", err)
	}
	if control.Format != "bullet_list" || control.MaxWords != 50 {
		t.Fatalf("control = %#v", control)
	}
	if !strings.Contains(control.SystemPrompt, "маркированный Markdown-список") {
		t.Fatalf("system prompt does not contain format instruction: %q", control.SystemPrompt)
	}
	if !strings.Contains(control.SystemPrompt, "<STOP>") {
		t.Fatalf("system prompt does not contain stop sequence: %q", control.SystemPrompt)
	}
}

func TestBuildResponseControlCanBeDisabled(t *testing.T) {
	config := defaultAppConfig().ResponseControl
	config.Enabled = false

	control, err := buildResponseControl(config)
	if err != nil {
		t.Fatalf("buildResponseControl() returned error: %v", err)
	}
	if control != nil {
		t.Fatalf("buildResponseControl() = %#v, want nil", control)
	}
}
