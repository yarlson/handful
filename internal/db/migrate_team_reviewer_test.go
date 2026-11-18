package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// A team from before the reviewer setting has no reviewer and keeps its other settings, and
// running the migration again changes nothing.
func TestTheTeamReviewerMigrationGivesNoTeamAReviewerAndRunsAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "handful.db")
	old := openAt(t, path, 31)
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES
		  ('ana', 'Ana', '', '', 'agent', 1, 1), ('bo', 'Bo', '', '', 'agent', 1, 1), ('ship', 'Ship', '', '', 'room', 1, 1)`,
		`INSERT INTO room_members (room_id, bot_id) VALUES ('ship', 'ana'), ('ship', 'bo')`,
		`INSERT INTO teams (team_id, lead_bot_id, decision_minutes) VALUES ('ship', 'bo', 45)`,
	} {
		if _, err := old.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("prepare the database: %v", err)
		}
	}
	old.Close()

	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	q := New(conn)
	if team, err := q.GetTeam(ctx, "ship"); err != nil || team.LeadBotID != "bo" || team.DecisionMinutes != 45 {
		t.Fatalf("team = %+v, %v; want lead bo and decision time 45", team, err)
	}
	if reviewer, err := q.GetTeamReviewer(ctx, "ship"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("reviewer = %q, %v; want none", reviewer, err)
	}
	if err := q.SetTeamReviewer(ctx, SetTeamReviewerParams{TeamID: "ship", BotID: "ana"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 31"); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	again, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open after migration 032 ran: %v", err)
	}
	defer again.Close()
	if reviewer, err := New(again).GetTeamReviewer(ctx, "ship"); err != nil || reviewer != "ana" {
		t.Fatalf("reviewer after a second run = %q, %v; want ana", reviewer, err)
	}
}
