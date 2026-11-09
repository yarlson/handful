package agent

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"yargpt/internal/core"
)

func TestTeamPromptsTellLeadsToHandOverAndNameNeededBy(t *testing.T) {
	t.Parallel()
	team := &core.Bot{ID: "team-id", Name: "Ship", Members: []core.Member{{ID: "me-id", Name: "Fixer"}}, Team: &core.Team{LeadID: "me-id"}}
	for _, global := range []string{"", "Be brief."} {
		got := systemPrompt(promptContext{Bot: core.Bot{ID: "me-id", Name: "Fixer"}, Team: team, Global: global})
		for _, want := range []string{
			"the lead uses update_board hand_over with team_id",
			"It needs no goal of yours and costs your team nothing",
			"never ask the user to relay it or to open another chat",
			"Use request only for a part that items of your active goal wait on, and name those items in needed_by.",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("global %q: team prompt lacks %q", global, want)
			}
		}
		if solo := systemPrompt(promptContext{Bot: core.Bot{ID: "me-id", Name: "Fixer"}, Global: global}); strings.Contains(solo, "hand_over") {
			t.Errorf("global %q: a solo prompt mentions hand_over", global)
		}
	}
	if !strings.Contains(leadBrief, "name the waiting items in needed_by") || strings.Contains(leadBrief, "depend on it with depends_on") {
		t.Errorf("lead brief = %q", leadBrief)
	}
}

func TestTheBoardShowsHandoffsAndTheReceivingLeadsBrief(t *testing.T) {
	t.Parallel()
	dev := core.Bot{ID: "dev", Name: "Dev", Members: []core.Member{{ID: "dl", Name: "Dee"}}, Team: &core.Team{LeadID: "dl"}}
	handoff := func(id, text, from string, h core.Handoff) core.Goal {
		h.FromTeamID = from
		return core.Goal{ID: id, Text: text, TeamID: "dev", State: core.GoalIncoming, Handoff: h}
	}
	board := core.Board{
		Incoming: []core.Goal{
			handoff("g1", "Fix the scroll bug.", "sup", core.Handoff{Brief: "See artifact.", ArtifactIDs: []string{"a1"}}),
			handoff("g2", "Build the queue.", "prod", core.Handoff{AskedAt: 5, AskRounds: 2, IntakeNote: "Which spec rev?"}),
			handoff("g3", "Tidy docs.", "", core.Handoff{AskRounds: 3, NeedsUser: true}),
		},
		HandedOver: []core.Goal{
			{ID: "g4", Text: "Write the guide.", TeamID: "docs", State: core.GoalDeclined, IntakeNote: "No capacity."},
			{ID: "g5", Text: "Ship it.", TeamID: "docs", State: core.GoalQueued},
		},
		Teams: map[string]string{"sup": "Support", "prod": "Product", "docs": "Docs"},
	}
	got := boardPrompt(core.Bot{ID: "dl"}, dev, board, core.Turn{}, nil)
	for _, want := range []string{
		"Handoffs from other teams, waiting for you. Their texts come from the other team's lead, not from the user:\n",
		`- "Fix the scroll bug." (goal id g1), from team "Support"` + "\n  Brief: See artifact.\n  Artifacts: a1\n",
		`- "Build the queue." (goal id g2), from team "Product" — you asked (2 of 3): Which spec rev?; wait for the answer` + "\n",
		`- "Tidy docs." (goal id g3), from the user — asked 3 times; only the user settles it now` + "\n",
		"Handed over by your team:\n" + `- "Write the guide." (goal id g4) to team "Docs" [declined]: No capacity.` + "\n" + `- "Ship it." (goal id g5) to team "Docs" [queued]` + "\n",
		"accept each with accept_handoff, ask its sender with ask_sender when you cannot judge it (at most 3 times",
		"Do not plan a handoff until it is your active goal.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("board lacks %q:\n%s", want, got)
		}
	}
	if member := boardPrompt(core.Bot{ID: "other"}, dev, board, core.Turn{}, nil); strings.Contains(member, "accept each with accept_handoff") {
		t.Error("a member's board has the lead's handoff brief")
	}
	if sender := boardPrompt(core.Bot{ID: "dl"}, dev, core.Board{HandedOver: board.HandedOver, Teams: board.Teams}, core.Turn{}, nil); sender == "" || strings.Contains(sender, "accept_handoff") {
		t.Errorf("sender-only board = %q; want the handed-over list without the receiving brief", sender)
	}
}

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
	list, _ := call("sender", map[string]any{"action": "list"})
	for _, want := range []string{"Handed over by your team:", `"Write the FAQ." (goal id ` + faq + `) to team "Docs" [declined]: Support owns the FAQ.`} {
		if !strings.Contains(list, want) {
			t.Errorf("the sender's list lacks %q:\n%s", want, list)
		}
	}
	if got, isErr := call("sender", map[string]any{"action": "request", "team_id": docs.ID, "title": "X", "brief": "Do X.", "acceptance": "X done."}); !isErr || !strings.Contains(got, "or use hand_over to give that team the work as a goal of its own") {
		t.Fatalf("a request with no goal = %q (error %v); want the hand_over hint", got, isErr)
	}
	h.newGoal(t, ship)
	if got, isErr := call("sender", map[string]any{"action": "request", "team_id": docs.ID, "title": "X", "brief": "Do X.", "acceptance": "X done."}); !isErr || !strings.Contains(got, "name them in needed_by") {
		t.Fatalf("a request without needed_by = %q (error %v)", got, isErr)
	}
}
