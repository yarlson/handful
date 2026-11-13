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
		stored, err := svc.KnownPrompt(ctx, ana.ID, chatID)
		if err != nil {
			t.Fatal(err)
		}
		prompt, ok := stored.For(sessionID)
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
			t.Errorf("known prompt of chat %s, session %s = %+v; want %+v", c.chat, c.session, got, c.want)
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

// ReplaceKnownPrompt stores only over what the caller read: nothing, or the same session and
// prompt. A prompt stored in between, as by a compaction, stays.
func TestReplaceKnownPromptKeepsWhatWasStoredInBetween(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	ana, err := svc.CreateBot(ctx, "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	read := func() KnownPrompt {
		t.Helper()
		k, err := svc.KnownPrompt(ctx, ana.ID, ana.ID)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	replace := func(was KnownPrompt, sessionID, prompt string) bool {
		t.Helper()
		ok, err := svc.ReplaceKnownPrompt(ctx, ana.ID, ana.ID, was, sessionID, prompt)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	empty := read()
	if !replace(empty, "s1", "P0") || read() != (KnownPrompt{"s1", "P0", true}) {
		t.Fatalf("store over nothing: stored %+v; want s1, P0", read())
	}
	if replace(empty, "s1", "other") || read() != (KnownPrompt{"s1", "P0", true}) {
		t.Fatalf("store over nothing when P0 was stored since: stored %+v; want P0 kept", read())
	}
	p0 := read()
	if !replace(p0, "s1", "P1") || read() != (KnownPrompt{"s1", "P1", true}) {
		t.Fatalf("store over P0: stored %+v; want P1", read())
	}
	if err := svc.SetKnownPrompt(ctx, ana.ID, ana.ID, "s1", "P0"); err != nil {
		t.Fatal(err)
	}
	if replace(KnownPrompt{"s1", "P1", true}, "s1", "P1") || read() != (KnownPrompt{"s1", "P0", true}) {
		t.Fatalf("store over P1 after a compaction stored P0: stored %+v; want P0 kept", read())
	}
	if replace(KnownPrompt{"s0", "P0", true}, "s2", "P2") || read() != (KnownPrompt{"s1", "P0", true}) {
		t.Fatalf("store over another session's row: stored %+v; want P0 of s1 kept", read())
	}
	if !replace(read(), "s2", "P2") || read() != (KnownPrompt{"s2", "P2", true}) {
		t.Fatalf("a fresh session over the old session's row: stored %+v; want s2, P2", read())
	}
}
