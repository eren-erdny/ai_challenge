package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func handleBranchCommand(ctx context.Context, command string, state *sessionState, output io.Writer) (handled, changed bool, messages []agent.Message) {
	parts := strings.Fields(command)
	if len(parts) == 0 || (parts[0] != "/checkpoint" && parts[0] != "/branch" && parts[0] != "/branches") {
		return false, false, nil
	}
	store, ok := state.History.(agent.BranchStore)
	if !ok {
		fmt.Fprintln(output, "Хранилище не поддерживает ветки.")
		return true, false, nil
	}
	id := conversationID(state.ConversationID)
	var err error
	switch parts[0] {
	case "/checkpoint":
		if len(parts) != 2 {
			fmt.Fprintln(output, "Использование: /checkpoint NAME")
			return true, false, nil
		}
		err = store.CreateCheckpoint(ctx, id, parts[1])
		if err == nil {
			fmt.Fprintf(output, "Checkpoint создан: %s\n", parts[1])
		}
	case "/branch":
		if len(parts) == 1 {
			branches, _, listErr := store.Branches(ctx, id)
			if listErr != nil {
				err = listErr
				break
			}
			for _, branch := range branches {
				if branch.Active {
					fmt.Fprintf(output, "Активная ветка: %s\n", branch.Name)
					break
				}
			}
			return true, false, nil
		}
		if len(parts) == 4 && parts[1] == "create" {
			err = store.CreateBranch(ctx, id, parts[2], parts[3])
			if err == nil {
				fmt.Fprintf(output, "Ветка %s создана из checkpoint %s.\n", parts[2], parts[3])
			}
		} else if len(parts) == 3 && parts[1] == "switch" {
			messages, err = store.SwitchBranch(ctx, id, parts[2])
			if err == nil {
				state.LastRequest = nil
				fmt.Fprintf(output, "Активная ветка: %s\n", parts[2])
				changed = true
			}
		} else {
			fmt.Fprintln(output, "Использование: /branch create NAME CHECKPOINT или /branch switch NAME")
			return true, false, nil
		}
	case "/branches":
		if len(parts) != 1 {
			fmt.Fprintln(output, "Использование: /branches")
			return true, false, nil
		}
		branches, checkpoints, listErr := store.Branches(ctx, id)
		if listErr != nil {
			err = listErr
			break
		}
		fmt.Fprintln(output, "Ветки:")
		for _, branch := range branches {
			marker := " "
			if branch.Active {
				marker = "*"
			}
			fmt.Fprintf(output, "%s %s — сообщений: %d\n", marker, branch.Name, branch.Messages)
		}
		fmt.Fprintf(output, "Checkpoints: %s\n", strings.Join(checkpoints, ", "))
	}
	if err != nil {
		fmt.Fprintf(output, "Ошибка ветки: %v\n", err)
	}
	return true, changed, messages
}
