package taskstates_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/taskstates"
)

func state(stage agent.TaskStage, paused bool) agent.TaskState {
	action := "Continue"
	if stage == agent.TaskDone {
		action = ""
	}
	reason := ""
	if paused {
		reason = "wait for user"
	}
	return agent.TaskState{
		Goal:           "Ship release",
		Stage:          stage,
		CurrentStep:    string(stage),
		ExpectedAction: action,
		Paused:         paused,
		PauseReason:    reason,
		Updated:        time.Now().UTC(),
	}
}

func TestStorePersistsPauseAndResumeAtEveryStage(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "tasks")
	store := &taskstates.JSON{Dir: dir}
	previous := agent.TaskState{}

	for _, stage := range []agent.TaskStage{agent.TaskPlanning, agent.TaskExecution, agent.TaskValidation, agent.TaskDone} {
		current := state(stage, false)
		if err := store.SaveTaskState(ctx, "conversation", previous, current); err != nil {
			t.Fatalf("save %s: %v", stage, err)
		}
		paused := state(stage, true)
		if err := store.SaveTaskState(ctx, "conversation", current, paused); err != nil {
			t.Fatalf("pause %s: %v", stage, err)
		}

		reopened := &taskstates.JSON{Dir: dir}
		loaded, err := reopened.LoadTaskState(ctx, "conversation")
		if err != nil || !loaded.Paused || loaded.Stage != stage || loaded.Goal != "Ship release" {
			t.Fatalf("loaded=%+v err=%v", loaded, err)
		}
		if stage != agent.TaskDone {
			advanced := state(next(stage), false)
			if err := reopened.SaveTaskState(ctx, "conversation", paused, advanced); err == nil {
				t.Fatalf("paused task advanced from %s", stage)
			}
		}
		resumed := state(stage, false)
		if err := reopened.SaveTaskState(ctx, "conversation", paused, resumed); err != nil {
			t.Fatalf("resume %s: %v", stage, err)
		}
		store, previous = reopened, resumed
	}
}

func TestStoreScopesStateAndRejectsStaleWrites(t *testing.T) {
	ctx := context.Background()
	store := &taskstates.JSON{Dir: t.TempDir()}
	planning := state(agent.TaskPlanning, false)
	if err := store.SaveTaskState(ctx, "task-a", agent.TaskState{}, planning); err != nil {
		t.Fatal(err)
	}
	other, err := store.LoadTaskState(ctx, "task-b")
	if err != nil || other.Stage != "" {
		t.Fatalf("other=%+v err=%v", other, err)
	}
	if err := store.SaveTaskState(ctx, "task-a", agent.TaskState{}, state(agent.TaskExecution, false)); err == nil {
		t.Fatal("stale write succeeded")
	}
}

func next(stage agent.TaskStage) agent.TaskStage {
	switch stage {
	case agent.TaskPlanning:
		return agent.TaskExecution
	case agent.TaskExecution:
		return agent.TaskValidation
	default:
		return agent.TaskDone
	}
}
