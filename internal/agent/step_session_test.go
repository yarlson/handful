package agent

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"handful/internal/core"
)

var sessionArg = regexp.MustCompile(`--(session-id|resume) (\S+)`)

// runQueuedTurnOf starts the queued turns, waits until they end and returns the session flag
// ("session-id" or "resume"), the session id and the message of the claude call of the
// member named name. The tests start turns one batch at a time, so that no work turn ends
// with its step blocked for want of a live process.
func runQueuedTurnOf(t *testing.T, h *harness, name string) (flag, session, message string) {
	t.Helper()
	before := len(h.messages(t))
	h.runner.dispatch(context.Background())
	h.runner.wg.Wait()
	calls, messages := h.argsLog(t), h.messages(t)
	for i := before; i < len(calls); i++ {
		if !strings.Contains(messages[i], "Members: you ("+name+")") {
			continue
		}
		args, _, _ := strings.Cut(calls[i], "<<SYSTEM>>")
		m := sessionArg.FindStringSubmatch(args)
		if m == nil {
			t.Fatalf("%s's claude call has no session flag: %s", name, args)
		}
		return m[1], m[2], messages[i]
	}
	t.Fatalf("no claude call of %s in %d new calls", name, len(calls)-before)
	return "", "", ""
}

func latestTurnID(t *testing.T, h *harness, botID, role string) string {
	t.Helper()
	var id string
	if err := h.conn.QueryRowContext(context.Background(), `SELECT id FROM turns WHERE bot_id = ? AND board_role = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, botID, role).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// A step's owner and reviewer each start a session of their own on their first turn, resume
// it on every later round, and go back to their room session once the step is done.
func TestEachRoleOfAStepRunsInASessionOfItsOwn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, ana, bo, cy := h.newTeam(t)
	team, err := h.svc.SetRoomMembers(ctx, team.ID, []string{ana.ID, bo.ID, cy.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []core.Bot{bo, cy} {
		if err := h.svc.SetRoomSession(ctx, team.ID, b.ID, "room-"+b.Name); err != nil {
			t.Fatal(err)
		}
	}
	// Ana's step keeps the task open after Parse is done.
	items := h.newGoal(t, team, boardItem("Parse", "Parse the CSV file.", bo.ID, cy.ID), boardItem("Docs", "Write docs.md.", ana.ID, ""))
	owner := core.Actor{BotID: bo.ID, TeamID: team.ID, Role: core.RoleWork}
	reviewer := core.Actor{BotID: cy.ID, TeamID: team.ID, Role: core.RoleReview}
	type call struct{ flag, session string }
	turn := func(name string) (call, string) {
		t.Helper()
		flag, session, message := runQueuedTurnOf(t, h, name)
		return call{flag, session}, message
	}

	work, message := turn("Bo")
	if work.flag != "session-id" || work.session == "room-Bo" {
		t.Fatalf("Bo's first work turn = %+v; want a new session, not Bo's room session", work)
	}
	if !strings.Contains(message, "This turn starts a new session for this step. Recent messages:\n") {
		t.Errorf("Bo's first work turn lacks the new-session line:\n%s", message)
	}
	if _, err := h.svc.SubmitItem(ctx, owner, items[0].ID, "parse.go"); err != nil {
		t.Fatal(err)
	}
	review, _ := turn("Cy")
	if review.flag != "session-id" || review.session == "room-Cy" || review.session == work.session {
		t.Fatalf("Cy's first review turn = %+v; want a new session, apart from Cy's room session and Bo's work session %s", review, work.session)
	}
	if _, err := h.svc.RequestChanges(ctx, reviewer, items[0].ID, "Handle empty lines."); err != nil {
		t.Fatal(err)
	}
	if got, message := turn("Bo"); got != (call{"resume", work.session}) || !strings.Contains(message, "New messages since your last turn on this step:\n") {
		t.Fatalf("Bo's second work turn = %+v; want to resume %s with the messages since Bo's last turn on the step:\n%s", got, work.session, message)
	}
	if _, err := h.svc.SubmitItem(ctx, owner, items[0].ID, "parse.go, with empty lines"); err != nil {
		t.Fatal(err)
	}
	if got, _ := turn("Cy"); got != (call{"resume", review.session}) {
		t.Fatalf("Cy's second review turn = %+v; want to resume %s", got, review.session)
	}
	if _, err := h.svc.PassItem(ctx, reviewer, items[0].ID, "Good."); err != nil {
		t.Fatal(err)
	}
	// A post from the review turn wakes Bo on the same step, which is done now.
	if _, err := h.svc.PostToRoom(ctx, team.ID, cy.ID, latestTurnID(t, h, cy.ID, core.RoleReview), "@Bo passed.", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := turn("Bo"); got != (call{"resume", "room-Bo"}) {
		t.Fatalf("Bo's turn after the step is done = %+v; want to resume Bo's room session", got)
	}
}

// A step turn reads and marks the room through its own cursor, so the member's next room turn
// gets every message since its last room turn once, its own step posts included.
func TestARoomTurnAfterAStepTurnGetsEachMessageOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, ana, bo, _ := h.newTeam(t)
	if err := h.svc.SetRoomSession(ctx, team.ID, bo.ID, "room-Bo"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.PostToRoom(ctx, team.ID, bo.ID, "", "Before the step.", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	var seen int64
	if err := h.conn.QueryRowContext(ctx, `SELECT MAX(seq) FROM entries WHERE bot_id = ?`, team.ID).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.MarkRoomSeen(ctx, team.ID, bo.ID, core.Step{}, seen); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.PostToRoom(ctx, team.ID, ana.ID, "", "The CSV is in data/.", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	h.newGoal(t, team, boardItem("Parse", "Parse the CSV file.", bo.ID, ""))

	if _, _, message := runQueuedTurnOf(t, h, "Bo"); !strings.Contains(message, "Your step:") || !strings.Contains(message, "Bo: Before the step.") {
		t.Fatalf("Bo's first work turn lacks the step or the recent messages:\n%s", message)
	}
	if _, err := h.svc.PostToRoom(ctx, team.ID, bo.ID, latestTurnID(t, h, bo.ID, core.RoleWork), "Parsed it in parse.go.", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	seats, err := h.svc.RoomSeats(ctx, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range seats {
		if s.ID == bo.ID && (s.LastSeenSeq != seen || s.ClaudeSessionID != "room-Bo") {
			t.Fatalf("Bo's room seat after the work turn: seen %d, session %q; want %d, room-Bo", s.LastSeenSeq, s.ClaudeSessionID, seen)
		}
	}

	if _, _, err := h.svc.SubmitUserMessage(ctx, team.ID, "@Bo how did it go?", "n1"); err != nil {
		t.Fatal(err)
	}
	flag, session, message := runQueuedTurnOf(t, h, "Bo")
	if flag != "resume" || session != "room-Bo" {
		t.Fatalf("Bo's room turn: %s %s; want to resume room-Bo", flag, session)
	}
	for text, want := range map[string]int{"Ana: The CSV is in data/.": 1, "Bo: Parsed it in parse.go.": 1, "@Bo how did it go?": 1, "Before the step.": 0} {
		if got := strings.Count(message, text); got != want {
			t.Errorf("Bo's room turn has %q %d times; want %d:\n%s", text, got, want, message)
		}
	}
	if _, _, err := h.svc.SubmitUserMessage(ctx, team.ID, "@Bo and the tests?", "n2"); err != nil {
		t.Fatal(err)
	}
	_, _, message = runQueuedTurnOf(t, h, "Bo")
	for _, text := range []string{"The CSV is in data/.", "Parsed it in parse.go.", "how did it go?"} {
		if strings.Contains(message, text) {
			t.Errorf("Bo's next room turn repeats %q:\n%s", text, message)
		}
	}
}

// A live process on a member's step counts as the member's session in that step's room, so the
// step is not settled as stopped while it runs; a process in another room does not count.
func TestAStepProcessCountsAsASessionInItsRoomOnly(t *testing.T) {
	t.Parallel()
	r := newHarness(t, "reply").runner
	onStep := sessionKey{botID: "bo", roomID: "team", step: core.Step{ItemID: "i1", Role: core.RoleWork}}
	elsewhere := sessionKey{botID: "cy", roomID: "other"}
	r.sessions[onStep] = &session{key: onStep}
	r.sessions[elsewhere] = &session{key: elsewhere}
	for _, c := range []struct {
		botID, roomID string
		want          bool
	}{
		{"bo", "team", true},
		{"bo", "other", false},
		{"cy", "team", false},
		{"cy", "other", true},
	} {
		if got := r.hasSession(c.botID, c.roomID); got != c.want {
			t.Errorf("hasSession(%s, %s) = %v; want %v", c.botID, c.roomID, got, c.want)
		}
	}
}

// After the team's context is cleared, the owner's next turn on the step starts a new session.
func TestClearContextStartsANewStepSession(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, ana, bo, _ := h.newTeam(t)
	items := h.newGoal(t, team, boardItem("Parse", "Parse the CSV file.", bo.ID, ""))
	_, first, _ := runQueuedTurnOf(t, h, "Bo")
	if err := h.svc.ClearContext(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.BlockItem(ctx, core.Actor{BotID: bo.ID, TeamID: team.ID, Role: core.RoleWork}, items[0].ID, "Which file?"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.UnblockItem(ctx, core.Actor{BotID: ana.ID, TeamID: team.ID, Role: core.RoleLead}, items[0].ID, "data.csv"); err != nil {
		t.Fatal(err)
	}
	if flag, session, _ := runQueuedTurnOf(t, h, "Bo"); flag != "session-id" || session == first {
		t.Fatalf("Bo's work turn after Clear context: %s %s; want a new session, not %s", flag, session, first)
	}
}
