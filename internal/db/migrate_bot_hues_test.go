package db

import (
	"context"
	"database/sql"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"testing"
)

// The bot hue migration gives existing bots, not rooms, the hues in creation order: sky,
// emerald, violet, fuchsia, lime, then sky again. Run again after a partial run, it gives the
// missing bots the hues a full run gives and keeps every hue already stored.
func TestTheBotHueMigrationDealsHuesInCreationOrderAndRunsAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "handful.db")
	old := openAt(t, path, 25)
	if _, err := old.ExecContext(ctx, `INSERT INTO bots (id, name, kind, created_at, updated_at) VALUES
		('f', 'F', 'agent', 6, 6), ('a', 'A', 'agent', 1, 1), ('room', 'Room', 'room', 2, 2),
		('c', 'C', 'agent', 3, 3), ('b', 'B', 'agent', 2, 2), ('e', 'E', 'agent', 5, 5), ('d', 'D', 'agent', 4, 4)`); err != nil {
		t.Fatalf("prepare the database: %v", err)
	}
	old.Close()
	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	want := map[string]string{"a": "sky", "b": "emerald", "c": "violet", "d": "fuchsia", "e": "lime", "f": "sky"}
	assertHues(t, conn, want)

	// A partial run: two bots lost their hue, and the user chose lime for b.
	for _, stmt := range []string{`DELETE FROM bot_hues WHERE bot_id IN ('c', 'f')`, `UPDATE bot_hues SET hue = 'lime' WHERE bot_id = 'b'`} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	script, err := fs.ReadFile(migrations, "migrations/026_bot_hues.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(script)); err != nil {
		t.Fatalf("run the migration again: %v", err)
	}
	want["b"] = "lime"
	assertHues(t, conn, want)
}

// A new bot gets the hue the fewest bots have; a tie goes to the first hue in palette order.
func TestAssignLeastUsedHuePicksTheLeastUsedHueAndBreaksTiesInPaletteOrder(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(ctx, filepath.Join(t.TempDir(), "handful.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	q := New(conn)
	add := func(id string) string {
		t.Helper()
		if _, err := conn.ExecContext(ctx, `INSERT INTO bots (id, name, created_at, updated_at) VALUES (?, ?, 1, 1)`, id, id); err != nil {
			t.Fatal(err)
		}
		hue, err := q.AssignLeastUsedHue(ctx, id)
		if err != nil {
			t.Fatalf("assign a hue to %s: %v", id, err)
		}
		return hue
	}
	var got []string
	for _, id := range []string{"1", "2", "3", "4", "5", "6"} {
		got = append(got, add(id))
	}
	if want := []string{"sky", "emerald", "violet", "fuchsia", "lime", "sky"}; !slices.Equal(got, want) {
		t.Fatalf("hues of six new bots = %v; want %v", got, want)
	}
	// Bots 2 and 4 move to violet: sky 2, emerald 0, violet 3, fuchsia 0, lime 1.
	if _, err := conn.ExecContext(ctx, `UPDATE bot_hues SET hue = 'violet' WHERE bot_id IN ('2', '4')`); err != nil {
		t.Fatal(err)
	}
	if hue := add("7"); hue != "emerald" {
		t.Fatalf("hue with emerald and fuchsia unused = %q; want emerald (the first in palette order)", hue)
	}
	if hue := add("8"); hue != "fuchsia" {
		t.Fatalf("hue with fuchsia the only unused = %q; want fuchsia", hue)
	}
}

func assertHues(t *testing.T, conn *sql.DB, want map[string]string) {
	t.Helper()
	rows, err := New(conn).ListBotHues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.BotID] = r.Hue
	}
	if !maps.Equal(got, want) {
		t.Fatalf("bot hues = %v; want %v", got, want)
	}
}
