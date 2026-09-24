package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/gitmcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func run() error {
	repo := flag.String("repo", "", "absolute path to a trusted working clone (required)")
	listen := flag.String("listen", "127.0.0.1:8080", "loopback HTTP address; expose via SSH tunnel or HTTPS reverse proxy")
	stdio := flag.Bool("stdio", false, "serve MCP over stdin/stdout instead of HTTP")
	dataDir := flag.String("data-dir", "", "persistent scheduler directory outside the clone; default: user config/git-mcp/repository-hash")
	flag.Parse()
	if *repo == "" || flag.NArg() != 0 {
		return fmt.Errorf("usage: git-mcp --repo /srv/repo [--listen 127.0.0.1:8080 | --stdio]")
	}
	s, err := gitmcp.NewServer(*repo)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *dataDir == "" {
		base, e := os.UserConfigDir()
		if e != nil {
			return e
		}
		root, e := filepath.Abs(*repo)
		if e != nil {
			return e
		}
		root, e = filepath.EvalSymlinks(root)
		if e != nil {
			return e
		}
		hash := sha256.Sum256([]byte(root))
		*dataDir = filepath.Join(base, "git-mcp", fmt.Sprintf("%x", hash[:8]))
	}
	scheduler, err := gitmcp.OpenScheduler(*repo, *dataDir, func(report gitmcp.Report) { log.Printf("[git-summary] %s", report.Summary) })
	if err != nil {
		return err
	}
	defer scheduler.Close()
	scheduler.Register(s)
	if *stdio {
		return supervise(ctx, scheduler, func(ctx context.Context) error { return s.Run(ctx, &mcp.StdioTransport{}) })
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("HTTP must listen on a loopback IP; use SSH or an HTTPS reverse proxy")
	}
	handler, err := gitmcp.HTTPHandler(s, os.Getenv("GIT_MCP_TOKEN"))
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	return supervise(ctx, scheduler, func(ctx context.Context) error {
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
		}()
		log.Printf("Git MCP listening on %s/mcp (Git read-only, persistent scheduler enabled)", listener.Addr())
		err := srv.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	})
}

// Either component failing stops both; no scheduler errors disappear in a goroutine.
func supervise(ctx context.Context, scheduler *gitmcp.Scheduler, serve func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- scheduler.Run(ctx) }()
	go func() { results <- serve(ctx) }()
	first := <-results
	cancel()
	second := <-results
	if first != nil {
		return first
	}
	return second
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
