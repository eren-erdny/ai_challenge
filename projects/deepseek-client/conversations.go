package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type conversationManager interface {
	NewConversation(context.Context) (string, error)
	Select(context.Context, string) ([]agent.Message, error)
}

func conversationID(id string) string {
	if id == "" {
		return "default"
	}
	return id
}

// UI state changes only after the new selection has been persisted successfully.
func handleConversationCommand(ctx context.Context, command string, state *sessionState, output io.Writer) (handled, changed bool, messages []agent.Message) {
	parts := strings.Fields(command)
	if len(parts) == 0 || (parts[0] != "/new" && parts[0] != "/conversation") {
		return false, false, nil
	}
	if parts[0] == "/conversation" && len(parts) == 1 {
		fmt.Fprintf(output, "Conversation ID: %s\n", conversationID(state.ConversationID))
		return true, false, nil
	}
	if (parts[0] == "/new" && len(parts) != 1) || (parts[0] == "/conversation" && len(parts) != 2) {
		fmt.Fprintln(output, "Использование: /new или /conversation ID")
		return true, false, nil
	}
	manager, ok := state.History.(conversationManager)
	if !ok {
		fmt.Fprintln(output, "Хранилище диалогов недоступно.")
		return true, false, nil
	}
	var id string
	var err error
	if parts[0] == "/new" {
		id, err = manager.NewConversation(ctx)
	} else {
		id = parts[1]
		messages, err = manager.Select(ctx, id)
	}
	if err != nil {
		fmt.Fprintf(output, "Не удалось переключить диалог: %v\n", err)
		return true, false, nil
	}
	state.ConversationID = id
	state.LastRequest = nil
	fmt.Fprintf(output, "Conversation ID: %s\n", id)
	return true, true, messages
}

func printConversationExit(output io.Writer, state sessionState) {
	fmt.Fprintf(output, "Conversation ID: %s\n", conversationID(state.ConversationID))
}

func printConversationMessages(output io.Writer, messages []agent.Message) {
	for _, message := range messages {
		label := "Вы"
		if message.Role == "assistant" {
			label = "Ассистент"
		}
		fmt.Fprintf(output, "\n%s: %s\n", label, message.Content)
	}
}
