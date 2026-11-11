package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// redirectToFile points the file descriptor fd at path, which it creates or
// appends to. A path that starts with ~/ is in the home directory.
func redirectToFile(fd int, path string) error {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find home directory: %w", err)
		}
		path = filepath.Join(home, rest)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()
	if err := unix.Dup2(int(f.Fd()), fd); err != nil {
		return fmt.Errorf("send output to %s: %w", path, err)
	}
	return nil
}
