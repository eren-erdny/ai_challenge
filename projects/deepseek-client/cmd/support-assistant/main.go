package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/mcptools"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/support"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	crm := flag.String("crm", "", "trusted JSON users/tickets file")
	user := flag.String("user", "U-100", "operator-bound user ID")
	docs := flag.String("docs", "", "FAQ Markdown directory")
	state := flag.String("state", ".support-assistant", "private state directory")
	listen := flag.String("listen", "127.0.0.1:8766", "loopback listen address")
	budget := flag.Float64("budget-usd", 0, "explicit shared limit, at most 0.20")
	stdio := flag.Bool("mcp-stdio", false, "run only the bound CRM MCP server")
	flag.Parse()
	if *crm == "" {
		return errors.New("--crm required")
	}
	db, e := support.LoadDatabase(*crm)
	if e != nil {
		return errors.New("invalid CRM JSON")
	}
	server, e := support.NewCRM(db, *user)
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *stdio {
		return server.Run(ctx, &mcp.StdioTransport{})
	}
	host, _, e := net.SplitHostPort(*listen)
	if e != nil || host != "127.0.0.1" {
		return errors.New("listen must use numeric 127.0.0.1")
	}
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		return errors.New("DEEPSEEK_API_KEY required")
	}
	corpus, e := support.LoadCorpus(*docs)
	if e != nil {
		return e
	}
	ledger, e := support.OpenBudget(filepath.Join(*state, "budget.json"), *budget)
	if e != nil {
		return e
	}
	defer ledger.Close()
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	child := exec.Command(executable, "--mcp-stdio", "--crm", *crm, "--user", *user)
	child.Env = []string{}
	// Keep process launch essentials; the MCP child does not receive provider credentials.
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP", "HOME", "USERPROFILE"} {
		if v, ok := os.LookupEnv(name); ok {
			child.Env = append(child.Env, name+"="+v)
		}
	}
	client, e := mcptools.Connect(ctx, &mcp.CommandTransport{Command: child}, nil)
	if e != nil {
		return e
	}
	defer client.Close()
	provider := llm.New(&http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, map[string]string{"deepseek": os.Getenv("DEEPSEEK_API_KEY")})
	service := &support.Service{CRM: client, Corpus: corpus, Client: provider, Budget: ledger, BoundUser: *user}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		return e
	}
	token := hex.EncodeToString(secret)
	if e = os.WriteFile(filepath.Join(*state, "access-token.txt"), []byte(token), 0600); e != nil {
		return e
	}
	httpServer := &http.Server{Addr: *listen, Handler: support.Handler(service, token, *listen, stop), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 110 * time.Second, IdleTimeout: 30 * time.Second}
	listener, e := net.Listen("tcp", *listen)
	if e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	fmt.Printf("Support ready: http://%s | user %s | FAQ chunks %d | MCP tools %d\n", *listen, *user, len(corpus.Chunks), len(client.Definitions()))
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdown)
	case e := <-done:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	}
}
