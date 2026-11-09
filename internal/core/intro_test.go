package core

import (
	"context"
	"encoding/json"
	"testing"
)

func TestWithoutSelfIntroRemovesOnlyTheSendersOwnPreamble(t *testing.T) {
	cases := []struct {
		name, sender, text, want string
	}{
		{"name here", "Atlas Security", "Atlas Security here. I haven't created the routine yet.", "I haven't created the routine yet."},
		{"name colon", "Atlas Guru", "Atlas Guru: the user confirmed the routine.", "The user confirmed the routine."},
		{"this is name", "Atlas Security", "This is Atlas Security. Done.", "Done."},
		{"bold name colon", "Atlas Security", "**Atlas Security:** Done.", "Done."},
		{"other case and dash", "Atlas Security", "atlas security here — the scan is clean.", "The scan is clean."},
		{"name with symbols", "Deps & CI", "Deps & CI here: two updates are pending.", "Two updates are pending."},
		{"lowercase identifier stays", "Atlas Guru", "Atlas Guru: atlas#656 is still open.", "atlas#656 is still open."},
		{"name as subject stays", "Atlas Security", "Atlas Security reviewed the PR.", "Atlas Security reviewed the PR."},
		{"another bot's name stays", "Atlas Security", "Atlas Guru here. I created it.", "Atlas Guru here. I created it."},
		{"name later in text stays", "Atlas Security", "Ask Atlas Security here: it knows.", "Ask Atlas Security here: it knows."},
		{"nothing left stays", "Atlas Guru", "Atlas Guru here.", "Atlas Guru here."},
		{"greeting stays", "Planner", "Hi Yar, I'm Planner. What should we plan?", "Hi Yar, I'm Planner. What should we plan?"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := withoutSelfIntro(c.text, c.sender); got != c.want {
				t.Fatalf("withoutSelfIntro(%q, %q) = %q; want %q", c.text, c.sender, got, c.want)
			}
		})
	}
}

func TestStoredMessagesDropTheSendersPreamble(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Atlas Security", "Atlas Guru")
	sec, guru := bots["Atlas Security"], bots["Atlas Guru"]
	room, err := svc.CreateRoom(ctx, "Atlas", "", []string{sec.ID, guru.ID})
	if err != nil {
		t.Fatal(err)
	}
	lastText := func(id string) string {
		t.Helper()
		entries, err := svc.Entries(ctx, id, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		var body struct{ Text string }
		_ = json.Unmarshal(entries[len(entries)-1].Body, &body)
		return body.Text
	}

	if _, err := svc.AppendBotMessage(ctx, sec.ID, "t1", "Atlas Security here. The scan is clean.", nil, false, false, PostMeta{}); err != nil {
		t.Fatal(err)
	}
	if got := lastText(sec.ID); got != "The scan is clean." {
		t.Fatalf("message to the user = %q; want the preamble removed", got)
	}
	if _, err := svc.PostToRoom(ctx, room.ID, guru.ID, "t2", "Atlas Guru: the routine is set.", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	if got := lastText(room.ID); got != "The routine is set." {
		t.Fatalf("room post = %q; want the preamble removed", got)
	}
	if _, err := svc.SendAgentMessage(ctx, sec.ID, guru.ID, "Atlas Security here. Please confirm.", nil, false, 0); err != nil {
		t.Fatal(err)
	}
	if got, want := lastText(sec.ID), "Please confirm."; got != want {
		t.Fatalf("sent copy = %q; want %q", got, want)
	}
	if got, want := lastText(guru.ID), "Please confirm."; got != want {
		t.Fatalf("received copy = %q; want %q", got, want)
	}
	inbox, err := svc.Inbox(ctx, guru.ID)
	if err != nil || len(inbox) != 1 || inbox[0].Text != "Please confirm." {
		t.Fatalf("inbox = %+v, %v; want the message without the preamble", inbox, err)
	}
}
