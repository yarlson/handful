package core

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"yargpt/internal/db"
)

// userEntry sends "hello" to the bot and returns the entry of that message.
func userEntry(t *testing.T, svc *Service, botID string) Entry {
	t.Helper()
	ctx := context.Background()
	if _, _, err := svc.SubmitUserMessage(ctx, botID, "hello", "nonce-hello"); err != nil {
		t.Fatal(err)
	}
	entries, err := svc.Entries(ctx, botID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Kind == KindUser {
			return e
		}
	}
	t.Fatal("no user entry")
	return Entry{}
}

// reactionsOf returns the stored reactions of the entry and whether the body has the key.
func reactionsOf(t *testing.T, svc *Service, id string) ([]string, bool) {
	t.Helper()
	e, err := svc.GetEntry(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(e.Body, &body); err != nil {
		t.Fatal(err)
	}
	raw, ok := body["reactions"]
	if !ok {
		return nil, false
	}
	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got, true
}

func react(t *testing.T, svc *Service, id, emoji string, on bool) Entry {
	t.Helper()
	e, err := svc.SetReaction(context.Background(), id, emoji, on)
	if err != nil {
		t.Fatalf("SetReaction(%s, %v): %v", emoji, on, err)
	}
	return e
}

func TestReactionsAreTheFixedSet(t *testing.T) {
	want := []string{"👍", "👎", "❤️", "😂", "👀", "✅"}
	if !equal(Reactions, want) {
		t.Fatalf("Reactions = %q; want %q (the UI uses the same list)", Reactions, want)
	}
}

func TestSetReactionAddsReactionToMessage(t *testing.T) {
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	e := userEntry(t, svc, bot.ID)

	got := react(t, svc, e.ID, "👍", true)
	if got.ID != e.ID || got.Rev <= e.Rev {
		t.Fatalf("returned entry = %s rev %d; want %s with rev above %d", got.ID, got.Rev, e.ID, e.Rev)
	}
	if r, _ := reactionsOf(t, svc, e.ID); !equal(r, []string{"👍"}) {
		t.Fatalf("reactions = %q; want [👍]", r)
	}
	stored, _ := svc.GetEntry(context.Background(), e.ID)
	if stored.Rev != got.Rev || string(stored.Body) != string(got.Body) {
		t.Fatalf("returned entry %d %s differs from stored %d %s", got.Rev, got.Body, stored.Rev, stored.Body)
	}
}

func TestSetReactionRemovesReaction(t *testing.T) {
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	e := userEntry(t, svc, bot.ID)

	react(t, svc, e.ID, "👍", true)
	react(t, svc, e.ID, "❤️", true)
	react(t, svc, e.ID, "👍", false)
	if r, _ := reactionsOf(t, svc, e.ID); !equal(r, []string{"❤️"}) {
		t.Fatalf("after removing 👍, reactions = %q; want [❤️]", r)
	}
	react(t, svc, e.ID, "❤️", false)
	if r, ok := reactionsOf(t, svc, e.ID); ok {
		t.Fatalf("after removing every reaction, reactions = %q; want no reactions key", r)
	}
}

func TestSetReactionKeepsSetOrder(t *testing.T) {
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	e := userEntry(t, svc, bot.ID)

	react(t, svc, e.ID, "✅", true)
	react(t, svc, e.ID, "👍", true)
	react(t, svc, e.ID, "😂", true)
	if r, _ := reactionsOf(t, svc, e.ID); !equal(r, []string{"👍", "😂", "✅"}) {
		t.Fatalf("reactions = %q; want them in the order of the set", r)
	}
}

func TestSetReactionIsIdempotent(t *testing.T) {
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	e := userEntry(t, svc, bot.ID)
	first := react(t, svc, e.ID, "👍", true)

	events, stop := svc.Hub().Subscribe()
	defer stop()
	second := react(t, svc, e.ID, "👍", true)
	if second.Rev != first.Rev || string(second.Body) != string(first.Body) {
		t.Fatalf("second add = rev %d %s; want unchanged rev %d %s", second.Rev, second.Body, first.Rev, first.Body)
	}
	removedNothing := react(t, svc, e.ID, "👎", false)
	if removedNothing.Rev != first.Rev {
		t.Fatalf("removing an absent reaction changed rev to %d; want %d", removedNothing.Rev, first.Rev)
	}
	if r, _ := reactionsOf(t, svc, e.ID); !equal(r, []string{"👍"}) {
		t.Fatalf("reactions = %q; want one 👍", r)
	}
	select {
	case ev := <-events:
		t.Fatalf("an unchanged reaction published %+v; want no event", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSetReactionPublishesTheUpdatedEntry(t *testing.T) {
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	e := userEntry(t, svc, bot.ID)

	events, stop := svc.Hub().Subscribe()
	defer stop()
	got := react(t, svc, e.ID, "👀", true)
	select {
	case ev := <-events:
		if ev.Type != "entry" || ev.BotID != bot.ID || ev.Entry == nil || ev.Entry.Rev != got.Rev || string(ev.Entry.Body) != string(got.Body) {
			t.Fatalf("event = %+v; want the updated entry of bot %s", ev, bot.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("no event after a reaction")
	}
}

func TestReactionsSurviveReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "yargpt.db")
	conn, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(conn, dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	bot := newTestBot(t, svc)
	e := userEntry(t, svc, bot.ID)
	react(t, svc, e.ID, "✅", true)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err = db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	svc, err = New(conn, dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := reactionsOf(t, svc, e.ID); !equal(r, []string{"✅"}) {
		t.Fatalf("after reopening, reactions = %q; want [✅]", r)
	}
}

func TestSetReactionRejectsUnknownEmoji(t *testing.T) {
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	e := userEntry(t, svc, bot.ID)

	for _, emoji := range []string{"🎉", "", "❤", "👍👍"} {
		if _, err := svc.SetReaction(context.Background(), e.ID, emoji, true); !errors.Is(err, ErrInvalid) {
			t.Fatalf("add %q: err = %v; want ErrInvalid", emoji, err)
		}
	}
	if r, ok := reactionsOf(t, svc, e.ID); ok {
		t.Fatalf("reactions = %q; want none", r)
	}
}

func TestSetReactionAllowsRemovingUnknownStoredEmoji(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	e := userEntry(t, svc, bot.ID)
	if _, err := svc.UpdateEntry(ctx, e.ID, map[string]any{"text": "hello", "reactions": []string{"👍", "🎉"}}); err != nil {
		t.Fatal(err)
	}

	react(t, svc, e.ID, "🎉", false)
	if r, _ := reactionsOf(t, svc, e.ID); !equal(r, []string{"👍"}) {
		t.Fatalf("after removing 🎉, reactions = %q; want [👍]", r)
	}
}

func TestSetReactionKeepsAStoredEmojiOutsideTheSet(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	e := userEntry(t, svc, bot.ID)
	if _, err := svc.UpdateEntry(ctx, e.ID, map[string]any{"text": "hello", "reactions": []string{"🎉"}}); err != nil {
		t.Fatal(err)
	}

	react(t, svc, e.ID, "👍", true)
	if r, _ := reactionsOf(t, svc, e.ID); !equal(r, []string{"👍", "🎉"}) {
		t.Fatalf("reactions = %q; want [👍 🎉]: a stored emoji outside the set stays until removed", r)
	}
}

func TestSetReactionRejectsNonMessageEntry(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	for _, kind := range []string{KindActivity, KindEvent, KindApproval} {
		e, err := svc.AppendEntry(ctx, bot.ID, kind, "", map[string]string{"text": "x"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetReaction(ctx, e.ID, "👍", true); !errors.Is(err, ErrInvalid) {
			t.Fatalf("react to %s entry: err = %v; want ErrInvalid", kind, err)
		}
	}
}

func TestSetReactionAcceptsEveryMessageKind(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	for _, kind := range []string{KindUser, KindBot, KindAgent} {
		e, err := svc.AppendEntry(ctx, bot.ID, kind, "", map[string]string{"text": "x"})
		if err != nil {
			t.Fatal(err)
		}
		react(t, svc, e.ID, "👍", true)
		if r, _ := reactionsOf(t, svc, e.ID); !equal(r, []string{"👍"}) {
			t.Fatalf("%s entry reactions = %q; want [👍]", kind, r)
		}
	}
}

func TestSetReactionUnknownEntry(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.SetReaction(context.Background(), "missing", "👍", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v; want ErrNotFound", err)
	}
}

func TestSetReactionDoesNotWakeBots(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostToRoom(ctx, room.ID, bots["Ana"].ID, "", "Done.", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	drainQueue(t, svc)
	seen := map[string]int64{}
	for n, b := range bots {
		u, err := svc.PendingRoomUpdate(ctx, room.ID, b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.MarkRoomSeen(ctx, room.ID, b.ID, u.LastSeq); err != nil {
			t.Fatal(err)
		}
		seen[n] = u.LastSeq
	}
	entries, err := svc.Entries(ctx, room.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	post := entries[len(entries)-1]

	react(t, svc, post.ID, "👍", true)
	react(t, svc, post.ID, "✅", true)
	if got := queued(t, svc, bots); len(got) != 0 {
		t.Fatalf("a reaction queued %v; want no turn", got)
	}
	for n, b := range bots {
		if inbox, _ := svc.Inbox(ctx, b.ID); len(inbox) != 0 {
			t.Fatalf("%s inbox = %+v; want empty", n, inbox)
		}
		u, err := svc.PendingRoomUpdate(ctx, room.ID, b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(u.Messages) != 0 || u.LastSeq != seen[n] {
			t.Fatalf("%s room update = %d messages, last seq %d; want nothing new after %d", n, len(u.Messages), u.LastSeq, seen[n])
		}
	}
}

func TestSetReactionPreservesOtherBodyFields(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	original := map[string]json.RawMessage{
		"text":       json.RawMessage(`"see <this> & that"`),
		"media":      json.RawMessage(`[{"name":"a.png","type":"image/png"}]`),
		"speakerId":  json.RawMessage(`"bot-1"`),
		"needsReply": json.RawMessage(`true`),
	}
	e, err := svc.AppendEntry(ctx, bot.ID, KindBot, "", original)
	if err != nil {
		t.Fatal(err)
	}
	var before map[string]json.RawMessage
	if err := json.Unmarshal(e.Body, &before); err != nil {
		t.Fatal(err)
	}

	react(t, svc, e.ID, "❤️", true)
	stored, err := svc.GetEntry(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(stored.Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != len(original)+1 || string(body["reactions"]) != `["❤️"]` {
		t.Fatalf("body = %s; want the original fields and the reaction", stored.Body)
	}
	for k := range original {
		if string(body[k]) != string(before[k]) {
			t.Fatalf("field %s = %s; want %s", k, body[k], before[k])
		}
	}
	react(t, svc, e.ID, "❤️", false)
	if stored, _ := svc.GetEntry(ctx, e.ID); string(stored.Body) != string(e.Body) {
		t.Fatalf("after removing the reaction, body = %s; want the original %s", stored.Body, e.Body)
	}
}
