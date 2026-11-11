package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// BackupBeforeHandful copies the database at path to backup when no Handful
// migration has run on it yet (user_version at most LastYarGPTSchema) and
// backup doesn't exist, so that going back to YarGPT can restore it. It
// reports whether it wrote the backup. A database that doesn't exist yet
// needs none. It never migrates the database.
func BackupBeforeHandful(ctx context.Context, path, backup string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if _, err := os.Stat(backup); err == nil {
		return false, nil
	}
	conn, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return false, fmt.Errorf("read the schema version of %s: %w", path, err)
	}
	if version > LastYarGPTSchema {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		return false, err
	}
	// VACUUM INTO writes a consistent copy, the WAL included; the copy goes in
	// under its name only when it is whole. A start that a crash or a full
	// disk cut short left a part of one, which goes first.
	tmp := backup + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return false, fmt.Errorf("copy %s: %w", path, err)
	}
	if err := os.Rename(tmp, backup); err != nil {
		return false, err
	}
	return true, nil
}
