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

func TestTheQuestionRuleAsksForSelfContainedQuestionsInEveryPrompt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC)
	team := &core.Bot{ID: "team-id", Name: "Ship", Members: []core.Member{{ID: "me-id", Name: "Fixer"}}, Team: &core.Team{LeadID: "me-id"}}
	for _, c := range []struct {
		team   *core.Bot
		global string
	}{{nil, ""}, {nil, "Be brief."}, {team, ""}, {team, "Be brief."}} {
		got := systemPrompt(promptContext{Bot: core.Bot{ID: "me-id", Name: "Fixer"}, Now: now, Team: c.team, Global: c.global})
		for _, want := range []string{
			"Write the question so it stands on its own, because the user may not have read earlier messages",
			"the options, each with a one-line consequence",
			"your recommendation and why",
			"what you will do if they do not answer",
			"Do not ask again a question that is still open",
			"give it a short question line; the app lists it as open until the user answers or dismisses it",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("team %v, global %q: prompt lacks %q", c.team != nil, c.global, want)
			}
		}
		if strings.Contains(got, "The app then shows the user that you are waiting.") {
			t.Errorf("team %v, global %q: the old sentence is still there", c.team != nil, c.global)
		}
	}
}

func TestQuestionNotesNameAnswersDismissalsAndOpenQuestions(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 9, 8, 12, 0, 0, time.UTC).UnixMilli()
	q := func(seq int64, text string) core.OpenQuestion {
		return core.OpenQuestion{Seq: seq, Question: text, CreatedAt: at}
	}
	if got := questionNotes(nil, nil, nil, time.UTC); got != "" {
		t.Fatalf("no questions = %q; want nothing", got)
	}
	got := questionNotes([]core.OpenQuestion{q(1, "Which CRM?")}, []core.OpenQuestion{q(2, "Sign up?")}, []core.OpenQuestion{q(412, "Shortlist?"), q(430, "Budget?")}, time.UTC)
	want := "The user answers your question (08:12): Which CRM?\n" +
		"The user dismissed your question: Sign up?. Do not ask it again.\n" +
		`Your open questions here (the user hasn't answered): #412 08:12 "Shortlist?"; #430 08:12 "Budget?". Close one with closes_questions when it's answered or no longer needed; don't ask it again.` + "\n\n"
	if got != want {
		t.Fatalf("notes =\n%q\nwant\n%q", got, want)
	}
}

func TestChangeHistoryListsEveryRound(t *testing.T) {
	t.Parallel()
	if got := changeHistory(nil); got != "" {
		t.Fatalf("no changes = %q", got)
	}
	got := changeHistory([]core.ItemEvent{{Round: 1, ActorName: "Rev", Text: "Fix A."}, {Round: 2, ActorName: "Rev", Text: "Fix B."}})
	want := "Change requests so far, oldest first; each still holds unless a later one says otherwise:\n- round 1 (Rev): Fix A.\n- round 2 (Rev): Fix B.\n"
	if got != want {
		t.Fatalf("history = %q; want %q", got, want)
	}
}

// After two rounds of changes, the owner's work turn and the reviewer's
// review turn both list round 1 and round 2.
func TestWorkAndReviewTurnsListEveryChangeRequest(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, ana, bo, _ := h.newTeam(t)
	items := h.newGoal(t, team, boardItem("Parse", "Parse it.", bo.ID, ana.ID))
	owner := core.Actor{BotID: bo.ID, TeamID: team.ID, Role: core.RoleWork}
	reviewer := core.Actor{BotID: ana.ID, TeamID: team.ID, Role: core.RoleReview}
	for _, note := range []string{"Handle empty input.", "Keep the empty-input fix and add a test."} {
		if err := h.svc.StartItemWork(ctx, items[0].ID, bo.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.SubmitItem(ctx, owner, items[0].ID, "parse.go"); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.RequestChanges(ctx, reviewer, items[0].ID, note); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		bot  core.Bot
		role string
	}{{bo, core.RoleWork}, {ana, core.RoleReview}} {
		if c.role == core.RoleReview {
			// The owner resubmits after round 2, and the reviewer checks it.
			if err := h.svc.StartItemWork(ctx, items[0].ID, bo.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := h.svc.SubmitItem(ctx, owner, items[0].ID, "parse.go with a test"); err != nil {
				t.Fatal(err)
			}
		}
		prompt, _, err := h.runner.turnPrompt(ctx, c.bot, core.Turn{ID: "t-" + c.role, BotID: c.bot.ID, RoomID: team.ID, BoardRole: c.role, ItemID: items[0].ID})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"- round 1 (Ana): Handle empty input.\n", "- round 2 (Ana): Keep the empty-input fix and add a test.\n"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s turn lacks %q", c.role, want)
			}
		}
	}
	if changes, err := h.runner.itemChanges(ctx, core.Turn{BoardRole: core.RoleLead, ItemID: items[0].ID}); err != nil || changes != nil {
		t.Errorf("a lead turn's changes = %v, %v; want none", changes, err)
	}
}

func directSession(t *testing.T, h *harness, bot core.Bot, turnID string) *mcp.ClientSession {
	t.Helper()
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	token, err := h.runner.tokens.issue(runContext{BotID: bot.ID, BotName: bot.Name, TurnID: turnID})
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

func TestSendToUserTakesAQuestionLineAndClosesOwnQuestions(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	bot := h.newBot(t)
	cs := directSession(t, h, bot, "t1")
	send := func(args map[string]any) (string, bool) {
		t.Helper()
		return callText(t, cs, "send_to_user", args)
	}
	if text, isErr := send(map[string]any{}); !isErr || !strings.Contains(text, "message text is empty") {
		t.Fatalf("an empty send = %q (error %v); want the empty-message error", text, isErr)
	}
	if text, isErr := send(map[string]any{"text": "Done.", "question": "Ship it?"}); !isErr || !strings.Contains(text, "only with needs_reply") {
		t.Fatalf("a question line without needs_reply = %q (error %v)", text, isErr)
	}
	if text, isErr := send(map[string]any{"text": "Which CRM should I set up?\n1. HubSpot…", "needs_reply": true, "question": "Which CRM should I set up?"}); isErr {
		t.Fatal(text)
	}
	open, err := h.svc.OpenQuestionsOf(ctx, bot.ID, bot.ID)
	if err != nil || len(open) != 1 || open[0].Question != "Which CRM should I set up?" {
		t.Fatalf("open questions = %+v, %v", open, err)
	}
	if text, isErr := send(map[string]any{"closes_questions": []int64{open[0].Seq + 100}}); !isErr || !strings.Contains(text, "#") {
		t.Fatalf("closing a question that isn't open = %q (error %v); want an error naming the seq", text, isErr)
	}
	if text, isErr := send(map[string]any{"closes_questions": []int64{open[0].Seq}}); isErr || text != "Closed 1 question(s)." {
		t.Fatalf("closing the question = %q (error %v)", text, isErr)
	}
	if open, _ := h.svc.OpenQuestionsOf(ctx, bot.ID, bot.ID); len(open) != 0 {
		t.Fatalf("still open: %+v", open)
	}
	if text, isErr := send(map[string]any{"text": "Next?", "needs_reply": true}); isErr {
		t.Fatal(text)
	}
	open, _ = h.svc.OpenQuestionsOf(ctx, bot.ID, bot.ID)
	if text, isErr := send(map[string]any{"text": "Never mind, done.", "closes_questions": []int64{open[0].Seq}}); isErr || text != "Delivered." {
		t.Fatalf("closing with a message = %q (error %v)", text, isErr)
	}
	if open, _ := h.svc.OpenQuestionsOf(ctx, bot.ID, bot.ID); len(open) != 0 {
		t.Fatalf("still open after the message: %+v", open)
	}
}

// The bot's turn after the user answers tells it which question was
// answered; after a dismissal, its next turn says so once; open questions
// are listed every turn.
func TestTurnsTellTheBotAboutItsQuestions(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	bot := h.newBot(t)
	ask := func(text string) core.Entry {
		t.Helper()
		e, err := h.svc.AppendBotMessageWith(ctx, bot.ID, "t0", text, nil, false, true, core.PostMeta{Question: text})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	crm, sign := ask("Which CRM?"), ask("May I sign up?")
	h.start(t)
	if _, _, err := h.svc.SubmitAnswer(ctx, bot.ID, "HubSpot.", "n1", []string{crm.ID}); err != nil {
		t.Fatal(err)
	}
	calls := h.waitCalls(t, 1)
	for _, want := range []string{"The user answers your question (", "): Which CRM?\n", `Your open questions here (the user hasn't answered): #`, `"May I sign up?"`} {
		if !strings.Contains(calls[0], want) {
			t.Errorf("first turn lacks %q", want)
		}
	}
	if _, err := h.svc.DismissQuestion(ctx, sign.ID); err != nil {
		t.Fatal(err)
	}
	for i, nonce := range []string{"n2", "n3"} {
		if _, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "Anything else?", nonce); err != nil {
			t.Fatal(err)
		}
		calls = h.waitCalls(t, i+2)
		got := calls[i+1]
		if has := strings.Contains(got, "The user dismissed your question: May I sign up?. Do not ask it again."); has != (i == 0) {
			t.Errorf("turn %d mentions the dismissal = %v; want only the first turn after it", i+2, has)
		}
		if strings.Contains(got, "Your open questions here") || strings.Contains(got, "The user answers your question") {
			t.Errorf("turn %d lists questions that are closed", i+2)
		}
	}
}
