// Command yargpt runs the YarGPT server: the API, the web UI and the local
// Claude Code workers for every bot.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"yargpt/internal/agent"
	"yargpt/internal/core"
	"yargpt/internal/db"
	"yargpt/internal/httpapi"
	"yargpt/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "yargpt:", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find home directory: %w", err)
	}
	addr := flag.String("addr", "127.0.0.1:7777", "loopback address to listen on")
	dataDir := flag.String("data", filepath.Join(home, ".yargpt"), "directory for the database, bot workspaces and skills")
	var cfg settings
	flag.StringVar(&cfg.Claude, "claude", "claude", "path to the Claude Code CLI")
	flag.StringVar(&cfg.Model, "model", "", "model alias for every bot (default: Claude Code's default)")
	flag.IntVar(&cfg.MaxParallel, "max-parallel", 3, "turns that may run at once across all bots")
	flag.Parse()
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if err := applyConfigFile(filepath.Join(*dataDir, "config.json"), set, &cfg); err != nil {
		return err
	}

	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return fmt.Errorf("parse -addr %q: %w", *addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("-addr %q is not a loopback address; YarGPT has no authentication and must not listen on the network", *addr)
	}
	claudePath, err := exec.LookPath(cfg.Claude)
	if err != nil {
		return fmt.Errorf("find Claude Code CLI %q: %w", cfg.Claude, err)
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := db.Open(ctx, filepath.Join(*dataDir, "yargpt.db"))
	if err != nil {
		return err
	}
	defer conn.Close()
	svc, err := core.New(conn, *dataDir, time.Local)
	if err != nil {
		return err
	}
	if err := svc.RecoverAfterRestart(ctx); err != nil {
		return err
	}

	// Listen before the runner starts: the OAuth redirect URI must name the actual port.
	// Not ctx: a signal during startup must still reach the clean shutdown below.
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	base := "http://" + ln.Addr().String()
	runner := agent.NewRunner(svc, agent.Config{ClaudePath: claudePath, MCPURL: base + "/mcp", OAuthCallbackURL: base + "/oauth/callback", Model: cfg.Model, MaxParallel: cfg.MaxParallel}, log)
	ui, err := web.Assets()
	if err != nil {
		log.Warn("web UI not available", "err", err)
		ui = nil
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.New(svc, runner, ui, log),
		ReadHeaderTimeout: 10 * time.Second,
		// Requests end with the server: event streams and pending approvals stop on shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	runnerDone := make(chan struct{})
	go func() { runner.Run(ctx); close(runnerDone) }()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	log.Info("YarGPT is running", "url", base, "data", *dataDir, "claude", claudePath)

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			stop()
			<-runnerDone
			return fmt.Errorf("serve %s: %w", *addr, err)
		}
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Running turns stop with ctx; their MCP calls need the server until then.
	<-runnerDone
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	return nil
}
