package main

import (
	"context"
	"crypto/sha256"
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
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/releaseprep"
)

func runReleaseAssistant(args []string, output, errorOutput io.Writer) int {
	f := flag.NewFlagSet("release-assistant", flag.ContinueOnError)
	f.SetOutput(errorOutput)
	repo := f.String("repo", ".", "project Git repository")
	base := f.String("base", "", "previous tag/ref")
	head := f.String("head", "HEAD", "candidate tag/ref")
	version := f.String("version", "", "future release label; does not create a tag")
	out := f.String("output", "", "fresh output directory")
	ledger := f.String("ledger", "", "shared release budget file")
	limit := f.Float64("budget-usd", 0, "explicit authorized budget <= 0.20")
	node := f.String("node", "node", "Node executable for fixed checks")
	python := f.String("python", "python", "Python executable for fixed checks")
	dry := f.Bool("dry-run", false, "collect and check without AI or credentials")
	verify := f.Bool("verify", false, "submit the release goal through actual TUI handlers")
	if f.Parse(args) != nil {
		return 2
	}
	fail := func(e error) int { fmt.Fprintln(errorOutput, e); return 1 }
	if *base == "" || *version == "" || *out == "" || f.NArg() != 0 {
		return fail(errors.New("--base, --version and fresh --output required"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	root, e := releaseprep.Root(ctx, *repo)
	if e != nil {
		return fail(e)
	}
	if e = os.Mkdir(*out, 0700); e != nil {
		return fail(errors.New("output directory must be new; existing results are preserved"))
	}
	in, patch, e := releaseprep.Collect(ctx, root, *base, *head, *version)
	if e != nil {
		return fail(e)
	}
	if e = releaseprep.WriteJSON(filepath.Join(*out, "input.json"), in); e != nil {
		return fail(e)
	}
	if e = os.WriteFile(filepath.Join(*out, "changes.diff"), patch, 0600); e != nil {
		return fail(e)
	}
	if e = os.WriteFile(filepath.Join(*out, "changed-files.md"), []byte(releaseprep.ChangedMarkdown(in)), 0600); e != nil {
		return fail(e)
	}
	fmt.Fprintf(output, "Selected immutable range: %s -> %s; %d commits; %d changed files.\n", in.Base, in.Head, len(in.Commits), len(in.Files))
	snapshot := filepath.Join(*out, "check-workspace")
	if e = releaseprep.Snapshot(ctx, root, in.Head, snapshot); e != nil {
		return fail(e)
	}
	fmt.Fprintln(output, "Running fixed checks in exported head snapshot; no provider keys or live-test flags passed to subprocesses.")
	in.Checks, e = releaseprep.RunChecks(ctx, snapshot, *out, *node, *python)
	if e != nil {
		return fail(e)
	}
	if e = releaseprep.WriteJSON(filepath.Join(*out, "input.json"), in); e != nil {
		return fail(e)
	}
	for _, c := range in.Checks {
		fmt.Fprintf(output, "%s: %s (%d)\n", c.Name, c.Status, c.ExitCode)
	}
	if e = releaseprep.Readiness(*out, in, "not_requested"); e != nil {
		return fail(e)
	}
	if *dry {
		fmt.Fprintln(output, "Dry run completed; no AI request. Readiness remains blocked until an AI draft is validated and reviewed.")
		if !releaseprep.ChecksPassed(in.Checks) {
			return 1
		}
		return 0
	}
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		p, err := defaultConfigPath()
		if err != nil {
			return fail(errors.New("configuration unavailable"))
		}
		c, err := loadConfig(p)
		if err != nil {
			return fail(errors.New("configuration unavailable"))
		}
		profile, ok := c.Profiles["deepseek"]
		if !ok || !isDeepSeekEndpoint(profile.BaseURL) {
			return fail(errors.New("official DeepSeek profile required"))
		}
		if profile.APIKeyEnv != "" {
			key = strings.TrimSpace(os.Getenv(profile.APIKeyEnv))
		}
		if key == "" && (c.ActiveProfile == "deepseek" || c.ActiveProfile == "") {
			key = c.APIToken
		}
	}
	if key == "" {
		return fail(errors.New("DeepSeek credential unavailable; checks are retained"))
	}
	if *ledger == "" {
		return fail(errors.New("explicit shared --ledger required"))
	}
	budget, e := releaseprep.OpenBudget(*ledger, *limit)
	if e != nil {
		return fail(e)
	}
	defer budget.Close()
	client := llm.New(&http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, map[string]string{"deepseek": key})
	goal := "Подготовь черновик релиза " + *version + " по выбранному диапазону и результатам проверок."
	var result *agent.GroundedAnswer
	ask := func(callCtx context.Context, _ string, prompt string, _ requestSettings) (completionResult, error) {
		if prompt != goal {
			return completionResult{}, errors.New("this release session accepts the printed release goal only")
		}
		if result == nil {
			result, e = releaseprep.Generate(callCtx, client, in, budget, *out)
			if e != nil {
				_ = releaseprep.Readiness(*out, in, "generation_failed")
				return completionResult{}, e
			}
			if e = releaseprep.Render(*out, in, result); e != nil {
				return completionResult{}, e
			}
			if e = releaseprep.Readiness(*out, in, "validated"); e != nil {
				return completionResult{}, e
			}
		}
		return completionResult{Content: "Черновик подготовлен. Нужна ручная проверка перед публикацией.\n\n" + result.Answer + "\n\nФайлы: release-notes.md, changelog-draft.md, readiness-report.md.\nКаталог: " + *out, Model: "deepseek-flash", FinishReason: "stop"}, nil
	}
	c := defaultAppConfig()
	c.ResponseControl.Enabled = false
	c.ActiveProfile = "deepseek"
	c.Profiles = map[string]apiProfile{"deepseek": {BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", ContextWindow: 32768, MaxOutputTokens: 3500}}
	c.Generation.Temperature = 0
	c.Generation.Strategy = strategyStandard
	fmt.Fprintln(output, "TUI goal:", goal)
	code := 0
	if *verify {
		model := newTUIModel(c, ask)
		model.ctx = ctx
		model.textarea.SetValue(goal)
		next, cmd := model.submit()
		model = next.(tuiModel)
		if cmd == nil {
			return fail(errors.New("TUI did not submit release goal"))
		}
		batch, ok := cmd().(tea.BatchMsg)
		if !ok || len(batch) == 0 {
			return fail(errors.New("TUI release job missing"))
		}
		message, ok := batch[0]().(answerMessage)
		if !ok {
			return fail(errors.New("TUI release answer missing"))
		}
		_, _ = model.Update(message)
		if e = releaseprep.WriteJSON(filepath.Join(*out, "tui-answer.json"), map[string]any{"goal": goal, "visible": message.text, "scripted_TUI": true, "visible_terminal": false}); e != nil {
			return fail(e)
		}
		fmt.Fprintln(output, message.text)
		if message.lastStatus == nil {
			code = 1
		}
	} else {
		code = runTUI(c, ask)
	}
	if result == nil {
		return fail(errors.New("no validated AI draft; checks and any failed attempt retained"))
	}
	files := []string{"input.json", "changes.diff", "changed-files.md", "checks.json", "completion.json", "draft.json", "usage.json", "release-notes.md", "changelog-draft.md", "readiness-report.md", "readiness.json"}
	hashes := map[string]string{}
	for _, name := range files {
		b, err := os.ReadFile(filepath.Join(*out, name))
		if err != nil {
			return fail(err)
		}
		h := sha256.Sum256(b)
		hashes[name] = fmt.Sprintf("%x", h[:])
	}
	if e = releaseprep.WriteJSON(filepath.Join(*out, "manifest.json"), map[string]any{"version": in.Version, "base": in.Base, "head": in.Head, "sha256": hashes, "usage": budget.Snapshot(), "checks_passed": releaseprep.ChecksPassed(in.Checks), "manual_review_required": true, "published": false}); e != nil {
		return fail(e)
	}
	if !releaseprep.ChecksPassed(in.Checks) {
		code = 1
	}
	return code
}
