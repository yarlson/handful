package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"yargpt/internal/core"
)

func TestTaskEndpointsSendTakeOrderPinStopReopenAndShowHistory(t *testing.T) {
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
	lead := core.Actor{BotID: ana.ID, TeamID: team.ID, Role: core.RoleLead}
	send := func(body string) core.Goal {
		t.Helper()
		rec := do(h, "POST", "/api/teams/"+team.ID+"/tasks", body, jsonHeader)
		var g core.Goal
		if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusCreated || err != nil {
			t.Fatalf("send %s: %d %s", body, rec.Code, rec.Body)
		}
		return g
	}
	inbox := send(`{"text":"Into the inbox.","brief":"Why."}`)
	if inbox.State != core.GoalIncoming || inbox.Brief != "Why." {
		t.Fatalf("user task = %+v; want it in the inbox", inbox)
	}
	now := send(`{"text":"Start now.","startNow":true}`)
	if now.State != core.GoalActive {
		t.Fatalf("user task started now = %+v; want it active", now)
	}
	if rec := do(h, "POST", "/api/teams/"+team.ID+"/tasks", `{"text":""}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("a task without text: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/teams/"+team.ID+"/tasks", `{`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("a task with bad JSON: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/tasks/"+inbox.ID+"/take", `{"position":1}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("the user takes a task its lead decides on: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/tasks/"+inbox.ID+"/take", `{`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("take with bad JSON: %d", rec.Code)
	}
	one, err := svc.TakeTask(ctx, lead, inbox.ID, 0)
	if err != nil || one.State != core.GoalQueued {
		t.Fatalf("the lead takes it = %+v, %v", one, err)
	}
	two := send(`{"text":"Two.","startNow":true}`)
	if rec := do(h, "POST", "/api/teams/"+team.ID+"/plan", `{"order":["`+one.ID+`","`+two.ID+`"]}`, jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("order: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/teams/"+team.ID+"/plan", `{"order":["`+one.ID+`"]}`, jsonHeader); rec.Code != http.StatusConflict {
		t.Fatalf("an order that misses a task: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/teams/"+team.ID+"/plan", `{`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("order with bad JSON: %d", rec.Code)
	}
	rec := do(h, "POST", "/api/tasks/"+one.ID+"/pin", ``, jsonHeader)
	var g core.Goal
	if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusOK || err != nil || !g.Pinned {
		t.Fatalf("pin: %d %s", rec.Code, rec.Body)
	}
	if err := svc.OrderPlan(ctx, lead, team.ID, []string{two.ID, one.ID}); err == nil {
		t.Fatal("the lead moved a pinned task")
	}
	rec = do(h, "DELETE", "/api/tasks/"+one.ID+"/pin", "", nil)
	var unpinned core.Goal
	if err := json.Unmarshal(rec.Body.Bytes(), &unpinned); rec.Code != http.StatusOK || err != nil || unpinned.Pinned {
		t.Fatalf("unpin: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/tasks/"+now.ID+"/pin", ``, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("pin the Now task: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "POST", "/api/tasks/"+now.ID+"/stop", ``, jsonHeader)
	if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusOK || err != nil || g.State != core.GoalStopped {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "POST", "/api/tasks/"+now.ID+"/reopen", ``, jsonHeader)
	if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusOK || err != nil || g.State != core.GoalIncoming {
		t.Fatalf("reopen: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "POST", "/api/tasks/"+now.ID+"/push-back", `{"text":"Not now."}`, jsonHeader)
	if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusOK || err != nil || g.State != core.GoalDeclined {
		t.Fatalf("push back: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/tasks/"+now.ID+"/events", "", nil)
	var events []core.ItemEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); rec.Code != http.StatusOK || err != nil || len(events) != 4 ||
		events[0].Kind != "sent" || events[1].Kind != "stopped" || events[2].Kind != "reopened" || events[3].Kind != "pushed_back" || events[3].Text != "Not now." || events[3].GoalID != now.ID {
		t.Fatalf("events: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/tasks/missing/events", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("events of a missing task: %d", rec.Code)
	}
	rec = do(h, "POST", "/api/tasks/"+two.ID+"/withdraw", ``, jsonHeader)
	if err := json.Unmarshal(rec.Body.Bytes(), &g); rec.Code != http.StatusOK || err != nil || g.State != core.GoalEnded {
		t.Fatalf("withdraw: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/teams/"+team.ID+"/board", "", nil)
	// Stopping the Now task started the next one; the other was withdrawn.
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"goal":{"id":"`+one.ID+`"`) || !strings.Contains(rec.Body.String(), `"plan":[]`) || !strings.Contains(rec.Body.String(), `"inbox":[]`) {
		t.Fatalf("board: %s", rec.Body)
	}
}

func TestTaskEndpointsApproveAndAskForChanges(t *testing.T) {
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
	lead := core.Actor{BotID: ana.ID, TeamID: team.ID, Role: core.RoleLead}
	round := 0
	closeTask := func(g core.Goal) {
		t.Helper()
		round++
		it, err := svc.CreateItem(ctx, lead, core.NewItem{Title: "Do " + g.Text + strings.Repeat("!", round), Brief: "Do it.", Acceptance: "Done.", OwnerID: ana.ID})
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.StartItemWork(ctx, it.ID, ana.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SubmitItem(ctx, core.Actor{BotID: ana.ID, TeamID: team.ID, Role: core.RoleWork}, it.ID, "Done."); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CloseTask(ctx, lead, g.ID, "Done."); err != nil {
			t.Fatal(err)
		}
	}
	g, err := svc.UserTask(ctx, team.ID, core.NewTask{Text: "Ship it."}, true)
	if err != nil {
		t.Fatal(err)
	}
	closeTask(g)
	rec := do(h, "POST", "/api/tasks/"+g.ID+"/changes", `{"text":"Also docs."}`, jsonHeader)
	var got core.Goal
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != http.StatusOK || err != nil || got.State != core.GoalActive {
		t.Fatalf("changes: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/tasks/"+g.ID+"/changes", `{"text":""}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("changes to a task that is not done: %d %s", rec.Code, rec.Body)
	}
	closeTask(g)
	rec = do(h, "POST", "/api/tasks/"+g.ID+"/approve", ``, jsonHeader)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != http.StatusOK || err != nil || got.State != core.GoalDone {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/tasks/missing/approve", ``, jsonHeader); rec.Code != http.StatusNotFound {
		t.Fatalf("approve a missing task: %d", rec.Code)
	}
}

func TestNeedsYouListsTheUsersTasksAndQuestionsAndTheDecisionTimeIsATeamSetting(t *testing.T) {
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
	if rec := do(h, "GET", "/api/needs-you", "", nil); rec.Code != http.StatusOK || rec.Body.String() != "[]\n" {
		t.Fatalf("needs you with nothing = %d %s; want []", rec.Code, rec.Body)
	}
	if _, err := svc.AppendBotMessage(ctx, ana.ID, "", "Which format?", nil, false, true); err != nil {
		t.Fatal(err)
	}
	rec := do(h, "GET", "/api/needs-you", "", nil)
	var needs []core.NeedsYou
	if err := json.Unmarshal(rec.Body.Bytes(), &needs); rec.Code != http.StatusOK || err != nil || len(needs) != 1 || needs[0].Kind != core.NeedQuestion || needs[0].ChatID != ana.ID {
		t.Fatalf("needs you = %d %s; want Ana's question", rec.Code, rec.Body)
	}
	for body, want := range map[string]int{`{"decisionMinutes":5}`: 200, `{"decisionMinutes":1440}`: 200, `{"decisionMinutes":4}`: 400, `{"decisionMinutes":1441}`: 400, `{"decisionMinutes":0}`: 400} {
		if rec := do(h, "PATCH", "/api/teams/"+team.ID, body, jsonHeader); rec.Code != want {
			t.Errorf("PATCH %s = %d %s; want %d", body, rec.Code, rec.Body, want)
		}
	}
	if rec := do(h, "PATCH", "/api/teams/"+team.ID, `{"decisionMinutes":45}`, jsonHeader); !strings.Contains(rec.Body.String(), `"decisionMinutes":45`) {
		t.Fatalf("PATCH 45 = %s; want the team with 45 minutes", rec.Body)
	}
}
