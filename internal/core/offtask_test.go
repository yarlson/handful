package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// turnSpec is a turn that a test stores as it was run: the rest of a turn's
// columns take their defaults.
type turnSpec struct {
	id, bot, room, source string
	hops                  int64
	goal, role            string
	decision              int64
	started               time.Time
}

func storeTurn(t *testing.T, svc *Service, s turnSpec) {
	t.Helper()
	if s.source == "" {
		s.source = SourceRoom
	}
	_, err := svc.sqlDB.ExecContext(context.Background(),
		`INSERT INTO turns (id, bot_id, source, prompt, status, room_id, hops, goal_id, board_role, decision, created_at, started_at)
		 VALUES (?, ?, ?, ?, 'done', ?, ?, ?, ?, ?, ?, ?)`,
		s.id, s.bot, s.source, "Prompt of "+s.id+".\nMore.", s.room, s.hops, s.goal, s.role, s.decision, s.started.UnixMilli(), s.started.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
}

// offTaskFixture is the board fixture on a fixed clock, with Ship's turns of
// the last hours.
type offTaskFixture struct {
	*boardFixture
	now time.Time
}

func newOffTaskFixture(t *testing.T) *offTaskFixture {
	t.Helper()
	f := &offTaskFixture{boardFixture: newBoardFixture(t, 30), now: time.UnixMilli(1_800_000_000_000)}
	f.svc.now = func() time.Time { return f.now }
	return f
}

func (f *offTaskFixture) ago(d time.Duration) time.Time { return f.now.Add(-d) }

func (f *offTaskFixture) offTaskIDs(t *testing.T) []string {
	t.Helper()
	off, err := f.svc.RecentOffTask(context.Background(), f.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, tr := range off.Turns {
		ids = append(ids, tr.TurnID)
	}
	if int(off.Count) != len(ids) {
		t.Fatalf("off-task count = %d; want the %d turns listed", off.Count, len(ids))
	}
	return ids
}

func TestTheBoardShowsTheTurnsOfTheLastHourThatWereOnNoTask(t *testing.T) {
	f := newOffTaskFixture(t)
	ctx := context.Background()
	room, err := f.svc.CreateRoom(ctx, "Chat", "", []string{f.ana.ID, f.bo.ID})
	if err != nil {
		t.Fatal(err)
	}
	team := f.team.ID
	for _, s := range []turnSpec{
		{id: "chat", bot: f.ana.ID, room: team, hops: 1, started: f.ago(10 * time.Minute)},
		{id: "report", bot: f.cy.ID, room: team, source: "task", started: f.ago(5 * time.Minute)},
		{id: "user-to-ana", bot: f.ana.ID, room: team, started: f.ago(4 * time.Minute)},
		// Left out:
		{id: "old", bot: f.ana.ID, room: team, hops: 1, started: f.ago(61 * time.Minute)},
		{id: "on-task", bot: f.ana.ID, room: team, hops: 1, goal: f.goal.ID, started: f.ago(9 * time.Minute)},
		{id: "decision", bot: f.bo.ID, room: team, source: SourceSystem, role: RoleLead, goal: f.goal.ID, decision: 1, started: f.ago(8 * time.Minute)},
		{id: "retro", bot: f.bo.ID, room: team, source: SourceSystem, role: RoleRetro, started: f.ago(7 * time.Minute)},
		{id: "user-to-lead", bot: f.bo.ID, room: team, started: f.ago(6 * time.Minute)},
		{id: "plain-room", bot: f.ana.ID, room: room.ID, hops: 1, started: f.ago(3 * time.Minute)},
		{id: "direct", bot: f.ana.ID, source: SourceUser, started: f.ago(2 * time.Minute)},
	} {
		storeTurn(t, f.svc, s)
	}
	if _, err := f.svc.AppendEntry(ctx, team, KindBot, "chat", map[string]any{"text": "\n  Which format do we use?\nCSV.", "speakerId": f.ana.ID}); err != nil {
		t.Fatal(err)
	}
	if got, want := f.offTaskIDs(t), []string{"chat", "report", "user-to-ana"}; !slices.Equal(got, want) {
		t.Fatalf("off-task turns = %q; want %q", got, want)
	}
	board, err := f.svc.Board(ctx, team)
	if err != nil || board.OffTask.Count != 3 {
		t.Fatalf("board off-task = %+v, %v; want 3", board.OffTask, err)
	}
	first := board.OffTask.Turns[0]
	if first.BotID != f.ana.ID || first.BotName != "Ana" || first.StartedAt != f.ago(10*time.Minute).UnixMilli() || first.FirstLine != "Which format do we use?" {
		t.Fatalf("first off-task turn = %+v; want Ana's, 10 min ago, with her first post's first line", first)
	}
	if got := board.OffTask.Turns[1].FirstLine; got != "Prompt of report." {
		t.Fatalf("a turn without a post = %q; want its prompt's first line", got)
	}
	if _, err := f.svc.OffTaskTurns(ctx, room.ID, f.ago(time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("off-task turns of a plain room: err = %v; want ErrInvalid", err)
	}
}

func TestFirstLineIsTheFirstLineWithTextCutToItsLimit(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "",
		"\n \n":                  "",
		"  One. \nTwo.":          "One.",
		strings.Repeat("é", 200): strings.Repeat("é", maxFirstLine-1) + "…",
		strings.Repeat("a", 140): strings.Repeat("a", 140),
	} {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestMakeATaskTakesItAndAttachesTheTurnsThatWereOnNoTask(t *testing.T) {
	f := newOffTaskFixture(t)
	ctx := context.Background()
	docs, dee, _ := f.docsTeam(t)
	team := f.team.ID
	for _, s := range []turnSpec{
		{id: "a", bot: f.ana.ID, room: team, hops: 1, started: f.ago(10 * time.Minute)},
		{id: "b", bot: f.cy.ID, room: team, hops: 1, started: f.ago(5 * time.Minute)},
		{id: "c", bot: f.ana.ID, room: team, hops: 1, started: f.ago(2 * time.Minute)},
		{id: "on-task", bot: f.ana.ID, room: team, hops: 1, goal: f.goal.ID, started: f.ago(9 * time.Minute)},
		{id: "docs", bot: dee.ID, room: docs.ID, hops: 1, started: f.ago(9 * time.Minute)},
	} {
		storeTurn(t, f.svc, s)
	}
	user := Actor{Role: RoleUser}
	task := NewTask{Text: "Decide the format."}
	many := make([]string, maxMadeFromTurns+1)
	for i := range many {
		many[i] = fmt.Sprint("t", i)
	}
	for name, c := range map[string]struct {
		a     Actor
		turns []string
		want  string
	}{
		"a member":                 {f.as(f.ana, ""), []string{"a"}, "only the lead of Ship"},
		"no turns":                 {f.lead, nil, "from 1 to 50 turns"},
		"too many turns":           {f.lead, many, "from 1 to 50 turns"},
		"a turn already on a task": {f.lead, []string{"a", "on-task"}, "turn on-task is not a turn of this team on no task"},
		"another team's turn":      {user, []string{"docs"}, "turn docs is not a turn of this team on no task"},
		"a missing turn":           {user, []string{"gone"}, "turn gone is not a turn of this team on no task"},
	} {
		if _, err := f.svc.MakeTask(ctx, c.a, team, task, c.turns); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("make a task as %s: err = %v; want ErrInvalid with %q", name, err, c.want)
		}
	}
	if got := f.offTaskIDs(t); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("off-task after refused makes = %q; want nothing attached", got)
	}
	if _, err := f.svc.MakeTask(ctx, f.lead, team, NewTask{Text: "x", WaitsOn: "item"}, []string{"a"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("make a task that waits on a step: err = %v; want ErrInvalid", err)
	}

	// The lead makes a task of two turns; Ship works on its goal, so it is planned.
	g, err := f.svc.MakeTask(ctx, f.lead, team, task, []string{"c", "a", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if g.State != GoalQueued || g.QueuePos != 1 || g.Origin != OriginProposal || g.FromBotID != f.bo.ID || g.Used != 0 {
		t.Fatalf("made task = %+v; want Bo's task, planned first, with no turns used", g)
	}
	if got := f.offTaskIDs(t); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("off-task after the make = %q; want only b left", got)
	}
	if text := lastEventText(t, f.svc, team); text != `Bo made the task "Decide the format." from 2 turns that were on no task; it is #1 in the plan.` {
		t.Fatalf("Ship channel = %q", text)
	}
	events, err := f.svc.TaskEvents(ctx, g.ID)
	if err != nil || len(events) != 1 || events[0].Kind != TaskTaken || events[0].Text != "made from 2 turns that were on no task; it is #1 in the plan" {
		t.Fatalf("task events = %+v, %v", events, err)
	}
	// The user makes one of the last turn: the end of the plan.
	g2, err := f.svc.MakeTask(ctx, user, team, NewTask{Text: "Tidy up."}, []string{"b"})
	if err != nil || g2.Origin != OriginUser || g2.QueuePos != 2 {
		t.Fatalf("the user's made task = %+v, %v; want the user's, #2 in the plan", g2, err)
	}
	if got := f.offTaskIDs(t); len(got) != 0 {
		t.Fatalf("off-task after the second make = %q; want none", got)
	}
}

func TestATaskMadeWhenNothingIsNowStartsAndItsTurnsCostNothing(t *testing.T) {
	f := newOffTaskFixture(t)
	ctx := context.Background()
	docs, dee, _ := f.docsTeam(t)
	storeTurn(t, f.svc, turnSpec{id: "a", bot: dee.ID, room: docs.ID, hops: 1, started: f.ago(time.Minute)})
	docsLead := Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead}
	g, err := f.svc.MakeTask(ctx, docsLead, docs.ID, NewTask{Text: "Write the guide."}, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if g.State != GoalActive || g.Used != 0 {
		t.Fatalf("made task = %+v; want it started, with no turns used", g)
	}
	if got, _ := f.svc.goal(ctx, f.svc.q, g.ID); got.Used != 0 {
		t.Fatalf("stored task used = %d; want 0: the attached turn is history", got.Used)
	}
}

func TestTheBoardOfABotThatIsNoTeamHasNoOffTaskTurns(t *testing.T) {
	f := newOffTaskFixture(t)
	storeTurn(t, f.svc, turnSpec{id: "direct", bot: f.ana.ID, source: SourceUser, started: f.ago(time.Minute)})
	board, err := f.svc.Board(context.Background(), f.ana.ID)
	if err != nil || board.OffTask.Count != 0 || board.OffTask.Turns == nil || len(board.OffTask.Turns) != 0 {
		t.Fatalf("Ana's board off-task = %+v, %v; want none, as an empty list", board.OffTask, err)
	}
	if _, err := f.svc.Board(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("board of a missing bot: err = %v; want ErrNotFound", err)
	}
}
