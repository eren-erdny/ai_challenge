package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/githubmcp"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/mcptools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connectGitHubMCP(fallback agent.ToolExecutor) (*mcptools.Client, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return mcptools.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(executable, "--mcp-github-server")}, fallback)
}

func handleMCPCommand(args []string, output, errorOutput io.Writer) (bool, int) {
	if len(args) == 1 && args[0] == "--mcp-github-server" {
		if err := githubmcp.NewServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			fmt.Fprintln(errorOutput, err)
			return true, 1
		}
		return true, 0
	}
	if args[0] != "--mcp-github-check" {
		return false, 0
	}
	if len(args) != 3 {
		fmt.Fprintln(errorOutput, "Использование: --mcp-github-check OWNER REPO")
		return true, 2
	}
	client, err := connectGitHubMCP(nil)
	if err != nil {
		fmt.Fprintln(errorOutput, err)
		return true, 1
	}
	defer client.Close()
	fmt.Fprintln(output, "MCP connection established")
	for _, tool := range client.Definitions() {
		fmt.Fprintf(output, "- %s: %s\n", tool.Function.Name, tool.Function.Description)
	}
	input, _ := json.Marshal(githubmcp.Input{Owner: args[1], Repo: args[2]})
	result, err := client.Execute(context.Background(), githubmcp.ToolName, string(input))
	if err != nil {
		fmt.Fprintln(errorOutput, err)
		return true, 1
	}
	fmt.Fprintln(output, result)
	return true, 0
}
