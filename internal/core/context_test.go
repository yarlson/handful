package core

import (
	"context"
	"testing"
)

func TestClearContextDropsSessionsAndKeepsTheTranscript(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	ana := bots["Ana"]
	if err := svc.SetSession(ctx, ana.ID, "ana-main"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SubmitUserMessage(ctx, ana.ID, "remember this", "n1"); err != nil {
		t.Fatal(err)
	}
	before, _ := svc.Entries(ctx, ana.ID, 0, 0)

	if err := svc.ClearContext(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.GetBot(ctx, ana.ID)
	if got.ClaudeSessionID != "" {
		t.Fatalf("session after clear = %q; want empty so the next turn starts fresh", got.ClaudeSessionID)
	}
	after, _ := svc.Entries(ctx, ana.ID, 0, 0)
	if len(after) != len(before)+1 || after[len(after)-1].Kind != KindEvent {
		t.Fatalf("entries after clear = %d (last %s); want the old %d plus a context-cleared event", len(after), after[len(after)-1].Kind, len(before))
	}

	room, err := svc.CreateRoom(ctx, "Plan", "", []string{ana.ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bots {
		if err := svc.SetRoomSession(ctx, room.ID, b.ID, "room-"+b.Name); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.ClearContext(ctx, room.ID); err != nil {
		t.Fatal(err)
	}
	seats, _ := svc.RoomSeats(ctx, room.ID)
	for _, seat := range seats {
		if seat.ClaudeSessionID != "" {
			t.Fatalf("%s room session after clear = %q; want empty", seat.Name, seat.ClaudeSessionID)
		}
	}
}

func TestQueueCompactionQueuesOneCompactionPerSession(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	if n, err := svc.QueueCompaction(ctx, bots["Ana"].ID); err != nil || n != 1 {
		t.Fatalf("bot compaction: n=%d err=%v; want 1", n, err)
	}
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := svc.QueueCompaction(ctx, room.ID); err != nil || n != 2 {
		t.Fatalf("room compaction: n=%d err=%v; want one per member", n, err)
	}
	turns, _ := svc.QueuedTurns(ctx, 10)
	if len(turns) != 3 {
		t.Fatalf("queued = %d; want 3", len(turns))
	}
	for i, tr := range turns {
		wantRoom := ""
		if i > 0 {
			wantRoom = room.ID
		}
		if tr.Source != SourceCompact || tr.RoomID != wantRoom {
			t.Fatalf("turn %d = %+v; want a compact turn in room %q", i, tr, wantRoom)
		}
	}
}
