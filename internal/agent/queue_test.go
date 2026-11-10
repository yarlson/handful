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
	want := `Queued as #1 for Ship; it starts when "Ship CSV export." is closed or stopped. Don't plan it now.`
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
	if list, _ := callText(t, lead, "update_board", map[string]any{"action": "list"}); !strings.Contains(list, "Plan, next tasks in order (don't plan them until they are Now):\n- #1 \"Ship PDF export.\" (task id ") {
		t.Fatalf("list = %q; want the plan in order", list)
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
	got := boardTable(team, board, time.Time{})
	for _, want := range []string{
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
	if got := boardTable(team, board, time.Time{}); !strings.Contains(got, "Requests from other teams (no slot limit: your team has no active goal).") {
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
