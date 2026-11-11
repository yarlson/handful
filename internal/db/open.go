package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"math"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// LastYarGPTSchema is the number of YarGPT's migrations at the bridge release
// v0.13.0, the last YarGPT release, which offers the move to Handful, the
// app's new name. Handful's own migrations are numbered above it, so a
// database at this version or below is one that YarGPT can still open.
const LastYarGPTSchema = 19

// Open opens the SQLite database at path and applies pending migrations.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	// One connection serializes all statements. The entry sequence allocation
	// (MAX()+1 inside INSERT) relies on that to stay race-free.
	conn.SetMaxOpenConns(1)
	if err := migrate(ctx, conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return conn, nil
}

// migrate applies the migrations whose number is above PRAGMA user_version.
// Migration N is the Nth file in name order. Each one runs in a transaction
// together with its user_version update, so a failed run leaves no partial step.
// Databases created before versioning have user_version 0 and the full
// 001 schema; 001 uses IF NOT EXISTS, so it is a no-op for them.
func migrate(ctx context.Context, conn *sql.DB) error {
	return migrateTo(ctx, conn, math.MaxInt)
}

// migrateTo is migrate up to migration last.
func migrateTo(ctx context.Context, conn *sql.DB, last int) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(names)
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(names) {
		return fmt.Errorf("database schema version %d is newer than this YarGPT (%d); use a newer YarGPT", version, len(names))
	}
	for i := version; i < min(last, len(names)); i++ {
		if err := applyMigration(ctx, conn, names[i], i+1); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration runs migration file name and sets user_version to version,
// in one transaction.
func applyMigration(ctx context.Context, conn *sql.DB, name string, version int) error {
	script, err := fs.ReadFile(migrations, name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, string(script)); err != nil {
		tx.Rollback()
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		tx.Rollback()
		return fmt.Errorf("record version after %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}
