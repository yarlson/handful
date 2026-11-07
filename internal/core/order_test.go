package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func names(bots []Bot, kind string) string {
	var out []string
	for _, b := range bots {
		if b.Kind == kind {
			out = append(out, b.Name)
		}
	}
	return strings.Join(out, ",")
}

func TestReorderBotsSetsTheSidebarAndRoomTurnOrder(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo", "Cy")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID, bots["Cy"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReorderBots(ctx, []string{bots["Cy"].ID, bots["Ana"].ID, bots["Bo"].ID}); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(listed, kindAgentBot); got != "Cy,Ana,Bo" {
		t.Fatalf("agents = %s; want Cy,Ana,Bo", got)
	}
	got, _ := svc.GetBot(ctx, room.ID)
	if m := got.Members; len(m) != 3 || m[0].Name != "Cy" || m[1].Name != "Ana" || m[2].Name != "Bo" {
		t.Fatalf("room members = %+v; want the new order", m)
	}
	if _, _, err := svc.SubmitUserMessage(ctx, room.ID, "@here status?", "n1"); err != nil {
		t.Fatal(err)
	}
	if q := queued(t, svc, bots); !equal(q, []string{"Cy@room", "Ana@room", "Bo@room"}) {
		t.Fatalf("@here turn order = %v; want Cy, Ana, Bo", q)
	}

	extra, err := svc.CreateBot(ctx, "Dee", "")
	if err != nil {
		t.Fatal(err)
	}
	listed, _ = svc.ListBots(ctx)
	if got := names(listed, kindAgentBot); got != "Cy,Ana,Bo,Dee" {
		t.Fatalf("after adding %s, agents = %s; want the new bot last", extra.Name, got)
	}
}

func TestReorderBotsNeedsTheWholeSectionOnce(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID})
	if err != nil {
		t.Fatal(err)
	}
	for name, ids := range map[string][]string{
		"missing a bot":  {bots["Ana"].ID},
		"duplicate":      {bots["Ana"].ID, bots["Ana"].ID},
		"mixed sections": {bots["Ana"].ID, bots["Bo"].ID, room.ID},
		"unknown id":     {bots["Ana"].ID, "nope"},
		"empty":          {},
	} {
		if err := svc.ReorderBots(ctx, ids); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v; want a validation error", name, err)
		}
	}
	if err := svc.ReorderBots(ctx, []string{room.ID}); err != nil {
		t.Fatalf("reordering the rooms section: %v", err)
	}
}
