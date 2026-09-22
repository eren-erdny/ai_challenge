package userprofiles_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/userprofiles"
)

func TestProfilesPersistAndActiveSelectionSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory", "profiles.json")
	store := &userprofiles.JSON{Path: path}
	formal, err := store.Create(ctx, "formal")
	if err != nil {
		t.Fatal(err)
	}
	formal.Style = "formal and concise"
	formal.Format = "bullet list"
	formal.Constraints = []string{"no emoji"}
	if err := store.Save(ctx, formal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, "teacher"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Select(ctx, "formal"); err != nil {
		t.Fatal(err)
	}
	reopened := &userprofiles.JSON{Path: path}
	active, err := reopened.Active(ctx)
	if err != nil || active.Name != "formal" || active.Style != "formal and concise" || active.Format != "bullet list" || len(active.Constraints) != 1 {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	profiles, err := reopened.List(ctx)
	if err != nil || len(profiles) != 2 || profiles[0].Name != "formal" || profiles[1].Name != "teacher" {
		t.Fatalf("profiles=%+v err=%v", profiles, err)
	}
	if err := reopened.Delete(ctx, "formal"); err != nil {
		t.Fatal(err)
	}
	active, err = reopened.Active(ctx)
	if err != nil || active.Name != "" {
		t.Fatalf("active after delete=%+v err=%v", active, err)
	}
}

func TestProfileValidationAndMissingSelection(t *testing.T) {
	ctx := context.Background()
	store := &userprofiles.JSON{Path: filepath.Join(t.TempDir(), "profiles.json")}
	if _, err := store.Create(ctx, "bad name"); err == nil {
		t.Fatal("invalid name accepted")
	}
	profile, err := store.Create(ctx, "valid")
	if err != nil {
		t.Fatal(err)
	}
	profile.Constraints = make([]string, 21)
	for i := range profile.Constraints {
		profile.Constraints[i] = "constraint"
	}
	if err := store.Save(ctx, profile); err == nil {
		t.Fatal("too many constraints accepted")
	}
	if _, err := store.Select(ctx, "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing select err=%v", err)
	}
	if err := agent.ValidateUserProfile(agent.UserProfile{Name: "valid"}); err != nil {
		t.Fatal(err)
	}
}
