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
	"yargpt/internal/shellenv"
	"yargpt/web"
)

func main() {
	exit(run(os.Args[1:]))
}

// exit ends the process: with status 0 when err is nil, else with 1 after
// printing err.
func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "yargpt:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// options holds the parsed flags and what prepare derives from them.
type options struct {
	addr    string
	dataDir string
	settings
	loginEnv   bool
	logFile    string
	tray       bool
	set        map[string]bool // flags given on the command line
	claudePath string
}

func run(args []string) error {
	o, log, err := setUp(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveUntil := func(ctx context.Context) error { return serve(ctx, o, log) }
	if o.tray {
		return runWithTray(ctx, serveUntil)
	}
	return serveUntil(ctx)
}

// setUp parses the flags and prepares the process: the log file, the
// environment, the config file and the checks.
func setUp(args []string) (options, *slog.Logger, error) {
	o, err := parseFlags(args)
	if err != nil {
		return o, nil, err
	}
	if o.logFile != "" {
		if err := redirectToFile(int(os.Stderr.Fd()), o.logFile); err != nil {
			return o, nil, err
		}
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if o.loginEnv {
		if err := useLoginShellEnv(); err != nil {
			log.Warn("keeping the environment that the server started with", "err", err)
		}
	}
	return o, log, prepare(&o)
}

// useLoginShellEnv replaces the environment with the one of the user's login
// shell, so that Claude Code and the tools of bots are on PATH.
func useLoginShellEnv() error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh" // the default login shell of macOS
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env, err := shellenv.Load(ctx, shell)
	if err != nil {
		return err
	}
	for name, value := range env {
		// Setenv fails only on a name that is empty or holds "=", which Load never returns.
		_ = os.Setenv(name, value)
	}
	return nil
}

func parseFlags(args []string) (options, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return options{}, fmt.Errorf("find home directory: %w", err)
	}
	o := options{set: map[string]bool{}}
	fs := flag.NewFlagSet("yargpt", flag.ExitOnError)
	fs.StringVar(&o.addr, "addr", "127.0.0.1:7777", "loopback address to listen on")
	fs.StringVar(&o.dataDir, "data", filepath.Join(home, ".yargpt"), "directory for the database, bot workspaces and skills")
	fs.StringVar(&o.Claude, "claude", "claude", "path to the Claude Code CLI")
	fs.StringVar(&o.Model, "model", "", "model alias for every bot (default: Claude Code's default)")
	fs.IntVar(&o.MaxParallel, "max-parallel", 3, "turns that may run at once across all bots")
	fs.BoolVar(&o.loginEnv, "login-env", false, "take the environment, such as PATH, from the user's login shell (launchd starts the server with a minimal PATH)")
	fs.BoolVar(&o.tray, "tray", false, "show a menu-bar icon whose Exit server item stops the server (macOS)")
	fs.StringVar(&o.logFile, "log", "", "append the log and all other error output to this file; ~/ is the home directory")
	// ExitOnError: Parse exits on -h or a bad flag, as flag.Parse does.
	_ = fs.Parse(args)
	fs.Visit(func(f *flag.Flag) { o.set[f.Name] = true })
	return o, nil
}

// prepare applies the config file, checks the address and finds Claude Code.
func prepare(o *options) error {
	if err := applyConfigFile(filepath.Join(o.dataDir, "config.json"), o.set, &o.settings); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(o.addr)
	if err != nil {
		return fmt.Errorf("parse -addr %q: %w", o.addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("-addr %q is not a loopback address; YarGPT has no authentication and must not listen on the network", o.addr)
	}
	if o.claudePath, err = exec.LookPath(o.Claude); err != nil {
		return fmt.Errorf("find Claude Code CLI %q: %w", o.Claude, err)
	}
	if err := os.MkdirAll(o.dataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	return nil
}

// serve runs the server until ctx ends, then shuts it down.
func serve(ctx context.Context, o options, log *slog.Logger) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	// Listen first: a second server on the same address must fail before
	// RecoverAfterRestart marks the running turns of the first as interrupted.
	// The runner also needs the actual port for the OAuth redirect URI.
	// Not ctx: a signal during startup must still reach the clean shutdown below.
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", o.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", o.addr, err)
	}
	defer ln.Close()
	conn, err := db.Open(ctx, filepath.Join(o.dataDir, "yargpt.db"))
	if err != nil {
		return err
	}
	defer conn.Close()
	svc, err := core.New(conn, o.dataDir, time.Local)
	if err != nil {
		return err
	}
	if err := svc.RecoverAfterRestart(ctx); err != nil {
		return err
	}

	base := "http://" + ln.Addr().String()
	runner := agent.NewRunner(svc, agent.Config{ClaudePath: o.claudePath, MCPURL: base + "/mcp", OAuthCallbackURL: base + "/oauth/callback", Model: o.Model, MaxParallel: o.MaxParallel}, log)
	ui, err := web.Assets()
	if err != nil {
		log.Warn("web UI not available", "err", err)
		ui = nil
	}
	srv := &http.Server{
		Addr:              o.addr,
		Handler:           httpapi.New(svc, runner, ui, log),
		ReadHeaderTimeout: 10 * time.Second,
		// Requests end with the server: event streams and pending approvals stop on shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	runnerDone := make(chan struct{})
	go func() { runner.Run(ctx); close(runnerDone) }()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	log.Info("YarGPT is running", "url", base, "data", o.dataDir, "claude", o.claudePath)

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			stop()
			<-runnerDone
			return fmt.Errorf("serve %s: %w", o.addr, err)
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
