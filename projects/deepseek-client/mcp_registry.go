package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/mcptools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpServerConfig struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	URL       string `json:"url,omitempty"`
	TokenEnv  string `json:"token_env,omitempty"`
}
type mcpRegistryConfig struct {
	Servers []mcpServerConfig `json:"servers"`
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func decodeMCPRegistry(data []byte) (mcpRegistryConfig, error) {
	var config mcpRegistryConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("invalid MCP registry JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return config, errors.New("MCP registry must contain one JSON object")
	}
	if len(config.Servers) == 0 || len(config.Servers) > 8 {
		return config, errors.New("MCP registry must contain 1 to 8 servers")
	}
	names := map[string]bool{}
	for _, s := range config.Servers {
		if err := mcptools.ValidateServerName(s.Name); err != nil {
			return config, err
		}
		if names[s.Name] {
			return config, fmt.Errorf("duplicate MCP server name: %s", s.Name)
		}
		names[s.Name] = true
		switch s.Transport {
		case "builtin-github":
			if s.URL != "" || s.TokenEnv != "" {
				return config, errors.New("builtin-github does not accept URL or token_env")
			}
		case "http":
			if s.URL == "" || !envName.MatchString(s.TokenEnv) {
				return config, fmt.Errorf("HTTP server %s requires URL and token_env", s.Name)
			}
		default:
			return config, fmt.Errorf("unsupported transport for MCP server %s", s.Name)
		}
	}
	return config, nil
}

func loadMCPRegistry() (mcpRegistryConfig, error) {
	if path := os.Getenv("MCP_SERVERS_FILE"); path != "" {
		f, err := os.Open(path)
		if err != nil {
			return mcpRegistryConfig{}, err
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		if err != nil {
			return mcpRegistryConfig{}, err
		}
		if len(data) > 64<<10 {
			return mcpRegistryConfig{}, errors.New("MCP registry exceeds 64 KiB")
		}
		return decodeMCPRegistry(data)
	}
	config := mcpRegistryConfig{Servers: []mcpServerConfig{{Name: "github", Transport: "builtin-github"}}}
	if endpoint := os.Getenv("GIT_MCP_URL"); endpoint != "" {
		config.Servers = append(config.Servers, mcpServerConfig{Name: "git", Transport: "http", URL: endpoint, TokenEnv: "GIT_MCP_TOKEN"})
	}
	return config, nil
}

func connectAgentMCP(fallback agent.ToolExecutor) (*mcptools.Registry, error) {
	config, err := loadMCPRegistry()
	if err != nil {
		return nil, err
	}
	registrations := make([]mcptools.Registration, 0, len(config.Servers))
	for _, server := range config.Servers {
		registrations = append(registrations, mcptools.Registration{Name: server.Name, Connect: func(ctx context.Context) (mcptools.Session, error) {
			if server.Transport == "http" {
				client, err := mcptools.ConnectHTTP(ctx, server.URL, os.Getenv(server.TokenEnv), nil)
				if err != nil {
					return nil, err
				}
				return client, nil
			}
			executable, err := os.Executable()
			if err != nil {
				return nil, err
			}
			client, err := mcptools.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(executable, "--mcp-github-server")}, nil)
			if err != nil {
				return nil, err
			}
			return client, nil
		}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	return mcptools.ConnectMany(ctx, registrations, fallback)
}
