// Command handful runs the Handful server: the API, the web UI and the local
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

	"handful/internal/agent"
	"handful/internal/config"
	"handful/internal/core"
	"handful/internal/db"
	"handful/internal/httpapi"
	"handful/internal/move"
	"handful/internal/shellenv"
	"handful/ui"
)

func main() {
	exit(run(os.Args[1:]))
}

// exit ends the process: with status 0 when err is nil, with
// restartExitCode for a restart, else with 1 after printing err.
func exit(err error) {
	if errors.Is(err, errRestart) {
		fmt.Fprintln(os.Stderr, "handful:", err)
		os.Exit(restartExitCode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "handful:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// options holds the parsed flags and what prepare derives from them.
type options struct {
	addr    string
	dataDir string
	settings
	loginEnv bool
	logFile  string
	tray     bool
	set      map[string]bool // flags given on the command line
	// flagSettings are the settings of the flags and defaults alone, before
	// the config file: what a restart starts from.
	flagSettings settings
	claudePath   string
	defaults     core.Defaults
	// mover moves the old data folder, ~/.yargpt, to dataDir at the start; nil with
	// -data, which names the folder to use as it is.
	mover    *move.Mover
	moveBack bool // -move-back: go back to the old app instead of serving
	yes      bool
	// key is Handful's key, which writes to the API need, and keyFile the file that holds it.
	key, keyFile string
}

func run(args []string) error {
	o, log, err := setUp(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if o.moveBack {
		return moveBack(ctx, o)
	}
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
	if o.moveBack {
		return o, log, nil
	}
	if o.loginEnv {
		if err := useLoginShellEnv(); err != nil {
			log.Warn("keeping the environment that the server started with", "err", err)
		}
	}
	o.flagSettings = o.settings
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
	o := options{set: map[string]bool{}, Updates: updateSettings{AutoCheck: config.DefaultAutoCheck, DrainMinutes: config.DefaultDrainMinutes, UpdateCheck: config.DefaultUpdateCheck}}
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&o.addr, "addr", "127.0.0.1:7777", "loopback address to listen on")
	fs.StringVar(&o.dataDir, "data", filepath.Join(home, ".handful"), "directory for the database, bot workspaces and skills; without it, a start moves YarGPT's ~/.yargpt here")
	fs.StringVar(&o.Claude, "claude", config.DefaultClaude, "path to the Claude Code CLI")
	fs.StringVar(&o.Model, "model", "", "model alias for every bot (default: Claude Code's default)")
	fs.StringVar(&o.Effort, "effort", "", "reasoning effort for every bot: low, medium, high, xhigh or max (default: the model's own)")
	fs.IntVar(&o.MaxParallel, "max-parallel", config.DefaultMaxParallel, "turns that may run at once across all bots")
	fs.BoolVar(&o.loginEnv, "login-env", false, "take the environment, such as PATH, from the user's login shell (launchd starts the server with a minimal PATH)")
	fs.BoolVar(&o.tray, "tray", false, "show a menu-bar icon with Check for updates…, which opens the app, and Exit server, which stops the server (macOS)")
	fs.StringVar(&o.logFile, "log", "", "append the log and all other error output to this file; ~/ is the home directory")
	fs.BoolVar(&o.moveBack, "move-back", false, "go back to YarGPT: say what would be lost, and with -yes restore ~/.yargpt as it was at the move")
	fs.BoolVar(&o.yes, "yes", false, "with -move-back, go back")
	// ExitOnError: Parse exits on -h or a bad flag, as flag.Parse does.
	_ = fs.Parse(args)
	fs.Visit(func(f *flag.Flag) { o.set[f.Name] = true })
	if !o.set["data"] {
		addrs := []string{o.addr, appAddr}
		o.mover = &move.Mover{Paths: move.HomePaths(home), Running: func() string { return answering(addrs) }, Now: time.Now, Version: version}
	}
	return o, nil
}

// moveNotice is the welcome notice of a move to the data folder; a server
// with -data shows none.
func (o options) moveNotice() httpapi.MoveNotice {
	if o.mover == nil {
		return httpapi.MoveNotice{}
	}
	dir := o.dataDir
	return httpapi.MoveNotice{
		Show: func() (bool, error) {
			mv, moved, err := move.ReadMoved(dir)
			return moved && !mv.NoticeDone, err
		},
		Close: func() error { return move.CloseNotice(dir) },
	}
}

// movedLine is what a start that moved the data says.
const movedLine = "handful: moved your data from ~/.yargpt to ~/.handful; ~/.yargpt is now a link to it, so leave it in place (a backup is in ~/.handful/backup)"

// moveBack goes back to the old app, as -move-back asks.
func moveBack(ctx context.Context, o options) error {
	if o.mover == nil {
		return errors.New("-move-back works on the default data folder only; leave out -data")
	}
	return o.mover.Back(ctx, o.yes, os.Stdout)
}

// appAddr is where the desktop apps, old and new, run their servers.
const appAddr = "127.0.0.1:7777"

// answering returns the first of addrs on which a server answers, or "".
func answering(addrs []string) string {
	for _, a := range addrs {
		if serverAnswers(a) {
			return a
		}
	}
	return ""
}

// serverAnswers reports whether a server accepts connections on addr.
func serverAnswers(addr string) bool {
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "tcp", addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// prepare moves the old data folder, ~/.yargpt, when it may, applies the config file,
// checks the address and finds Claude Code.
func prepare(o *options) error {
	if o.mover != nil {
		linked, err := o.mover.Data(context.Background())
		if err != nil {
			return err
		}
		if linked {
			fmt.Fprintln(os.Stderr, movedLine)
		}
	}
	if err := move.RenameSkillTools(o.dataDir); err != nil {
		return fmt.Errorf("rename the tools in the skills: %w", err)
	}
	if err := o.loadSettings(os.Getenv, exec.LookPath); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(o.addr)
	if err != nil {
		return fmt.Errorf("parse -addr %q: %w", o.addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("-addr %q is not a loopback address; Handful must not listen on the network", o.addr)
	}
	if err := os.MkdirAll(o.dataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	var widened bool
	if o.key, o.keyFile, widened, err = ensureKey(o.dataDir); err != nil {
		return err
	}
	if widened {
		fmt.Fprintf(os.Stderr, "%s was readable by others; it is now readable by you only.\n", o.keyFile)
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
	svc, err := core.New(conn, o.dataDir, time.Local, core.WithDefaults(o.defaults), core.WithClock(computerClock(log)))
	if err != nil {
		return err
	}
	if err := svc.RecoverAfterRestart(ctx); err != nil {
		return err
	}
	if err := svc.SeedBuilder(ctx); err != nil {
		return err
	}

	base := "http://" + ln.Addr().String()
	runner := agent.NewRunner(svc, agent.Config{ClaudePath: o.claudePath, MCPURL: base + "/mcp", OAuthCallbackURL: base + "/oauth/callback", MaxParallel: o.MaxParallel}, log)
	assets, err := ui.Assets()
	if err != nil {
		log.Warn("web UI not available", "err", err)
		assets = nil
	}
	updates := startUpdateNotice(ctx, o, log)
	restarts, settings := o.settingsOf(stop, os.Getenv, os.Getppid, svc.PublishRestart, func(on bool) { svc.HoldDrain(core.DrainRestart, on) }, log)
	apiOpts := []httpapi.Option{httpapi.WithModelReporter(runner), httpapi.WithUpdates(updates), httpapi.WithSettings(settings), httpapi.WithMoveNotice(o.moveNotice()), httpapi.WithKey(o.key, o.keyFile)}
	// Phone access is off unless config.json or Settings turns it on; then a second listener opens.
	phone := httpapi.NewPhone(o.Phone, log)
	apiOpts = append(apiOpts, httpapi.WithPhone(phone))
	srv := &http.Server{
		Addr:              o.addr,
		Handler:           httpapi.New(svc, runner, assets, log, apiOpts...),
		ReadHeaderTimeout: 10 * time.Second,
		// Requests end with the server: event streams and pending approvals stop on shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	// Before the runner: a taken phone address stops the server with nothing to wait for.
	if err := phone.Start(ctx); err != nil {
		return err
	}
	defer phone.Stop()
	runnerDone := make(chan struct{})
	go func() { runner.Run(ctx); close(runnerDone) }()
	waitRestarts := restarts.watch(ctx, runner)
	defer func() { stop(); waitRestarts() }()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	log.Info("Handful is running", "url", base, "data", o.dataDir, "claude", o.claudePath)

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
	return restarts.result()
}

// startUpdateNotice returns what the API reports about the server's version
// and updates. For a source build that may check, it starts looking for new
// releases until ctx ends.
func startUpdateNotice(ctx context.Context, o options, log *slog.Logger) httpapi.Updates {
	exe, err := os.Executable()
	if err != nil {
		log.Debug("find the server's executable", "err", err)
	}
	u := httpapi.Updates{Version: version, SourceBuild: !inAppBundle(exe)}
	if !checksForUpdates(version, exe, o.Updates.UpdateCheck, os.Getenv, log) {
		return u
	}
	n := newUpdateNotice(o.dataDir, log)
	u.Latest = n.Latest
	go n.run(ctx)
	return u
}
