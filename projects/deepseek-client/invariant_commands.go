package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type invariantStore interface {
	agent.InvariantStore
	AddInvariant(context.Context, agent.InvariantKind, string) (agent.Invariant, error)
	DeleteInvariant(context.Context, int) (bool, error)
}

func printInvariants(ctx context.Context, store invariantStore, output io.Writer) {
	values, err := store.LoadInvariants(ctx)
	if err != nil {
		fmt.Fprintf(output, "Инварианты не прочитаны: %v\n", err)
		return
	}
	if len(values) == 0 {
		fmt.Fprintln(output, "Инварианты не заданы")
		return
	}
	fmt.Fprintf(output, "Инварианты: %d\n", len(values))
	for _, value := range values {
		fmt.Fprintf(output, "  %d [%s] %s\n", value.ID, value.Kind, value.Text)
	}
}

func handleInvariantCommand(ctx context.Context, command string, state *sessionState, output io.Writer) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 || fields[0] != "/invariant" {
		return false
	}
	if state.Invariants == nil {
		fmt.Fprintln(output, "Хранилище инвариантов недоступно")
		return true
	}
	if len(fields) == 1 {
		fmt.Fprintln(output, "Использование: /invariant add architecture|decision|stack|business TEXT | list | remove ID")
		return true
	}
	switch fields[1] {
	case "list", "show":
		if len(fields) != 2 {
			fmt.Fprintln(output, "Использование: /invariant list")
			return true
		}
		printInvariants(ctx, state.Invariants, output)
	case "add":
		if len(fields) < 4 {
			fmt.Fprintln(output, "Использование: /invariant add architecture|decision|stack|business TEXT")
			return true
		}
		value, err := state.Invariants.AddInvariant(ctx, agent.InvariantKind(fields[2]), strings.Join(fields[3:], " "))
		if err != nil {
			fmt.Fprintf(output, "Инвариант не добавлен: %v\n", err)
			return true
		}
		fmt.Fprintf(output, "Добавлен инвариант %d [%s]\n", value.ID, value.Kind)
	case "remove":
		if len(fields) != 3 {
			fmt.Fprintln(output, "Использование: /invariant remove ID")
			return true
		}
		id, err := strconv.Atoi(fields[2])
		if err != nil || id < 1 {
			fmt.Fprintln(output, "ID инварианта должен быть положительным числом")
			return true
		}
		deleted, err := state.Invariants.DeleteInvariant(ctx, id)
		if err != nil {
			fmt.Fprintf(output, "Инвариант не удалён: %v\n", err)
		} else if !deleted {
			fmt.Fprintf(output, "Инвариант %d не найден\n", id)
		} else {
			fmt.Fprintf(output, "Инвариант %d удалён\n", id)
		}
	default:
		fmt.Fprintln(output, "Использование: /invariant add architecture|decision|stack|business TEXT | list | remove ID")
	}
	return true
}
