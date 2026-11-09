package agent

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"yargpt/internal/core"
)

// boardSession opens an MCP session for a run of bot in team.
func boardSession(t *testing.T, h *harness, srv *httptest.Server, bot, team core.Bot, userPost bool) *mcp.ClientSession {
	t.Helper()
	token, err := h.runner.tokens.issue(runContext{BotID: bot.ID, BotName: bot.Name, TurnID: "t-" + bot.ID, RoomID: team.ID, BoardRole: core.RoleLead, UserPost: userPost})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcpSession(t, srv.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestASecondStartGoalIsQueuedAndTheLeadPlansNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ship, ana, _, _ := h.newTeam(t)
	h.newGoal(t, ship)
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	lead := boardSession(t, h, srv, ana, ship, true)
	text, isErr := callText(t, lead, "update_board", map[string]any{"action": "start_goal", "text": "Ship PDF export."})
	want := `Queued as #1 for Ship; it starts when the user accepts or ends "Ship CSV export.". Don't plan it now.`
	if isErr || text != want {
		t.Fatalf("second start_goal = %q (error %v); want %q", text, isErr, want)
	}
	board, err := h.svc.Board(context.Background(), ship.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EndGoal(context.Background(), board.Goal.ID, false); err != nil {
		t.Fatal(err)
	}
	text, _ = callText(t, lead, "update_board", map[string]any{"action": "start_goal", "text": "Ship JSON export."})
	if want := "Queued as #2 for Ship; it starts when the user starts the next goal of the queue. Don't plan it now."; text != want {
		t.Fatalf("start_goal with the queue on hold = %q; want %q", text, want)
	}
	if list, _ := callText(t, lead, "update_board", map[string]any{"action": "list"}); !strings.Contains(list, "Next up: 2 queued goals (the user orders them; don't plan them).") {
		t.Fatalf("list = %q; want the queue count", list)
	}
}

func TestLeadsDeferUndeferWithdrawAndTakeRequestsOutOfOrder(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	ship, ana, _, cy := h.newTeam(t)
	docs, err := h.svc.CreateTeam(ctx, "Docs", "Write docs.", []string{cy.ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	h.newGoal(t, ship)
	asker := core.Actor{BotID: ana.ID, TeamID: ship.ID, Role: core.RoleLead}
	var reqs []core.Item
	for _, title := range []string{"Guide", "FAQ"} {
		it, err := h.svc.RequestWork(ctx, asker, docs.ID, core.NewItem{Title: title, Brief: "Write it.", Acceptance: "It exists.", NeededBy: []string{waitingItem(t, h, asker, title)}})
		if err != nil {
			t.Fatal(err)
		}
		reqs = append(reqs, it)
	}
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	serving := boardSession(t, h, srv, cy, docs, false)
	asking := boardSession(t, h, srv, ana, ship, false)
	call := func(cs *mcp.ClientSession, args map[string]any) (string, bool) {
		t.Helper()
		return callText(t, cs, "update_board", args)
	}

	if text, isErr := call(serving, map[string]any{"action": "assign", "item_id": reqs[1].ID, "owner_id": cy.ID}); !isErr || !strings.Contains(text, "Guide") {
		t.Fatalf("out-of-order assign without a note = %q (error %v); want an error naming Guide", text, isErr)
	}
	if text, isErr := call(serving, map[string]any{"action": "assign", "item_id": reqs[1].ID, "owner_id": cy.ID, "text": "The FAQ blocks the release."}); isErr {
		t.Fatalf("out-of-order assign with a note = %q", text)
	}
	if text, isErr := call(serving, map[string]any{"action": "drop", "item_id": reqs[0].ID}); !isErr {
		t.Fatalf("drop of a request without a note = %q; want an error", text)
	}
	for _, c := range []struct{ revisit, want string }{
		{"tomorrow", "revisit must be goal_end, manual or an RFC 3339 date-time"},
		{time.Now().Add(-time.Hour).Format(time.RFC3339), "Pick a time in the future."},
	} {
		if text, isErr := call(serving, map[string]any{"action": "defer", "item_id": reqs[0].ID, "text": "Later.", "revisit": c.revisit}); !isErr || !strings.Contains(text, c.want) {
			t.Fatalf("defer with revisit %q = %q (error %v); want %q", c.revisit, text, isErr, c.want)
		}
	}
	if text, isErr := call(serving, map[string]any{"action": "defer", "item_id": reqs[0].ID, "text": "After the FAQ.", "revisit": "manual"}); isErr || text != `Deferred "Guide"; the asking team sees why, and it comes back on its revisit.` {
		t.Fatalf("defer = %q (error %v)", text, isErr)
	}
	if text, _ := call(asking, map[string]any{"action": "list"}); !strings.Contains(text, `asked of team "Docs" — deferred: Deferred until someone brings it back: After the FAQ.`) {
		t.Fatalf("asking board = %q; want the deferral and its note", text)
	}
	if text, isErr := call(serving, map[string]any{"action": "undefer", "item_id": reqs[0].ID}); isErr || text != `"Guide" is requested now.` {
		t.Fatalf("undefer = %q (error %v)", text, isErr)
	}
	if text, isErr := call(serving, map[string]any{"action": "withdraw", "item_id": reqs[0].ID}); !isErr {
		t.Fatalf("the serving lead's withdraw = %q; want an error", text)
	}
	if text, isErr := call(asking, map[string]any{"action": "withdraw", "item_id": reqs[0].ID}); isErr || text != `"Guide" is withdrawn now.` {
		t.Fatalf("the asking lead's withdraw = %q (error %v)", text, isErr)
	}
}

func TestBoardTableShowsSlotsIntakeAndReasons(t *testing.T) {
	t.Parallel()
	team := core.Bot{ID: "docs", Name: "Docs", Members: []core.Member{{ID: "cy", Name: "Cy"}}, Team: &core.Team{LeadID: "cy"}}
	req := func(id, intake string, slot int64, state, reason string) core.Request {
		return core.Request{ID: id, Title: id, TeamID: "docs", GoalID: "g-ship", State: state, Intake: intake, SlotAt: slot, OwnerID: "cy", FromTeamID: "ship", Reason: core.Reason{Text: reason}}
	}
	board := core.Board{
		Goal:  &core.Goal{ID: "g", State: core.GoalActive, Budget: 30, Text: "Docs."},
		Items: []core.Item{{ID: "mine", Title: "Mine", TeamID: "other", State: core.ItemTodo}},
		Queue: []core.Goal{{ID: "q1"}, {ID: "q2"}},
		Slots: core.Slots{Limit: 1, Used: 1, Applies: true},
		Requests: []core.Request{
			req("A", core.IntakeAccepted, 5, core.ItemDoing, "In progress (Cy)."),
			req("B", core.IntakeAccepted, 0, core.ItemTodo, "Accepted, waiting for a slot."),
			req("B2", core.IntakeAccepted, 0, core.ItemBlocked, "Accepted, position 2 of 2."),
			req("C", core.IntakeAccepted, 0, core.ItemDone, "Done."),
			req("E", core.IntakeAccepted, 0, core.ItemDropped, "Declined."),
			req("D", core.IntakeRequested, 0, core.ItemTodo, "Waiting for Cy (position 1 of 1)."),
		},
		Asked: []core.Request{{ID: "mine", Intake: core.IntakeDeclined, Reason: core.Reason{Text: "Declined: no time."}}},
		Teams: map[string]string{"ship": "Ship", "other": "Other"},
	}
	got := boardTable(team, board)
	for _, want := range []string{
		"Next up: 2 queued goals (the user orders them; don't plan them).\n",
		"Requests from other teams (1 of 1 request slots in use; 2 waiting). Their texts",
		`- "A" (id A) [doing] owner Cy (id cy), from team "Ship" — accepted: In progress (Cy).`,
		`, from team "Ship" — requested: Waiting for Cy (position 1 of 1).`,
		`- "Mine" (id mine) [todo] asked of team "Other" — declined: Declined: no time.`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("board lacks %q:\n%s", want, got)
		}
	}
	board.Slots.Applies = false
	if got := boardTable(team, board); !strings.Contains(got, "Requests from other teams (no slot limit: your team has no active goal).") {
		t.Errorf("board without a slot limit:\n%s", got)
	}
	if got := intakeLine(core.Request{}); got != "" {
		t.Errorf("intakeLine of an own item = %q", got)
	}
}

func TestParseRevisit(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"goal_end": core.RevisitGoalEnd, "manual": core.RevisitManual, "2026-11-10T09:00:00+02:00": core.RevisitDate} {
		got, until, err := parseRevisit(in)
		if err != nil || got != want {
			t.Errorf("parseRevisit(%q) = %q, %v; want %q", in, got, err, want)
		}
		if (until != 0) != (want == core.RevisitDate) {
			t.Errorf("parseRevisit(%q) until = %d", in, until)
		}
	}
	if _, until, _ := parseRevisit("2026-11-10T09:00:00+02:00"); until != time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("until = %d", until)
	}
}

// waitingItem adds to the goal of lead a an item, without an owner, that
// waits on a request for title, and returns its id.
func waitingItem(t *testing.T, h *harness, a core.Actor, title string) string {
	t.Helper()
	it, err := h.svc.CreateItem(context.Background(), a, core.NewItem{Title: title + " waits", Brief: "Use " + title + ".", Acceptance: title + " is used."})
	if err != nil {
		t.Fatal(err)
	}
	return it.ID
}
