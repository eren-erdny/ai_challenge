package history_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
)

func TestPruneConversations(t *testing.T) {
	ctx := context.Background()
	s := &history.JSON{Dir: t.TempDir()}
	now := time.Now().Truncate(time.Second)
	for id, age := range map[string]int{"active": 60, "old": 31, "recent": 29, "boundary": 30} {
		if err := s.Save(ctx, id, nil); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-time.Duration(age) * 24 * time.Hour)
		if err := os.Chtimes(filepath.Join(s.Dir, id+".json"), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".active.json", "broken.json", "other.txt"} {
		path := filepath.Join(s.Dir, name)
		if err := os.WriteFile(path, []byte("not a conversation"), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-60 * 24 * time.Hour)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(s.Dir, "folder.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if count, err := s.Prune(ctx, "active", 0, now); count != 0 || err != nil {
		t.Fatalf("disabled: %d %v", count, err)
	}
	count, err := s.Prune(ctx, "active", 30, now)
	if count != 1 || err == nil {
		t.Fatalf("count=%d error=%v (broken file should report a warning)", count, err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "old.json")); !os.IsNotExist(err) {
		t.Fatal("old file remains")
	}
	for _, name := range []string{"active.json", "recent.json", "boundary.json", ".active.json", "broken.json", "other.txt", "folder.json"} {
		if _, err := os.Stat(filepath.Join(s.Dir, name)); err != nil {
			t.Fatalf("protected file %s: %v", name, err)
		}
	}
}

func TestPruneRejectsInvalidAndCancelledRequests(t *testing.T) {
	s := &history.JSON{Dir: t.TempDir()}
	ctx := context.Background()
	for _, days := range []int{-1, 106752} {
		if _, err := s.Prune(ctx, "default", days, time.Now()); err == nil {
			t.Fatal("invalid days accepted")
		}
	}
	if _, err := s.Prune(ctx, "", 30, time.Now()); err == nil {
		t.Fatal("missing active ID accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Prune(cancelled, "default", 30, time.Now()); err == nil {
		t.Fatal("cancellation ignored")
	}
}
