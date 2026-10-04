package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/memorylayers"
)

func TestBriefTypedCommandSubmitsOnEnter(t *testing.T) {
	config := defaultAppConfig()
	config.Memory = &memorylayers.JSON{Dir: t.TempDir()}
	config.ConversationID = "brief-keyboard"
	model := newTUIModel(config, nil)
	for _, command := range []string{"/brief goal Build the checklist", "/brief", "/brief term signal HTTP 431", "/brief"} {
		next, _ := model.Update(tea.PasteMsg{Content: command})
		model = next.(tuiModel)
		next, cmd := model.Update(keyPress(tea.KeyEnter))
		model = next.(tuiModel)
		if cmd != nil || model.busy || model.textarea.Value() != "" {
			t.Fatalf("command %q was completed instead of submitted", command)
		}
	}
	if !strings.Contains(strings.Join(model.history, "\n"), "signal = HTTP 431") {
		t.Fatal("brief display missing persisted term")
	}
}

func TestBriefPersistenceIsolationCorrectionAndInvalidWrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	state := sessionState{ConversationID: "one", Memory: &memorylayers.JSON{Dir: dir}}
	run := func(command string) string {
		var out strings.Builder
		if !handleBriefCommand(ctx, command, &state, &out) {
			t.Fatal("not handled")
		}
		return out.String()
	}
	for _, cmd := range []string{"/brief goal Build the HTTP checklist", "/brief constraint format Concise English", "/brief term signal HTTP 429", "/brief clarify scope HTTP API"} {
		if !strings.Contains(run(cmd), "сохранена") {
			t.Fatal(cmd)
		}
	}
	state.Memory = &memorylayers.JSON{Dir: dir} // New instance, same persisted files.
	if !strings.Contains(run("/brief show"), "Build the HTTP checklist") {
		t.Fatal("goal lost on reopen")
	}
	run("/brief term signal HTTP 431")
	b, err := agent.LoadTaskBrief(ctx, state.Memory, "one")
	if err != nil || b.Terms["signal"] != "HTTP 431" {
		t.Fatal(b, err)
	}
	if !strings.Contains(run("/brief term Signal wrong"), "не сохранена") {
		t.Fatal("case collision accepted")
	}
	if !strings.Contains(run("/brief goal "+strings.Repeat("a", 601)), "не сохранена") {
		t.Fatal("oversized goal accepted")
	}
	b, _ = agent.LoadTaskBrief(ctx, state.Memory, "one")
	if b.Goal != "Build the HTTP checklist" {
		t.Fatal("invalid command changed goal")
	}
	state.ConversationID = "two"
	b, _ = agent.LoadTaskBrief(ctx, state.Memory, "two")
	if !b.Empty() {
		t.Fatal("other chat leaked")
	}
	state.ConversationID = "one"
	run("/brief delete term signal")
	b, _ = agent.LoadTaskBrief(ctx, state.Memory, "one")
	if len(b.Terms) != 0 || b.Goal == "" {
		t.Fatal("delete touched other memory")
	}
}

func TestContextualSearchFollowupsAndExplicitSubject(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	memory := &memorylayers.JSON{Dir: filepath.Join(dir, "memory")}
	h := &history.JSON{Dir: filepath.Join(dir, "history")}
	state := sessionState{ConversationID: "one", Memory: memory, History: h}
	var out strings.Builder
	handleBriefCommand(ctx, "/brief term signal HTTP 431", &state, &out)
	h.Save(ctx, "one", []agent.Message{{Role: "user", Content: "What does HTTP 429 mean?"}, {Role: "assistant", Content: "It means rate limiting. Источники: rfc6585.md"}, {Role: "user", Content: "Can it be cached?"}, {Role: "assistant", Content: "No. Источники: rfc6585.md"}})
	q, err := contextualKnowledgeQuery(ctx, state, "А его можно кэшировать?")
	if err != nil || !strings.Contains(q, "HTTP 429") {
		t.Fatal(q, err)
	}
	q, err = contextualKnowledgeQuery(ctx, state, "Can HTTP 428 responses be cached?")
	if err != nil || q != "Can HTTP 428 responses be cached?" {
		t.Fatal("stale subject", q, err)
	}
	q, _ = contextualKnowledgeQuery(ctx, state, "What does signal mean?")
	if q != "What does HTTP 431 mean?" {
		t.Fatal("correction missing", q)
	}
	state.ConversationID = "two"
	q, _ = contextualKnowledgeQuery(ctx, state, "Can it be cached?")
	if q != "Can it be cached?" {
		t.Fatal("history crossed chat boundary", q)
	}
	if q = resolveBriefTerms("a $x", map[string]string{"a": "b", "b": "wrong"}); q != "b $x" {
		t.Fatal("recursive term expansion", q)
	}
	h.Save(ctx, "two", []agent.Message{{Role: "user", Content: "How long are backup copies retained?"}, {Role: "assistant", Content: "Fourteen days. Источники: policy.md"}})
	q, err = contextualKnowledgeQuery(ctx, state, "Can that be changed?")
	if err != nil || !strings.Contains(q, "backup copies retained") {
		t.Fatal("generic document follow-up lost", q, err)
	}
}

func TestBriefSurvivesTwelveTurnsWithSlidingHistoryAndCurrentSources(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	id := "long-dialog"
	h := &history.JSON{Dir: filepath.Join(dir, "history")}
	m := &memorylayers.JSON{Dir: filepath.Join(dir, "memory")}
	m.SetMemory(ctx, id, agent.MemoryWorking, "brief.goal", "Draft the rate-limiting checklist")
	m.SetMemory(ctx, id, agent.MemoryWorking, "brief.constraint.format", "Concise English")
	for turn := 0; turn < 12; turn++ {
		chunk := fmt.Sprintf("chunk-%d", turn)
		evidence := fmt.Sprintf(`{"results":[{"source":"current.md","section":"Rate limits","chunk_id":%q,"text":"A response MAY include Retry-After."}]}`, chunk)
		client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
			joined := ""
			users := 0
			for _, v := range s.Messages {
				joined += v.Content + "\n"
				if v.Role == "user" && strings.HasPrefix(v.Content, "Question ") {
					users++
				}
			}
			if !strings.Contains(joined, "Draft the rate-limiting checklist") || !strings.Contains(joined, "Concise English") {
				t.Fatal("brief lost", turn)
			}
			if turn > 2 && strings.Contains(joined, "Question 0") {
				t.Fatal("full history sent despite sliding window")
			}
			if users > 2 {
				t.Fatal("unbounded prior questions")
			}
			b, _ := json.Marshal(map[string]any{"status": "answered", "claims": []map[string]any{{"text": "A response may include Retry-After.", "quote_ids": []string{"q1"}}}, "quotes": []map[string]string{{"id": "q1", "chunk_id": chunk, "text": "A response MAY include Retry-After."}}, "clarification": ""})
			return agent.Completion{Content: string(b)}, nil
		})
		result, err := agent.NewWithHistory(client, h).WithMemoryLayers(m).Run(ctx, agent.Request{ConversationID: id, Prompt: fmt.Sprintf("Question %d", turn), Target: agent.Target{Model: "fixture"}, Evidence: evidence, Compression: agent.CompressionConfig{Strategy: agent.MemorySliding, KeepLast: 2}})
		if err != nil || len(result.Responses) != 1 || result.Last().Grounding.Sources[0].ChunkID != chunk {
			t.Fatal(turn, result, err)
		}
		if turn == 5 {
			h = &history.JSON{Dir: h.Dir}
			m = &memorylayers.JSON{Dir: m.Dir}
		}
	}
	messages, err := h.Load(ctx, id)
	if err != nil || len(messages) != 24 {
		t.Fatal("history not persisted", len(messages), err)
	}
}

func TestUnknownAlwaysDisplaysExplicitSourceStatus(t *testing.T) {
	g := &agent.GroundedAnswer{Status: "unknown", Answer: "Не знаю", Clarification: "Уточните вопрос", Local: true}
	var out strings.Builder
	printAgentResponse(&out, agent.Response{Grounding: g})
	if !strings.Contains(out.String(), "Источники: нет подтверждающих фрагментов") {
		t.Fatal(out.String())
	}
}
