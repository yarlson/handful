package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Note test 8: the key file is made once, mode 0600, and kept across starts.
func TestTheKeyIsMadeOnceAndKept(t *testing.T) {
	dir := t.TempDir()
	key, path, widened, err := ensureKey(dir)
	if err != nil || widened || path != filepath.Join(dir, "ui-key") || !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(key) {
		t.Fatalf("first start = %q, %q, %v, %v; want a 43-character key in ui-key", key, path, widened, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("ui-key mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	again, _, _, err := ensureKey(dir)
	if err != nil || again != key {
		t.Fatalf("second start = %q, %v; want the same key", again, err)
	}
	other, _, _, err := ensureKey(t.TempDir())
	if err != nil || other == key {
		t.Fatalf("another data folder's key = %q, %v; want a different one", other, err)
	}
}

func TestAKeyFileThatOthersCanReadIsNarrowed(t *testing.T) {
	dir := t.TempDir()
	key, path, _, err := ensureKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, widened, err := ensureKey(dir)
	if err != nil || got != key || !widened {
		t.Fatalf("a 0644 file = %q, %v, %v; want the same key, reported as widened", got, widened, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode after the start = %v, %v; want 0600", info.Mode().Perm(), err)
	}
}

func TestAKeyFileThatIsNoKeyStopsTheStart(t *testing.T) {
	cases := map[string]func(path string) error{
		"a folder":     func(path string) error { return os.Mkdir(path, 0o700) },
		"a symlink":    func(path string) error { return os.Symlink(filepath.Join(filepath.Dir(path), "elsewhere"), path) },
		"empty":        func(path string) error { return os.WriteFile(path, nil, 0o600) },
		"not a key":    func(path string) error { return os.WriteFile(path, []byte("hello\n"), 0o600) },
		"a short key":  func(path string) error { return os.WriteFile(path, []byte(strings.Repeat("a", 42)), 0o600) },
		"a long key":   func(path string) error { return os.WriteFile(path, []byte(strings.Repeat("a", 44)), 0o600) },
		"a bad symbol": func(path string) error { return os.WriteFile(path, []byte(strings.Repeat("a", 42)+"+"), 0o600) },
	}
	for name, place := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "ui-key")
			if err := place(path); err != nil {
				t.Fatal(err)
			}
			if key, _, _, err := ensureKey(dir); err == nil || key != "" || !strings.Contains(err.Error(), path) {
				t.Fatalf("ensureKey = %q, %v; want an error naming %s", key, err, path)
			}
		})
	}
}

func TestAKeyFileThatCantBeMadeStopsTheStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if _, _, _, err := ensureKey(dir); err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "ui-key")) {
		t.Fatalf("ensureKey in a missing folder = %v; want an error naming the file", err)
	}
}

// The key never shows in what the server prints at its start, and the server gets it.
func TestPrepareMakesTheKeyAndPrintsNoKey(t *testing.T) {
	dir := t.TempDir()
	o := options{addr: "127.0.0.1:7777", dataDir: dir, Claude: fakeClaude(t), MaxParallel: 3, set: map[string]bool{}}
	stderr := captureStderr(t, func() {
		if err := prepare(&o); err != nil {
			t.Fatal(err)
		}
	})
	raw, err := os.ReadFile(filepath.Join(dir, "ui-key"))
	if err != nil || o.key != strings.TrimSpace(string(raw)) || o.keyFile != filepath.Join(dir, "ui-key") {
		t.Fatalf("prepare's key = %q in %q; want the file's (%v)", o.key, o.keyFile, err)
	}
	if strings.Contains(stderr, o.key) {
		t.Fatal("the start printed the key")
	}
	if err := os.Chmod(o.keyFile, 0o640); err != nil {
		t.Fatal(err)
	}
	o2 := options{addr: "127.0.0.1:7777", dataDir: dir, Claude: fakeClaude(t), MaxParallel: 3, set: map[string]bool{}}
	stderr = captureStderr(t, func() {
		if err := prepare(&o2); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stderr, o.keyFile+" was readable by others") || strings.Contains(stderr, o.key) {
		t.Fatalf("stderr after a 0640 file = %q; want the narrowing noted, without the key", stderr)
	}
}

// captureStderr returns what fn writes to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
