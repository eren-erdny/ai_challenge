package main

import (
	"bufio"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/userprofiles"
)

func TestPersonaCommandsCreateConfigureSwitchAndDelete(t *testing.T) {
	store := &userprofiles.JSON{Path: filepath.Join(t.TempDir(), "profiles.json")}
	state := sessionState{UserProfiles: store}
	var output strings.Builder
	commands := []string{
		"/persona create concise",
		"/persona set style Кратко и делово",
		"/persona set format Markdown bullets",
		"/persona add-constraint Не использовать emoji",
		"/persona create teacher",
		"/persona use concise",
		"/persona show",
	}
	for _, command := range commands {
		if !handlePersonalizationCommand(context.Background(), command, &state, &output) {
			t.Fatalf("not handled: %s", command)
		}
	}
	if state.UserProfile.Name != "concise" || state.UserProfile.Style != "Кратко и делово" || state.UserProfile.Format != "Markdown bullets" || len(state.UserProfile.Constraints) != 1 {
		t.Fatalf("profile=%+v", state.UserProfile)
	}
	if !strings.Contains(output.String(), "Не использовать emoji") {
		t.Fatal(output.String())
	}
	if !handlePersonalizationCommand(context.Background(), "/persona remove-constraint 1", &state, &output) || len(state.UserProfile.Constraints) != 0 {
		t.Fatal("constraint not removed")
	}
	if !handlePersonalizationCommand(context.Background(), "/persona delete concise", &state, &output) || state.UserProfile.Name != "" {
		t.Fatal("active profile not cleared")
	}
}

func TestPersonaCreateWizardCollectsInitialPreferences(t *testing.T) {
	store := &userprofiles.JSON{Path: filepath.Join(t.TempDir(), "profiles.json")}
	state := sessionState{UserProfiles: store}
	var output strings.Builder
	if !handlePersonalizationCommand(context.Background(), "/persona create guided", &state, &output) {
		t.Fatal("create not handled")
	}
	input := bufio.NewReader(strings.NewReader("Кратко и делово\nMarkdown bullets\nБез emoji; Не больше трёх пунктов\n"))
	runPersonaWizard(input, &state, &output, &output)
	if state.UserProfile.Style != "Кратко и делово" || state.UserProfile.Format != "Markdown bullets" || len(state.UserProfile.Constraints) != 2 {
		t.Fatalf("profile=%+v", state.UserProfile)
	}
	reopened, err := store.Active(context.Background())
	if err != nil || reopened.Style != state.UserProfile.Style || len(reopened.Constraints) != 2 {
		t.Fatalf("reopened=%+v err=%v", reopened, err)
	}
}
