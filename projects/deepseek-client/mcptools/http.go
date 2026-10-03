package mcptools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerTransport struct{ token string }

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func ConnectHTTP(ctx context.Context, endpoint, token string, fallback agent.ToolExecutor) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("MCP URL must be an HTTP(S) endpoint without credentials, query or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return nil, fmt.Errorf("MCP requires HTTPS, or HTTP on a loopback IP for an SSH tunnel")
	}
	if len(token) < 32 || strings.ContainsAny(token, " \r\n\t") {
		return nil, fmt.Errorf("GIT_MCP_TOKEN must contain at least 32 non-whitespace characters")
	}
	client := &http.Client{Transport: bearerTransport{token: token}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("MCP redirects are disabled") }}
	return Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client}, fallback)
}
