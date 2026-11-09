package core

import (
	"context"
	"encoding/json"
	"testing"
)

func attentionOf(t *testing.T, svc *Service, id string) string {
	t.Helper()
	bots, err := svc.ListBots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bots {
		if b.ID == id {
			return b.Attention
		}
	}
	t.Fatalf("bot %s not listed", id)
	return ""
}

func TestAttentionShowsPendingApprovalsUntilResolved(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	ana := bots["Ana"]
	if got := attentionOf(t, svc, ana.ID); got != "" {
		t.Fatalf("attention of an idle bot = %q; want none", got)
	}
	events, stop := svc.Hub().Subscribe()
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = svc.RequestApproval(ctx, ApprovalRequest{BotID: ana.ID, TranscriptID: ana.ID, TurnID: "t1", Tool: "Bash", Input: json.RawMessage(`{}`)})
	}()
	var card string
	for card == "" {
		if e := <-events; e.Type == "entry" && e.Entry.Kind == KindApproval {
			card = e.Entry.ID
		}
	}
	if got := attentionOf(t, svc, ana.ID); got != AttentionApproval {
		t.Fatalf("attention with a pending card = %q; want %q", got, AttentionApproval)
	}
	if got := attentionOf(t, svc, bots["Bo"].ID); got != "" {
		t.Fatalf("attention of another bot = %q; want none", got)
	}
	if err := svc.ResolveApproval(ctx, card, DecisionDeny); err != nil {
		t.Fatal(err)
	}
	<-done
	if got := attentionOf(t, svc, ana.ID); got != "" {
		t.Fatalf("attention after the card was resolved = %q; want none", got)
	}
}

func TestAttentionShowsAQuestionUntilTheUserAnswers(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	ana := newAgents(t, svc, "Ana")["Ana"]
	if _, err := svc.AppendBotMessage(ctx, ana.ID, "t1", "Done with the draft.", nil, false, false, PostMeta{}); err != nil {
		t.Fatal(err)
	}
	if got := attentionOf(t, svc, ana.ID); got != "" {
		t.Fatalf("attention after a plain message = %q; want none", got)
	}
	if _, err := svc.AppendBotMessage(ctx, ana.ID, "t1", "Which repo should I use?", nil, false, true, PostMeta{}); err != nil {
		t.Fatal(err)
	}
	if got := attentionOf(t, svc, ana.ID); got != AttentionQuestion {
		t.Fatalf("attention after a question = %q; want %q", got, AttentionQuestion)
	}
	if _, _, err := svc.SubmitUserMessage(ctx, ana.ID, "Use yarstack.", "n1"); err != nil {
		t.Fatal(err)
	}
	if got := attentionOf(t, svc, ana.ID); got != "" {
		t.Fatalf("attention after the user answered = %q; want none", got)
	}
}

func TestRoomShowsAttentionForQuestionsAndCardsInIt(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostToRoom(ctx, room.ID, bots["Ana"].ID, "t1", "Should we ship today?", nil, 0, true); err != nil {
		t.Fatal(err)
	}
	if got := attentionOf(t, svc, room.ID); got != AttentionQuestion {
		t.Fatalf("room attention after a member's question = %q; want %q", got, AttentionQuestion)
	}
	if got := attentionOf(t, svc, bots["Ana"].ID); got != "" {
		t.Fatalf("the asking member's own attention = %q; want none (the question is in the room)", got)
	}
}
