package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func TestConversationUsageSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := &history.JSON{Dir: dir}
	client := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		return agent.Completion{Content: "answer", PromptTokens: 100, CompletionTokens: 20, CachedInputTokens: 10, UsageKnown: true}, nil
	})
	r := agent.Request{ConversationID: "old", Prompt: "question", Target: agent.Target{Model: "deepseek-v4-flash", BaseURL: "https://api.deepseek.com"}}
	for i := 0; i < 2; i++ {
		if _, err := agent.NewWithHistory(client, store).Run(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	reopened := &history.JSON{Dir: dir}
	u, err := reopened.Usage(ctx, "old")
	if err != nil || u.Turns != 2 || u.Input != 200 || u.Output != 40 || u.Total != 240 || u.CachedInput != 20 || u.CostUSD <= 0 || u.UnknownCostTurns != 0 {
		t.Fatalf("totals: %+v %v", u, err)
	}
	id, err := reopened.NewConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	state := sessionState{History: reopened, ConversationID: id}
	handleSessionCommand("/status", &state, &output)
	if !strings.Contains(output.String(), "всего=0") {
		t.Fatal(output.String())
	}
	state.ConversationID = "old"
	output.Reset()
	handleSessionCommand("/status", &state, &output)
	if !strings.Contains(output.String(), "всего=240") {
		t.Fatal(output.String())
	}
	failing := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		return agent.Completion{}, errors.New("offline")
	})
	if _, err := agent.NewWithHistory(failing, reopened).Run(ctx, r); err == nil {
		t.Fatal("failure expected")
	}
	after, _ := reopened.Usage(ctx, "old")
	if after != u {
		t.Fatal("failure changed totals")
	}
}

func TestLegacyAndMissingUsageAreIncomplete(t *testing.T) {
	ctx := context.Background()
	s := &history.JSON{Dir: t.TempDir()}
	if err := s.Save(ctx, "legacy", []agent.Message{{Role: "user", Content: "old"}, {Role: "assistant", Content: "old answer"}}); err != nil {
		t.Fatal(err)
	}
	c := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		return agent.Completion{Content: "unknown usage"}, nil
	})
	_, err := agent.NewWithHistory(c, s).Run(ctx, agent.Request{ConversationID: "legacy", Prompt: "new", Target: agent.Target{Model: "unknown"}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Usage(ctx, "legacy")
	if err != nil || u.Turns != 2 || u.UnknownUsageTurns != 2 || u.UnknownCostTurns != 2 {
		t.Fatalf("legacy: %+v %v", u, err)
	}
	var output strings.Builder
	printConversationStatus(&output, sessionState{History: s, ConversationID: "legacy"})
	if !strings.Contains(output.String(), "итог стоимости неполный") {
		t.Fatal(output.String())
	}
}
