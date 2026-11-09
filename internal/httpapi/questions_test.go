package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"yargpt/internal/core"
)

func TestItemEventsEndpointShowsEveryReviewRound(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	ana, _ := svc.CreateBot(ctx, "Ana", "")
	cy, _ := svc.CreateBot(ctx, "Cy", "")
	team, err := svc.CreateTeam(ctx, "Ship", "", []string{ana.ID, cy.ID}, core.TeamSettings{LeadID: ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartGoal(ctx, team.ID, "Ship it."); err != nil {
		t.Fatal(err)
	}
	lead := core.Actor{BotID: ana.ID, TeamID: team.ID, Role: core.RoleLead}
	it, err := svc.CreateItem(ctx, lead, core.NewItem{Title: "Parse", Brief: "Parse it.", Acceptance: "Parsed.", OwnerID: ana.ID, ReviewerID: cy.ID})
	if err != nil {
		t.Fatal(err)
	}
	owner, reviewer := core.Actor{BotID: ana.ID, TeamID: team.ID, Role: core.RoleWork}, core.Actor{BotID: cy.ID, TeamID: team.ID, Role: core.RoleReview}
	if err := svc.StartItemWork(ctx, it.ID, ana.ID); err != nil {
		t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { _, err := svc.SubmitItem(ctx, owner, it.ID, "v1"); return err },
		func() error { _, err := svc.RequestChanges(ctx, reviewer, it.ID, "Handle quotes."); return err },
		func() error { _, err := svc.SubmitItem(ctx, owner, it.ID, "v2"); return err },
		func() error { _, err := svc.RequestChanges(ctx, reviewer, it.ID, "Add tabs."); return err },
		func() error { _, err := svc.SubmitItem(ctx, owner, it.ID, "v3"); return err },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	rec := do(h, "GET", "/api/items/"+it.ID+"/events", "", nil)
	var events []core.ItemEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("events: %d %s", rec.Code, rec.Body)
	}
	var changes []core.ItemEvent
	for _, e := range events {
		if e.Kind == core.EventChanges {
			changes = append(changes, e)
		}
	}
	if len(events) != 6 || len(changes) != 2 || changes[0].Round != 1 || changes[0].Text != "Handle quotes." || changes[0].ActorName != "Cy" ||
		changes[1].Round != 2 || changes[1].Text != "Add tabs." {
		t.Fatalf("events = %s; want both change requests in order", rec.Body)
	}
	if rec := do(h, "GET", "/api/items/missing/events", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("events of a missing item: %d", rec.Code)
	}
}

func TestQuestionEndpointsListAnswerDismissAndReopen(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	ana, _ := svc.CreateBot(ctx, "Ana", "")
	first, err := svc.AppendBotMessageWith(ctx, ana.ID, "t1", "Which repo?", nil, false, true, core.PostMeta{Question: "Repo?"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.AppendBotMessage(ctx, ana.ID, "t1", "Which branch?", nil, false, true)
	if err != nil {
		t.Fatal(err)
	}
	rec := do(h, "GET", "/api/bots/"+ana.ID+"/questions", "", nil)
	var open []core.OpenQuestion
	if err := json.Unmarshal(rec.Body.Bytes(), &open); rec.Code != http.StatusOK || err != nil || len(open) != 2 || open[0].Question != "Repo?" {
		t.Fatalf("chat questions: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/bots/missing/questions", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("questions of a missing chat: %d", rec.Code)
	}
	rec = do(h, "POST", "/api/bots/"+ana.ID+"/messages", `{"text":"yarstack","clientNonce":"n1","answers":["`+first.ID+`"]}`, jsonHeader)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/bots/"+ana.ID+"/messages", `{"text":"x","clientNonce":"n2","answers":["`+first.ID+`"]}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("answer a closed question: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "POST", "/api/entries/"+second.ID+"/dismiss", "", jsonHeader)
	var e core.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &e); rec.Code != http.StatusOK || err != nil || e.ID != second.ID {
		t.Fatalf("dismiss: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/entries/"+second.ID+"/dismiss", "", jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("dismiss twice: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/questions", "", nil)
	var w core.Waiting
	if err := json.Unmarshal(rec.Body.Bytes(), &w); rec.Code != http.StatusOK || err != nil || len(w.Questions) != 0 || w.ClosedRecently != 2 {
		t.Fatalf("waiting: %d %s; want nothing open and 2 closed", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/entries/"+second.ID+"/reopen", "", jsonHeader); rec.Code != http.StatusOK {
		t.Fatalf("reopen: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/entries/"+second.ID+"/reopen", "", jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("reopen an open question: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/questions", "", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &w); err != nil || len(w.Questions) != 1 || w.Questions[0].EntryID != second.ID || w.Questions[0].AskerName != "Ana" {
		t.Fatalf("waiting after undo: %s", rec.Body)
	}
	rec = do(h, "GET", "/api/bots", "", nil)
	var bots []struct {
		ID            string `json:"id"`
		Attention     string `json:"attention"`
		OpenQuestions int64  `json:"openQuestions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bots); err != nil {
		t.Fatal(err)
	}
	for _, b := range bots {
		if b.ID == ana.ID && (b.OpenQuestions != 1 || b.Attention != core.AttentionQuestion) {
			t.Fatalf("bot in the list = %+v; want 1 open question", b)
		}
	}
}
