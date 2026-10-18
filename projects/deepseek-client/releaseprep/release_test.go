package releaseprep

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/prreview"
)

func testGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Release Test", "-c", "user.email=release@example.invalid", "-c", "commit.gpgsign=false", "-C", repo}, args...)...)
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("Git fixture: %v: %s", e, b)
	}
	return strings.TrimSpace(string(b))
}
func writeTest(t *testing.T, root, name, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func fixtureRepo(t *testing.T) (string, string, string) {
	t.Helper()
	repo := t.TempDir()
	testGit(t, repo, "init")
	writeTest(t, repo, "projects/deepseek-client/main.go", "package main\nfunc main() {}\n")
	writeTest(t, repo, "projects/deepseek-client/docs/release.md", "# Release preparation\nOld release behavior.\n")
	writeTest(t, repo, "scripts/ai-review/example.py", "# no code execution\n")
	writeTest(t, repo, "examples/file-assistant/result/example.md", "# Example\n")
	writeTest(t, repo, "examples/support-assistant/example.md", "# Support fixture\n")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "baseline")
	base := testGit(t, repo, "rev-parse", "HEAD")
	writeTest(t, repo, "projects/deepseek-client/docs/release.md", "# Release preparation\nCLI creates a local release draft. Setup requires a provider key. Publication is manual.\n")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "feat: prepare release drafts")
	return repo, base, testGit(t, repo, "rev-parse", "HEAD")
}
func TestGitCollectionUsesImmutableHeadAndRejectsBadRanges(t *testing.T) {
	repo, base, head := fixtureRepo(t)
	writeTest(t, repo, "projects/deepseek-client/docs/release.md", "DIRTY PRIVATE LOCAL DATA")
	in, patch, e := Collect(context.Background(), repo, base, head, "v1-preview")
	if e != nil {
		t.Fatal(e)
	}
	if len(in.Commits) != 1 || len(in.Files) != 1 || len(in.Evidence) == 0 || !strings.Contains(string(patch), "CLI creates a local release draft") || strings.Contains(EvidenceJSON(in), "PRIVATE") {
		t.Fatal("not reading committed evidence")
	}
	for _, pair := range [][2]string{{head, base}, {base, base}, {"--help", head}, {"HEAD..other", head}} {
		if _, _, e = Collect(context.Background(), repo, pair[0], pair[1], "v1"); e == nil {
			t.Fatal("bad range accepted")
		}
	}
	dst := filepath.Join(t.TempDir(), "snapshot")
	if e = Snapshot(context.Background(), repo, head, dst); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(dst, "projects/deepseek-client/docs/release.md"))
	if e != nil || strings.Contains(string(b), "PRIVATE") {
		t.Fatal("snapshot included dirty working file")
	}
}
func TestSnapshotRejectsGitSymlinks(t *testing.T) {
	repo, _, _ := fixtureRepo(t)
	cmd := exec.Command("git", "-C", repo, "hash-object", "-w", "--stdin")
	cmd.Stdin = strings.NewReader("../../private")
	b, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	testGit(t, repo, "update-index", "--add", "--cacheinfo", "120000", strings.TrimSpace(string(b)), "projects/deepseek-client/escape")
	testGit(t, repo, "commit", "-m", "test symlink")
	if e = Snapshot(context.Background(), repo, testGit(t, repo, "rev-parse", "HEAD"), filepath.Join(t.TempDir(), "snapshot")); e == nil {
		t.Fatal("symlink archive accepted")
	}
}
func draftInput() Input {
	return Input{Version: "preview", Base: strings.Repeat("a", 40), Head: strings.Repeat("b", 40), Evidence: []prreview.Chunk{{ID: "release-doc", Path: "docs/release.md", Start: 1, End: 1, Text: "CLI creates a local draft. Setup requires a provider key. Publication is manual."}}}
}

const validDraft = `{"status":"answered","claims":[{"text":"CHANGE: Добавлена подготовка черновика.","quote_ids":["q1"]},{"text":"UPGRADE: Нужен ключ провайдера.","quote_ids":["q2"]},{"text":"RISK: Публикация выполняется вручную.","quote_ids":["q3"]}],"quotes":[{"id":"q1","chunk_id":"release-doc","text":"CLI creates a local draft."},{"id":"q2","chunk_id":"release-doc","text":"Setup requires a provider key."},{"id":"q3","chunk_id":"release-doc","text":"Publication is manual."}],"clarification":""}`

func TestDraftValidationAndApplicationOwnedReadiness(t *testing.T) {
	in := draftInput()
	g, e := Validate(validDraft, in)
	if e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{strings.ReplaceAll(validDraft, "release-doc", "invented"), strings.Replace(validDraft, "Publication is manual.", "Publication is automatic.", 1), strings.Replace(validDraft, "RISK:", "READY:", 1), strings.Replace(validDraft, `"status":"answered"`, `"status":"answered","status":"answered"`, 1)} {
		if _, e = Validate(raw, in); e == nil {
			t.Fatal("unsupported draft accepted")
		}
	}
	dir := t.TempDir()
	if e = Render(dir, in, g); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 5; i++ {
		in.Checks = append(in.Checks, Check{Status: "passed", ExitCode: 0})
	}
	if e = Readiness(dir, in, "validated"); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "readiness.json"))
	if !strings.Contains(string(data), "manual_review_required") || !strings.Contains(string(data), `"published": false`) {
		t.Fatal("readiness overclaimed")
	}
	in.Checks[0].Status = "failed"
	_ = Readiness(dir, in, "validated")
	data, _ = os.ReadFile(filepath.Join(dir, "readiness.json"))
	if !strings.Contains(string(data), `"status": "blocked"`) {
		t.Fatal("AI overrode failed check")
	}
}
func TestBudgetReservesBeforeNetworkAndPersistsFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "budget.json")
	b, e := OpenBudget(path, .20)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = OpenBudget(path, .20); e == nil {
		t.Fatal("parallel budget lock accepted")
	}
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		calls++
		data, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		var persisted Usage
		if json.Unmarshal(data, &persisted) != nil || persisted.Reserved <= 0 || persisted.Calls != 1 {
			t.Fatal("network before durable reserve")
		}
		if s.MaxOutputTokens != 3500 || !s.SendThinkingDisabled || len(s.Tools) != 0 {
			t.Fatal("unbounded generation")
		}
		return agent.Completion{}, os.ErrPermission
	})
	if _, e = Generate(context.Background(), client, draftInput(), b, dir); e == nil || calls != 1 {
		t.Fatal("failed provider unexpectedly accepted")
	}
	reserved := b.Snapshot().Reserved
	if e = b.Close(); e != nil {
		t.Fatal(e)
	}
	b, e = OpenBudget(path, .20)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if b.Snapshot().Reserved != reserved || b.Snapshot().Calls != 1 {
		t.Fatal("failed spend reset")
	}
	if _, e = Generate(context.Background(), client, draftInput(), b, dir); e == nil || calls != 1 {
		t.Fatal("same output reran network")
	}
	b.usage.Reserved = .20
	if _, e = Generate(context.Background(), client, draftInput(), b, t.TempDir()); e == nil || calls != 1 {
		t.Fatal("exhausted budget dispatched")
	}
}
func TestCheckEnvironmentExcludesCredentialsAndLiveOptIns(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "synthetic-test-key")
	t.Setenv("SUPPORT_LIVE", "1")
	t.Setenv("FILE_ASSISTANT_REPLAY", "private-local-path")
	t.Setenv("PROJECT_ASSISTANT_PAID", "1")
	for _, item := range cleanEnv() {
		if strings.HasPrefix(item, "DEEPSEEK_") || strings.HasPrefix(item, "SUPPORT_") || strings.HasPrefix(item, "FILE_ASSISTANT_") || strings.HasPrefix(item, "PROJECT_ASSISTANT_") {
			t.Fatal("private or live-test environment propagated")
		}
	}
}
