package agent

import (
	"context"
	"slices"
	"strings"
	"testing"

	"yargpt/internal/core"
)

// otherLead makes a second team, led by a new bot, and returns its lead.
// Make it before queueing the turns a test controls: newNamedBot drops
// every queued turn.
func otherLead(t *testing.T, h *harness) core.Actor {
	t.Helper()
	dee := h.newNamedBot(t, "Dee")
	docs, err := h.svc.CreateTeam(context.Background(), "Docs", "Write docs.", []string{dee.ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	return core.Actor{BotID: dee.ID, TeamID: docs.ID, Role: core.RoleLead}
}

// leadWake hands work from the other team over to team, which queues a
// decision turn (board_role lead) for team's lead. Its text is QUICK, so the
// fake runs it short even though the room's history quotes a LONG post.
func leadWake(t *testing.T, h *harness, from core.Actor, team core.Bot) {
	t.Helper()
	if _, err := h.svc.HandOver(context.Background(), from, "", team.ID, core.NewHandoff{Text: "QUICK: build the export."}); err != nil {
		t.Fatal(err)
	}
}

// AC7: with a member's chat turn running in the room, the lead's decision
// turn starts at once.
func TestALeadDecisionStartsWhileTheRoomsChatRuns(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "slow")
	team, _, _, _ := h.newTeam(t)
	docsLead := otherLead(t, h)
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(context.Background(), team.ID, "@Bo LONG review the parser", "n1"); err != nil {
		t.Fatal(err)
	}
	h.orderLog(t, 1)
	leadWake(t, h, docsLead, team)
	lines := h.orderLog(t, 3)
	want := []string{"start " + team.ID + "-long", "start " + team.ID, "end " + team.ID}
	if !slices.Equal(lines[:3], want) {
		t.Fatalf("order.log = %q; want the lead's decision to run while Bo's chat turn runs", lines)
	}
}

// With one turn at a time, the lead's decision goes ahead of a member's
// older turn, and the two still never run together.
func TestALeadDecisionGoesAheadOfAnOlderQueuedTurn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "slow")
	h.runner.cfg.MaxParallel = 1
	team, _, bo, _ := h.newTeam(t)
	docsLead := otherLead(t, h)
	if _, _, err := h.svc.SubmitUserMessage(context.Background(), bo.ID, "LONG review the parser", "n1"); err != nil {
		t.Fatal(err)
	}
	leadWake(t, h, docsLead, team)
	h.start(t)
	lines := h.orderLog(t, 4)
	want := []string{"start " + team.ID, "end " + team.ID, "start " + bo.ID + "-long", "end " + bo.ID + "-long"}
	if !slices.Equal(lines[:4], want) {
		t.Fatalf("order.log = %q; want the lead's decision first, then Bo's older turn", lines)
	}
}

func TestMayStartLetsALeadDecisionSkipOnlyTheChatHold(t *testing.T) {
	t.Parallel()
	r := newHarness(t, "reply").runner
	r.cfg.MaxParallel = 3
	lead := core.Turn{ID: "l", BotID: "ana", RoomID: "team", BoardRole: core.RoleLead}
	chat := core.Turn{ID: "c", BotID: "cy", RoomID: "team"}
	cases := []struct {
		name    string
		running map[string]runningTurn
		turn    core.Turn
		want    bool
	}{
		{"a lead decision while the room's chat runs", map[string]runningTurn{"bo": {roomID: "team"}}, lead, true},
		{"a chat turn while the room's chat runs", map[string]runningTurn{"bo": {roomID: "team"}}, chat, false},
		{"a retro while the room's chat runs", map[string]runningTurn{"bo": {roomID: "team"}}, core.Turn{ID: "r", BotID: "ana", RoomID: "team", BoardRole: core.RoleRetro}, true},
		{"a lead decision while the lead runs another turn", map[string]runningTurn{"ana": {roomID: "other"}}, lead, false},
		{"a lead decision at the board turns' cap", map[string]runningTurn{"x": {board: true}, "y": {board: true}}, lead, false},
		{"a lead decision below the cap", map[string]runningTurn{"x": {board: true}, "y": {}}, lead, true},
		{"a chat turn at the board turns' cap", map[string]runningTurn{"x": {board: true, roomID: "a"}, "y": {board: true, roomID: "b"}}, chat, true},
	}
	for _, c := range cases {
		r.mu.Lock()
		r.running = c.running
		got := r.mayStartLocked(context.Background(), c.turn)
		r.mu.Unlock()
		if got != c.want {
			t.Errorf("%s: mayStartLocked = %v; want %v", c.name, got, c.want)
		}
	}
}

func TestBoardTurnsAreGoalWorkAndLeadDecisions(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		turn core.Turn
		want bool
	}{
		{core.Turn{GoalID: "g", BoardRole: core.RoleWork}, true},
		{core.Turn{GoalID: "g", BoardRole: core.RoleReview}, true},
		{core.Turn{BoardRole: core.RoleLead}, true},
		{core.Turn{GoalID: "g", BoardRole: core.RoleRetro}, true},
		{core.Turn{RoomID: "team"}, false},
		// A conversation turn that inherits a task or a step stays conversation.
		{core.Turn{RoomID: "team", GoalID: "g", ItemID: "i"}, false},
	} {
		if got := boardTurn(c.turn); got != c.want {
			t.Errorf("boardTurn(%+v) = %v; want %v", c.turn, got, c.want)
		}
	}
	r := newHarness(t, "reply").runner
	r.mu.Lock()
	r.running = map[string]runningTurn{"a": {board: true}, "b": {goalID: "g"}, "c": {}}
	n := r.boardTurnsLocked()
	r.mu.Unlock()
	if n != 1 {
		t.Fatalf("board turns = %d; want only the one marked as a board turn", n)
	}
}

func TestDispatchOrderPutsLeadsBoardTurnsFirstAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	turns := []core.Turn{
		{ID: "chat1"},
		{ID: "retro", BoardRole: core.RoleRetro},
		{ID: "work", BoardRole: core.RoleWork, GoalID: "g"},
		{ID: "plan", BoardRole: core.RoleLead},
		{ID: "chat2"},
	}
	var got []string
	for _, t := range dispatchOrder(turns) {
		got = append(got, t.ID)
	}
	if want := []string{"retro", "plan", "chat1", "work", "chat2"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v; want %v", got, want)
	}
}

// Accepting a goal queues its retro before the next goal's planning. With a
// member's chat turn running in the team, the retro still runs first.
func TestTheRetroRunsBeforeTheNextGoalsPlanningWhileTheRoomsChatRuns(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "slow")
	ctx := context.Background()
	team, ana, bo, _ := h.newTeam(t)
	items := h.newGoal(t, team, boardItem("Parse", "Parse it.", bo.ID, ""))
	if err := h.svc.StartItemWork(ctx, items[0].ID, bo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.SubmitItem(ctx, core.Actor{BotID: bo.ID, TeamID: team.ID, Role: core.RoleWork}, items[0].ID, "parse.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.DeliverGoal(ctx, core.Actor{BotID: ana.ID, TeamID: team.ID, Role: core.RoleLead}, "Done."); err != nil {
		t.Fatal(err)
	}
	queued, err := h.svc.QueuedTurns(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range queued {
		if err := h.svc.FinishTurn(ctx, q.ID, "done", ""); err != nil {
			t.Fatal(err)
		}
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, team.ID, "@Bo LONG review the parser", "n1"); err != nil {
		t.Fatal(err)
	}
	h.orderLog(t, 1)
	board, err := h.svc.Board(ctx, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AcceptGoal(ctx, board.Goal.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.StartGoal(ctx, team.ID, "Next: JSON export."); err != nil {
		t.Fatal(err)
	}
	calls := h.waitCalls(t, 3)
	at := func(want ...string) int {
		for i, c := range calls {
			if !slices.ContainsFunc(want, func(w string) bool { return !strings.Contains(c, w) }) {
				return i
			}
		}
		return -1
	}
	chat, retro, plan := at("@Bo LONG review the parser", "You are Bo"), at("The user accepted this goal."), at("Next: JSON export.", "You lead this goal.")
	if chat != 0 || retro != 1 || plan != 2 {
		t.Fatalf("calls: Bo's chat at %d, the retro at %d, the planning at %d; want 0, 1, 2", chat, retro, plan)
	}
}
