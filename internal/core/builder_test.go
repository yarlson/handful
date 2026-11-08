package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedBuilderGreetsTheUserOfAnEmptyAppWithoutATurn(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if err := svc.SeedBuilder(ctx); err != nil {
		t.Fatal(err)
	}
	bots, err := svc.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 || bots[0].Name != "Builder" || bots[0].Role != RoleBuilder || bots[0].IsRoom() || bots[0].Attention != AttentionQuestion {
		t.Fatalf("bots = %+v; want only Builder, with role builder, waiting for the user", bots)
	}
	builder := bots[0]
	if _, err := os.Readlink(filepath.Join(svc.Workspace(builder.ID), ".claude", "skills")); err != nil {
		t.Fatalf("Builder's workspace has no skills link: %v", err)
	}
	entries, err := svc.Entries(ctx, builder.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != KindBot {
		t.Fatalf("entries = %+v; want one message from Builder", entries)
	}
	var welcome struct {
		Text       string `json:"text"`
		NeedsReply bool   `json:"needsReply"`
	}
	if err := json.Unmarshal(entries[0].Body, &welcome); err != nil || !welcome.NeedsReply {
		t.Fatalf("welcome = %s, %v; want a message that waits for a reply", entries[0].Body, err)
	}
	for _, start := range []string{"Personal assistant", "Software team", "Research desk", "Writing studio", "I create nothing until you confirm it"} {
		if !strings.Contains(welcome.Text, start) {
			t.Errorf("welcome %q does not offer %q", welcome.Text, start)
		}
	}
	turns, err := svc.QueuedTurns(ctx, 10)
	if err != nil || len(turns) != 0 {
		t.Fatalf("queued turns = %+v, %v; want none", turns, err)
	}

	if err := svc.SeedBuilder(ctx); err != nil {
		t.Fatal(err)
	}
	if bots, err := svc.ListBots(ctx); err != nil || len(bots) != 1 {
		t.Fatalf("bots after a second start = %+v, %v; want only the first Builder", bots, err)
	}
}

func TestSeedBuilderLeavesAnAppThatHasBotsAsItIs(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	scout := newTestBot(t, svc)
	if err := svc.SeedBuilder(ctx); err != nil {
		t.Fatal(err)
	}
	bots, err := svc.ListBots(ctx)
	if err != nil || len(bots) != 1 || bots[0].ID != scout.ID || bots[0].Role != "" {
		t.Fatalf("bots = %+v, %v; want only %s", bots, err, scout.Name)
	}
}

func TestSeedBuilderCreatesNothingWhenItsWelcomeCannotBeSaved(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if _, err := svc.sqlDB.ExecContext(ctx, `CREATE TRIGGER no_entries BEFORE INSERT ON entries BEGIN SELECT RAISE(ABORT, 'entries are read-only'); END`); err != nil {
		t.Fatal(err)
	}
	if err := svc.SeedBuilder(ctx); err == nil || !strings.Contains(err.Error(), "entries are read-only") {
		t.Fatalf("SeedBuilder = %v; want the failed welcome", err)
	}
	if bots, err := svc.ListBots(ctx); err != nil || len(bots) != 0 {
		t.Fatalf("bots = %+v, %v; want none", bots, err)
	}
	if dirs, err := os.ReadDir(filepath.Join(svc.DataDir(), "bots")); err != nil || len(dirs) != 0 {
		t.Fatalf("bot folders = %v, %v; want none", dirs, err)
	}
}
