package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedirectToFileAppendsWritesToTheFileUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "Library", "Logs", "Handful", "server.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("earlier line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A temporary file stands in for stderr, so the test's own output stays.
	stand, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer stand.Close()

	if err := redirectToFile(int(stand.Fd()), "~/Library/Logs/Handful/server.log"); err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(stand, "panic: boom\n")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "earlier line\npanic: boom\n" {
		t.Fatalf("log file = %q; want the earlier line, then the redirected write", got)
	}
}

func TestRedirectToFileCreatesAPrivateFileAndDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "server.log")
	stand, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer stand.Close()
	if err := redirectToFile(int(stand.Fd()), path); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("log file: %v, %v; want mode 0600", info, err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("log directory: %v, %v; want mode 0700", info, err)
	}
}

func TestRedirectToFileFailsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := redirectToFile(2, filepath.Join(blocker, "server.log"))
	if err == nil || !strings.Contains(err.Error(), "create log directory") {
		t.Fatalf("redirectToFile under a regular file = %v; want a directory error", err)
	}
}
