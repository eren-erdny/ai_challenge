package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func TestBranchCommands(t *testing.T) {
	store := &history.JSON{Dir: t.TempDir()}
	ctx := context.Background()
	base := []agent.Message{{Role: "user", Content: "shared"}, {Role: "assistant", Content: "base"}}
	if err := store.Save(ctx, "test", base); err != nil {
		t.Fatal(err)
	}
	state := sessionState{History: store, ConversationID: "test"}
	var output strings.Builder
	for _, command := range []string{"/checkpoint fork", "/branch create alpha fork", "/branch create beta fork", "/branch switch alpha", "/branches"} {
		handled, _, _ := handleBranchCommand(ctx, command, &state, &output)
		if !handled {
			t.Fatalf("not handled: %s", command)
		}
	}
	text := output.String()
	if !strings.Contains(text, "Checkpoint создан") || !strings.Contains(text, "* alpha") || !strings.Contains(text, "beta") {
		t.Fatal(text)
	}
}
