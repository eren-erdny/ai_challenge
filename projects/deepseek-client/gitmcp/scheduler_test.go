package gitmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/mcptools"
)

func TestScheduledMCPRunsWithoutClientAndSurvivesRestart(t *testing.T) {
	repo, dir := fixture(t), t.TempDir()
	events := make(chan Report, 2)
	s, err := OpenScheduler(repo, dir, func(r Report) { events <- r })
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(repo)
	if err != nil {
		t.Fatal(err)
	}
	s.Register(server)
	handler, err := HTTPHandler(server, testToken)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	client, err := mcptools.ConnectHTTP(ctx, httpServer.URL+"/mcp", testToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.Definitions()) != 8 {
		t.Fatalf("missing scheduler tools: %v", client.Definitions())
	}
	turns := 0
	model := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		turns++
		if turns == 1 {
			return agent.Completion{ToolCalls: []agent.ToolCall{{ID: "schedule", Type: "function", Function: agent.FunctionCall{Name: "git_summary_schedule", Arguments: `{"id":"once","delay_seconds":1}`}}}}, nil
		}
		last := settings.Messages[len(settings.Messages)-1]
		var r Report
		if last.Role != "tool" || json.Unmarshal([]byte(last.Content), &r) != nil || r.ID != "once" || !r.Active {
			t.Fatalf("schedule not delivered to agent: %+v", last)
		}
		return agent.Completion{Content: "Scheduled " + r.ID}, nil
	})
	result, err := agent.New(model).WithTools(client).Run(ctx, agent.Request{Prompt: "Schedule a summary", Mode: agent.Free, Target: agent.Target{Model: "test"}})
	if err != nil || result.Last().Answer.Content != "Scheduled once" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	client.Close() // Worker must run without any client session.
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- s.Run(workerCtx) }()
	select {
	case r := <-events:
		if r.Runs != 1 || r.Active || r.DirtySamples != 1 || r.Latest.Snapshot.ChangedEntries != 1 {
			t.Fatalf("unexpected summary: %+v", r)
		}
	case <-ctx.Done():
		t.Fatal("scheduled execution did not happen")
	}
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	client, err = mcptools.ConnectHTTP(ctx, httpServer.URL+"/mcp", testToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.Execute(ctx, "git_summary_get", `{"id":"once"}`)
	if err != nil || !strings.Contains(value, `"runs":1`) {
		t.Fatalf("get: %s %v", value, err)
	}
	if _, err := client.Execute(ctx, "git_summary_list", `{}`); err != nil {
		t.Fatal(err)
	}
	client.Close()
	httpServer.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenScheduler(repo, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	r, err := restored.Get("once")
	if err != nil || r.Runs != 1 || r.Active {
		t.Fatalf("restart lost result: %+v %v", r, err)
	}
	if err := restored.runDue(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	r, _ = restored.Get("once")
	if r.Runs != 1 {
		t.Fatal("one-shot ran again")
	}
}

func TestPeriodicAggregationCatchUpAndCancel(t *testing.T) {
	repo, dir := fixture(t), t.TempDir()
	s, err := OpenScheduler(repo, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	in := ScheduleInput{ID: "periodic", DelaySeconds: 1, IntervalSeconds: 10}
	r, err := s.Create(in, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.Create(in, now)
	if err != nil || !duplicate.NextRun.Equal(r.NextRun) {
		t.Fatal("retry changed schedule")
	}
	s.Close()
	s, err = OpenScheduler(repo, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	calls := 0
	s.collect = func(context.Context) (Snapshot, error) {
		calls++
		if calls == 3 {
			return Snapshot{}, errors.New("fixture failure")
		}
		return Snapshot{Branch: "main", Head: string(rune('a' + calls)), ChangedEntries: calls, Conflicts: calls - 1}, nil
	}
	ctx := context.Background()
	if err = s.runDue(ctx, now); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Get(in.ID)
	if r.Runs != 1 || !r.NextRun.After(now) {
		t.Fatalf("missed ticks replayed: %+v", r)
	}
	if err = s.runDue(ctx, now); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("ran before due")
	}
	for i := 0; i < 2; i++ {
		r, _ = s.Get(in.ID)
		if err = s.runDue(ctx, r.NextRun); err != nil {
			t.Fatal(err)
		}
	}
	r, _ = s.Get(in.ID)
	if r.Runs != 3 || r.Failures != 1 || r.HeadChanges != 1 || r.DirtySamples != 2 || r.MaxConflicts != 1 {
		t.Fatalf("aggregate: %+v", r)
	}
	for i := 0; i < 35; i++ {
		r, _ = s.Get(in.ID)
		if err = s.runDue(ctx, r.NextRun); err != nil {
			t.Fatal(err)
		}
	}
	r, _ = s.Get(in.ID)
	if r.RetainedSamples != retainedSamples || r.Runs != 38 {
		t.Fatalf("retention: %+v", r)
	}
	if _, err = s.Cancel(in.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.runDue(ctx, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if calls != 38 {
		t.Fatal("cancelled schedule executed")
	}
}

func TestSchedulePersistenceAndValidation(t *testing.T) {
	repo, dir := fixture(t), t.TempDir()
	inside := filepath.Join(repo, "scheduler-data")
	if other, e := OpenScheduler(repo, inside, nil); e == nil {
		other.Close()
		t.Fatal("in-repo data accepted")
	}
	if _, e := os.Stat(inside); !os.IsNotExist(e) {
		t.Fatal("rejected data directory was created inside clone")
	}
	s, err := OpenScheduler(repo, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := OpenScheduler(repo, dir, nil); err == nil {
		other.Close()
		t.Fatal("two writers acquired same directory")
	}
	for _, in := range []ScheduleInput{{ID: "../bad", DelaySeconds: 1}, {ID: "bad", DelaySeconds: 0}, {ID: "bad", DelaySeconds: 1, IntervalSeconds: 1}} {
		if _, err = s.Create(in, time.Now()); err == nil {
			t.Fatal("invalid schedule accepted")
		}
	}
	if _, err = s.Create(ScheduleInput{ID: "ok", DelaySeconds: 1}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Create(ScheduleInput{ID: "ok", DelaySeconds: 2}, time.Now()); err == nil {
		t.Fatal("conflicting ID accepted")
	}
	// A failed save cannot publish a job as successfully persisted.
	before := len(s.Reports())
	original := s.path
	s.path = filepath.Join(dir, "missing", "schedules.json")
	if _, err = s.Create(ScheduleInput{ID: "unsaved", DelaySeconds: 1}, time.Now()); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if len(s.Reports()) != before {
		t.Fatal("unsaved job leaked into memory")
	}
	s.path = original
	s.Close()
	if e := os.WriteFile(original, []byte("broken-json"), 0600); e != nil {
		t.Fatal(e)
	}
	if other, e := OpenScheduler(repo, dir, nil); e == nil {
		other.Close()
		t.Fatal("corrupt state accepted")
	}
	b, e := os.ReadFile(original)
	if e != nil || string(b) != "broken-json" {
		t.Fatal("corrupt file overwritten")
	}
}
