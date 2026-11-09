package main

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yargpt/internal/httpapi"
)

func TestOnlyASourceBuildOfAReleaseChecksForUpdates(t *testing.T) {
	none := func(string) string { return "" }
	optOut := func(name string) string {
		if name == "YARGPT_NO_UPDATE_CHECK" {
			return "1"
		}
		return ""
	}
	source := "/Users/me/git/yargpt/bin/yargpt"
	bundled := "/Applications/YarGPT.app/Contents/Resources/yargpt"
	cases := []struct {
		name    string
		version string
		exe     string
		on      bool
		getenv  func(string) string
		want    bool
	}{
		{"source build of a release", "1.3.2", source, true, none, true},
		{"release candidate", "1.4.0-rc.1", source, true, none, true},
		{"inside the app", "1.3.2", bundled, true, none, false},
		{"config turns it off", "1.3.2", source, false, none, false},
		{"environment turns it off", "1.3.2", source, true, optOut, false},
		{"dev build", "dev-5dce9db", source, true, none, false},
		{"plain go build", "dev", source, true, none, false},
	}
	for _, c := range cases {
		if got := checksForUpdates(c.version, c.exe, c.on, c.getenv); got != c.want {
			t.Errorf("%s: checksForUpdates = %v; want %v", c.name, got, c.want)
		}
	}
	if !inAppBundle(bundled) || inAppBundle(source) || inAppBundle("") {
		t.Fatal("inAppBundle misjudges the app, a source build or an unknown path")
	}
}

func TestNewerComparesReleaseVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.4.0", "1.3.2", true},
		{"1.3.2", "1.4.0", false},
		{"1.3.2", "1.3.2", false},
		{"2.0.0", "1.9.9", true},
		{"1.10.0", "1.9.0", true},
		{"1.3.10", "1.3.9", true},
		{"v1.4.0", "1.3.2", true},
		{"1.4.0", "1.4.0-rc.1", true},
		{"1.4.0-rc.1", "1.4.0", false},
		{"1.4.0-rc.2", "1.4.0-rc.1", true},
		{"1.4.0-rc.1", "1.4.0-rc.2", false},
		{"1.4.0-rc.1", "1.4.0-rc.1", false},
		{"1.4", "1.3.2", false},
		{"1.4.0", "dev", false},
		{"1.x.0", "1.3.2", false},
		{"1.-4.0", "1.3.2", false},
		{"", "1.3.2", false},
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v; want %v", c.a, c.b, got, c.want)
		}
	}
}

// releaseFeed is a local stand-in for the latest release page.
type releaseFeed struct {
	requests atomic.Int64
	location atomic.Value // string; "" answers 200 without a redirect
}

func newReleaseFeed(t *testing.T, location string) (*releaseFeed, *httptest.Server) {
	t.Helper()
	f := &releaseFeed{}
	f.location.Store(location)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.Method != http.MethodHead {
			t.Errorf("method = %s; want HEAD", r.Method)
		}
		if loc := f.location.Load().(string); loc != "" {
			http.Redirect(w, r, loc, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

// syncLog is a log buffer that the notice may write from its goroutine.
type syncLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *syncLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newTestNotice(t *testing.T, url string) (*updateNotice, *syncLog, *time.Time) {
	t.Helper()
	logs := &syncLog{}
	now := time.UnixMilli(1_800_000_000_000)
	n := &updateNotice{
		version: "1.3.2", url: url, cachePath: filepath.Join(t.TempDir(), updateCheckFile),
		client: http.DefaultClient, now: func() time.Time { return now },
		log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	return n, logs, &now
}

func TestUpdateNoticeLogsANewerReleaseOnceAndAsksOncePerDay(t *testing.T) {
	feed, srv := newReleaseFeed(t, "/yarlson/yargpt/releases/tag/v1.4.0")
	n, logs, now := newTestNotice(t, srv.URL+"/yarlson/yargpt/releases/latest")
	ctx := context.Background()

	n.check(ctx)
	if got := n.Latest(); got != "1.4.0" {
		t.Fatalf("latest = %q; want 1.4.0", got)
	}
	line := "Update available: v1.4.0 (you have v1.3.2): " + httpapi.LatestReleaseURL
	if strings.Count(logs.String(), line) != 1 || !strings.Contains(logs.String(), "level=INFO") {
		t.Fatalf("log = %q; want one info line %q", logs, line)
	}
	if raw, err := os.ReadFile(n.cachePath); err != nil || string(raw) != `{"checkedAt":1800000000000,"latest":"1.4.0"}` {
		t.Fatalf("cache = %s, %v; want the check's time and release", raw, err)
	}

	*now = now.Add(updateCheckEvery - time.Minute)
	n.check(ctx)
	if got := feed.requests.Load(); got != 1 {
		t.Fatalf("requests within a day = %d; want 1", got)
	}
	if strings.Count(logs.String(), "Update available") != 1 {
		t.Fatalf("log after a second check = %q; want the line once", logs)
	}

	feed.location.Store("/yarlson/yargpt/releases/tag/v1.5.0")
	*now = now.Add(time.Minute)
	n.check(ctx)
	if got := feed.requests.Load(); got != 2 || n.Latest() != "1.5.0" || !strings.Contains(logs.String(), "Update available: v1.5.0") {
		t.Fatalf("after a day: requests %d, latest %q, log %q; want a new check that finds 1.5.0", got, n.Latest(), logs)
	}
}

func TestUpdateNoticeStaysQuietWhenUpToDate(t *testing.T) {
	_, srv := newReleaseFeed(t, "/yarlson/yargpt/releases/tag/v1.3.2")
	n, logs, _ := newTestNotice(t, srv.URL)
	n.check(context.Background())
	if n.Latest() != "" || strings.Contains(logs.String(), "Update available") {
		t.Fatalf("latest %q, log %q; want nothing for the same version", n.Latest(), logs)
	}
	// An older cached release also counts as nothing new.
	n.note("1.3.1")
	if n.Latest() != "" {
		t.Fatalf("latest after an older release = %q; want none", n.Latest())
	}
}

func TestUpdateNoticeLogsFailuresAtDebugOnlyAndTriesAgain(t *testing.T) {
	cases := map[string]string{
		"no redirect":     "",
		"not a release":   "/yarlson/yargpt/releases",
		"not a version":   "/yarlson/yargpt/releases/tag/nightly",
		"no such address": "offline",
	}
	for name, location := range cases {
		t.Run(name, func(t *testing.T) {
			feed, srv := newReleaseFeed(t, location)
			url := srv.URL
			if location == "offline" {
				srv.Close()
			}
			n, logs, _ := newTestNotice(t, url)
			n.check(context.Background())
			n.check(context.Background())
			if n.Latest() != "" || strings.Contains(logs.String(), "level=INFO") || !strings.Contains(logs.String(), "level=DEBUG") {
				t.Fatalf("latest %q, log %q; want nothing found and only debug lines", n.Latest(), logs)
			}
			if _, err := os.Stat(n.cachePath); !os.IsNotExist(err) {
				t.Fatalf("cache after a failed check: %v; want none, so the next check asks again", err)
			}
			if location != "offline" && feed.requests.Load() != 2 {
				t.Fatalf("requests = %d; want a retry after the failure", feed.requests.Load())
			}
		})
	}
	n, logs, _ := newTestNotice(t, "http://bad url")
	n.check(context.Background())
	if !strings.Contains(logs.String(), "level=DEBUG") {
		t.Fatalf("log after a bad URL = %q; want a debug line", logs)
	}
}

func TestUpdateNoticeKeepsWorkingWhenItCannotSaveTheCache(t *testing.T) {
	_, srv := newReleaseFeed(t, "/yarlson/yargpt/releases/tag/v1.4.0")
	n, logs, _ := newTestNotice(t, srv.URL)
	n.cachePath = filepath.Join(t.TempDir(), "missing", updateCheckFile)
	n.check(context.Background())
	if n.Latest() != "1.4.0" || !strings.Contains(logs.String(), "save the release check") {
		t.Fatalf("latest %q, log %q; want the release noted and a debug line about the cache", n.Latest(), logs)
	}
}

func TestUpdateNoticeRunChecksAtOnceAndStopsWithTheContext(t *testing.T) {
	feed, srv := newReleaseFeed(t, "/yarlson/yargpt/releases/tag/v1.4.0")
	n, _, _ := newTestNotice(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { n.run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for n.Latest() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop with its context")
	}
	if feed.requests.Load() != 1 || n.Latest() != "1.4.0" {
		t.Fatalf("requests %d, latest %q; want one check at start", feed.requests.Load(), n.Latest())
	}
}

func TestStartUpdateNoticeChecksOnlyForASourceBuildThatMay(t *testing.T) {
	feed, srv := newReleaseFeed(t, "/yarlson/yargpt/releases/tag/v1.4.0")
	oldURL, oldVersion := latestReleaseURL, version
	t.Cleanup(func() { latestReleaseURL, version = oldURL, oldVersion })
	latestReleaseURL = srv.URL
	log := slog.New(slog.NewTextHandler(&syncLog{}, nil))
	ctx := t.Context()
	o := options{dataDir: t.TempDir(), Updates: httpapi.UpdateSettings{AutoCheck: true, DrainMinutes: 30, UpdateCheck: true}}

	version = "dev"
	u := startUpdateNotice(ctx, o, log)
	if u.Latest != nil || u.Version != "dev" || !u.SourceBuild || u.Settings != o.Updates {
		t.Fatalf("updates of a dev build = %+v; want no check", u)
	}
	version = "1.3.2"
	o.Updates.UpdateCheck = false
	if u := startUpdateNotice(ctx, o, log); u.Latest != nil {
		t.Fatal("a check started with updateCheck off")
	}
	o.Updates.UpdateCheck = true
	u = startUpdateNotice(ctx, o, log)
	if u.Latest == nil || u.Version != "1.3.2" {
		t.Fatalf("updates of a release source build = %+v; want a check", u)
	}
	deadline := time.Now().Add(5 * time.Second)
	for u.Latest() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if u.Latest() != "1.4.0" || feed.requests.Load() != 1 {
		t.Fatalf("latest %q after %d requests; want 1.4.0 from one", u.Latest(), feed.requests.Load())
	}
	if _, err := os.Stat(filepath.Join(o.dataDir, updateCheckFile)); err != nil {
		t.Fatalf("cache in the data directory: %v", err)
	}
}
