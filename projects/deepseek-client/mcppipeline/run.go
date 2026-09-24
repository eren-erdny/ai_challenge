// Package mcppipeline runs three sequential MCP calls without LLM mediation.
package mcppipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/gitmcp"
)

type Result struct {
	Steps   []string             `json:"steps"`
	Summary gitmcp.CommitSummary `json:"summary"`
	Saved   gitmcp.SavedReport   `json:"saved"`
}

// Run passes each raw structured result to the next tool without rewriting it.
// Any failure aborts the remaining steps. Saving is not retried automatically.
func Run(ctx context.Context, tools agent.ToolExecutor, input gitmcp.SearchInput) (Result, error) {
	var out Result
	commitCount := 0
	args, err := json.Marshal(input)
	if err != nil {
		return out, err
	}
	value := string(args)
	for _, name := range []string{"git_search_commits", "git_summarize_commits", "git_save_report"} {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		value, err = tools.Execute(ctx, name, value)
		if err != nil {
			return out, fmt.Errorf("pipeline step %s: %w", name, err)
		}
		out.Steps = append(out.Steps, name)
		switch name {
		case "git_search_commits":
			var r gitmcp.SearchResult
			if err = json.Unmarshal([]byte(value), &r); err != nil {
				return out, fmt.Errorf("decode search result: %w", err)
			}
			if r.Commits == nil || r.Query != input.Query {
				return out, fmt.Errorf("invalid search result")
			}
			commitCount = len(r.Commits)
		case "git_summarize_commits":
			if err = json.Unmarshal([]byte(value), &out.Summary); err != nil {
				return out, fmt.Errorf("decode summary: %w", err)
			}
			if out.Summary.Markdown == "" || out.Summary.Query != input.Query || out.Summary.CommitCount != commitCount {
				return out, fmt.Errorf("invalid summary result")
			}
		case "git_save_report":
			if err = json.Unmarshal([]byte(value), &out.Saved); err != nil {
				return out, fmt.Errorf("decode saved report: %w", err)
			}
			sum := sha256.Sum256([]byte(out.Summary.Markdown))
			if out.Saved.File == "" || out.Saved.Bytes != len(out.Summary.Markdown) || out.Saved.SHA256 != fmt.Sprintf("%x", sum) {
				return out, fmt.Errorf("saved report integrity mismatch")
			}
		}
	}
	return out, nil
}
