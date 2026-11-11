package db

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// yargptDatabase creates a database as YarGPT left it, at LastYarGPTSchema,
// with saved tool names in every place that holds them.
func yargptDatabase(t *testing.T, path string) {
	t.Helper()
	conn := openAt(t, path, LastYarGPTSchema)
	defer conn.Close()
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, created_at, updated_at) VALUES ('b1', 'Dev', 'Post with mcp__yargpt__send_to_user.', 1, 1)`,
		`INSERT INTO bots (id, name, kind, created_at, updated_at) VALUES ('t1', 'YarGPT Dev', 'room', 1, 1)`,
		`INSERT INTO teams (team_id, lead_bot_id, agreement) VALUES ('t1', 'b1', 'Use mcp__yargpt__update_board for items.')`,
		`INSERT INTO tool_grants (bot_id, tool_name, created_at) VALUES ('b1', 'mcp__yargpt__update_memory', 1)`,
		`INSERT INTO tool_grants (bot_id, tool_name, created_at) VALUES ('b1', 'mcp__yargpt__read_room', 1)`,
		`INSERT INTO tool_grants (bot_id, tool_name, created_at) VALUES ('b1', 'mcp__handful__read_room', 2)`,
		`INSERT INTO tool_grants (bot_id, tool_name, created_at) VALUES ('b1', 'Bash', 1)`,
		`INSERT INTO memories (id, scope_key, tier, fact, learned_by, created_at) VALUES ('m1', 'agent:b1', 'profile', 'Call mcp__yargpt__recall_memory first.', 'b1', 1)`,
		`INSERT INTO memories (id, scope_key, tier, fact, learned_by, created_at) VALUES ('m2', 'agent:b1', 'log', 'Use mcp__yargpt__a, then mcp__handful__b.', 'b1', 1)`,
		`INSERT INTO memories (id, scope_key, tier, fact, learned_by, created_at) VALUES ('m3', 'agent:b1', 'log', 'Use mcp__handful__a, then mcp__yargpt__b.', 'b1', 1)`,
		`INSERT INTO memories (id, scope_key, tier, fact, learned_by, created_at) VALUES ('m4', 'agent:b1', 'note', 'YarGPT keeps a yargpt.db.', 'b1', 1)`,
		`INSERT INTO routines (id, bot_id, name, prompt, schedule, enabled, created_at, updated_at) VALUES ('r1', 'b1', 'Daily', 'Sum up with mcp__yargpt__send_to_user.', '@daily', 1, 1, 1)`,
	} {
		if _, err := conn.ExecContext(t.Context(), stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func column(t *testing.T, conn *sql.DB, query string) []string {
	t.Helper()
	rows, err := conn.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// The MCP server is "handful" now: every saved mcp__yargpt__ tool name
// becomes mcp__handful__, a grant or fact that would then repeat another is
// dropped, other text stays, and running the rewrite again changes nothing
// (spec ad09e72c AC8).
func TestTheToolNamesMigrationRenamesEverySavedToolName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yargpt.db")
	yargptDatabase(t, path)
	conn, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	check := func(when string) {
		t.Helper()
		for query, want := range map[string][]string{
			`SELECT tool_name FROM tool_grants ORDER BY tool_name`: {"Bash", "mcp__handful__read_room", "mcp__handful__update_memory"},
			`SELECT fact FROM memories ORDER BY id`:                {"Call mcp__handful__recall_memory first.", "Use mcp__handful__a, then mcp__handful__b.", "YarGPT keeps a yargpt.db."},
			`SELECT instructions FROM bots WHERE id = 'b1'`:        {"Post with mcp__handful__send_to_user."},
			`SELECT agreement FROM teams`:                          {"Use mcp__handful__update_board for items."},
			`SELECT prompt FROM routines`:                          {"Sum up with mcp__handful__send_to_user."},
		} {
			if got := column(t, conn, query); !slices.Equal(got, want) {
				t.Errorf("%s: %s = %q; want %q", when, query, got, want)
			}
		}
	}
	check("after the migration")
	script, err := migrations.ReadFile("migrations/020_handful_tool_names.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(t.Context(), string(script)); err != nil {
		t.Fatalf("run the rewrite again: %v", err)
	}
	check("after running it again")
}

// The backup is taken once, before any Handful migration, and is a copy of
// the data as YarGPT left it (spec ad09e72c §4.3).
func TestTheBackupCopiesYarGPTsDatabaseOnceBeforeHandfulMigratesIt(t *testing.T) {
	dir := t.TempDir()
	path, backup := filepath.Join(dir, "yargpt.db"), filepath.Join(dir, "backup", "yargpt-before-handful.db")
	if made, err := BackupBeforeHandful(t.Context(), path, backup); made || err != nil {
		t.Fatalf("no database: made %v, %v; want no backup", made, err)
	}
	yargptDatabase(t, path)
	// A start that a full disk cut short left part of a copy.
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup+".tmp", []byte("part of a copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if made, err := BackupBeforeHandful(t.Context(), path, backup); !made || err != nil {
		t.Fatalf("backup: made %v, %v; want one", made, err)
	}
	saved, err := sql.Open("sqlite", "file:"+backup+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer saved.Close()
	if got := column(t, saved, `SELECT tool_name FROM tool_grants ORDER BY tool_name`); !slices.Equal(got, []string{"Bash", "mcp__handful__read_room", "mcp__yargpt__read_room", "mcp__yargpt__update_memory"}) {
		t.Fatalf("backup grants = %q; want them as YarGPT left them", got)
	}
	if got := column(t, saved, `PRAGMA user_version`); !slices.Equal(got, []string{"19"}) {
		t.Fatalf("backup user_version = %q; want 19", got)
	}
	first, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if made, err := BackupBeforeHandful(t.Context(), path, backup); made || err != nil {
		t.Fatalf("after Handful's migration: made %v, %v; want no new backup", made, err)
	}
	if again, _ := os.ReadFile(backup); !bytes.Equal(again, first) {
		t.Fatal("the backup changed; want the first one kept")
	}
	if _, err := os.Stat(backup + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary copy: %v; want none left", err)
	}
}

// A backup that can't be written leaves no copy behind and says why.
func TestABackupThatFailsLeavesNoPartialCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "yargpt.db")
	yargptDatabase(t, path)
	backupDir := filepath.Join(dir, "backup")
	if err := os.Mkdir(backupDir, 0o500); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(backupDir, "yargpt-before-handful.db")
	if made, err := BackupBeforeHandful(t.Context(), path, backup); made || err == nil || !strings.Contains(err.Error(), "copy "+path) {
		t.Fatalf("backup into a read-only folder: made %v, %v; want an error naming the database", made, err)
	}
	if entries, _ := os.ReadDir(backupDir); len(entries) != 0 {
		t.Fatalf("backup folder holds %v; want nothing", entries)
	}
}

// An old YarGPT started on the moved database, through the link, refuses it
// and changes nothing: its migrations end at LastYarGPTSchema (spec ad09e72c
// §8, AC16).
func TestYarGPTRefusesTheDatabaseThatHandfulMigrated(t *testing.T) {
	dir := t.TempDir()
	handful := filepath.Join(dir, ".handful")
	if err := os.Mkdir(handful, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".yargpt")
	if err := os.Symlink(handful, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(handful, "yargpt.db")
	yargptDatabase(t, path)
	conn, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	old, err := sql.Open("sqlite", "file:"+filepath.Join(link, "yargpt.db")+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	err = migrateNames(context.Background(), old, names[:LastYarGPTSchema], len(names))
	old.Close()
	if err == nil || !strings.Contains(err.Error(), "database schema version 20 is newer") {
		t.Fatalf("YarGPT's migrations on Handful's database: %v; want the schema version error", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, before) {
		t.Fatal("the database changed; want it as Handful left it")
	}
	files, _ := filepath.Glob(filepath.Join(handful, "*.db"))
	if len(files) != 1 {
		t.Fatalf("database files = %q; want only yargpt.db", files)
	}
}

func TestSchemaVersionReadsTheVersionOrZeroForNoDatabase(t *testing.T) {
	dir := t.TempDir()
	if v, err := SchemaVersion(t.Context(), filepath.Join(dir, "none.db")); v != 0 || err != nil {
		t.Fatalf("no database: %d, %v; want 0", v, err)
	}
	path := filepath.Join(dir, "yargpt.db")
	yargptDatabase(t, path)
	if v, err := SchemaVersion(t.Context(), path); v != LastYarGPTSchema || err != nil {
		t.Fatalf("YarGPT's database: %d, %v; want %d", v, err, LastYarGPTSchema)
	}
}
