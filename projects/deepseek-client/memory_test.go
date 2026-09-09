package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

// Each child process constructs a fresh session, agent and HTTP client.
func TestMemoryProcessHelper(t *testing.T) {
	if os.Getenv("DEEPSEEK_MEMORY_HELPER") != "1" {
		return
	}
	config := defaultAppConfig()
	config.ResponseControl.Enabled = false
	profile := config.Profiles[config.ActiveProfile]
	profile.BaseURL = os.Getenv("DEEPSEEK_TEST_URL")
	config.Profiles[config.ActiveProfile] = profile
	config.History = &history.JSON{Dir: os.Getenv("DEEPSEEK_TEST_HISTORY")}
	var err error
	config.InitialMessages, err = config.History.Load(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	var output, errors bytes.Buffer
	code := runInteractiveSession(bufio.NewReader(strings.NewReader(os.Getenv("DEEPSEEK_TEST_INPUT")+"\n/exit\n")), &output, &errors, config, askDeepSeek)
	if code != 0 || errors.Len() != 0 {
		t.Fatalf("session failed: %s", errors.String())
	}
	fmt.Print(output.String())
}

func TestConversationSurvivesProcessRestart(t *testing.T) {
	requests := make(chan []agent.Message, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []agent.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		requests <- payload.Messages
		answer := "Remembered."
		if len(payload.Messages) == 3 && payload.Messages[0].Content == "Project North has budget 75000." {
			answer = "North, 75000."
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	dir := t.TempDir()
	for i, prompt := range []string{"Project North has budget 75000.", "What is my project and budget?"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10_000_000_000)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMemoryProcessHelper$")
		cmd.Env = append(os.Environ(), "DEEPSEEK_MEMORY_HELPER=1", "DEEPSEEK_TEST_URL="+server.URL, "DEEPSEEK_TEST_HISTORY="+dir, "DEEPSEEK_TEST_INPUT="+prompt)
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("process %d: %v\n%s", i, err, output)
		}
		if i == 1 && (!strings.Contains(string(output), "North, 75000.") || !strings.Contains(string(output), "Remembered.")) {
			t.Fatalf("history or answer missing: %s", output)
		}
	}
	first, second := <-requests, <-requests
	if len(first) != 1 || len(second) != 3 || second[1].Role != "assistant" || second[2].Content != "What is my project and budget?" {
		t.Fatalf("unexpected HTTP messages: %#v / %#v", first, second)
	}
	messages, err := (&history.JSON{Dir: dir}).Load(context.Background(), "default")
	if err != nil || len(messages) != 4 {
		t.Fatalf("history after restart: %#v, %v", messages, err)
	}
}
