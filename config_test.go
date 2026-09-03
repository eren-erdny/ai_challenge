package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveAndLoadConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "deepseek-client", "config.json")
	want := defaultAppConfig()
	want.APIToken = "test-token"
	want.ResponseControl.Format = "json"
	want.ResponseControl.MaxWords = 120

	if err := saveConfig(configPath, want); err != nil {
		t.Fatalf("saveConfig() returned error: %v", err)
	}

	got, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig() returned error: %v", err)
	}
	if got.APIToken != want.APIToken {
		t.Fatalf("loaded token = %q, want %q", got.APIToken, want.APIToken)
	}
	if got.ResponseControl.Format != "json" || got.ResponseControl.MaxWords != 120 {
		t.Fatalf("loaded response control = %#v", got.ResponseControl)
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
