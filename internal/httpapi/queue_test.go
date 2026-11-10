package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"yargpt/internal/core"
)

func TestGoalQueueEndpointsQueueReorderStartAndCancelGoals(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	ana, err := svc.CreateBot(ctx, "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	team, err := svc.CreateTeam(ctx, "Ship", "", []string{ana.ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	start := func(text string) core.Goal {
		t.Helper()
		rec := do(h, "POST", "/api/teams/"+team.ID+"/goals", `{"text":"`+text+`"}`, jsonHeader)
		var g core.Goal
		if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusCreated || err != nil {
			t.Fatalf("start goal: %d %s", rec.Code, rec.Body)
		}
		return g
	}
	first, second, third := start("One."), start("Two."), start("Three.")
	if first.State != core.GoalActive || second.State != core.GoalQueued || second.QueuePos != 1 || third.QueuePos != 2 {
		t.Fatalf("goals = %+v %+v %+v; want one active and two queued", first, second, third)
	}
	if rec := do(h, "POST", "/api/teams/"+team.ID+"/goal-queue", `{"order":["`+third.ID+`","`+second.ID+`"]}`, jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("reorder: %d %s", rec.Code, rec.Body)
	}
	rec := do(h, "GET", "/api/teams/"+team.ID+"/board", "", nil)
	var board core.Board
	if err := json.Unmarshal(rec.Body.Bytes(), &board); err != nil || len(board.Queue) != 2 || board.Queue[0].ID != third.ID || board.Queue[0].QueuePos != 1 {
		t.Fatalf("board: %s; want Three first in the queue", rec.Body)
	}
	for name, c := range map[string]struct {
		path, body string
		code       int
	}{
		"a reorder of another queue": {"/api/teams/" + team.ID + "/goal-queue", `{"order":["` + second.ID + `"]}`, http.StatusConflict},
		"a reorder with bad JSON":    {"/api/teams/" + team.ID + "/goal-queue", `{"order":`, http.StatusBadRequest},
		"a start while one is open":  {"/api/teams/" + team.ID + "/goal-queue/start", ``, http.StatusConflict},
		"a cancel of the active":     {"/api/goals/" + first.ID + "/cancel", ``, http.StatusBadRequest},
		"an end with bad JSON":       {"/api/goals/" + first.ID + "/end", `{"startNext":`, http.StatusBadRequest},
		"an accept with bad JSON":    {"/api/goals/" + first.ID + "/accept", `{"startNext":`, http.StatusBadRequest},
	} {
		if rec := do(h, "POST", c.path, c.body, jsonHeader); rec.Code != c.code {
			t.Errorf("%s: %d %s; want %d", name, rec.Code, rec.Body, c.code)
		}
	}
	if rec := do(h, "POST", "/api/goals/"+first.ID+"/end", `{"startNext":false}`, jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("end and hold: %d %s", rec.Code, rec.Body)
	}
	if b, _ := svc.Board(ctx, team.ID); len(b.Queue) != 2 {
		t.Fatalf("queue after an end that holds = %+v; want both goals", b.Queue)
	}
	rec = do(h, "POST", "/api/teams/"+team.ID+"/goal-queue/start", ``, jsonHeader)
	var started core.Goal
	if err := json.Unmarshal(rec.Body.Bytes(), &started); rec.Code != http.StatusOK || err != nil || started.ID != third.ID || started.State != core.GoalActive {
		t.Fatalf("start the next goal: %d %s; want Three active", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/goals/"+second.ID+"/cancel", ``, jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/goals/"+third.ID+"/end", ``, jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("end without a body: %d %s", rec.Code, rec.Body)
	}
	if b, _ := svc.Board(ctx, team.ID); len(b.Queue) != 0 || b.Goal.ID != third.ID || b.Goal.State != core.GoalEnded {
		t.Fatalf("board after the end = %+v; want Three ended and no queue", b)
	}
}

func TestCloseStartsTheNextGoalAndAcceptComesLater(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	ana, err := svc.CreateBot(ctx, "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	team, err := svc.CreateTeam(ctx, "Ship", "", []string{ana.ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	lead := core.Actor{BotID: ana.ID, TeamID: team.ID, Role: core.RoleLead, UserPost: true}
	deliver := func() core.Goal {
		t.Helper()
		b, _ := svc.Board(ctx, team.ID)
		it, err := svc.CreateItem(ctx, lead, core.NewItem{Title: "Do " + b.Goal.Text, Brief: "Do it.", Acceptance: "Done.", OwnerID: ana.ID})
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.StartItemWork(ctx, it.ID, ana.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SubmitItem(ctx, core.Actor{BotID: ana.ID, TeamID: team.ID, Role: core.RoleWork}, it.ID, "Done."); err != nil {
			t.Fatal(err)
		}
		g, err := svc.DeliverGoal(ctx, lead, "Done.")
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	first, err := svc.StartGoal(ctx, team.ID, "One.")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := svc.StartGoal(ctx, team.ID, "Two.")
	third, _ := svc.StartGoal(ctx, team.ID, "Three.")
	// A close starts the next goal at once; the accept comes later.
	deliver()
	if b, _ := svc.Board(ctx, team.ID); b.Goal.ID != second.ID || len(b.Queue) != 1 || len(b.Done) != 1 || b.Done[0].ID != first.ID {
		t.Fatalf("board after a close = %+v; want the next goal Now and the closed one done", b)
	}
	if rec := do(h, "POST", "/api/goals/"+first.ID+"/accept", `{"startNext":false}`, jsonHeader); rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	deliver()
	if rec := do(h, "POST", "/api/goals/"+second.ID+"/accept", ``, jsonHeader); rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	if b, _ := svc.Board(ctx, team.ID); b.Goal.ID != third.ID || b.Goal.State != core.GoalActive {
		t.Fatalf("board after the second close = %+v; want the third goal active", b.Goal)
	}
}

// The request endpoints are gone: requests are tasks now. Team settings
// still take requestSlots, which no longer limits anything, until the UI
// stops sending it.
func TestRequestEndpointsAreGoneAndRequestSlotsAreStillAccepted(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	var bots []core.Bot
	for _, name := range []string{"Dee", "Eve"} {
		b, err := svc.CreateBot(ctx, name, "")
		if err != nil {
			t.Fatal(err)
		}
		bots = append(bots, b)
	}
	docs, err := svc.CreateTeam(ctx, "Docs", "", []string{bots[0].ID, bots[1].ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.StartGoal(ctx, docs.ID, "Write docs.")
	if err != nil {
		t.Fatal(err)
	}
	it, err := svc.CreateItem(ctx, core.Actor{BotID: bots[0].ID, TeamID: docs.ID, Role: core.RoleLead}, core.NewItem{Title: "Guide", Brief: "Write it.", Acceptance: "Written."})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"withdraw", "undefer"} {
		if rec := do(h, "POST", "/api/items/"+it.ID+"/"+action, ``, jsonHeader); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST /api/items/{id}/%s: %d %s; want no such endpoint", action, rec.Code, rec.Body)
		}
	}
	rec := do(h, "PATCH", "/api/teams/"+docs.ID, `{"requestSlots":2}`, jsonHeader)
	var team struct{ Team core.Team }
	if err := json.Unmarshal(rec.Body.Bytes(), &team); rec.Code != http.StatusOK || err != nil || team.Team.RequestSlots != 2 {
		t.Fatalf("set request slots: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PATCH", "/api/teams/"+docs.ID, `{"requestSlots":3}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("too many request slots: %d %s; want 400", rec.Code, rec.Body)
	}
}
