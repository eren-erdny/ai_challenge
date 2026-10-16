package main

import (
	"encoding/json"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/prreview"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Explicit opt-in only. Keys stay in memory and the child process environment.
func TestPRReviewLive(t *testing.T) {
	if os.Getenv("PR_REVIEW_LIVE") != "1" {
		t.Skip("paid external test is opt-in")
	}
	if os.Getenv("PR_REVIEW_BUDGET_USD") != "0.20" {
		t.Fatal("current explicit $0.20 authorization required")
	}
	fixturePath, output, ledger := os.Getenv("PR_REVIEW_FIXTURE"), os.Getenv("PR_REVIEW_OUTPUT"), os.Getenv("PR_REVIEW_LEDGER")
	if fixturePath == "" || output == "" || ledger == "" {
		t.Fatal("isolated fixture, fresh output and shared ledger required")
	}
	var manifest struct {
		Repo  string "json:\"repo\""
		Base  string "json:\"base_sha\""
		Head  string "json:\"head_sha\""
		Scope string "json:\"scope\""
	}
	b, err := os.ReadFile(fixturePath)
	if err != nil || json.Unmarshal(b, &manifest) != nil {
		t.Fatal("fixture unavailable")
	}
	configPath, err := defaultConfigPath()
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	config, err := loadConfig(configPath)
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	profile, ok := config.Profiles["deepseek"]
	if !ok || !isDeepSeekEndpoint(profile.BaseURL) {
		t.Fatal("official DeepSeek configuration required")
	}
	key := strings.TrimSpace(os.Getenv(profile.APIKeyEnv))
	if key == "" {
		key = config.APIToken
	}
	if key == "" {
		t.Fatal("credential unavailable")
	}
	binary := filepath.Join(t.TempDir(), "review-pr.exe")
	build := exec.Command("go", "build", "-o", binary, "./cmd/review-pr")
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, data)
	}
	cmd := exec.Command(binary, "--repo", manifest.Repo, "--base", manifest.Base, "--head", manifest.Head, "--scope", manifest.Scope, "--output", output, "--ledger", ledger, "--budget-usd", "0.20")
	for _, item := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(item), "DEEPSEEK_API_KEY=") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "DEEPSEEK_API_KEY="+key)
	data, err := cmd.CombinedOutput()
	t.Log(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal("live CLI failed; evidence retained")
	}
	var review prreview.Review
	b, err = os.ReadFile(filepath.Join(output, "review.json"))
	if err != nil || json.Unmarshal(b, &review) != nil {
		t.Fatal("review missing")
	}
	found := false
	for _, f := range review.Findings {
		if f.Category == "bug" && strings.HasSuffix(f.Path, "user.go") && strings.Contains(strings.ToLower(f.Explanation), "nil") {
			found = true
		}
	}
	if !found {
		t.Fatal("live model missed deliberate nil regression; evidence retained")
	}
}
