package db

import (
	"context"
	"path/filepath"
	"testing"
)

// A team from before the reviewer setting has no reviewer, and keeps its other settings.
func TestTheTeamReviewerMigrationGivesNoTeamAReviewer(t *testing.T) {
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
	defer conn.Close()
	team, err := New(conn).GetTeam(ctx, "ship")
	if err != nil {
		t.Fatal(err)
	}
	if team.ReviewerBotID != "" || team.LeadBotID != "bo" || team.DecisionMinutes != 45 {
		t.Fatalf("team = %+v; want lead bo, decision time 45 and no reviewer", team)
	}
}
