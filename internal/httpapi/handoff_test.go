package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"handful/internal/core"
)

func TestHandoffEndpointsHandOverAnswerAcceptDeclineAndWithdraw(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	var bots []core.Bot
	for _, name := range []string{"Ana", "Dee"} {
		b, err := svc.CreateBot(ctx, name, "")
		if err != nil {
			t.Fatal(err)
		}
		bots = append(bots, b)
	}
	ship, err := svc.CreateTeam(ctx, "Ship", "", []string{bots[0].ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	docs, err := svc.CreateTeam(ctx, "Docs", "", []string{bots[1].ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	handOver := func(text string) core.Goal {
		t.Helper()
		rec := do(h, "POST", "/api/teams/"+docs.ID+"/handoffs", `{"fromTeamId":"`+ship.ID+`","text":"`+text+`","brief":"Why."}`, jsonHeader)
		var g core.Goal
		if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusCreated || err != nil || g.State != core.GoalIncoming || g.FromTeamID != ship.ID || g.Brief != "Why." {
			t.Fatalf("hand over: %d %s", rec.Code, rec.Body)
		}
		return g
	}
	if rec := do(h, "POST", "/api/teams/"+docs.ID+"/handoffs", `{"text":""}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("hand over without a goal text: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/teams/"+docs.ID+"/handoffs", `{`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("hand over with bad JSON: %d %s", rec.Code, rec.Body)
	}
	first, second, third := handOver("One."), handOver("Two."), handOver("Three.")

	// The receiving lead asks the user's handoff; the user answers.
	if _, err := svc.AskSender(ctx, core.Actor{BotID: bots[1].ID, TeamID: docs.ID, Role: core.RoleLead}, first.ID, "Why now?"); err != nil {
		t.Fatal(err)
	}
	rec := do(h, "POST", "/api/goals/"+first.ID+"/answer-handoff", `{"text":"Users wait."}`, jsonHeader)
	var g core.Goal
	if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusOK || err != nil || !strings.HasSuffix(g.Brief, "Answer from The user: Users wait.") {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/goals/"+first.ID+"/answer-handoff", `{`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("answer with bad JSON: %d %s", rec.Code, rec.Body)
	}
	// The user has no Accept: the lead takes a task in.
	if rec := do(h, "POST", "/api/goals/"+first.ID+"/accept-handoff", ``, jsonHeader); rec.Code != http.StatusNotFound {
		t.Fatalf("the old accept: %d %s; want it gone", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/tasks/"+first.ID+"/take", ``, jsonHeader); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "lead decides") {
		t.Fatalf("the user takes a task its lead decides on: %d %s; want 400", rec.Code, rec.Body)
	}
	if g, err = svc.TakeTask(ctx, core.Actor{BotID: bots[1].ID, TeamID: docs.ID, Role: core.RoleLead}, first.ID, 0); err != nil || g.State != core.GoalActive {
		t.Fatalf("the lead takes it = %+v, %v; want it active", g, err)
	}
	rec = do(h, "POST", "/api/goals/"+second.ID+"/decline-handoff", `{"text":"Not ours."}`, jsonHeader)
	if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusOK || err != nil || g.State != core.GoalDeclined || g.IntakeNote != "Not ours." {
		t.Fatalf("decline: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/goals/"+third.ID+"/decline-handoff", `{"text":""}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("decline without a reason: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "POST", "/api/goals/"+third.ID+"/withdraw-handoff", ``, jsonHeader)
	if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusOK || err != nil || g.State != core.GoalEnded {
		t.Fatalf("withdraw: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/goals/missing/accept-handoff", ``, jsonHeader); rec.Code != http.StatusNotFound {
		t.Fatalf("accept a missing goal: %d %s", rec.Code, rec.Body)
	}

	rec = do(h, "GET", "/api/teams/"+ship.ID+"/board", "", nil)
	var board core.Board
	if err := json.Unmarshal(rec.Body.Bytes(), &board); err != nil || len(board.HandedOver) != 3 || board.Teams[docs.ID] != "Docs" {
		t.Fatalf("Ship's board: %s", rec.Body)
	}
	fourth := handOver("Four.")
	rec = do(h, "GET", "/api/teams/"+docs.ID+"/board", "", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &board); err != nil || len(board.Inbox) != 1 || board.Inbox[0].ID != fourth.ID || !strings.Contains(rec.Body.String(), `"origin":"handoff"`) {
		t.Fatalf("Docs's board: %s", rec.Body)
	}
	rec = do(h, "GET", "/api/bots", "", nil)
	if !strings.Contains(rec.Body.String(), `"incomingHandoffs":1`) {
		t.Fatalf("bots: %s; want Docs's incoming handoff", rec.Body)
	}

	// startHandoffs is gone: a form that still sends it saves the rest.
	rec = do(h, "PATCH", "/api/teams/"+docs.ID, `{"startHandoffs":"ask","inboxMax":5,"inboxPerSender":2}`, jsonHeader)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "startHandoffs") || !strings.Contains(rec.Body.String(), `"inboxMax":5,"inboxPerSender":2`) {
		t.Fatalf("set the inbox limits: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PATCH", "/api/teams/"+docs.ID, `{"inboxPerSender":6}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("set more per sender than the inbox holds: %d %s", rec.Code, rec.Body)
	}
}
