package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/fileassistant"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
)

func runFileAssistantDemo(args []string, output, errorOutput io.Writer) int {
	f := flag.NewFlagSet("file-assistant-demo", flag.ContinueOnError)
	f.SetOutput(errorOutput)
	run := f.String("output", "", "new run directory")
	ledger := f.String("ledger", "", "shared ledger for all runs")
	budget := f.Float64("budget-usd", 0, "explicit authorized dollar limit, at most 0.20")
	verify := f.Bool("verify", false, "script both goals through the real TUI submit/update path")
	if f.Parse(args) != nil {
		return 2
	}
	if *run == "" || *ledger == "" || f.NArg() != 0 {
		fmt.Fprintln(errorOutput, "--output, --ledger and --budget-usd required")
		return 2
	}
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		path, err := defaultConfigPath()
		if err != nil {
			fmt.Fprintln(errorOutput, "configuration unavailable")
			return 1
		}
		config, err := loadConfig(path)
		if err != nil {
			fmt.Fprintln(errorOutput, "configuration unavailable")
			return 1
		}
		profile, ok := config.Profiles["deepseek"]
		if !ok || !isDeepSeekEndpoint(profile.BaseURL) {
			fmt.Fprintln(errorOutput, "official DeepSeek profile required")
			return 1
		}
		if profile.APIKeyEnv != "" {
			key = strings.TrimSpace(os.Getenv(profile.APIKeyEnv))
		}
		if key == "" && (config.ActiveProfile == "deepseek" || config.ActiveProfile == "") {
			key = config.APIToken
		}
	}
	if key == "" {
		fmt.Fprintln(errorOutput, "DeepSeek credential unavailable")
		return 1
	}
	s, err := fileassistant.Open(*run, *ledger, *budget)
	if err != nil {
		fmt.Fprintln(errorOutput, err)
		return 1
	}
	defer s.Close()
	base := llm.New(&http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, map[string]string{"deepseek": key})
	client := s.Client(base)
	config := defaultAppConfig()
	config.ActiveProfile = "deepseek"
	config.Profiles = map[string]apiProfile{"deepseek": {BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", ContextWindow: 32768, MaxOutputTokens: 2200}}
	config.ResponseControl.Enabled = false
	config.Generation.Temperature = 0
	config.Generation.Strategy = strategyStandard
	config.DocumentsDirectory = s.Workspace
	config.Tools = s
	config.ConversationID = "file-assistant"
	config.History = &history.JSON{Dir: filepath.Join(s.Dir, "history")}
	ask := func(ctx context.Context, _ string, prompt string, settings requestSettings) (completionResult, error) {
		return client.Complete(ctx, agent.Target{Profile: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", SendThinkingDisabled: true}, prompt, settings)
	}
	fmt.Fprintln(output, "TaskDesk workspace:", s.Workspace)
	for i, goal := range fileassistant.Goals {
		fmt.Fprintf(output, "Goal %d: %s\n", i+1, goal)
	}
	code := 0
	if *verify {
		err = verifyFileAssistantTUI(config, ask, s.Dir, output)
		if err != nil {
			fmt.Fprintln(errorOutput, "TUI scenario failed:", err)
			code = 1
		}
	} else {
		code = runTUI(config, ask)
	}
	if err = s.Finish(); err != nil {
		fmt.Fprintln(errorOutput, err)
		return 1
	}
	check := s.Verify()
	message := "passed"
	if check != nil {
		message = check.Error()
		code = 1
	}
	report := map[string]any{"kind": "Real DeepSeek tool calling + existing agent + real file executor + TUI", "verification": message, "goals": fileassistant.Goals, "changes": s.Changes, "usage": s.Ledger, "scripted_TUI": *verify, "pricing": "https://api-docs.deepseek.com/quick_start/pricing/ checked 2026-10-17; peak USD per 1M: input 0.30 cache 0.006 output 1.20"}
	if err = projectFileJSON(filepath.Join(s.Dir, "verification.json"), report); err != nil {
		fmt.Fprintln(errorOutput, err)
		return 1
	}
	fmt.Fprintln(output, "Verification:", message, "; diff:", filepath.Join(s.Dir, "changes.diff"))
	return code
}

func projectFileJSON(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

// Executes the actual application's submission and answer update, without claiming
// that a visible terminal or OBS recording was used during this verification.
func verifyFileAssistantTUI(config appConfig, ask askFunction, dir string, output io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	model := newTUIModel(config, ask)
	model.ctx = ctx
	answers := []map[string]string{}
	for _, goal := range fileassistant.Goals {
		model.textarea.SetValue(goal)
		next, cmd := model.submit()
		model = next.(tuiModel)
		if cmd == nil {
			return errors.New("TUI did not submit goal")
		}
		batch, ok := cmd().(tea.BatchMsg)
		if !ok || len(batch) == 0 {
			return errors.New("TUI job missing")
		}
		message, ok := batch[0]().(answerMessage)
		if !ok {
			return errors.New("TUI answer missing")
		}
		next, _ = model.Update(message)
		model = next.(tuiModel)
		answers = append(answers, map[string]string{"goal": goal, "visible": message.text})
		if err := projectFileJSON(filepath.Join(dir, "answers.json"), answers); err != nil {
			return err
		}
		fmt.Fprintln(output, message.text)
		if message.lastStatus == nil {
			return errors.New("goal failed; see answers.json and tools.json")
		}
	}
	return nil
}
