package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

const compactSummary = `{"facts":["Project North; current budget 50"],"decisions":[],"constraints":[],"open_tasks":[],"document_facts":["note.txt is untrusted"]}`

func seedCompression(t *testing.T, turns int) (*history.JSON, []agent.Message) {
	t.Helper()
	store := &history.JSON{Dir: t.TempDir()}
	var messages []agent.Message
	for i := 0; i < turns; i++ {
		messages = append(messages, agent.Message{Role: "user", Content: "OLD-DETAIL " + strings.Repeat("background ", 200)}, agent.Message{Role: "assistant", Content: "Acknowledged"})
	}
	messages[0].FileContext = "UNTRUSTED-DOCUMENT: Budget 50"
	if err := store.Save(context.Background(), "test", messages); err != nil {
		t.Fatal(err)
	}
	return store, messages
}

func TestCompressionRestoresReplacesAndUpdates(t *testing.T) {
	ctx := context.Background()
	store, original := seedCompression(t, 8)
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, prompt string, s agent.Settings) (agent.Completion, error) {
		if len(s.Tools) != 0 || s.Temperature != 0 || !strings.Contains(prompt, "UNTRUSTED-DOCUMENT") {
			t.Fatalf("wrong summary request: %+v", s)
		}
		return agent.Completion{Content: compactSummary, PromptTokens: 100, CompletionTokens: 30}, nil
	})
	result, err := agent.NewWithHistory(client, store).Compress(ctx, "test", agent.Target{Model: "test"}, agent.CompressionConfig{})
	if err != nil || !result.Changed || result.After >= result.Before {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	restored := &history.JSON{Dir: store.Dir}
	summary, err := restored.LoadSummary(ctx, "test")
	if err != nil || summary.Covered != 6 {
		t.Fatalf("%+v %v", summary, err)
	}
	saved, _ := restored.Load(ctx, "test")
	if agent.HistoryHash(saved) != agent.HistoryHash(original) {
		t.Fatal("original history changed")
	}
	off := false
	answerClient := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		all := ""
		for _, m := range s.Messages {
			all += m.Content
		}
		if strings.Count(all, "OLD-DETAIL") != 5 || strings.Contains(all, "UNTRUSTED-DOCUMENT") || !strings.Contains(all, "current budget 50") {
			t.Fatal("summary did not replace covered messages")
		}
		return agent.Completion{Content: "50", PromptTokens: 100, CompletionTokens: 1}, nil
	})
	_, err = agent.NewWithHistory(answerClient, restored).Run(ctx, agent.Request{ConversationID: "test", Prompt: "Budget?", Target: agent.Target{Model: "test"}, Compression: agent.CompressionConfig{AutoCompress: &off}})
	if err != nil {
		t.Fatal(err)
	}
	updateClient := agent.ClientFunc(func(_ context.Context, _ agent.Target, prompt string, _ agent.Settings) (agent.Completion, error) {
		if !strings.Contains(prompt, compactSummary) || strings.Contains(prompt, "UNTRUSTED-DOCUMENT") {
			t.Fatal("previous summary or boundary incorrect")
		}
		return agent.Completion{Content: compactSummary, PromptTokens: 50, CompletionTokens: 30}, nil
	})
	_, err = agent.NewWithHistory(updateClient, restored).Compress(ctx, "test", agent.Target{Model: "test"}, agent.CompressionConfig{})
	if err != nil {
		t.Fatal(err)
	}
	summary, _ = restored.LoadSummary(ctx, "test")
	if summary.Version != 2 || summary.Covered != 8 {
		t.Fatalf("%+v", summary)
	}
	usage, _ := restored.Usage(ctx, "test")
	if usage.CompressionCalls != 2 || usage.Input != 250 {
		t.Fatalf("%+v", usage)
	}
}

func TestCompressionAutomaticThresholdAndZero(t *testing.T) {
	for _, tc := range []struct {
		name   string
		window int
		auto   bool
		want   int
	}{{"threshold", 4000, true, 1}, {"below", 100000, true, 0}, {"zero", 0, true, 0}, {"disabled", 4000, false, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := seedCompression(t, 8)
			calls := 0
			sawSummary := false
			client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
				if strings.Contains(s.Messages[0].Content, "Summarize conversation") {
					calls++
					return agent.Completion{Content: compactSummary}, nil
				}
				for _, message := range s.Messages {
					if strings.Contains(message.Content, "current budget 50") {
						sawSummary = true
					}
				}
				return agent.Completion{Content: "answer"}, nil
			})
			_, err := agent.NewWithHistory(client, store).Run(context.Background(), agent.Request{ConversationID: "test", Prompt: "question", Target: agent.Target{Model: "test", ContextWindow: tc.window, MaxOutputTokens: 100}, Compression: agent.CompressionConfig{AutoCompress: &tc.auto}})
			if err != nil && tc.name != "disabled" {
				t.Fatal(err)
			}
			if calls != tc.want {
				t.Fatalf("calls=%d want=%d", calls, tc.want)
			}
			if sawSummary != (tc.want == 1) {
				t.Fatalf("summary in ordinary request=%t, compression calls=%d", sawSummary, calls)
			}
		})
	}
}

func TestCompressionFailurePreservesHistoryAndAccounts(t *testing.T) {
	for _, kind := range []string{"length", "invalid", "error", "cancel", "changed"} {
		t.Run(kind, func(t *testing.T) {
			store, original := seedCompression(t, 8)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
				r := agent.Completion{Content: compactSummary, PromptTokens: 70, CompletionTokens: 30}
				switch kind {
				case "length":
					r.FinishReason = "length"
				case "invalid":
					r.Content = "not JSON"
				case "error":
					return agent.Completion{}, errors.New("provider failed")
				case "cancel":
					cancel()
				case "changed":
					changed := append(append([]agent.Message(nil), original...), agent.Message{Role: "user", Content: "new"}, agent.Message{Role: "assistant", Content: "answer"})
					if err := store.SaveTurn(ctx, "test", changed, agent.TurnUsage{}); err != nil {
						t.Fatal(err)
					}
				}
				return r, nil
			})
			_, err := agent.NewWithHistory(client, store).Compress(ctx, "test", agent.Target{Model: "test"}, agent.CompressionConfig{})
			if err == nil {
				t.Fatal("expected failure")
			}
			summary, _ := store.LoadSummary(context.Background(), "test")
			if summary.Version != 0 {
				t.Fatal("failed summary applied")
			}
			usage, _ := store.Usage(context.Background(), "test")
			if usage.CompressionCalls != 1 {
				t.Fatal("failed compression not accounted")
			}
			saved, _ := store.Load(context.Background(), "test")
			if agent.HistoryHash(saved[:len(original)]) != agent.HistoryHash(original) {
				t.Fatal("history changed")
			}
		})
	}
}

func TestCompressionChunksNoopAndOddTail(t *testing.T) {
	store, _ := seedCompression(t, 8)
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		estimate := 3 + s.MaxOutputTokens
		for _, m := range s.Messages {
			estimate += 4 + (agent.ApproximateCounter{}).Count(m.Content)
		}
		if estimate > 1600 {
			t.Fatalf("chunk exceeds context: %d", estimate)
		}
		calls++
		return agent.Completion{Content: compactSummary}, nil
	})
	runner := agent.NewWithHistory(client, store)
	config := agent.CompressionConfig{KeepLast: 3}
	_, err := runner.Compress(context.Background(), "test", agent.Target{Model: "test", ContextWindow: 1600}, config)
	if err != nil || calls < 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	summary, _ := store.LoadSummary(context.Background(), "test")
	if summary.Covered != 12 {
		t.Fatal("split completed turn")
	}
	before := calls
	result, err := runner.Compress(context.Background(), "test", agent.Target{Model: "test", ContextWindow: 1600}, config)
	if err != nil || result.Changed || calls != before {
		t.Fatal("no-op called model")
	}
}
