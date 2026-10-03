// Package mcptools adapts MCP tools to the agent's tool loop.
package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Client struct {
	session     *mcp.ClientSession
	definitions []agent.ToolDefinition
	names       map[string]bool
	fallback    agent.ToolExecutor
}

// Connect discovers all pages and refuses ambiguous tool names.
func Connect(ctx context.Context, transport mcp.Transport, fallback agent.ToolExecutor) (*Client, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "deepseek-agent", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("MCP connect: %w", err)
	}
	c := &Client{session: session, names: map[string]bool{}, fallback: fallback}
	ok := false
	defer func() {
		if !ok {
			session.Close()
		}
	}()
	seen := map[string]bool{}
	if fallback != nil {
		c.definitions = append(c.definitions, fallback.Definitions()...)
		for _, d := range c.definitions {
			seen[d.Function.Name] = true
		}
	}
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("MCP tools/list: %w", err)
		}
		if seen[tool.Name] {
			return nil, fmt.Errorf("duplicate tool name: %s", tool.Name)
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, err
		}
		seen[tool.Name], c.names[tool.Name] = true, true
		c.definitions = append(c.definitions, agent.ToolDefinition{Type: "function", Function: agent.ToolFunction{Name: tool.Name, Description: tool.Description, Parameters: schema}})
	}
	ok = true
	return c, nil
}

func (c *Client) Close() error { return c.session.Close() }
func (c *Client) Definitions() []agent.ToolDefinition {
	return append([]agent.ToolDefinition(nil), c.definitions...)
}

func (c *Client) Execute(ctx context.Context, name, arguments string) (string, error) {
	if !c.names[name] {
		if c.fallback != nil {
			return c.fallback.Execute(ctx, name, arguments)
		}
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arguments), &input); err != nil || input == nil {
		return "", fmt.Errorf("MCP arguments must be a JSON object")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		return "", fmt.Errorf("MCP tools/call: %w", err)
	}
	var text []string
	for _, content := range result.Content {
		if block, ok := content.(*mcp.TextContent); ok {
			text = append(text, block.Text)
		}
	}
	if result.IsError {
		return "", fmt.Errorf("MCP tool failed: %s", strings.Join(text, "\n"))
	}
	if result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		return string(data), err
	}
	return strings.Join(text, "\n"), nil
}
