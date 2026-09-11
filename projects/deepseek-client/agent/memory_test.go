package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func TestSlidingWindowSendsTailAndPreservesFullHistory(t *testing.T) {
	store, original := seedCompression(t, 6)
	summary := agent.Summary{Text: compactSummary, Covered: 2, PrefixHash: agent.HistoryHash(original[:2]), Version: 1}
	if err := store.SaveSummary(context.Background(), "test", agent.HistoryHash(original), 0, summary); err != nil {
		t.Fatal(err)
	}
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		all := ""
		for _, message := range settings.Messages {
			all += message.Content
		}
		if strings.Count(all, "OLD-DETAIL") != 2 || !strings.Contains(all, "new question") || strings.Contains(all, "current budget 50") {
			t.Fatalf("sliding request contains wrong history: %q", all)
		}
		return agent.Completion{Content: "answer"}, nil
	})
	request := agent.Request{ConversationID: "test", Prompt: "new question", Target: agent.Target{Model: "test"}, Compression: agent.CompressionConfig{Strategy: agent.MemorySliding, KeepLast: 4}}
	if _, err := agent.NewWithHistory(client, store).Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	saved, _ := store.Load(context.Background(), "test")
	if len(saved) != len(original)+2 || agent.HistoryHash(saved[:len(original)]) != agent.HistoryHash(original) {
		t.Fatal("sliding strategy modified stored history")
	}
}

func TestFactsUpdateAfterTurnsAndReturnToRequest(t *testing.T) {
	store := &history.JSON{Dir: t.TempDir()}
	factsCalls := 0
	ordinaryCalls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		if strings.Contains(settings.Messages[0].Content, "Maintain key-value memory") {
			factsCalls++
			value := `{"goal":"prepare specification","limit":"budget 50"}`
			return agent.Completion{Content: value, UsageKnown: true, PromptTokens: 20, CompletionTokens: 8}, nil
		}
		ordinaryCalls++
		if ordinaryCalls == 2 {
			all := ""
			for _, message := range settings.Messages {
				all += message.Content
			}
			if !strings.Contains(all, `"goal":"prepare specification"`) {
				t.Fatal("facts were not sent on the next turn")
			}
		}
		return agent.Completion{Content: "answer", UsageKnown: true, PromptTokens: 10, CompletionTokens: 2}, nil
	})
	request := agent.Request{ConversationID: "facts", Prompt: "Goal: prepare specification; budget 50", Target: agent.Target{Model: "test"}, Compression: agent.CompressionConfig{Strategy: agent.MemoryFacts, KeepLast: 2}}
	runner := agent.NewWithHistory(client, store)
	first, err := runner.Run(context.Background(), request)
	if err != nil || first.Facts == nil || !first.Facts.Changed {
		t.Fatalf("first result=%+v err=%v", first, err)
	}
	request.Prompt = "Use the agreed constraints"
	if _, err := agent.NewWithHistory(client, &history.JSON{Dir: store.Dir}).Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if factsCalls != 2 || ordinaryCalls != 2 {
		t.Fatalf("facts=%d ordinary=%d", factsCalls, ordinaryCalls)
	}
	u, err := store.Usage(context.Background(), "facts")
	if err != nil || u.FactVersion != 2 || u.FactKeys != 2 || u.FactsCalls != 2 || u.FactsTotal != 56 || u.Total != 80 {
		t.Fatalf("usage=%+v err=%v", u, err)
	}
}

func TestFactsFailureIsRetriedBeforeNextAnswer(t *testing.T) {
	store := &history.JSON{Dir: t.TempDir()}
	failFacts := true
	ordinary := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		if strings.Contains(settings.Messages[0].Content, "Maintain key-value memory") {
			if failFacts {
				failFacts = false
				return agent.Completion{}, errors.New("extractor unavailable")
			}
			return agent.Completion{Content: `{"goal":"stable"}`}, nil
		}
		ordinary++
		return agent.Completion{Content: "answer"}, nil
	})
	request := agent.Request{ConversationID: "facts", Prompt: "goal stable", Target: agent.Target{Model: "test"}, Compression: agent.CompressionConfig{Strategy: agent.MemoryFacts}}
	result, err := agent.NewWithHistory(client, store).Run(context.Background(), request)
	if err == nil || len(result.Responses) != 1 {
		t.Fatal("answer should be saved before facts failure is reported")
	}
	request.Prompt = "continue"
	if _, err := agent.NewWithHistory(client, store).Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if ordinary != 2 {
		t.Fatalf("ordinary calls=%d", ordinary)
	}
}

func TestRecentHistoryKeepsCompletedTurns(t *testing.T) {
	messages := []agent.Message{{Role: "user", Content: "u1"}, {Role: "assistant", Content: "a1"}, {Role: "user", Content: "u2"}, {Role: "assistant", Content: "a2"}}
	got := agent.RecentHistory(messages, 3)
	if len(got) != 4 || got[0].Content != "u1" {
		t.Fatalf("completed turn boundary lost: %+v", got)
	}
}

func TestFactsContextWindowZeroDefersLimitToProvider(t *testing.T) {
	store := &history.JSON{Dir: t.TempDir()}
	if err := store.Save(context.Background(), "facts", []agent.Message{{Role: "user", Content: strings.Repeat("large ", 12000)}, {Role: "assistant", Content: "saved"}}); err != nil {
		t.Fatal(err)
	}
	called := false
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		called = true
		if settings.MaxOutputTokens != 2048 {
			t.Fatalf("max output=%d", settings.MaxOutputTokens)
		}
		return agent.Completion{}, errors.New("provider context_length_exceeded")
	})
	request := agent.Request{ConversationID: "facts", Prompt: "continue", Target: agent.Target{Model: "test", ContextWindow: 0}, Compression: agent.CompressionConfig{Strategy: agent.MemoryFacts}}
	if _, err := agent.NewWithHistory(client, store).Run(context.Background(), request); err == nil || !strings.Contains(err.Error(), "context_length_exceeded") {
		t.Fatalf("err=%v", err)
	}
	if !called {
		t.Fatal("provider was not called")
	}
}
