package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func commandMemoryLayer(value string) (agent.MemoryLayer, bool) {
	switch value {
	case "working":
		return agent.MemoryWorking, true
	case "long-term", "longterm", "long":
		return agent.MemoryLongTerm, true
	default:
		return "", false
	}
}

func printExplicitLayer(output io.Writer, title string, entries map[string]agent.MemoryEntry) {
	fmt.Fprintf(output, "%s: %d записей\n", title, len(entries))
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(output, "  %s = %s\n", key, entries[key].Value)
	}
}

func shortMemoryText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const limit = 160
	if len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}

func printShortTermMemory(ctx context.Context, state *sessionState, output io.Writer) error {
	if state.History == nil {
		return fmt.Errorf("хранилище диалога недоступно")
	}
	messages, err := state.History.Load(ctx, conversationID(state.ConversationID))
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "Краткосрочная память: %d сообщений в диалоге %s\n", len(messages), conversationID(state.ConversationID))
	start := max(0, len(messages)-6)
	if start > 0 {
		fmt.Fprintf(output, "  … ещё %d более ранних сообщений\n", start)
	}
	for _, message := range messages[start:] {
		fmt.Fprintf(output, "  %s: %s\n", message.Role, shortMemoryText(message.Content))
	}
	return nil
}

func showMemoryLayers(ctx context.Context, state *sessionState, requested string, output io.Writer) error {
	if requested == "" || requested == "all" || requested == "short-term" || requested == "short" {
		if err := printShortTermMemory(ctx, state, output); err != nil {
			return err
		}
		if requested == "short-term" || requested == "short" {
			return nil
		}
	}
	if state.Memory == nil {
		return fmt.Errorf("хранилище слоёв памяти недоступно")
	}
	if requested == "" || requested == "all" || requested == "working" {
		entries, err := state.Memory.LoadMemory(ctx, conversationID(state.ConversationID), agent.MemoryWorking)
		if err != nil {
			return err
		}
		printExplicitLayer(output, "Рабочая память", entries)
		if requested == "working" {
			return nil
		}
	}
	if requested == "" || requested == "all" || requested == "long-term" || requested == "longterm" || requested == "long" {
		entries, err := state.Memory.LoadMemory(ctx, conversationID(state.ConversationID), agent.MemoryLongTerm)
		if err != nil {
			return err
		}
		printExplicitLayer(output, "Долговременная память", entries)
		return nil
	}
	return fmt.Errorf("неизвестный слой %q", requested)
}

// handleMemoryLayerCommand handles explicit memory operations. Strategy
// selection remains backward-compatible in handleSessionCommand.
func handleMemoryLayerCommand(ctx context.Context, command string, state *sessionState, output io.Writer) bool {
	fields := strings.Fields(command)
	if len(fields) < 2 || fields[0] != "/memory" {
		return false
	}
	switch fields[1] {
	case "show":
		if len(fields) > 3 {
			fmt.Fprintln(output, "Использование: /memory show [short-term|working|long-term|all]")
			return true
		}
		requested := "all"
		if len(fields) == 3 {
			requested = fields[2]
		}
		if err := showMemoryLayers(ctx, state, requested, output); err != nil {
			fmt.Fprintf(output, "Ошибка памяти: %v\n", err)
		}
		return true
	case "set":
		if len(fields) < 5 {
			fmt.Fprintln(output, "Использование: /memory set working|long-term KEY VALUE")
			return true
		}
		layer, ok := commandMemoryLayer(fields[2])
		if !ok {
			fmt.Fprintln(output, "Сохранять явно можно только в working или long-term; краткосрочная память пополняется успешными ходами диалога")
			return true
		}
		if state.Memory == nil {
			fmt.Fprintln(output, "Хранилище слоёв памяти недоступно")
			return true
		}
		value := strings.Join(fields[4:], " ")
		if err := state.Memory.SetMemory(ctx, conversationID(state.ConversationID), layer, fields[3], value); err != nil {
			fmt.Fprintf(output, "Память не сохранена: %v\n", err)
			return true
		}
		fmt.Fprintf(output, "Сохранено в %s: %s\n", layer, fields[3])
		return true
	case "delete":
		if len(fields) != 4 {
			fmt.Fprintln(output, "Использование: /memory delete working|long-term KEY")
			return true
		}
		layer, ok := commandMemoryLayer(fields[2])
		if !ok || state.Memory == nil {
			fmt.Fprintln(output, "Удалять явно можно только из working или long-term")
			return true
		}
		deleted, err := state.Memory.DeleteMemory(ctx, conversationID(state.ConversationID), layer, fields[3])
		if err != nil {
			fmt.Fprintf(output, "Память не удалена: %v\n", err)
		} else if !deleted {
			fmt.Fprintf(output, "Ключ не найден в %s: %s\n", layer, fields[3])
		} else {
			fmt.Fprintf(output, "Удалено из %s: %s\n", layer, fields[3])
		}
		return true
	default:
		return false
	}
}
