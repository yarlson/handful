package agent

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"handful/internal/core"
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

// A task the lead takes while another is Now goes into the plan, and the
// lead plans nothing for it yet.
func TestATaskTakenWhileAnotherIsNowWaitsInThePlan(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ship, ana, _, _ := h.newTeam(t)
	h.newGoal(t, ship)
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	lead := boardSession(t, h, srv, ana, ship, true)
	text, isErr := callText(t, lead, "update_board", map[string]any{"action": "propose", "text": "Ship PDF export."})
	if isErr || !strings.HasPrefix(text, `Proposed "Ship PDF export." (task id `) {
		t.Fatalf("propose = %q (error %v)", text, isErr)
	}
	board, err := h.svc.Board(context.Background(), ship.ID)
	if err != nil || len(board.Inbox) != 1 {
		t.Fatalf("inbox = %+v, %v; want the proposal", board.Inbox, err)
	}
	text, isErr = callText(t, lead, "update_board", map[string]any{"action": "take", "goal_id": board.Inbox[0].ID})
	if want := `Took "Ship PDF export." as #1 in your plan; it starts when the tasks before it are closed. Don't plan it now.`; isErr || text != want {
		t.Fatalf("take while a task is Now = %q (error %v); want %q", text, isErr, want)
	}
	if list, _ := callText(t, lead, "update_board", map[string]any{"action": "list"}); !strings.Contains(list, "Plan, next tasks in order (don't plan them until they are Now):\n- #1 \"Ship PDF export.\" (task id ") {
		t.Fatalf("list = %q; want the plan in order", list)
	}
}

// A step of the Now task that another team does names that team, and the
// board has no section for other teams' requests.
func TestBoardTableNamesTheTeamOfAStepAskedOfAnotherTeam(t *testing.T) {
	t.Parallel()
	team := core.Bot{ID: "docs", Name: "Docs", Members: []core.Member{{ID: "cy", Name: "Cy"}}, Team: &core.Team{LeadID: "cy"}}
	board := core.Board{
		Goal:  &core.Goal{ID: "g", State: core.GoalActive, Text: "Docs."},
		Items: []core.Item{{ID: "mine", Title: "Mine", TeamID: "other", State: core.ItemDone}},
		Teams: map[string]string{"other": "Other"},
	}
	got := boardTable(team, board, time.Time{})
	if !strings.Contains(got, `- "Mine" (id mine) [done] asked of team "Other"`+"\n") || strings.Contains(got, "Requests") || strings.Contains(got, "slot") {
		t.Errorf("board = %q; want the step's team and no requests section", got)
	}
}
