package core

import (
	"context"
	"testing"
	"time"
)

// The latest recorded turn of a bot, in any of its chats, is its last turn usage; a second record
// of the same turn replaces the first, and other bots' turns don't count.
func TestLastTurnUsageIsTheBotsLatestRecord(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	at := time.UnixMilli(1000)
	svc.now = func() time.Time { return at }
	ana, err := svc.CreateBot(ctx, "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	bo, err := svc.CreateBot(ctx, "Bo", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := func(bot Bot) string {
		t.Helper()
		tr, err := svc.StartTaskTurn(ctx, bot.ID, "", "[task]", 0, "")
		if err != nil {
			t.Fatal(err)
		}
		return tr.ID
	}
	record := func(bot Bot, turnID string, u TurnUsage) {
		t.Helper()
		at = at.Add(time.Second)
		if err := svc.RecordTurnUsage(ctx, bot.ID, turnID, u); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := svc.LastTurnUsage(ctx, ana.ID); ok || err != nil {
		t.Fatalf("usage before any turn: ok %v, err %v; want none", ok, err)
	}
	first, second := turn(ana), turn(ana)
	record(ana, first, TurnUsage{Calls: 1, ContextTokens: 10})
	cold := TurnUsage{InputTokens: 1, CacheReadTokens: 2, CacheWriteTokens: 3, OutputTokens: 4, Calls: 5, ContextTokens: 6, Cold: true}
	record(ana, second, cold)
	record(bo, turn(bo), TurnUsage{Calls: 9})
	if got, ok, err := svc.LastTurnUsage(ctx, ana.ID); !ok || err != nil || got != cold {
		t.Fatalf("Ana's last usage = %+v, %v, %v; want %+v", got, ok, err, cold)
	}
	again := TurnUsage{Calls: 2, ContextTokens: 20}
	record(ana, first, again)
	if got, _, _ := svc.LastTurnUsage(ctx, ana.ID); got != again {
		t.Fatalf("Ana's last usage after the first turn is recorded again = %+v; want %+v", got, again)
	}
}
