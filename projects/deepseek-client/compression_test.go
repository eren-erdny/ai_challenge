package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func compressionTestConfig(t *testing.T) appConfig {
	t.Helper()
	config := defaultAppConfig()
	config.History = &history.JSON{Dir: t.TempDir()}
	config.ConversationID = "test"
	var messages []agent.Message
	for i := 0; i < 8; i++ {
		messages = append(messages, agent.Message{Role: "user", Content: strings.Repeat("old background ", 100)}, agent.Message{Role: "assistant", Content: "ok"})
	}
	if err := config.History.Save(context.Background(), "test", messages); err != nil {
		t.Fatal(err)
	}
	return config
}

func summaryAsk(_ context.Context, _ string, _ string, s requestSettings) (completionResult, error) {
	return completionResult{Content: `{"facts":["kept fact"],"decisions":[],"constraints":[],"open_tasks":[],"document_facts":[]}`, PromptTokens: 100, CompletionTokens: 30}, nil
}

func TestCompressCommandCLIAndTUI(t *testing.T) {
	t.Run("CLI", func(t *testing.T) {
		config := compressionTestConfig(t)
		var output, errors bytes.Buffer
		code := runInteractiveSession(bufio.NewReader(strings.NewReader("/compress\n/exit\n")), &output, &errors, config, summaryAsk)
		if code != 0 || errors.Len() != 0 || !strings.Contains(output.String(), "применено=true") {
			t.Fatalf("%d %s %s", code, output.String(), errors.String())
		}
		messages, _ := config.History.Load(context.Background(), "test")
		if len(messages) != 16 {
			t.Fatal("command saved as user turn")
		}
	})
	t.Run("TUI", func(t *testing.T) {
		config := compressionTestConfig(t)
		model := newTUIModel(config, summaryAsk)
		model.textarea.SetValue("/compress")
		updated, command := model.Update(keyPress(tea.KeyEnter))
		next := updated.(tuiModel)
		if !next.busy || command == nil {
			t.Fatal("compression not scheduled asynchronously")
		}
		// The same worker used by the scheduled TUI command delivers the result.
		done := executeQuestionCommand(context.Background(), "", "/compress", next.state, summaryAsk)()
		updated, _ = next.Update(done)
		next = updated.(tuiModel)
		if next.busy || !strings.Contains(strings.Join(next.history, "\n"), "применено=true") {
			t.Fatal("compression result not rendered")
		}
	})
}

func TestCompressionConfigRoundTrip(t *testing.T) {
	config := defaultAppConfig()
	off := false
	config.HistoryPolicy.CompressionConfig = agent.CompressionConfig{Strategy: agent.MemoryFacts, KeepLast: 7, AutoCompress: &off}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfig(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HistoryPolicy.Keep() != 7 || loaded.HistoryPolicy.Automatic() || loaded.HistoryPolicy.Memory() != agent.MemoryFacts {
		t.Fatal("compression settings lost")
	}
	if err := os.WriteFile(path, []byte(`{"history":{"keep_last":-1}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("invalid tail accepted")
	}
	if err := os.WriteFile(path, []byte(`{"history":{"strategy":"unknown"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("invalid memory strategy accepted")
	}
}
