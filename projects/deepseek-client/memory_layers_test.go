package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/memorylayers"
)

func TestMemoryLayerCommandsChooseExplicitDestination(t *testing.T) {
	ctx := context.Background()
	historyStore := &history.JSON{Dir: t.TempDir()}
	if err := historyStore.Save(ctx, "task", []agent.Message{{Role: "user", Content: "short fact"}, {Role: "assistant", Content: "answer"}}); err != nil {
		t.Fatal(err)
	}
	layerStore := &memorylayers.JSON{Dir: t.TempDir()}
	state := sessionState{History: historyStore, Memory: layerStore, ConversationID: "task"}
	var output strings.Builder
	for _, command := range []string{
		"/memory set working deadline Friday 18:00",
		"/memory set long-term preferred_language Russian",
		"/memory show all",
	} {
		if !handleMemoryLayerCommand(ctx, command, &state, &output) {
			t.Fatalf("not handled: %s", command)
		}
	}
	text := output.String()
	for _, want := range []string{"Краткосрочная память: 2", "deadline = Friday 18:00", "preferred_language = Russian"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	working, _ := layerStore.LoadMemory(ctx, "task", agent.MemoryWorking)
	longTerm, _ := layerStore.LoadMemory(ctx, "another", agent.MemoryLongTerm)
	if working["deadline"].Value != "Friday 18:00" || longTerm["preferred_language"].Value != "Russian" {
		t.Fatalf("working=%+v long=%+v", working, longTerm)
	}
	if !handleMemoryLayerCommand(ctx, "/memory delete working deadline", &state, &output) {
		t.Fatal("delete not handled")
	}
	working, _ = layerStore.LoadMemory(ctx, "task", agent.MemoryWorking)
	if len(working) != 0 {
		t.Fatal(working)
	}
}

func TestMemoryStrategyCommandRemainsSeparate(t *testing.T) {
	state := sessionState{Compression: agent.CompressionConfig{Strategy: agent.MemorySummary}}
	if handleMemoryLayerCommand(context.Background(), "/memory sliding", &state, &strings.Builder{}) {
		t.Fatal("layer handler captured strategy selection")
	}
}
