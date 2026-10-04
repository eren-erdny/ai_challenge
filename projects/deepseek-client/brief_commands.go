package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func handleBriefCommand(ctx context.Context, command string, state *sessionState, output io.Writer) bool {
	f := strings.Fields(command)
	if len(f) == 0 || f[0] != "/brief" {
		return false
	}
	if state.Memory == nil {
		fmt.Fprintln(output, "Память задачи недоступна.")
		return true
	}
	id := conversationID(state.ConversationID)
	entries, err := state.Memory.LoadMemory(ctx, id, agent.MemoryWorking)
	if err != nil {
		fmt.Fprintf(output, "Ошибка памяти задачи: %v\n", err)
		return true
	}
	copyEntries := make(map[string]agent.MemoryEntry, len(entries))
	for k, v := range entries {
		copyEntries[k] = v
	}
	entries = copyEntries
	if len(f) == 1 || (len(f) == 2 && f[1] == "show") {
		b, err := agent.BriefFromEntries(entries)
		if err != nil {
			fmt.Fprintf(output, "Ошибка памяти задачи: %v\n", err)
			return true
		}
		fmt.Fprintf(output, "Память задачи · чат %s\nЦель: %s\nТема поиска: %s\n", id, b.Goal, b.Topic)
		for _, group := range []struct {
			name   string
			values map[string]string
		}{{"Ограничения", b.Constraints}, {"Термины", b.Terms}, {"Уточнения", b.Clarifications}} {
			keys := make([]string, 0, len(group.values))
			for k := range group.values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			fmt.Fprintf(output, "%s: %d\n", group.name, len(keys))
			for _, k := range keys {
				fmt.Fprintf(output, "  %s = %s\n", k, group.values[k])
			}
		}
		return true
	}
	if len(f) >= 3 && f[1] == "delete" {
		key := ""
		if len(f) == 4 {
			key = f[3]
		} else if len(f) != 3 {
			fmt.Fprintln(output, "/brief delete goal|topic или /brief delete constraint|term|clarify KEY")
			return true
		}
		stored, err := agent.BriefEntryKey(f[2], key)
		if err == nil {
			_, err = state.Memory.DeleteMemory(ctx, id, agent.MemoryWorking, stored)
		}
		if err != nil {
			fmt.Fprintf(output, "Память не удалена: %v\n", err)
		} else {
			fmt.Fprintln(output, "Запись памяти задачи удалена.")
		}
		return true
	}
	if len(f) < 3 {
		fmt.Fprintln(output, "/brief show | goal TEXT | topic TEXT | constraint KEY TEXT | term KEY TEXT | clarify KEY TEXT | delete KIND [KEY]")
		return true
	}
	kind, key, start := f[1], "", 2
	if kind != "goal" && kind != "topic" {
		if len(f) < 4 {
			fmt.Fprintln(output, "Нужны ключ и значение.")
			return true
		}
		key, start = f[2], 3
	}
	stored, err := agent.BriefEntryKey(kind, key)
	value := strings.Join(f[start:], " ")
	if err == nil {
		err = agent.ValidateBriefValue(value)
	}
	if err == nil {
		entries[stored] = agent.MemoryEntry{Value: value}
		_, err = agent.BriefFromEntries(entries)
	}
	if err == nil {
		err = state.Memory.SetMemory(ctx, id, agent.MemoryWorking, stored, value)
	}
	if err != nil {
		fmt.Fprintf(output, "Память задачи не сохранена: %v\n", err)
	} else {
		fmt.Fprintf(output, "Память задачи сохранена: %s%s\n", kind, func() string {
			if key != "" {
				return " / " + key
			}
			return ""
		}())
	}
	return true
}
