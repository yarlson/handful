package core

import (
	"context"
	"testing"
)

func TestTurnAnswersNamesTheQuestionsAUserMessageAnswers(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	ana := newAgents(t, svc, "Ana")["Ana"]
	crm := ask(t, svc, ana.ID, "Which CRM?\nOptions…", "Which CRM?")
	ask(t, svc, ana.ID, "Budget?", "")
	turnID, _, err := svc.SubmitAnswer(ctx, ana.ID, "HubSpot.", "n1", []string{crm.ID})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.TurnAnswers(ctx, ana.ID, turnID)
	if err != nil || len(got) != 1 || got[0].Question != "Which CRM?" || got[0].EntryID != crm.ID || got[0].AskerID != ana.ID {
		t.Fatalf("TurnAnswers = %+v, %v; want the CRM question", got, err)
	}
	plain, _, err := svc.SubmitUserMessage(ctx, ana.ID, "Thanks.", "n2")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.TurnAnswers(ctx, ana.ID, plain); err != nil || len(got) != 1 || got[0].Question != "Budget?" {
		t.Fatalf("a plain reply to the one open question = %+v, %v; want it answered", got, err)
	}
	if got, err := svc.TurnAnswers(ctx, ana.ID, "no-such-turn"); err != nil || got != nil {
		t.Fatalf("a turn without a user message = %+v, %v; want none", got, err)
	}
	if _, err := svc.sqlDB.ExecContext(ctx, "UPDATE entries SET body = '{' WHERE turn_id = ? AND kind = 'user'", turnID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TurnAnswers(ctx, ana.ID, turnID); err == nil {
		t.Fatal("an unreadable user message gave no error")
	}
	if _, err := svc.sqlDB.ExecContext(ctx, `UPDATE entries SET body = ? WHERE turn_id = ? AND kind = 'user'`, `{"answers":["gone","`+crm.ID+`"]}`, turnID); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.TurnAnswers(ctx, ana.ID, turnID); err != nil || len(got) != 1 || got[0].EntryID != crm.ID {
		t.Fatalf("an answer to a missing question = %+v, %v; want it skipped and the rest kept", got, err)
	}
	svc.sqlDB.Close()
	if _, err := svc.TurnAnswers(ctx, ana.ID, turnID); err == nil {
		t.Fatal("a closed database gave no error")
	}
	if _, err := svc.PreviousTurnStart(ctx, ana.ID, "", turnID); err == nil {
		t.Fatal("a closed database gave no error")
	}
}

func TestPreviousTurnStartIsTheLatestEarlierStartedTurnInTheChat(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	ana := bots["Ana"]
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{ana.ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(id, roomID string, started any) {
		t.Helper()
		if _, err := svc.sqlDB.ExecContext(ctx, "INSERT INTO turns (id, bot_id, source, prompt, status, room_id, created_at, started_at) VALUES (?, ?, 'user', '', 'done', ?, 1, ?)", id, ana.ID, roomID, started); err != nil {
			t.Fatal(err)
		}
	}
	insert("a", "", 100)
	insert("b", "", 300)
	insert("c", "", nil)
	insert("r", room.ID, 900)
	for _, c := range []struct {
		room, turn string
		want       int64
	}{{"", "c", 300}, {"", "b", 100}, {room.ID, "x", 900}, {room.ID, "r", 0}} {
		if got, err := svc.PreviousTurnStart(ctx, ana.ID, c.room, c.turn); err != nil || got != c.want {
			t.Errorf("PreviousTurnStart(room %q, before %s) = %d, %v; want %d", c.room, c.turn, got, err, c.want)
		}
	}
}
