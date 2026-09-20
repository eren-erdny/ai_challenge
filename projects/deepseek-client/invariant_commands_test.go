package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/invariants"
)

func TestInvariantCommandsAddListAndRemove(t *testing.T) {
	store := &invariants.JSON{Path: filepath.Join(t.TempDir(), "invariants.json")}
	state := sessionState{Invariants: store}
	var output strings.Builder
	for _, command := range []string{
		"/invariant add architecture Использовать модульный монолит",
		"/invariant add stack Только Go и PostgreSQL",
		"/invariant list",
		"/invariant remove 1",
	} {
		if !handleInvariantCommand(context.Background(), command, &state, &output) {
			t.Fatalf("command not handled: %s", command)
		}
	}
	text := output.String()
	for _, want := range []string{"Добавлен инвариант 1 [architecture]", "Добавлен инвариант 2 [stack]", "Только Go и PostgreSQL", "Инвариант 1 удалён"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	values, err := store.LoadInvariants(context.Background())
	if err != nil || len(values) != 1 || values[0].ID != 2 {
		t.Fatalf("values=%+v err=%v", values, err)
	}
}

func TestInvariantStoreIsAppliedThroughAdapter(t *testing.T) {
	store := &invariants.JSON{Path: filepath.Join(t.TempDir(), "invariants.json")}
	if _, err := store.AddInvariant(context.Background(), agent.InvariantStack, "Только Go"); err != nil {
		t.Fatal(err)
	}
	state := sessionState{Invariants: store, Mode: modeFree, Model: "test", Strategy: strategyStandard}
	calls := 0
	ask := func(context.Context, string, string, requestSettings) (completionResult, error) {
		calls++
		if calls == 1 {
			return completionResult{Content: "Используем Python"}, nil
		}
		return completionResult{Content: `{"compliant":false,"violations":[1],"explanation":"Python запрещён выбранным стеком."}`}, nil
	}
	var output, errors strings.Builder
	executeQuestion(context.Background(), "", "Перепиши на Python", state, &output, &errors, ask)
	if calls != 2 || errors.Len() != 0 || !strings.Contains(output.String(), "Не могу выполнить") || !strings.Contains(output.String(), "Инварианты: запрос отклонён") {
		t.Fatalf("calls=%d output=%q errors=%q", calls, output.String(), errors.String())
	}
}
