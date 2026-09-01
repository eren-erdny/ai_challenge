package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveAndLoadToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "deepseek-client", "config.json")
	const want = "test-token"

	if err := saveToken(configPath, want); err != nil {
		t.Fatalf("saveToken() returned error: %v", err)
	}

	got, err := loadToken(configPath)
	if err != nil {
		t.Fatalf("loadToken() returned error: %v", err)
	}
	if got != want {
		t.Fatalf("loadToken() = %q, want %q", got, want)
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

func TestLoadTokenRejectsEmptyToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"deepseek_api_token":""}`), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}

	if _, err := loadToken(configPath); err == nil {
		t.Fatal("loadToken() must reject an empty token")
	}
}
