// Package gitmcp exposes read-only Git operations for one operator-selected clone.
package gitmcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type output struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}
type logInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"Number of recent commits, default 10, maximum 50"`
}
type diffInput struct {
	Staged bool `json:"staged,omitempty" jsonschema:"Show staged changes instead of unstaged changes"`
}

type boundedBuffer struct {
	data      []byte
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	left := (256 << 10) - len(b.data)
	if len(p) > left {
		p = p[:left]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return n, nil
}

func run(ctx context.Context, dir string, args ...string) (output, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	base := []string{"--no-pager", "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "color.ui=false", "-C", dir}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	// Ignore Git routing/config overrides inherited from the launching shell.
	for _, item := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(item), "GIT_") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	cmd.WaitDelay = time.Second
	var b boundedBuffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	err := cmd.Run()
	if ctx.Err() != nil {
		return output{}, ctx.Err()
	}
	if err != nil {
		return output{}, fmt.Errorf("git failed: %w: %s", err, string(b.data))
	}
	return output{Text: string(b.data), Truncated: b.truncated}, nil
}

func NewServer(repo string) (*mcp.Server, error) {
	root, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	result, err := run(context.Background(), root, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("repository must be a working clone: %w", err)
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(result.Text))
	if err != nil {
		return nil, err
	}
	if filepath.Clean(root) != filepath.Clean(top) {
		return nil, fmt.Errorf("--repo must point to the repository root")
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "git-mcp", Version: "1.0.0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "git_status", Description: "Read branch and changed paths in the server's Git clone. Does not inspect the client's local files."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, output, error) {
		r, e := run(ctx, root, "status", "--short", "--branch", "--untracked-files=normal")
		return nil, r, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "git_log", Description: "Read recent commit summaries from the server clone."}, func(ctx context.Context, _ *mcp.CallToolRequest, in logInput) (*mcp.CallToolResult, output, error) {
		if in.Limit == 0 {
			in.Limit = 10
		}
		if in.Limit < 1 || in.Limit > 50 {
			return nil, output{}, fmt.Errorf("limit must be between 1 and 50")
		}
		r, e := run(ctx, root, "log", "--format=%h %s", "-n", strconv.Itoa(in.Limit))
		return nil, r, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "git_diff", Description: "Read staged or unstaged tracked-file changes in the server clone; untracked file contents are excluded."}, func(ctx context.Context, _ *mcp.CallToolRequest, in diffInput) (*mcp.CallToolResult, output, error) {
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all"}
		if in.Staged {
			args = append(args, "--cached")
		}
		args = append(args, "--")
		r, e := run(ctx, root, args...)
		return nil, r, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "git_branches", Description: "List local and cached remote branches in the server clone. Does not fetch."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, output, error) {
		r, e := run(ctx, root, "branch", "--all", "--no-color")
		return nil, r, e
	})
	return s, nil
}
