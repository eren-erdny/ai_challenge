package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type projectContextTools struct {
	calls int
	fail  bool
}

func (*projectContextTools) Definitions() []agent.ToolDefinition {
	return []agent.ToolDefinition{{Type: "function", Function: agent.ToolFunction{Name: "git__git_status", Parameters: []byte(`{"type":"object"}`)}}}
}
func (p *projectContextTools) Execute(_ context.Context, name, args string) (string, error) {
	p.calls++
	if name != "git__git_status" || args != "{}" {
		return "", errors.New("unexpected call")
	}
	if p.fail {
		return "", errors.New("MCP unavailable")
	}
	branch := "## current-branch"
	if p.calls > 1 {
		branch = "## new-branch"
	}
	b, _ := json.Marshal(map[string]any{"text": branch + "\n M private-path.md", "truncated": false})
	return string(b), nil
}
func TestProjectContextFreshAndCitableDespiteRetrievalCutoffs(t *testing.T) {
	tools := &projectContextTools{}
	for _, branch := range []string{"current-branch", "new-branch"} {
		evidence, err := projectHelpContext(context.Background(), tools, t.TempDir(), `{"results":[],"no_evidence":true,"min_similarity":0.99,"min_rerank_score":100}`, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(evidence, "private-path") {
			t.Fatal("changed paths should not enter branch evidence")
		}
		wire := `{"status":"answered","claims":[{"text":"branch","quote_ids":["q1"]}],"quotes":[{"id":"q1","chunk_id":"runtime-git-status","text":"Current Git status: ## ` + branch + `"}],"clarification":""}`
		answer, err := agent.ValidateGroundedAnswer(wire, evidence)
		if err != nil || answer.Sources[0].Source != "mcp://git/git_status" {
			t.Fatalf("answer=%+v err=%v", answer, err)
		}
	}
	if tools.calls != 2 {
		t.Fatal("stale context reused")
	}
	tools.fail = true
	if _, err := projectHelpContext(context.Background(), tools, t.TempDir(), `{"results":[]}`, io.Discard); err == nil {
		t.Fatal("failed MCP accepted")
	}
}
func TestBuiltinGitConfigurationValidation(t *testing.T) {
	repo := t.TempDir()
	good, _ := json.Marshal(map[string]any{"servers": []any{map[string]string{"name": "git", "transport": "builtin-git", "repo": repo}}})
	if _, err := decodeMCPRegistry(good); err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"", "relative"} {
		b, _ := json.Marshal(map[string]any{"servers": []any{map[string]string{"name": "git", "transport": "builtin-git", "repo": repo}}})
		if _, err := decodeMCPRegistry(b); err == nil {
			t.Fatal("relative repository accepted")
		}
	}
}
func TestProjectModeRejectsOtherMCPRouting(t *testing.T) {
	t.Setenv("DEEPSEEK_PROJECT_ROOT", t.TempDir())
	t.Setenv("GIT_MCP_URL", "https://example.invalid/mcp")
	if _, err := loadMCPRegistry(); err == nil {
		t.Fatal("project mode accepted different Git server")
	}
}

func TestProjectSourcesMustMatchBoundProject(t *testing.T) {
	root := t.TempDir()
	valid := []string{filepath.Join(root, "README.md"), filepath.Join(root, "docs")}
	if err := validateProjectSources(root, valid); err != nil {
		t.Fatal(err)
	}
	for _, sources := range [][]string{valid[:1], {t.TempDir()}, append(append([]string(nil), valid...), t.TempDir())} {
		if err := validateProjectSources(root, sources); err == nil {
			t.Fatal("unrelated or incomplete project base accepted")
		}
	}
}
