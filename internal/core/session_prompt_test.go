package core

import (
	"context"
	"testing"
)

// The known prompt belongs to one Claude session: a new session of the chat knows nothing yet,
// and each chat of a bot has its own.
func TestKnownPromptBelongsToOneSessionOfOneChat(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	ana, err := svc.CreateBot(ctx, "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	type known struct {
		prompt string
		ok     bool
	}
	get := func(chatID, sessionID string) known {
		t.Helper()
		prompt, ok, err := svc.KnownPrompt(ctx, ana.ID, chatID, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		return known{prompt, ok}
	}
	if got := get(ana.ID, "s1"); got != (known{}) {
		t.Fatalf("known prompt before any = %+v; want none", got)
	}
	if err := svc.SetKnownPrompt(ctx, ana.ID, ana.ID, "s1", "# You are Ana\n"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetKnownPrompt(ctx, ana.ID, room.ID, "r1", ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		chat, session string
		want          known
	}{
		{ana.ID, "s1", known{"# You are Ana\n", true}},
		{ana.ID, "s2", known{}},
		{room.ID, "r1", known{"", true}},
		{room.ID, "s1", known{}},
	} {
		if got := get(c.chat, c.session); got != c.want {
			t.Errorf("KnownPrompt(chat %s, session %s) = %+v; want %+v", c.chat, c.session, got, c.want)
		}
	}
	if err := svc.SetKnownPrompt(ctx, ana.ID, ana.ID, "s2", "# You are Ana, again\n"); err != nil {
		t.Fatal(err)
	}
	if got := get(ana.ID, "s1"); got != (known{}) {
		t.Fatalf("the replaced session's known prompt = %+v; want none", got)
	}
	if got := get(ana.ID, "s2"); got != (known{"# You are Ana, again\n", true}) {
		t.Fatalf("the new session's known prompt = %+v", got)
	}
}
