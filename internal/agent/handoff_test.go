package agent

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"yargpt/internal/core"
)

// Through MCP: a lead hands work over without a goal, the receiving lead
// asks, the sender answers, and the receiver accepts; another handoff is
// declined, and one is withdrawn.
func TestLeadsHandWorkOverAndSettleItThroughUpdateBoard(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	ship, ana, _, cy := h.newTeam(t)
	docs, err := h.svc.CreateTeam(ctx, "Docs", "Write docs.", []string{cy.ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := h.svc.CreateArtifact(ctx, ana.ID, core.ArtifactInput{Kind: new("plan"), Title: new("Spec"), Content: new("The spec.")})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	sender := boardSession(t, h, srv, ana, ship, false)
	receiver := boardSession(t, h, srv, cy, docs, false)
	call := func(cs any, args map[string]any) (string, bool) {
		t.Helper()
		if cs == "sender" {
			return callText(t, sender, "update_board", args)
		}
		return callText(t, receiver, "update_board", args)
	}
	handOver := func(text string) string {
		t.Helper()
		got, isErr := call("sender", map[string]any{"action": "hand_over", "team_id": docs.ID, "text": text, "brief": "Context.", "artifact_ids": []string{spec.ID}})
		if isErr || !strings.HasPrefix(got, "Handed over \""+text+"\" (goal id ") || !strings.Contains(got, "It costs your team nothing.") {
			t.Fatalf("hand_over = %q (error %v)", got, isErr)
		}
		board, err := h.svc.Board(ctx, docs.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range board.Incoming {
			if g.Text == text {
				if len(g.ArtifactIDs) != 1 || g.Brief != "Context." {
					t.Fatalf("incoming goal = %+v; want the brief and the artifact", g)
				}
				return g.ID
			}
		}
		t.Fatalf("no incoming %q on Docs", text)
		return ""
	}
	guide := handOver("Write the export guide.")
	if got, isErr := call("receiver", map[string]any{"action": "ask_sender", "goal_id": guide, "text": "For which release?"}); isErr || got != `"Write the export guide." is incoming now.` {
		t.Fatalf("ask_sender = %q (error %v)", got, isErr)
	}
	if got, isErr := call("sender", map[string]any{"action": "answer_handoff", "goal_id": guide, "text": "2.0."}); isErr || got != `"Write the export guide." is incoming now.` {
		t.Fatalf("answer_handoff = %q (error %v)", got, isErr)
	}
	if got, isErr := call("receiver", map[string]any{"action": "accept_handoff", "goal_id": guide}); isErr || got != `"Write the export guide." is active now.` {
		t.Fatalf("accept_handoff = %q (error %v)", got, isErr)
	}
	faq := handOver("Write the FAQ.")
	if got, isErr := call("receiver", map[string]any{"action": "decline_handoff", "goal_id": faq, "text": "Support owns the FAQ."}); isErr || got != `"Write the FAQ." is declined now.` {
		t.Fatalf("decline_handoff = %q (error %v)", got, isErr)
	}
	notes := handOver("Write release notes.")
	if got, isErr := call("receiver", map[string]any{"action": "withdraw_handoff", "goal_id": notes}); !isErr {
		t.Fatalf("the receiver's withdraw_handoff = %q; want an error", got)
	}
	if got, isErr := call("sender", map[string]any{"action": "withdraw_handoff", "goal_id": notes}); isErr || got != `"Write release notes." is ended now.` {
		t.Fatalf("withdraw_handoff = %q (error %v)", got, isErr)
	}
	if list, _ := call("sender", map[string]any{"action": "list"}); !strings.Contains(list, "Sent to other teams:\n- \"Write the export guide.\" (task id ") {
		t.Errorf("the sender's list lacks the task still at Docs:\n%s", list)
	}
	if got, isErr := call("sender", map[string]any{"action": "request", "team_id": docs.ID, "title": "X", "brief": "Do X.", "acceptance": "X done."}); !isErr || !strings.Contains(got, "send_task") {
		t.Fatalf("a request = %q (error %v); want the send_task hint", got, isErr)
	}
}
