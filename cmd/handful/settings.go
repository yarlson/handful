package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"handful/internal/agent"
	"handful/internal/config"
	"handful/internal/core"
	"handful/internal/httpapi"
)

// launchdLabel is the label of the server's LaunchAgent. launchd sets
// XPC_SERVICE_NAME to it in the job's environment; a server started from a
// terminal has no such variable, or "0".
const launchdLabel = "dev.yarlson.handful.server"

// restartExitCode is the status of a server that ends to restart. It is not
// 0, so launchd starts the job again (KeepAlive.SuccessfulExit is false).
const restartExitCode = 75

// errRestart ends a server that restarts; exit turns it into restartExitCode.
var errRestart = errors.New("restarting: launchd starts the server again")

// underLaunchd reports whether the server runs as the app's LaunchAgent,
// which launchd starts again when it ends. The -tray flag alone is not
// proof: anyone can pass it. The job's children inherit XPC_SERVICE_NAME,
// so the parent must be launchd (pid 1) too.
func underLaunchd(getenv func(string) string, getppid func() int) bool {
	return getppid() == 1 && getenv("XPC_SERVICE_NAME") == launchdLabel
}

// startupCheck returns a check of whether a server started again now would
// start with the config file: the startup checks, from the flags alone,
// as a restart starts. It returns the config key to blame ("" for the
// whole file) and the startup error, or nil.
func (o options) startupCheck(getenv func(string) string, lookPath func(string) (string, error)) func() (string, error) {
	return func() (string, error) {
		c := o
		c.settings = o.flagSettings
		err := c.loadSettings(getenv, lookPath)
		return startupKey(err), err
	}
}

// runningSettings are the values the server started with, by key.
func runningSettings(o options) map[string]any {
	return map[string]any{
		"model": o.Model, "effort": o.Effort, "maxParallel": o.MaxParallel, "claude": o.Claude,
		"updates.autoCheck": o.Updates.AutoCheck, "updates.drainMinutes": o.Updates.DrainMinutes, "updateCheck": o.Updates.UpdateCheck,
		"phone.enabled": o.Phone.Enabled, "phone.addr": o.Phone.Addr, "phone.hosts": o.Phone.Hosts, "phone.proxy": o.Phone.Proxy,
		"phone.cloudflare.teamDomain": o.Phone.TeamDomain, "phone.cloudflare.aud": o.Phone.AUD,
	}
}

// settingFlags names, by key, the flags that decide a setting for this run,
// with their values: "-effort high".
func settingFlags(o options) map[string]string {
	running := runningSettings(o)
	flags := map[string]string{}
	for _, key := range config.Keys {
		if name := config.Flag(key); name != "" && o.set[name] {
			flags[key] = fmt.Sprintf("-%s %v", name, running[key])
		}
	}
	return flags
}

// runtimeValues are the command-line values the page shows but cannot
// change. A flag that the LaunchAgent passes comes from the app.
func runtimeValues(o options, launchd bool) []httpapi.RuntimeValue {
	source := func(name string) string {
		if !o.set[name] {
			return httpapi.SourceDefault
		}
		if launchd {
			return httpapi.SourceApp
		}
		return httpapi.SourceFlag
	}
	return []httpapi.RuntimeValue{
		{Key: "addr", Value: o.addr, Source: source("addr")},
		{Key: "data", Value: o.dataDir, Source: source("data")},
		{Key: "log", Value: o.logFile, Source: source("log")},
		{Key: "login-env", Value: o.loginEnv, Source: source("login-env")},
		{Key: "tray", Value: o.tray, Source: source("tray")},
	}
}

// buildKind tells the server at exe apart: a source build, or the server in
// Handful.app, signed with a Developer ID (which updates itself) or built
// locally. sign returns what codesign prints about the bundle's signature.
func buildKind(exe string, sign func(bundle string) string) string {
	i := strings.Index(exe, ".app/Contents/Resources/")
	if i < 0 {
		return httpapi.BuildSource
	}
	if strings.Contains(sign(exe[:i+len(".app")]), "Authority=Developer ID Application") {
		return httpapi.BuildApp
	}
	return httpapi.BuildAdhoc
}

// codesignInfo returns what codesign prints about bundle's signature, or ""
// when it fails or takes longer than a few seconds.
func codesignInfo(bundle string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := codesignCommand(ctx, bundle).CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// codesignCommand describes bundle's signature. Only --verbose=2 prints the
// Authority lines that buildKind reads; the absolute path does not depend on
// the PATH that launchd gives the server.
func codesignCommand(ctx context.Context, bundle string) *exec.Cmd {
	return exec.CommandContext(ctx, "/usr/bin/codesign", "-dv", "--verbose=2", bundle)
}

// settingsOf returns what the Settings page needs of the server, and the
// restarter of a server under launchd (nil otherwise), which stop ends.
func (o options) settingsOf(stop context.CancelFunc, getenv func(string) string, getppid func() int, publish func(string), hold func(bool), log *slog.Logger) (*restarter, httpapi.Settings) {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	launchd := underLaunchd(getenv, getppid)
	check := o.startupCheck(getenv, exec.LookPath)
	s := httpapi.Settings{
		Path: filepath.Join(o.dataDir, "config.json"), Running: runningSettings(o), Flags: settingFlags(o),
		Runtime: runtimeValues(o, launchd), Build: buildKind(exe, codesignInfo), LookPath: exec.LookPath, StartupCheck: check,
	}
	if !launchd {
		return nil, s
	}
	r := newRestarter(stop, check, o.drainLimit, publish, hold, log)
	s.Restart, s.CancelRestart, s.RestartState = r.restart, r.cancel, r.state
	return r, s
}

// drainLimit is how long an idle restart waits for the work to end: the
// file's updates.drainMinutes when it loads, as the app reads it at each
// update, else the value the server started with.
func (o options) drainLimit() time.Duration {
	minutes := o.Updates.DrainMinutes
	if f, err := config.Load(filepath.Join(o.dataDir, "config.json")); err == nil {
		if v, ok := f.Get("updates.drainMinutes"); ok {
			minutes = min(max(v.(int), config.MinDrainMinutes), config.MaxDrainMinutes)
		}
	}
	return time.Duration(minutes) * time.Minute
}

// watch starts waitIdle for the runner's work, until ctx ends, and returns
// a func that waits for it to stop. A nil restarter watches nothing.
func (r *restarter) watch(ctx context.Context, runner *agent.Runner) (wait func()) {
	if r == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		r.waitIdle(ctx, tick.C, func() int { return len(runner.Busy(ctx)) })
	}()
	return func() { <-done }
}

// result is errRestart when the server ended to restart, else nil.
func (r *restarter) result() error {
	if r != nil && r.asked.Load() {
		return errRestart
	}
	return nil
}

// restarter ends the server so that launchd starts it again.
type restarter struct {
	stop    context.CancelFunc
	check   func() (string, error) // why a server started again would not start
	limit   func() time.Duration   // how long an idle restart waits for the work to end
	clock   func() time.Time
	publish func(state string) // tells the clients of a change, as core.Service.PublishRestart
	hold    func(on bool)      // holds drain mode while a restart waits, as core.Service.HoldDrain
	log     *slog.Logger
	asked   atomic.Bool // the server ends to restart

	mu      sync.Mutex
	wait    *httpapi.RestartWait // the idle restart that waits; nil when none
	expired bool                 // the last idle restart passed its limit
}

func newRestarter(stop context.CancelFunc, check func() (string, error), limit func() time.Duration, publish func(string), hold func(bool), log *slog.Logger) *restarter {
	return &restarter{stop: stop, check: check, limit: limit, clock: time.Now, publish: publish, hold: hold, log: log}
}

// state reports the idle restart that waits, and whether the last expired.
func (r *restarter) state() (*httpapi.RestartWait, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.wait == nil {
		return nil, r.expired
	}
	w := *r.wait
	return &w, r.expired
}

// end clears the waiting restart, if one waits, with state, lets go of its
// hold on drain mode, and tells the clients. A restart now also clears an
// expiry that no restart waits for. The server that ends keeps its hold:
// no turn starts before it exits.
func (r *restarter) end(state string) {
	r.mu.Lock()
	waited := r.wait != nil
	if waited || state == core.RestartRestarting {
		r.wait = nil
		r.expired = state == core.RestartExpired
	}
	r.mu.Unlock()
	if !waited {
		return
	}
	if state != core.RestartRestarting {
		r.hold(false)
	}
	r.publish(state)
}

// cancel drops a pending idle restart and its hold on drain mode.
func (r *restarter) cancel() { r.end(core.RestartCancelled) }

// restart ends the server now, or asks waitIdle to end it once no work runs
// or give up at the limit. Asking again while one waits keeps its deadline.
func (r *restarter) restart(idle bool) {
	if !idle {
		r.end(core.RestartRestarting)
		r.exit()
		return
	}
	r.mu.Lock()
	started := r.wait == nil
	if started {
		now := r.clock()
		r.wait = &httpapi.RestartWait{Since: now.UnixMilli(), Deadline: now.Add(r.limit()).UnixMilli()}
		r.expired = false
	}
	r.mu.Unlock()
	if started {
		r.hold(true)
		r.publish(core.RestartWaiting)
	}
}

func (r *restarter) exit() {
	r.asked.Store(true)
	r.stop()
}

// waitIdle ends the server once an idle restart is pending and busy reports
// no running work, checked at each tick. It returns when ctx ends or the
// server ends.
func (r *restarter) waitIdle(ctx context.Context, tick <-chan time.Time, busy func() int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			if r.idleTick(busy) {
				return
			}
		}
	}
}

// idleTick handles one tick of waitIdle and reports whether the server ends.
// Work still running at the deadline gives the restart up. The file may have
// changed while the work ran: a server that would not start with it gives
// the restart up too. Either way the restart lets go of drain mode, which
// stays on while the updater holds it.
func (r *restarter) idleTick(busy func() int) bool {
	wait, _ := r.state()
	if wait == nil {
		return false
	}
	if busy() != 0 {
		if r.clock().UnixMilli() >= wait.Deadline {
			r.end(core.RestartExpired)
			r.log.Warn("restart given up: work still ran at the time limit")
		}
		return false
	}
	if _, err := r.check(); err != nil {
		r.end(core.RestartCancelled)
		r.log.Warn("restart given up: the server would not start with its config file", "err", err)
		return false
	}
	r.end(core.RestartRestarting)
	r.exit()
	return true
}
