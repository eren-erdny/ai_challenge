package mcptools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type Session interface {
	agent.ToolExecutor
	Close() error
}
type Registration struct {
	Name    string
	Connect func(context.Context) (Session, error)
}
type route struct {
	executor agent.ToolExecutor
	original string
}

// Registry owns sessions and routes only explicitly advertised names. A failed
// call is never retried against a different server, even for same-named tools.
type Registry struct {
	definitions []agent.ToolDefinition
	routes      map[string]route
	sessions    []Session
	closed      atomic.Bool
	closeOnce   sync.Once
	closeError  error
}

var serverName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,15}$`)
var exposedName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func ValidateServerName(name string) error {
	if !serverName.MatchString(name) {
		return fmt.Errorf("invalid MCP server name %q: use 1-16 lowercase letters, digits or hyphens, starting with a letter", name)
	}
	return nil
}

func ConnectMany(ctx context.Context, registrations []Registration, fallback agent.ToolExecutor) (*Registry, error) {
	if len(registrations) == 0 || len(registrations) > 8 {
		return nil, errors.New("register between 1 and 8 MCP servers")
	}
	seen := map[string]bool{}
	for _, r := range registrations {
		if err := ValidateServerName(r.Name); err != nil {
			return nil, err
		}
		if seen[r.Name] || r.Connect == nil {
			return nil, fmt.Errorf("duplicate or incomplete MCP registration: %s", r.Name)
		}
		seen[r.Name] = true
	}
	registry := &Registry{routes: map[string]route{}}
	success := false
	defer func() {
		if !success {
			registry.Close()
		}
	}()
	if fallback != nil {
		for _, d := range fallback.Definitions() {
			if err := registry.add(d, fallback, d.Function.Name); err != nil {
				return nil, err
			}
		}
	}
	for _, registration := range registrations {
		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		session, err := registration.Connect(connectCtx)
		cancel()
		if err != nil {
			if session != nil {
				session.Close()
			}
			return nil, fmt.Errorf("MCP server %s: %w", registration.Name, err)
		}
		if session == nil {
			return nil, fmt.Errorf("MCP server %s returned no session", registration.Name)
		}
		registry.sessions = append(registry.sessions, session)
		for _, d := range session.Definitions() {
			original := d.Function.Name
			d.Function.Name = registration.Name + "__" + original
			d.Function.Description = "[MCP server: " + registration.Name + "] " + d.Function.Description
			if err := registry.add(d, session, original); err != nil {
				return nil, err
			}
		}
	}
	success = true
	return registry, nil
}
func (r *Registry) add(d agent.ToolDefinition, executor agent.ToolExecutor, original string) error {
	if d.Type != "function" || !exposedName.MatchString(d.Function.Name) {
		return fmt.Errorf("invalid or too long exposed tool name: %s", d.Function.Name)
	}
	if _, exists := r.routes[d.Function.Name]; exists {
		return fmt.Errorf("duplicate exposed tool name: %s", d.Function.Name)
	}
	d.Function.Parameters = append([]byte(nil), d.Function.Parameters...)
	r.definitions = append(r.definitions, d)
	r.routes[d.Function.Name] = route{executor: executor, original: original}
	return nil
}
func (r *Registry) Definitions() []agent.ToolDefinition {
	out := append([]agent.ToolDefinition(nil), r.definitions...)
	for i := range out {
		out[i].Function.Parameters = append([]byte(nil), out[i].Function.Parameters...)
	}
	return out
}
func (r *Registry) Execute(ctx context.Context, name, args string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r.closed.Load() {
		return "", errors.New("MCP registry is closed")
	}
	route, ok := r.routes[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	value, err := route.executor.Execute(ctx, route.original, args)
	if err != nil {
		return "", fmt.Errorf("tool %s: %w", name, err)
	}
	return value, nil
}
func (r *Registry) Close() error {
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		for i := len(r.sessions) - 1; i >= 0; i-- {
			r.closeError = errors.Join(r.closeError, r.sessions[i].Close())
		}
	})
	return r.closeError
}
