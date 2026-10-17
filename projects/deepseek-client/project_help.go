package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func projectHelpQuestion(text string) (string, bool) {
	fields := strings.Fields(text)
	if len(fields) < 2 || fields[0] != "/help" {
		return text, false
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "/help")), true
}
func isProjectHelp(text string) bool {
	_, ok := projectHelpQuestion(text)
	return ok
}

func projectHelpContext(ctx context.Context, tools agent.ToolExecutor, project, evidence string, out io.Writer) (string, error) {
	if tools == nil {
		return "", errors.New("Git MCP is not connected")
	}
	registered := false
	for _, definition := range tools.Definitions() {
		if definition.Function.Name == "git__git_status" {
			registered = true
			break
		}
	}
	if !registered {
		return "", errors.New("local git__git_status is not available")
	}
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// Fixed application-owned read-only call, not a model-selected operation.
	value, err := tools.Execute(callCtx, "git__git_status", "{}")
	if err != nil {
		return "", err
	}
	var status struct {
		Text      string `json:"text"`
		Truncated bool   `json:"truncated"`
	}
	if json.Unmarshal([]byte(value), &status) != nil || status.Truncated {
		return "", errors.New("invalid or truncated Git MCP result")
	}
	line := strings.SplitN(strings.TrimSpace(status.Text), "\n", 2)[0]
	if !strings.HasPrefix(line, "## ") {
		return "", errors.New("Git MCP result has no branch header")
	}
	text := "Project directory: " + filepath.ToSlash(project) + "\nCurrent Git status: " + line
	var wire map[string]json.RawMessage
	if json.Unmarshal([]byte(evidence), &wire) != nil || wire["results"] == nil {
		return "", errors.New("project retrieval did not provide evidence")
	}
	// Runtime context has its own provenance; it does not receive fake retrieval scores.
	runtime, _ := json.Marshal([]map[string]string{{"source": "mcp://git/git_status", "section": "Current repository branch", "chunk_id": "runtime-git-status", "text": text}})
	wire["context_results"] = runtime
	wire["no_evidence"] = json.RawMessage("false")
	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(out, "[MCP git__git_status: %s]\n", line)
	return string(encoded), nil
}

// Project mode must not silently combine a different knowledge base with this Git clone.
func validateProjectSources(project string, sources []string) error {
	root, err := filepath.Abs(project)
	if err != nil {
		return err
	}
	hasReadme, hasDocs := false, false
	for _, source := range sources {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, absolute)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("selected knowledge base contains sources outside the project")
		}
		if relative == "README.md" {
			hasReadme = true
		}
		if relative == "docs" {
			hasDocs = true
		}
	}
	if !hasReadme || !hasDocs {
		return errors.New("select the project README + docs knowledge base")
	}
	return nil
}
