// Command mcp-demo demonstrates the smallest useful local MCP round trip:
// it starts an MCP server over stdio, connects a client, and lists its tools.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverFlag = "--server"

type echoInput struct {
	Text string `json:"text" jsonschema:"text to return unchanged"`
}

type echoOutput struct {
	Text string `json:"text" jsonschema:"the unchanged input text"`
}

func echo(_ context.Context, _ *mcp.CallToolRequest, input echoInput) (*mcp.CallToolResult, echoOutput, error) {
	return nil, echoOutput{Text: input.Text}, nil
}

func newServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "deepseek-client-mcp-demo-server",
		Version: "1.0.0",
	}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "echo",
		Description: "Returns the supplied text unchanged",
	}, echo)
	return server
}

func runServer(ctx context.Context) error {
	return newServer().Run(ctx, &mcp.StdioTransport{})
}

func listTools(ctx context.Context, transport mcp.Transport) ([]*mcp.Tool, error) {
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "deepseek-client-mcp-demo-client",
		Version: "1.0.0",
	}, nil)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to MCP server: %w", err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list MCP tools: %w", err)
	}
	return result.Tools, nil
}

func runClient(ctx context.Context) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}

	tools, err := listTools(ctx, &mcp.CommandTransport{
		Command: exec.Command(executable, serverFlag),
	})
	if err != nil {
		return err
	}

	fmt.Println("MCP connection established")
	fmt.Printf("Available tools (%d):\n", len(tools))
	for _, tool := range tools {
		fmt.Printf("- %s: %s\n", tool.Name, tool.Description)
	}
	return nil
}

func main() {
	ctx := context.Background()
	if len(os.Args) == 2 && os.Args[1] == serverFlag {
		if err := runServer(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := runClient(ctx); err != nil {
		log.Fatal(err)
	}
}
