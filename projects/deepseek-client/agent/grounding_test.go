package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

const evidence = `{"results":[{"source":"policy.md","section":"Retention","chunk_id":"current","text":"Backups MUST NOT be retained for more than fourteen days.\nAudit records MAY be stored in Lighthouse.","vector_score":0.8,"rerank_score":2}],"min_similarity":0.49,"min_rerank_score":-3}`
const grounded = `{"status":"answered","claims":[{"text":"Backups must not be retained for more than fourteen days.","quote_ids":["q1"]}],"quotes":[{"id":"q1","chunk_id":"current","text":"Backups MUST NOT be retained for more than fourteen days."}],"clarification":""}`

func TestGroundingProvenanceAndBinding(t *testing.T) {
	g, err := agent.ValidateGroundedAnswer(grounded, evidence)
	if err != nil || g.Status != "answered" || len(g.Sources) != 1 || g.Sources[0].Source != "policy.md" || g.Sources[0].Section != "Retention" {
		t.Fatalf("%+v %v", g, err)
	}
	if !strings.Contains(g.Render(), "[q1]") || !strings.Contains(g.Render(), "[policy.md#current]") || !strings.Contains(g.Render(), "Цитаты:") {
		t.Fatal(g.Render())
	}
	// Whitespace may differ, but punctuation/case/numbers/negation must not.
	if _, err := agent.ValidateGroundedAnswer(strings.Replace(grounded, "Backups MUST NOT", "Backups  MUST\\nNOT", 1), evidence); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"fabricated quote":        strings.Replace(grounded, "MUST NOT", "MUST", 1),
		"wrong quantity":          strings.Replace(grounded, "than fourteen", "than ninety", -1),
		"old base":                strings.Replace(grounded, `"chunk_id":"current"`, `"chunk_id":"previous"`, 1),
		"unbound claim":           strings.Replace(grounded, `["q1"]`, `[]`, 1),
		"wrong reference":         strings.Replace(grounded, `["q1"]`, `["q9"]`, 1),
		"duplicate reference":     strings.Replace(grounded, `["q1"]`, `["q1","q1"]`, 1),
		"extra unchecked answer":  strings.Replace(grounded, `"status":`, `"answer":"free assertion","status":`, 1),
		"invented metadata":       strings.Replace(grounded, `"id":"q1"`, `"id":"q1","source":"made-up.md"`, 1),
		"duplicate JSON key":      strings.Replace(grounded, `"status":`, `"status":"unknown","status":`, 1),
		"missing field":           strings.Replace(grounded, `,"clarification":""`, ``, 1),
		"null clarification":      strings.Replace(grounded, `"clarification":""`, `"clarification":null`, 1),
		"null array":              strings.Replace(grounded, `["q1"]`, `null`, 1),
		"markdown":                "```json\n" + grounded + "\n```",
		"trailing":                grounded + "{}",
		"unsafe terminal control": strings.Replace(grounded, "Backups must", "\\u001bBackups must", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := agent.ValidateGroundedAnswer(bad, evidence); err == nil {
				t.Fatal("accepted invalid provenance/schema")
			}
		})
	}
}

func TestGroundingDoesNotPretendToCheckMeaning(t *testing.T) {
	// Exact provenance alone cannot detect a semantic inversion in a paraphrase.
	// This is a deliberately wrong answer for a real quote; the semantic review
	// must mark it wrong. Keep the limitation explicit and executable.
	badMeaning := strings.Replace(grounded, "must not be retained", "must be retained", 1)
	if _, err := agent.ValidateGroundedAnswer(badMeaning, evidence); err != nil {
		t.Fatal("provenance checker unexpectedly treated itself as a semantic judge", err)
	}
}

func TestGroundingUnknownAndCutoffs(t *testing.T) {
	unknown := `{"status":"unknown","claims":[],"quotes":[],"clarification":"Which retention policy do you mean?"}`
	g, err := agent.ValidateGroundedAnswer(unknown, evidence)
	if err != nil || !strings.Contains(g.Render(), "Не знаю") || len(g.Sources) != 0 || len(g.Quotes) != 0 {
		t.Fatalf("%+v %v", g, err)
	}
	for _, bad := range []string{strings.Replace(unknown, "unknown", "answered", 1), strings.Replace(unknown, "Which retention policy do you mean?", "", 1), strings.Replace(grounded, "answered", "unknown", 1)} {
		if _, err := agent.ValidateGroundedAnswer(bad, evidence); err == nil {
			t.Fatal("invalid abstention accepted")
		}
	}
	for _, weak := range []string{`{"results":[],"no_evidence":true}`, strings.Replace(evidence, `"vector_score":0.8`, `"vector_score":0.1`, 1), strings.Replace(evidence, `"rerank_score":2`, `"rerank_score":-9`, 1)} {
		if _, err := agent.ValidateGroundedAnswer(grounded, weak); err == nil {
			t.Fatal("answered below cutoff")
		}
	}
}

func TestGroundingControlledAndBenchmarkCompatibility(t *testing.T) {
	for _, mode := range []agent.Mode{agent.Controlled, agent.Compare, agent.TemperatureBenchmark, agent.ModelBenchmark} {
		request := requestFor(mode)
		request.Evidence = evidence
		calls := 0
		runner := agent.New(agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
			calls++
			if !s.JSONOutput {
				t.Fatal("grounding JSON required for each model")
			}
			if s.Control != nil && strings.Contains(s.Control.SystemPrompt, "summary") {
				t.Fatal("conflicting presentation schema")
			}
			return agent.Completion{Content: grounded, FinishReason: "stop", CompletionTokens: 50}, nil
		}))
		r, err := runner.Run(context.Background(), request)
		if err != nil || r.Analysis != nil {
			t.Fatalf("%s: %+v %v", mode, r, err)
		}
		expected := 1
		if mode == agent.Compare {
			expected = 2
		} else if mode == agent.ModelBenchmark || mode == agent.TemperatureBenchmark {
			expected = 3
		}
		if calls != expected {
			t.Fatal("unexpected extra judge call", calls)
		}
		for _, response := range r.Responses {
			if response.Grounding == nil || (response.Validation != nil && !response.Validation.Passed) {
				t.Fatalf("grounding/control mismatch %+v", response)
			}
		}
	}
}

func TestGroundingUnknownCannotHideAnswerInClarification(t *testing.T) {
	unknown := `{"status":"unknown","claims":[],"quotes":[],"clarification":"AES requires 256-bit keys; clarify?"}`
	g, err := agent.ValidateGroundedAnswer(unknown, evidence)
	if err != nil || strings.Contains(g.Render(), "256") || !strings.Contains(g.Render(), "Уточните вопрос") {
		t.Fatal("unchecked clarification displayed", g, err)
	}
}

func TestGroundingRejectedBeforeHistoryAndDisplay(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ content, reason string }{{strings.Replace(grounded, "MUST NOT", "MUST", 1), "stop"}, {grounded, "length"}} {
		store := &history.JSON{Dir: t.TempDir()}
		old := []agent.Message{{Role: "user", Content: "old"}, {Role: "assistant", Content: "old answer"}}
		if err := store.Save(ctx, "test", old); err != nil {
			t.Fatal(err)
		}
		calls := 0
		runner := agent.NewWithHistory(agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
			calls++
			if !s.JSONOutput {
				t.Fatal("missing JSON request mode")
			}
			return agent.Completion{Content: tc.content, FinishReason: tc.reason, UsageKnown: true, PromptTokens: 10, CompletionTokens: 5}, nil
		}), store)
		result, err := runner.Run(ctx, agent.Request{Prompt: "retention?", ConversationID: "test", Target: agent.Target{Model: "test"}, Evidence: evidence})
		loaded, _ := store.Load(ctx, "test")
		if err == nil || calls != 1 || len(result.Responses) != 0 || result.Failed == nil || result.Failed.Answer.Content != "" || result.Failed.Answer.TotalTokens != 15 || agent.HistoryHash(loaded) != agent.HistoryHash(old) {
			t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
		}
	}
}

func TestGroundingSuccessPersistsRenderedAnswerOnly(t *testing.T) {
	ctx := context.Background()
	store := &history.JSON{Dir: t.TempDir()}
	runner := agent.NewWithHistory(agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		return agent.Completion{Content: grounded}, nil
	}), store)
	r, err := runner.Run(ctx, agent.Request{Prompt: "retention?", ConversationID: "test", Target: agent.Target{Model: "test"}, Evidence: evidence})
	if err != nil || r.Responses[0].Grounding == nil {
		t.Fatal(err)
	}
	m, err := store.Load(ctx, "test")
	if err != nil || len(m) != 2 || m[0].FileContext != "" || m[1].Content != r.Responses[0].Grounding.Render() || strings.Contains(m[1].Content, "Audit records") {
		t.Fatalf("unexpected saved history %+v %v", m, err)
	}
}

func TestGroundingEmptyContextNeverCallsAnyLLM(t *testing.T) {
	ctx := context.Background()
	store, _ := seedCompression(t, 8)
	for _, mode := range []agent.Mode{agent.Free, agent.Task, agent.Controlled, agent.Compare, agent.TemperatureBenchmark, agent.ModelBenchmark} {
		request := requestFor(mode)
		request.ConversationID = "test"
		request.Evidence = `{"results":[],"no_evidence":true}`
		request.Compression = agent.CompressionConfig{Strategy: agent.MemoryFacts}
		runner := agent.NewWithHistory(agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
			t.Fatal("LLM called for empty context")
			return agent.Completion{}, nil
		}), store)
		r, err := runner.Run(ctx, request)
		if err != nil || len(r.Responses) != 1 || !r.Responses[0].Grounding.Local || r.Responses[0].Answer.TotalTokens != 0 || r.Responses[0].CostUSD == nil || *r.Responses[0].CostUSD != 0 {
			t.Fatalf("%s: %+v %v", mode, r, err)
		}
	}
}

func TestGroundingMalformedEvidenceStopsBeforeLLM(t *testing.T) {
	for _, bad := range []string{`{}`, `{"results":null}`, `{"results":[{"source":"x"}]}`, strings.TrimSuffix(evidence, "}") + `,"results":[]}`} {
		runner := agent.New(agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
			t.Fatal("LLM called for malformed evidence")
			return agent.Completion{}, nil
		}))
		if _, err := runner.Run(context.Background(), agent.Request{Prompt: "x", Target: agent.Target{Model: "test"}, Evidence: bad}); err == nil {
			t.Fatal("malformed evidence accepted")
		}
	}
}

func TestGroundingTaskEnvelopeAndNoRepair(t *testing.T) {
	store := &taskStoreStub{}
	request := requestFor(agent.Task)
	request.Evidence = evidence
	task := `{"goal":"Read policy","stage":"planning","current_step":"Inspect retention","expected_action":"Approve plan","paused":false,"pause_reason":""}`
	transition := `{"action":"start","from":"","to":"planning","gate":"none","reason":"Inspect","evidence":""}`
	encoded, _ := json.Marshal(grounded)
	outer := `{"answer":` + string(encoded) + `,"task_state":` + task + `,"transition":` + transition + `}`
	runner := agent.New(agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		return agent.Completion{Content: outer}, nil
	})).WithTaskStates(store)
	r, err := runner.Run(context.Background(), request)
	if err != nil || store.saves != 1 || r.Responses[0].Grounding == nil || r.TaskState.Stage != agent.TaskPlanning {
		t.Fatalf("%+v %v", r, err)
	}
	calls := 0
	runner = agent.New(agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		calls++
		return agent.Completion{Content: "bad"}, nil
	})).WithTaskStates(&taskStoreStub{state: agent.TaskState{Goal: "Read policy", Stage: agent.TaskPlanning, CurrentStep: "Inspect", ExpectedAction: "Approve", Updated: time.Now()}})
	if _, err := runner.Run(context.Background(), request); err == nil || calls != 1 {
		t.Fatal("invalid task-grounding must fail without repair", err, calls)
	}
}
