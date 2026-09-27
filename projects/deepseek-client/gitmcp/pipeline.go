package gitmcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchInput struct {
	Query string `json:"query" jsonschema:"Literal case-insensitive substring in commit messages; empty string selects recent commits"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum matching commits, default 20, range 1 to 50"`
}
type Commit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}
type SearchResult struct {
	Query   string   `json:"query"`
	Commits []Commit `json:"commits"`
}
type CommitSummary struct {
	Query       string         `json:"query"`
	CommitCount int            `json:"commit_count"`
	Categories  map[string]int `json:"categories"`
	Markdown    string         `json:"markdown" jsonschema:"The Markdown returned by git_summarize_commits, passed unchanged to git_save_report"`
}
type SavedReport struct {
	File   string `json:"file"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type PipelineTools struct {
	repo      string
	reports   *os.Root
	directory string
}

func OpenPipelineTools(repo, dataDir string) (*PipelineTools, error) {
	repo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if insideRepository(repo, dir) {
		return nil, fmt.Errorf("pipeline data must be outside the repository")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if insideRepository(repo, dir) {
		return nil, fmt.Errorf("pipeline data must be outside the repository")
	}
	dir = filepath.Join(dir, "reports")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	// Reject redirected storage; hold a directory handle for all subsequent writes.
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("reports must be a real directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &PipelineTools{repo: repo, reports: root, directory: dir}, nil
}
func (p *PipelineTools) Close() error { return p.reports.Close() }

func validQuery(query string) bool {
	return len(query) <= 200 && !strings.ContainsAny(query, "\x00\r\n")
}
func (p *PipelineTools) search(ctx context.Context, in SearchInput) (SearchResult, error) {
	result := SearchResult{Query: in.Query, Commits: []Commit{}}
	if !validQuery(in.Query) {
		return result, fmt.Errorf("query must be at most 200 bytes without NUL or line breaks")
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit < 1 || in.Limit > 50 {
		return result, fmt.Errorf("limit must be between 1 and 50")
	}
	args := []string{"log", "-z", "--format=%H%x00%s", "--max-count=" + strconv.Itoa(in.Limit), "--fixed-strings", "--regexp-ignore-case"}
	if in.Query != "" {
		args = append(args, "--grep="+in.Query)
	}
	args = append(args, "--")
	out, err := run(ctx, p.repo, args...)
	if err != nil {
		return result, err
	}
	if out.Truncated {
		return result, fmt.Errorf("search output truncated; reduce limit")
	}
	parts := strings.Split(strings.TrimSuffix(out.Text, "\x00"), "\x00")
	if out.Text == "" {
		return result, nil
	}
	if len(parts)%2 != 0 {
		return result, fmt.Errorf("invalid Git log output")
	}
	for i := 0; i < len(parts); i += 2 {
		result.Commits = append(result.Commits, Commit{Hash: parts[i], Subject: parts[i+1]})
	}
	if err = validateSearchResult(result); err != nil {
		return result, err
	}
	return result, nil
}

var commitHash = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

func validateSearchResult(in SearchResult) error {
	if !validQuery(in.Query) || len(in.Commits) > 50 {
		return fmt.Errorf("invalid search result size")
	}
	seen := map[string]bool{}
	for _, c := range in.Commits {
		if !commitHash.MatchString(c.Hash) || seen[c.Hash] || len(c.Subject) > 4096 || strings.ContainsAny(c.Subject, "\x00\r\n") {
			return fmt.Errorf("invalid or duplicate commit")
		}
		seen[c.Hash] = true
	}
	return nil
}
func escapeMarkdown(s string) string {
	return strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "#", "\\#", "!", "\\!").Replace(s)
}
func summarizeCommits(in SearchResult) (CommitSummary, error) {
	if err := validateSearchResult(in); err != nil {
		return CommitSummary{}, err
	}
	s := CommitSummary{Query: in.Query, CommitCount: len(in.Commits), Categories: map[string]int{"feat": 0, "fix": 0, "docs": 0, "test": 0, "other": 0}}
	for _, c := range in.Commits {
		kind := "other"
		for _, candidate := range []string{"feat", "fix", "docs", "test"} {
			if strings.HasPrefix(c.Subject, candidate+":") || strings.HasPrefix(c.Subject, candidate+"(") || strings.HasPrefix(c.Subject, candidate+"!:") {
				kind = candidate
				break
			}
		}
		s.Categories[kind]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Git commit summary\n\nQuery: %s\n\nMatching commits included: %d (bounded search, not the total repository count).\n\n", escapeMarkdown(in.Query), s.CommitCount)
	for _, kind := range []string{"feat", "fix", "docs", "test", "other"} {
		fmt.Fprintf(&b, "- %s: %d\n", kind, s.Categories[kind])
	}
	b.WriteString("\n## Commits\n\n")
	if len(in.Commits) == 0 {
		b.WriteString("No matching commits.\n")
	}
	for _, c := range in.Commits {
		fmt.Fprintf(&b, "- `%s` %s\n", c.Hash, escapeMarkdown(c.Subject))
	}
	s.Markdown = b.String()
	return s, nil
}
func (p *PipelineTools) save(ctx context.Context, in CommitSummary) (SavedReport, error) {
	if err := ctx.Err(); err != nil {
		return SavedReport{}, err
	}
	if in.Markdown == "" || len(in.Markdown) > 256<<10 || strings.ContainsRune(in.Markdown, 0) {
		return SavedReport{}, fmt.Errorf("report must be nonempty text, at most 256 KiB, without NUL")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return SavedReport{}, err
	}
	name := fmt.Sprintf("git-summary-%x.md", nonce)
	f, err := p.reports.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return SavedReport{}, err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			_ = p.reports.Remove(name)
		}
	}()
	n, err := f.WriteString(in.Markdown)
	if err != nil {
		return SavedReport{}, err
	}
	if err = f.Sync(); err != nil {
		return SavedReport{}, err
	}
	if err = f.Close(); err != nil {
		return SavedReport{}, err
	}
	if err = ctx.Err(); err != nil {
		return SavedReport{}, err
	}
	ok = true
	sum := sha256.Sum256([]byte(in.Markdown))
	return SavedReport{File: filepath.Join(p.directory, name), Bytes: n, SHA256: fmt.Sprintf("%x", sum)}, nil
}

func (p *PipelineTools) Register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "git_search_commits", Description: "Search commit messages reachable from current HEAD with a literal query. Returns the query and matching commit hashes and subjects. Read-only; does not fetch."}, func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchResult, error) {
		out, err := p.search(ctx, in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "git_summarize_commits", Description: "Create a deterministic Markdown summary from a query and supplied commit hashes and subjects. Returns commit count, categories and Markdown. Does not search the repository or write files."}, func(_ context.Context, _ *mcp.CallToolRequest, in SearchResult) (*mcp.CallToolResult, CommitSummary, error) {
		out, err := summarizeCommits(in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "git_save_report", Description: "Save supplied summary Markdown unchanged in the server's reports directory. Returns server path, byte count and SHA256. Creates a unique new file; no overwrite or caller-supplied paths."}, func(ctx context.Context, _ *mcp.CallToolRequest, in CommitSummary) (*mcp.CallToolResult, SavedReport, error) {
		out, err := p.save(ctx, in)
		return nil, out, err
	})
}
