package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryRetentionConfig(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		days       int
		invalid    bool
	}{
		{"legacy", `{}`, 30, false},
		{"empty", `{"history":{}}`, 30, false},
		{"disabled", `{"history":{"retention_days":0}}`, 0, false},
		{"custom", `{"history":{"retention_days":60}}`, 60, false},
		{"negative", `{"history":{"retention_days":-1}}`, 0, true},
		{"overflow", `{"history":{"retention_days":106752}}`, 0, true},
		{"string", `{"history":{"retention_days":"30"}}`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			config, err := loadConfig(path)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid config accepted")
				}
				return
			}
			if err != nil || config.HistoryPolicy.RetentionDays != tc.days {
				t.Fatalf("days=%d err=%v", config.HistoryPolicy.RetentionDays, err)
			}
			if err := saveConfig(path, config); err != nil {
				t.Fatal(err)
			}
			reloaded, err := loadConfig(path)
			if err != nil || reloaded.HistoryPolicy.RetentionDays != tc.days {
				t.Fatalf("round trip: %v", err)
			}
		})
	}
}
