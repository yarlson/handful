package db

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// YarGPT's migrations end at LastYarGPTSchema, the bridge release v0.13.0's
// count; Handful's are numbered above it. bridge-migrations.sha256 holds the
// bridge's migration files, so none of them changes and no Handful migration
// slips in at or below the boundary (spec ad09e72c §4.3, AC21).
func TestHandfulMigrationsComeAfterYarGPTsAndYarGPTsStayAsTheBridgeShippedThem(t *testing.T) {
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	want := bridgeMigrations(t)
	if len(want) != LastYarGPTSchema {
		t.Fatalf("bridge-migrations.sha256 lists %d files; LastYarGPTSchema is %d", len(want), LastYarGPTSchema)
	}
	if len(names) <= LastYarGPTSchema {
		t.Fatalf("%d migrations; want at least one Handful migration above %d", len(names), LastYarGPTSchema)
	}
	for i, line := range want {
		script, err := fs.ReadFile(migrations, names[i])
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(script)
		if got := hex.EncodeToString(sum[:]) + "  " + strings.TrimPrefix(names[i], "migrations/"); got != line {
			t.Errorf("migration %d is %q; the bridge shipped %q", i+1, got, line)
		}
	}
}

func bridgeMigrations(t *testing.T) []string {
	t.Helper()
	f, err := os.Open("bridge-migrations.sha256")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	for s := bufio.NewScanner(f); s.Scan(); {
		lines = append(lines, s.Text())
	}
	return lines
}
