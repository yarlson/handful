// Package dbtest gives tests a database file with every migration applied.
package dbtest

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"

	"handful/internal/db"
)

// migrated holds the bytes of a database file that has every migration
// applied. Under the race detector, applying the migrations takes most of a
// test's setup time, so each test binary applies them once.
var migrated = sync.OnceValues(func() ([]byte, error) {
	dir, err := os.MkdirTemp("", "dbtest-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "migrated.db")
	conn, err := db.Open(context.Background(), path)
	if err != nil {
		return nil, err
	}
	// Close checkpoints the write-ahead log into the file, so the file alone
	// holds the schema.
	if err := conn.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
})

// Open writes a migrated database file to path, which must not exist, and
// opens it with db.Open.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	data, err := migrated()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	return db.Open(ctx, path)
}
