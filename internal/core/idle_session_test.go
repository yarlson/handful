package core

import (
	"context"
	"testing"
)

// The session that a member's next turn resumes: its chat session, its seat's in a room it is
// in, or its session on a step that is still open.
func TestResumedSessionIsTheOneTheNextTurnResumes(t *testing.T) {
	t.Parallel()
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	docs := f.create(t, f.lead, item("Docs", f.ana.ID, ""))
	dropped := f.create(t, f.lead, item("Export", f.ana.ID, f.cy.ID))
	outsider, err := f.svc.CreateBot(ctx, "Dee", "")
	if err != nil {
		t.Fatal(err)
	}
	work := func(it Item) Step { return Step{ItemID: it.ID, Role: RoleWork} }
	if err := f.svc.SetSession(ctx, f.ana.ID, "chat-ana"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetRoomSession(ctx, f.team.ID, f.ana.ID, "room-ana"); err != nil {
		t.Fatal(err)
	}
	for _, it := range []Item{parse, docs, dropped} {
		if err := f.svc.SetStepSession(ctx, f.ana.ID, work(it), "step-"+it.Title); err != nil {
			t.Fatal(err)
		}
	}
	f.finish(t, docs, f.ana)
	if _, err := f.svc.DropItem(ctx, f.lead, dropped.ID, "Not needed."); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name        string
		bot, room   string
		step        Step
		wantSession string
	}{
		{"the chat", f.ana.ID, "", Step{}, "chat-ana"},
		{"the chat of a deleted bot", "gone", "", Step{}, ""},
		{"the seat in a room", f.ana.ID, f.team.ID, Step{}, "room-ana"},
		{"a room the bot is not in", outsider.ID, f.team.ID, Step{}, ""},
		{"an open step", f.ana.ID, f.team.ID, work(parse), "step-Parse"},
		{"a done step", f.ana.ID, f.team.ID, work(docs), ""},
		{"a dropped step", f.ana.ID, f.team.ID, work(dropped), ""},
		{"a deleted step", f.ana.ID, f.team.ID, Step{ItemID: "gone", Role: RoleWork}, ""},
	} {
		got, err := f.svc.ResumedSession(ctx, c.bot, c.room, c.step)
		if err != nil || got != c.wantSession {
			t.Errorf("%s: ResumedSession = %q, %v; want %q", c.name, got, err, c.wantSession)
		}
	}
}

// Each session counts only its own queued and running turns: the chat, the seat in the room and
// each role on each step.
func TestOpenSessionTurnsCountsTheQueuedAndRunningTurnsOfOneSession(t *testing.T) {
	t.Parallel()
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	finishQueued(t, f.svc)
	queue := func(tr Turn) string {
		t.Helper()
		id, err := f.svc.queueTurn(ctx, f.svc.q, tr)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	queue(Turn{BotID: f.ana.ID, Source: SourceUser})
	running := queue(Turn{BotID: f.ana.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, BoardRole: RoleLead})
	if _, err := f.svc.StartTurn(ctx, running); err != nil {
		t.Fatal(err)
	}
	queue(Turn{BotID: f.ana.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, ItemID: parse.ID, BoardRole: RoleWork})
	f.endTurn(t, queue(Turn{BotID: f.ana.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, ItemID: parse.ID, BoardRole: RoleWork}), "done")
	queue(Turn{BotID: f.cy.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, ItemID: parse.ID, BoardRole: RoleReview})

	for _, c := range []struct {
		name string
		bot  string
		room string
		step Step
		want int64
	}{
		{"Ana's chat", f.ana.ID, "", Step{}, 1},
		{"Ana's seat, with a running lead turn", f.ana.ID, f.team.ID, Step{}, 1},
		{"Ana's work on Parse, one turn done", f.ana.ID, f.team.ID, Step{ItemID: parse.ID, Role: RoleWork}, 1},
		{"Ana's review of Parse", f.ana.ID, f.team.ID, Step{ItemID: parse.ID, Role: RoleReview}, 0},
		{"Cy's review of Parse", f.cy.ID, f.team.ID, Step{ItemID: parse.ID, Role: RoleReview}, 1},
		{"Cy's chat", f.cy.ID, "", Step{}, 0},
	} {
		got, err := f.svc.OpenSessionTurns(ctx, c.bot, c.room, c.step)
		if err != nil || got != c.want {
			t.Errorf("%s: OpenSessionTurns = %d, %v; want %d", c.name, got, err, c.want)
		}
	}
}

// An idle compaction is a running turn of its own, on the task and step of the turn whose
// session it compacts.
func TestStartCompactionTurnRecordsARunningCompactTurnOnTheSameTask(t *testing.T) {
	t.Parallel()
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	from := Turn{ID: "t1", BotID: f.ana.ID, Source: SourceRoom, RoomID: f.team.ID, Hops: 2, GoalID: f.goal.ID, ItemID: parse.ID, BoardRole: RoleWork, Prompt: "Work on your step."}
	id, err := f.svc.StartCompactionTurn(ctx, from)
	if err != nil {
		t.Fatal(err)
	}
	var source, prompt, status, room, goal, itemID string
	var hops int64
	var started bool
	if err := f.svc.sqlDB.QueryRowContext(ctx, `SELECT source, prompt, status, room_id, goal_id, item_id, hops, started_at IS NOT NULL FROM turns WHERE id = ? AND bot_id = ?`, id, f.ana.ID).Scan(&source, &prompt, &status, &room, &goal, &itemID, &hops, &started); err != nil {
		t.Fatal(err)
	}
	if source != SourceCompact || prompt != "/compact" || status != "running" || room != f.team.ID || goal != f.goal.ID || itemID != parse.ID || hops != 2 || !started {
		t.Fatalf("compaction turn = %s %q %s room %s goal %s item %s hops %d started %v; want a running compact turn of /compact in the team, on the goal and the step, 2 hops, started", source, prompt, status, room, goal, itemID, hops, started)
	}
}
