// Package githubmcp exposes a read-only public GitHub API through MCP.
package githubmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const ToolName = "github_get_repository"

type Input struct {
	Owner string `json:"owner" jsonschema:"GitHub owner or organization, for example golang"`
	Repo  string `json:"repo" jsonschema:"Repository name, for example go (not a URL)"`
}

type Repository struct {
	FullName         string  `json:"full_name"`
	URL              string  `json:"html_url"`
	Description      *string `json:"description"`
	Language         *string `json:"language"`
	Stars            int     `json:"stargazers_count"`
	Forks            int     `json:"forks_count"`
	OpenIssuesAndPRs int     `json:"open_issues_count" jsonschema:"Number of open issues including pull requests"`
	DefaultBranch    string  `json:"default_branch"`
	Archived         bool    `json:"archived"`
}

var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// NewServer does not make an HTTP request until a tool is called.
func NewServer() *mcp.Server {
	return newServer(&http.Client{Timeout: 15 * time.Second}, "https://api.github.com")
}

func newServer(client *http.Client, baseURL string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "github-public-api", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: ToolName, Description: "Fetch current metadata of a public GitHub repository. Read-only. open_issues_count includes pull requests."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, Repository, error) {
			repo, err := fetchRepository(ctx, client, baseURL, input)
			return nil, repo, err
		})
	return server
}

func fetchRepository(ctx context.Context, client *http.Client, baseURL string, input Input) (Repository, error) {
	var repo Repository
	if !ownerPattern.MatchString(input.Owner) || !repoPattern.MatchString(input.Repo) || input.Repo == "." || input.Repo == ".." {
		return repo, fmt.Errorf("owner and repo must be GitHub names, not paths or URLs")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/repos/"+input.Owner+"/"+input.Repo, nil)
	if err != nil {
		return repo, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "deepseek-client-mcp")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	resp, err := client.Do(req)
	if err != nil {
		return repo, fmt.Errorf("GitHub request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 403 || resp.StatusCode == 429 {
			return repo, fmt.Errorf("GitHub HTTP %d: access denied or rate limit reached; retry later (remaining=%s, reset=%s)", resp.StatusCode, resp.Header.Get("X-RateLimit-Remaining"), resp.Header.Get("X-RateLimit-Reset"))
		}
		return repo, fmt.Errorf("GitHub HTTP %d: repository unavailable (only public repositories are supported)", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return repo, err
	}
	if len(body) > 1<<20 {
		return repo, fmt.Errorf("GitHub response exceeds 1 MiB")
	}
	if err := json.Unmarshal(body, &repo); err != nil {
		return repo, fmt.Errorf("decode GitHub response: %w", err)
	}
	if repo.FullName == "" || repo.URL == "" {
		return repo, fmt.Errorf("GitHub response is missing repository identity")
	}
	return repo, nil
}
