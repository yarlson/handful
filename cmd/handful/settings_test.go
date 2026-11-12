package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"handful/internal/config"

	"handful/internal/core"
	"handful/internal/db"
	"handful/internal/httpapi"
)

func TestUnderLaunchdNeedsTheAgentsServiceNameAndLaunchdAsParent(t *testing.T) {
	for _, c := range []struct {
		name string
		ppid int
		want bool
	}{
		{"dev.yarlson.handful.server", 1, true},
		// A child of the server, such as a bot's command, inherits the name.
		{"dev.yarlson.handful.server", 4242, false},
		{"0", 1, false},
		{"", 1, false},
		{"com.apple.Terminal", 1, false},
	} {
		getenv := func(k string) string {
			if k == "XPC_SERVICE_NAME" {
				return c.name
			}
			return ""
		}
		if got := underLaunchd(getenv, func() int { return c.ppid }); got != c.want {
			t.Errorf("XPC_SERVICE_NAME=%q, parent %d: underLaunchd = %v; want %v", c.name, c.ppid, got, c.want)
		}
	}
}

// writeConfig writes body as dir's config.json.
func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStartupCheckRunsTheStartupChecksFromTheFlags(t *testing.T) {
	dir := t.TempDir()
	lookPath := func(p string) (string, error) {
		if p == "/missing/claude" {
			return "", errors.New("not found")
		}
		return "/bin/" + p, nil
	}
	flags := settings{Claude: "claude", MaxParallel: 3}
	// What the server runs with now doesn't count: a restart starts from the flags.
	o := options{dataDir: dir, set: map[string]bool{}, flagSettings: flags, Claude: "/missing/claude", Effort: "ultra"}
	check := o.startupCheck(func(string) string { return "" }, lookPath)
	for _, c := range []struct{ file, key, err string }{
		{`{"effort": "max", "claude": "claude"}`, "", ""},
		{`{"maxParallel": 4,}`, "", "invalid character"},
		{`{"model": "bad model"}`, "model", `config.json: "model": A model ID can't contain spaces or start with '-'.`},
		{`{"effort": "ultra"}`, "effort", `config.json: "effort" must be one of low, medium, high, xhigh, max`},
		{`{"claude": "/missing/claude"}`, "claude", `find Claude Code CLI "/missing/claude": not found`},
		{`{"phone": {"addr": "0.0.0.0:7778"}}`, "", `"phone.addr": phone.addr must be a loopback address`},
		{`{"phone": {"enabled": true}}`, "phone.hosts", `"phone.hosts": Phone access needs the address your phone opens`},
		{`{"phone": {"enabled": true, "hosts": ["phone.example.com"]}}`, "phone.cloudflare.teamDomain", "team domain"},
		{`{"phone": {"enabled": true, "hosts": ["phone.example.com"], "proxy": "tailscale"}}`, "", ""},
		{`{"phone": {"hosts": ["x.trycloudflare.com"]}}`, "", "Quick tunnels"},
	} {
		writeConfig(t, dir, c.file)
		key, err := check()
		if key != c.key || (c.err == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.err) {
			t.Errorf("startup check of %s = %q, %v; want %q, %q", c.file, key, err, c.key, c.err)
		}
	}
	// A flag wins at startup, so a bad file value under it doesn't stop the server.
	o.set = map[string]bool{"effort": true}
	o.flagSettings.Effort = "high"
	writeConfig(t, dir, `{"effort": "ultra"}`)
	if key, err := o.startupCheck(func(string) string { return "" }, lookPath)(); err != nil {
		t.Fatalf("startup check under -effort = %q, %v; want the flag to win", key, err)
	}
}

func TestSettingsOfTheServerNameTheFlagsAndTheirSources(t *testing.T) {
	o := options{
		addr: "127.0.0.1:7777", dataDir: "/data", loginEnv: true, logFile: "~/server.log", tray: true,
		Claude: "claude", Model: "opus", Effort: "high", MaxParallel: 5, Updates: updateSettings{AutoCheck: true, DrainMinutes: 45, UpdateCheck: false},
		Phone: config.PhoneSettings{Enabled: true, Addr: "127.0.0.1:7900", Hosts: []string{"phone.example.com"}, Proxy: "cloudflare", TeamDomain: "you.cloudflareaccess.com", AUD: "abcdef0123456789"},
		set:   map[string]bool{"effort": true, "max-parallel": true, "tray": true, "login-env": true, "log": true},
	}
	running := map[string]any{
		"model": "opus", "effort": "high", "maxParallel": 5, "claude": "claude", "updates.autoCheck": true, "updates.drainMinutes": 45, "updateCheck": false,
		"phone.enabled": true, "phone.addr": "127.0.0.1:7900", "phone.hosts": []string{"phone.example.com"}, "phone.proxy": "cloudflare",
		"phone.cloudflare.teamDomain": "you.cloudflareaccess.com", "phone.cloudflare.aud": "abcdef0123456789",
	}
	if got := runningSettings(o); !reflect.DeepEqual(got, running) {
		t.Fatalf("running = %v; want %v", got, running)
	}
	if got, want := settingFlags(o), map[string]string{"effort": "-effort high", "maxParallel": "-max-parallel 5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("flags = %v; want %v", got, want)
	}
	app := []httpapi.RuntimeValue{
		{Key: "addr", Value: "127.0.0.1:7777", Source: "default"},
		{Key: "data", Value: "/data", Source: "default"},
		{Key: "log", Value: "~/server.log", Source: "app"},
		{Key: "login-env", Value: true, Source: "app"},
		{Key: "tray", Value: true, Source: "app"},
	}
	if got := runtimeValues(o, true); !reflect.DeepEqual(got, app) {
		t.Fatalf("runtime under launchd = %+v; want %+v", got, app)
	}
	if got := runtimeValues(o, false); got[2].Source != "flag" || got[0].Source != "default" {
		t.Fatalf("runtime from a terminal = %+v; want given flags from the command line", got)
	}
}

func TestSettingsOfAServerFromATerminalOfferNoRestart(t *testing.T) {
	o := options{dataDir: "/data", set: map[string]bool{}}
	log := slog.New(slog.DiscardHandler)
	r, s := o.settingsOf(func() {}, func(string) string { return "" }, func() int { return 1 }, func(string) {}, func(bool) {}, log)
	if r != nil || s.Restart != nil || s.CancelRestart != nil || s.RestartState != nil || s.Path != "/data/config.json" || s.LookPath == nil || s.StartupCheck == nil || s.Build != httpapi.BuildSource {
		t.Fatalf("settings from a terminal = %v, %+v; want no restarter, and a startup check", r, s)
	}
	stopped := false
	r, s = o.settingsOf(func() { stopped = true }, func(string) string { return launchdLabel }, func() int { return 1 }, func(string) {}, func(bool) {}, log)
	if r == nil || s.Restart == nil || s.CancelRestart == nil || s.RestartState == nil {
		t.Fatal("settings under launchd: want a restarter")
	}
	s.Restart(false)
	if !stopped || !errors.Is(r.result(), errRestart) {
		t.Fatalf("restart now: stopped %v, result %v; want the server to end to restart", stopped, r.result())
	}
}

func TestBuildKindTellsTheSignedAppFromALocalOneAndASourceBuild(t *testing.T) {
	signed := func(bundle string) string {
		if bundle != "/Applications/Handful.app" {
			t.Fatalf("codesign of %q; want the bundle", bundle)
		}
		return "Authority=Developer ID Application: Someone (TEAM)\nAuthority=Apple Root CA\n"
	}
	adhoc := func(string) string { return "Signature=adhoc\n" }
	exe := "/Applications/Handful.app/Contents/Resources/handful"
	if got := buildKind(exe, signed); got != httpapi.BuildApp {
		t.Errorf("signed app = %q; want app", got)
	}
	if got := buildKind(exe, adhoc); got != httpapi.BuildAdhoc {
		t.Errorf("ad-hoc app = %q; want adhoc", got)
	}
	if got := buildKind("/src/handful/bin/handful", func(string) string { t.Fatal("codesign ran for a source build"); return "" }); got != httpapi.BuildSource {
		t.Errorf("source build = %q; want source", got)
	}
	if got := codesignInfo("/no/such/bundle.app"); got != "" {
		t.Errorf("codesign of a missing bundle = %q; want empty", got)
	}
}

// Plain codesign -dv prints no Authority lines, so a Developer ID app looked
// built locally.
func TestCodesignAsksForTheAuthorityLines(t *testing.T) {
	cmd := codesignCommand(context.Background(), "/Applications/Handful.app")
	want := []string{"/usr/bin/codesign", "-dv", "--verbose=2", "/Applications/Handful.app"}
	if cmd.Path != want[0] || !slices.Equal(cmd.Args, want) {
		t.Fatalf("codesign command = %s %q; want %q", cmd.Path, cmd.Args, want)
	}
}

// restartWatch runs waitIdle with a tick that send drives. A send returns
// once waitIdle took the tick, so the tick before it is fully handled. held
// is the restart's own hold on drain mode.
func restartWatch(t *testing.T, held *atomic.Bool, busy *atomic.Int32) (r *restarter, send func(), stopped, done chan struct{}) {
	t.Helper()
	return restartWatchWith(t, held, busy, func() (string, error) { return "", nil })
}

// restartWatchWith is restartWatch with a startup check.
func restartWatchWith(t *testing.T, held *atomic.Bool, busy *atomic.Int32, check func() (string, error)) (r *restarter, send func(), stopped, done chan struct{}) {
	t.Helper()
	stopped = make(chan struct{}, 1)
	r = newRestarter(func() { stopped <- struct{}{} }, check, func() time.Duration { return 30 * time.Minute }, func(string) {}, held.Store, slog.New(slog.DiscardHandler))
	tick := make(chan time.Time)
	done = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() { cancel(); <-done })
	go func() {
		defer close(done)
		r.waitIdle(ctx, tick, func() int { return int(busy.Load()) })
	}()
	send = func() {
		select {
		case tick <- time.Time{}:
		case <-done:
		}
	}
	return r, send, stopped, done
}

func TestAnIdleRestartHoldsDrainModeAndWaitsUntilNoWorkRuns(t *testing.T) {
	var held atomic.Bool
	var busy atomic.Int32
	busy.Store(0)
	r, send, stopped, done := restartWatch(t, &held, &busy)
	send() // nothing asked yet: no restart, even with no work
	if r.result() != nil || held.Load() {
		t.Fatal("restarted or held drain mode with no restart asked")
	}
	busy.Store(2)
	r.restart(true)
	r.restart(true) // asking twice is one request
	if !held.Load() {
		t.Fatal("an idle restart does not hold drain mode")
	}
	send()
	send()
	if r.result() != nil {
		t.Fatal("restarted while work runs")
	}
	busy.Store(0)
	send()
	<-stopped
	<-done
	if !errors.Is(r.result(), errRestart) || !held.Load() {
		t.Fatalf("result = %v, held %v; want errRestart, with drain mode still held so no turn starts before the exit", r.result(), held.Load())
	}
}

func TestAnIdleRestartGivesUpWhenTheFileWentBadMeanwhile(t *testing.T) {
	var held atomic.Bool
	var busy atomic.Int32
	var bad atomic.Bool
	busy.Store(1)
	r, send, stopped, _ := restartWatchWith(t, &held, &busy, func() (string, error) {
		if bad.Load() {
			return "effort", errors.New("bad effort")
		}
		return "", nil
	})
	r.restart(true)
	send()
	bad.Store(true) // a hand edit while the work runs
	busy.Store(0)
	send()
	send()
	select {
	case <-stopped:
		t.Fatal("restarted into a file the server would not start with")
	default:
	}
	if wait, _ := r.state(); r.result() != nil || wait != nil || held.Load() {
		t.Fatalf("result %v, waiting %v, held %v; want the restart given up and its hold let go", r.result(), wait, held.Load())
	}
}

func TestCancelDropsAWaitingRestartAndItsHold(t *testing.T) {
	var held atomic.Bool
	var busy atomic.Int32
	busy.Store(1)
	r, send, _, _ := restartWatch(t, &held, &busy)
	r.restart(true)
	r.cancel()
	busy.Store(0)
	send()
	send()
	if r.result() != nil || held.Load() {
		t.Fatalf("result %v, held %v after cancel; want no restart and the hold let go", r.result(), held.Load())
	}
}

func TestANilRestarterWatchesNothing(t *testing.T) {
	var r *restarter
	r.watch(t.Context(), nil)()
	if r.result() != nil {
		t.Fatal("a nil restarter asked for a restart")
	}
}

func TestAnIdleRestartReportsItsDeadlineAndGivesUpAtIt(t *testing.T) {
	var held atomic.Bool
	busy := func() int { return 1 }
	start := time.UnixMilli(1_000_000)
	clock := start
	var states []string
	stopped := make(chan struct{}, 1)
	r := newRestarter(func() { stopped <- struct{}{} }, func() (string, error) { return "", nil },
		func() time.Duration { return 30 * time.Minute }, func(s string) { states = append(states, s) }, held.Store, slog.New(slog.DiscardHandler))
	r.clock = func() time.Time { return clock }

	r.restart(true)
	clock = start.Add(time.Minute)
	r.restart(true) // asking again keeps the first deadline
	want := httpapi.RestartWait{Since: start.UnixMilli(), Deadline: start.Add(30 * time.Minute).UnixMilli()}
	if wait, expired := r.state(); wait == nil || *wait != want || expired {
		t.Fatalf("state = %v, expired %v; want %+v", wait, expired, want)
	}
	clock = start.Add(30*time.Minute - time.Millisecond)
	if r.idleTick(busy) || !held.Load() {
		t.Fatal("gave up or restarted before the deadline")
	}
	clock = start.Add(30 * time.Minute)
	if r.idleTick(busy) {
		t.Fatal("restarted at the deadline while work runs")
	}
	if wait, expired := r.state(); wait != nil || !expired || held.Load() || r.result() != nil {
		t.Fatalf("after the deadline: waiting %v, expired %v, held %v, result %v; want given up, expired, hold let go", wait, expired, held.Load(), r.result())
	}
	// Cancelling nothing keeps the expiry; the next request clears it.
	r.cancel()
	if _, expired := r.state(); !expired {
		t.Fatal("a cancel with no waiting restart cleared the expiry")
	}
	r.restart(true)
	if _, expired := r.state(); expired {
		t.Fatal("a new restart request kept the old expiry")
	}
	r.cancel()
	if wait, expired := r.state(); wait != nil || expired {
		t.Fatalf("after cancel: waiting %v, expired %v; want neither", wait, expired)
	}
	if want := []string{core.RestartWaiting, core.RestartExpired, core.RestartWaiting, core.RestartCancelled}; !slices.Equal(states, want) {
		t.Fatalf("published states = %q; want %q", states, want)
	}
	select {
	case <-stopped:
		t.Fatal("the server ended")
	default:
	}
}

func TestARestartNowClearsAnExpiryAndSaysItRestarts(t *testing.T) {
	var states []string
	stopped := false
	r := newRestarter(func() { stopped = true }, func() (string, error) { return "", nil },
		func() time.Duration { return time.Minute }, func(s string) { states = append(states, s) }, func(bool) {}, slog.New(slog.DiscardHandler))
	r.mu.Lock()
	r.expired = true
	r.mu.Unlock()
	r.restart(false)
	if _, expired := r.state(); expired || !stopped || len(states) != 0 {
		t.Fatalf("expired %v, stopped %v, states %q; want the expiry cleared and the server ended, with no waiting restart to report", expired, stopped, states)
	}
	r = newRestarter(func() {}, func() (string, error) { return "", nil },
		func() time.Duration { return time.Minute }, func(s string) { states = append(states, s) }, func(bool) {}, slog.New(slog.DiscardHandler))
	r.restart(true)
	if !r.idleTick(func() int { return 0 }) {
		t.Fatal("an idle restart with no work did not restart")
	}
	if want := []string{core.RestartWaiting, core.RestartRestarting}; !slices.Equal(states, want) {
		t.Fatalf("states = %q; want %q", states, want)
	}
}

// The updater and a restart hold drain mode apart: either ending leaves the
// other's hold, so no turn starts while the other still waits.
func TestARestartAndTheUpdaterHoldDrainModeApart(t *testing.T) {
	svc := newServiceForTest(t)
	hold := func(on bool) { svc.HoldDrain(core.DrainRestart, on) }
	r := newRestarter(func() {}, func() (string, error) { return "", nil },
		func() time.Duration { return time.Minute }, func(string) {}, hold, slog.New(slog.DiscardHandler))
	draining := func() bool { on, _ := svc.Draining(); return on }

	r.restart(true)
	svc.SetDraining(true) // the updater asks while the restart waits
	r.cancel()
	if !draining() {
		t.Fatal("cancelling the restart ended drain mode that the updater holds")
	}
	r.restart(true)
	svc.SetDraining(false) // the updater gives up while the restart waits
	if wait, _ := r.state(); !draining() || wait == nil {
		t.Fatalf("draining %v, waiting %v after the updater let go; want the restart to keep waiting and holding", draining(), wait)
	}
	svc.SetDraining(true)
	r.clock = func() time.Time { return time.Now().Add(time.Hour) }
	r.idleTick(func() int { return 1 }) // the restart expires
	if !draining() {
		t.Fatal("an expired restart ended drain mode that the updater holds")
	}
	svc.SetDraining(false)
	if draining() {
		t.Fatal("drain mode still on after both let go")
	}
}

func TestTheDrainLimitComesFreshFromTheFile(t *testing.T) {
	dir := t.TempDir()
	o := options{dataDir: dir, Updates: updateSettings{DrainMinutes: 30}}
	if got := o.drainLimit(); got != 30*time.Minute {
		t.Fatalf("limit with no file = %v; want the running 30m", got)
	}
	for file, want := range map[string]time.Duration{
		`{"updates": {"drainMinutes": 45}}`:  45 * time.Minute,
		`{"updates": {"drainMinutes": 1}}`:   5 * time.Minute,
		`{"updates": {"drainMinutes": 999}}`: 240 * time.Minute,
		`{"updates": {}}`:                    30 * time.Minute,
		`{"maxParallel": 4,}`:                30 * time.Minute,
	} {
		writeConfig(t, dir, file)
		if got := o.drainLimit(); got != want {
			t.Errorf("limit with %s = %v; want %v", file, got, want)
		}
	}
}

// newServiceForTest opens a service on a new database in a temp folder.
func newServiceForTest(t *testing.T) *core.Service {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(t.Context(), filepath.Join(dir, "yargpt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	svc, err := core.New(conn, dir, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}
