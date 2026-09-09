package main

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func TestCLIConversationCommands(t *testing.T) {
	ctx := context.Background()
	store := &history.JSON{Dir: t.TempDir()}
	config := defaultAppConfig()
	config.ResponseControl.Enabled = false
	config.History = store
	var err error
	config.ConversationID, config.InitialMessages, err = store.Active(ctx)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	ask := func(_ context.Context, _ string, prompt string, settings requestSettings) (completionResult, error) {
		calls++
		want := 1
		if calls == 3 {
			want = 3
		}
		if len(settings.Messages) != want {
			t.Fatalf("call %d: %#v", calls, settings.Messages)
		}
		if calls == 3 && settings.Messages[0].Content != "old fact" {
			t.Fatal("restored wrong conversation")
		}
		return completionResult{Content: "answer to " + prompt}, nil
	}
	var output, errors strings.Builder
	code := runInteractiveSession(bufio.NewReader(strings.NewReader("old fact\n/new\nnew fact\n/conversation default\nrecall\n/exit\n")), &output, &errors, config, ask)
	if code != 0 || errors.Len() != 0 || calls != 3 {
		t.Fatalf("code=%d calls=%d errors=%s", code, calls, errors.String())
	}
	if !strings.HasSuffix(output.String(), "Conversation ID: default\n") {
		t.Fatalf("missing exit ID: %s", output.String())
	}
	messages, err := store.Load(ctx, "default")
	if err != nil || len(messages) != 4 {
		t.Fatalf("history: %v %v", messages, err)
	}
}

func TestTUIConversationSwitchReplacesScreen(t *testing.T) {
	ctx := context.Background()
	store := &history.JSON{Dir: t.TempDir()}
	if err := store.Save(ctx, "default", []agent.Message{{Role: "user", Content: "old question"}, {Role: "assistant", Content: "old answer"}}); err != nil {
		t.Fatal(err)
	}
	config := defaultAppConfig()
	config.History = store
	config.ConversationID, config.InitialMessages, _ = store.Active(ctx)
	model := newTUIModel(config, nil)
	model.state.LastRequest = &requestStatus{}
	model.textarea.SetValue("/new")
	next, _ := model.submit()
	model = next.(tuiModel)
	id := model.state.ConversationID
	if id == "default" || model.state.LastRequest != nil || strings.Contains(strings.Join(model.history, "\n"), "old question") {
		t.Fatal("new did not clear view and status")
	}
	model.textarea.SetValue("/conversation missing")
	next, _ = model.submit()
	model = next.(tuiModel)
	if model.state.ConversationID != id {
		t.Fatal("failed switch changed state")
	}
	model.textarea.SetValue("/conversation default")
	next, _ = model.submit()
	model = next.(tuiModel)
	if model.state.ConversationID != "default" || !strings.Contains(strings.Join(model.history, "\n"), "old question") {
		t.Fatal("old screen not restored")
	}
	if model.state.Mode != modeControlled {
		t.Fatal("mode was changed")
	}
}

func TestConversationCommandValidation(t *testing.T) {
	store := &history.JSON{Dir: t.TempDir()}
	state := sessionState{History: store, ConversationID: "default"}
	for _, command := range []string{"/new unexpected", "/conversation one two", "/conversation ../escape"} {
		handled, changed, _ := handleConversationCommand(context.Background(), command, &state, io.Discard)
		if !handled || changed || state.ConversationID != "default" {
			t.Fatal(command)
		}
	}
	var output strings.Builder
	handleConversationCommand(context.Background(), "/conversation", &state, &output)
	if !strings.Contains(output.String(), "default") {
		t.Fatal("missing ID")
	}
}
