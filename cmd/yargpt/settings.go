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
	"sync/atomic"
	"time"

	"yargpt/internal/agent"
	"yargpt/internal/config"
	"yargpt/internal/core"
	"yargpt/internal/httpapi"
)

// launchdLabel is the label of the server's LaunchAgent. launchd sets
// XPC_SERVICE_NAME to it in the job's environment; a server started from a
// terminal has no such variable, or "0".
const launchdLabel = "dev.yarlson.yargpt.server"

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
// YarGPT.app, signed with a Developer ID (which updates itself) or built
// locally. sign returns what codesign -dv prints about the bundle.
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

// codesignInfo returns what codesign -dv prints about bundle, or "" when it
// fails or takes longer than a few seconds.
func codesignInfo(bundle string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "codesign", "-dv", bundle).CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// settingsOf returns what the Settings page needs of the server, and the
// restarter of a server under launchd (nil otherwise), which stop ends.
func (o options) settingsOf(stop context.CancelFunc, getenv func(string) string, getppid func() int, log *slog.Logger) (*restarter, httpapi.Settings) {
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
	r := newRestarter(stop, check, log)
	s.Restart, s.CancelRestart = r.restart, r.cancel
	return r, s
}

// watch starts waitIdle for the runner's work, until ctx ends, and returns
// a func that waits for it to stop. A nil restarter watches nothing.
func (r *restarter) watch(ctx context.Context, svc *core.Service, runner *agent.Runner) (wait func()) {
	if r == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		draining := func() bool { on, _ := svc.Draining(); return on }
		r.waitIdle(ctx, tick.C, draining, func() int { return len(runner.Busy(ctx)) }, func() { svc.SetDraining(false) })
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
	log     *slog.Logger
	asked   atomic.Bool // the server ends to restart
	pending atomic.Bool // an idle restart waits for the work to end
}

func newRestarter(stop context.CancelFunc, check func() (string, error), log *slog.Logger) *restarter {
	return &restarter{stop: stop, check: check, log: log}
}

// cancel drops a pending idle restart, as leaving drain mode does.
func (r *restarter) cancel() { r.pending.Store(false) }

// restart ends the server now, or asks waitIdle to end it once no work runs.
func (r *restarter) restart(idle bool) {
	if idle {
		r.pending.Store(true)
		return
	}
	r.now()
}

func (r *restarter) now() {
	r.asked.Store(true)
	r.stop()
}

// waitIdle ends the server once an idle restart is pending and busy reports
// no running work, checked at each tick. Leaving drain mode cancels the
// pending restart. It returns when ctx ends or the server ends.
func (r *restarter) waitIdle(ctx context.Context, tick <-chan time.Time, draining func() bool, busy func() int, undrain func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			if r.idleTick(draining, busy, undrain) {
				return
			}
		}
	}
}

// idleTick handles one tick of waitIdle and reports whether the server ends.
// The file may have changed while the work ran: a server that would not
// start with it gives the restart up and leaves drain mode.
func (r *restarter) idleTick(draining func() bool, busy func() int, undrain func()) bool {
	if !r.pending.Load() {
		return false
	}
	if !draining() {
		r.pending.Store(false)
		return false
	}
	if busy() != 0 {
		return false
	}
	if _, err := r.check(); err != nil {
		r.pending.Store(false)
		undrain()
		r.log.Warn("restart given up: the server would not start with its config file", "err", err)
		return false
	}
	r.now()
	return true
}
