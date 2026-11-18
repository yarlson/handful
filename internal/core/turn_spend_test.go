package core

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// The spend of two results together sums each model's tokens and cost and the helpers, and
// leaves both parts as they were.
func TestTheSpendOfTwoResultsIsTheirSum(t *testing.T) {
	t.Parallel()
	first := TurnSpend{Models: map[string]ModelSpend{"opus": {InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4, CostUSD: 0.5}}, Helpers: 1}
	second := TurnSpend{Models: map[string]ModelSpend{
		"opus":  {InputTokens: 10, OutputTokens: 20, CacheReadTokens: 30, CacheWriteTokens: 40, CostUSD: 0.25},
		"haiku": {InputTokens: 7, CostUSD: 0.125},
	}, Helpers: 2}
	got := first.Plus(second)
	want := TurnSpend{Models: map[string]ModelSpend{
		"opus":  {InputTokens: 11, OutputTokens: 22, CacheReadTokens: 33, CacheWriteTokens: 44, CostUSD: 0.75},
		"haiku": {InputTokens: 7, CostUSD: 0.125},
	}, Helpers: 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sum = %+v; want %+v", got, want)
	}
	if got.CostUSD() != 0.875 {
		t.Errorf("cost of the sum = %v; want 0.875", got.CostUSD())
	}
	if first.Models["opus"].CostUSD != 0.5 || first.Helpers != 1 {
		t.Errorf("first part after the sum = %+v; want it unchanged", first)
	}
	if none := (TurnSpend{}).Plus(TurnSpend{}); none.Models == nil || len(none.Models) != 0 || none.CostUSD() != 0 {
		t.Errorf("sum of no spend = %+v; want an empty map and cost 0", none)
	}
}

// A bot's spend in a period sums its turns recorded in it; a turn recorded again replaces its
// first record, its models too; bots come with the largest cost first.
func TestSpendSinceSumsEachBotsTurnsInThePeriod(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	at := time.UnixMilli(10 * time.Hour.Milliseconds())
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
	record := func(bot Bot, turnID string, cost float64, helpers int64, models ...string) {
		t.Helper()
		sp := TurnSpend{Models: map[string]ModelSpend{}, Helpers: helpers}
		for _, m := range models {
			sp.Models[m] = ModelSpend{InputTokens: 1, CostUSD: cost / float64(len(models))}
		}
		if err := svc.RecordTurnSpend(ctx, bot.ID, turnID, sp); err != nil {
			t.Fatal(err)
		}
	}
	old := turn(ana)
	record(ana, old, 8, 4, "opus")
	at = at.Add(2 * time.Hour)
	first := turn(ana)
	record(ana, first, 1, 1, "opus", "haiku")
	var models int
	if err := svc.sqlDB.QueryRowContext(ctx, `SELECT count(*) FROM turn_model_spend WHERE turn_id = ?`, first).Scan(&models); err != nil || models != 2 {
		t.Fatalf("models of a turn with two = %d, %v; want 2", models, err)
	}
	record(ana, turn(ana), 0.5, 0, "opus")
	record(bo, turn(bo), 2, 1, "sonnet")
	record(ana, first, 0.25, 2, "opus")

	got, err := svc.SpendSince(ctx, time.Hour)
	want := []BotSpend{{BotID: bo.ID, CostUSD: 2, Helpers: 1}, {BotID: ana.ID, CostUSD: 0.75, Helpers: 2}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("spend of the last hour = %+v, %v; want %+v", got, err, want)
	}
	got, err = svc.SpendSince(ctx, 3*time.Hour)
	want = []BotSpend{{BotID: ana.ID, CostUSD: 8.75, Helpers: 6}, {BotID: bo.ID, CostUSD: 2, Helpers: 1}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("spend of the last 3 hours = %+v, %v; want %+v", got, err, want)
	}
	if err := svc.sqlDB.QueryRowContext(ctx, `SELECT count(*) FROM turn_model_spend WHERE turn_id = ?`, first).Scan(&models); err != nil || models != 1 {
		t.Fatalf("models of the turn recorded again = %d, %v; want only the second record's 1", models, err)
	}
}
