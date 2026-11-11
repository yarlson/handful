package agent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"handful/internal/core"
)

// The board actions of goals, requests and handoffs are gone: each call
// fails and names the task action to use instead, and the tool no longer
// offers them.
func TestRetiredBoardActionsNameTheirReplacement(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	ship, ana, _, cy := h.newTeam(t)
	docs, err := h.svc.CreateTeam(ctx, "Docs", "Write docs.", []string{cy.ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	lead := boardSession(t, h, srv, ana, ship, false)
	for action, want := range map[string]string{
		"start_goal":       "start_goal is gone: a task starts from your team's plan; propose one with propose, then take it",
		"deliver":          "deliver is gone: close the task with close, its goal_id and the result",
		"request":          "request is gone: send that team a task with send_task, and name your step that waits on it in waits_for",
		"hand_over":        "hand_over is gone: send that team a task with send_task",
		"accept_handoff":   "accept_handoff is gone: take the task from your inbox with take and its goal_id",
		"ask_sender":       "ask_sender is gone: ask the sender with ask and the task's goal_id",
		"answer_handoff":   "answer_handoff is gone: answer with answer_task and the task's goal_id",
		"decline_handoff":  "decline_handoff is gone: push the task back with push_back, its goal_id and a reason",
		"withdraw_handoff": "withdraw_handoff is gone: take back a task you sent with withdraw and its goal_id",
		"defer":            "defer is gone: defer a task of your inbox with later, its goal_id and until",
		"undefer":          "undefer is gone: a task deferred with later comes back to your inbox when it is due; then take it",
	} {
		got, isErr := callText(t, lead, "update_board", map[string]any{"action": action, "team_id": docs.ID, "text": "Write the guide."})
		if !isErr || got != want {
			t.Errorf("%s = %q (error %v); want the error %q", action, got, isErr, want)
		}
	}
	if board, err := h.svc.Board(ctx, docs.ID); err != nil || len(board.Inbox) != 0 || board.Goal != nil {
		t.Fatalf("Docs's board after the retired actions = %+v, %v; want nothing sent", board, err)
	}

	listed, err := lead.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name != "update_board" {
			continue
		}
		schema, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		for _, old := range []string{"start_goal", "deliver", "hand_over", "handoff", "ask_sender", "defer:", "undefer", "needed_by", "revisit", "request"} {
			if strings.Contains(string(schema), old) {
				t.Errorf("update_board still offers %q:\n%s", old, schema)
			}
		}
	}
}
