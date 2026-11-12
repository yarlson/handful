package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// newAgents creates bots by name and finishes their first-run turns, so each
// test sees only the turns it causes.
func newAgents(t *testing.T, svc *Service, names ...string) map[string]Bot {
	t.Helper()
	ctx := context.Background()
	bots := map[string]Bot{}
	for _, n := range names {
		b, err := svc.CreateBot(ctx, n, "")
		if err != nil {
			t.Fatal(err)
		}
		bots[n] = b
	}
	drainQueue(t, svc)
	return bots
}

func drainQueue(t *testing.T, svc *Service) {
	t.Helper()
	turns, err := svc.QueuedTurns(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range turns {
		if err := svc.FinishTurn(context.Background(), tr.ID, "done", ""); err != nil {
			t.Fatal(err)
		}
	}
}

// queued returns "botName@room" (or "botName") for each queued turn, in queue order.
func queued(t *testing.T, svc *Service, bots map[string]Bot) []string {
	t.Helper()
	names := map[string]string{}
	for n, b := range bots {
		names[b.ID] = n
	}
	turns, err := svc.QueuedTurns(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, tr := range turns {
		s := names[tr.BotID]
		if tr.RoomID != "" {
			s += "@room"
		}
		out = append(out, s)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestUserPostInRoomWakesMentionedMembersOrAllMembers(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo", "Cy")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID, bots["Cy"].ID})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := svc.SubmitUserMessage(ctx, room.ID, "Hello team", "n1"); err != nil {
		t.Fatal(err)
	}
	if got := queued(t, svc, bots); !equal(got, []string{"Ana@room", "Bo@room", "Cy@room"}) {
		t.Fatalf("after a post without mentions, queued = %v; want every member, in member order", got)
	}
	drainQueue(t, svc)

	if _, _, err := svc.SubmitUserMessage(ctx, room.ID, "@bo can you check this?", "n2"); err != nil {
		t.Fatal(err)
	}
	if got := queued(t, svc, bots); !equal(got, []string{"Bo@room"}) {
		t.Fatalf("after @bo, queued = %v; want only Bo", got)
	}
	drainQueue(t, svc)

	if _, dup, err := svc.SubmitUserMessage(ctx, room.ID, "@bo can you check this?", "n2"); err != nil || !dup {
		t.Fatalf("repeated nonce: dup=%v err=%v; want duplicate", dup, err)
	}
	if got := queued(t, svc, bots); len(got) != 0 {
		t.Fatalf("a repeated send queued %v; want nothing", got)
	}
}

func TestBotPostInRoomWakesOnlyMentionedMembersWithinTheHopLimit(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo", "Cy")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID, bots["Cy"].ID})
	if err != nil {
		t.Fatal(err)
	}

	woken, err := svc.PostToRoom(ctx, room.ID, bots["Ana"].ID, "", "Done. @Bo please review, and thanks @ana", nil, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(woken) != 1 || woken[0] != "Bo" {
		t.Fatalf("woken = %v; want only Bo (a bot does not wake itself; Cy was not mentioned)", woken)
	}
	turns, _ := svc.QueuedTurns(ctx, 10)
	if len(turns) != 1 || turns[0].Hops != 1 || turns[0].RoomID != room.ID {
		t.Fatalf("queued = %+v; want one room turn for Bo with hops 1", turns)
	}
	drainQueue(t, svc)

	if woken, err := svc.PostToRoom(ctx, room.ID, bots["Bo"].ID, "", "@Ana looks good", nil, maxAgentHops, false); err != nil || len(woken) != 0 {
		t.Fatalf("post at the hop limit: woken=%v err=%v; want no wake", woken, err)
	}
	entries, _ := svc.Entries(ctx, room.ID, 0, 0)
	last := entries[len(entries)-1]
	var body map[string]any
	_ = json.Unmarshal(last.Body, &body)
	if last.Kind != KindBot || body["speakerName"] != "Bo" || body["text"] != "@Ana looks good" {
		t.Fatalf("last room entry = %s %s; want Bo's post, even past the hop limit", last.Kind, last.Body)
	}

	if _, err := svc.PostToRoom(ctx, room.ID, newAgents(t, svc, "Outsider")["Outsider"].ID, "", "hi", nil, 0, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("post by a non-member: err = %v; want ErrInvalid", err)
	}
}

func TestPendingRoomUpdateReturnsOnlyUnseenMessages(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SubmitUserMessage(ctx, room.ID, "first", "n1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostToRoom(ctx, room.ID, bots["Ana"].ID, "", "reply from Ana", nil, 0, false); err != nil {
		t.Fatal(err)
	}

	u, err := svc.PendingRoomUpdate(ctx, room.ID, bots["Bo"].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !u.FirstVisit || len(u.Messages) != 2 {
		t.Fatalf("first update: first=%v messages=%d; want a first visit with 2 messages (events left out)", u.FirstVisit, len(u.Messages))
	}
	if u, _ := svc.PendingRoomUpdate(ctx, room.ID, bots["Bo"].ID); !u.FirstVisit || len(u.Messages) != 2 {
		t.Fatalf("update before marking seen: first=%v messages=%d; want the same 2 messages again", u.FirstVisit, len(u.Messages))
	}
	if err := svc.MarkRoomSeen(ctx, room.ID, bots["Bo"].ID, u.LastSeq); err != nil {
		t.Fatal(err)
	}
	u, _ = svc.PendingRoomUpdate(ctx, room.ID, bots["Bo"].ID)
	if u.FirstVisit || len(u.Messages) != 0 {
		t.Fatalf("second update: first=%v messages=%d; want nothing new", u.FirstVisit, len(u.Messages))
	}
	if _, _, err := svc.SubmitUserMessage(ctx, room.ID, "second", "n2"); err != nil {
		t.Fatal(err)
	}
	u, _ = svc.PendingRoomUpdate(ctx, room.ID, bots["Bo"].ID)
	if len(u.Messages) != 1 || u.Messages[0].Kind != KindUser {
		t.Fatalf("third update = %+v; want the one new user message", u.Messages)
	}
}

func TestDirectMessagePriorityWakesAndOtherwiseWaitsInTheInbox(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	ana, bo := bots["Ana"], bots["Bo"]

	res, err := svc.SendAgentMessage(ctx, ana.ID, "", bo.ID, "Please summarize the doc.", nil, true, 0)
	if err != nil || res.Delivered != "woke" {
		t.Fatalf("priority message: %+v, %v; want woke", res, err)
	}
	turns, _ := svc.QueuedTurns(ctx, 10)
	if len(turns) != 1 || turns[0].BotID != bo.ID || turns[0].Source != SourceAgent || turns[0].Hops != 1 {
		t.Fatalf("queued = %+v; want one agent turn for Bo with hops 1", turns)
	}
	for _, id := range []string{ana.ID, bo.ID} {
		entries, _ := svc.Entries(ctx, id, 0, 0)
		if last := entries[len(entries)-1]; last.Kind != KindAgent {
			t.Fatalf("last entry of %s = %s; want the agent message in both chats", id, last.Kind)
		}
	}
	drainQueue(t, svc)

	res, err = svc.SendAgentMessage(ctx, ana.ID, "", bo.ID, "FYI: done.", nil, false, 0)
	if err != nil || res.Delivered != "held" {
		t.Fatalf("message without priority: %+v, %v; want held", res, err)
	}
	if got := queued(t, svc, bots); len(got) != 0 {
		t.Fatalf("a held message queued %v; want no turn", got)
	}
	inbox, err := svc.Inbox(ctx, bo.ID)
	if err != nil || len(inbox) != 1 || inbox[0].Text != "FYI: done." || inbox[0].FromName != "Ana" {
		t.Fatalf("inbox = %+v, %v; want Ana's FYI", inbox, err)
	}
	if err := svc.AckInbox(ctx, inbox); err != nil {
		t.Fatal(err)
	}
	if inbox, _ := svc.Inbox(ctx, bo.ID); len(inbox) != 0 {
		t.Fatalf("inbox after ack = %+v; want empty", inbox)
	}

	if res, err := svc.SendAgentMessage(ctx, ana.ID, "", bo.ID, "loop?", nil, true, maxAgentHops); err != nil || res.Delivered != "held" {
		t.Fatalf("priority message at the hop limit: %+v, %v; want held without a wake", res, err)
	}
	if _, err := svc.SendAgentMessage(ctx, ana.ID, "", ana.ID, "me", nil, true, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("message to self: err = %v; want ErrInvalid", err)
	}
}

func TestRoomMembershipRules(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	if _, err := svc.CreateRoom(ctx, "Empty", "", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("room without members: err = %v; want ErrInvalid", err)
	}
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRoom(ctx, "Nested", "", []string{room.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("room as a member: err = %v; want ErrInvalid", err)
	}
	if err := svc.SetRoomSession(ctx, room.ID, bots["Ana"].ID, "ana-room-session"); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.SetRoomMembers(ctx, room.ID, []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Members) != 2 {
		t.Fatalf("members = %+v; want Ana and Bo", updated.Members)
	}
	seats, _ := svc.RoomSeats(ctx, room.ID)
	sort.Slice(seats, func(i, j int) bool { return seats[i].Name < seats[j].Name })
	if seats[0].ClaudeSessionID != "ana-room-session" {
		t.Fatalf("Ana's room session = %q; want it kept across a membership change", seats[0].ClaudeSessionID)
	}
	rooms, err := svc.RoomsOf(ctx, bots["Bo"].ID)
	if err != nil || len(rooms) != 1 || rooms[0].ID != room.ID {
		t.Fatalf("rooms of Bo = %+v, %v; want the room", rooms, err)
	}
}

func TestRoomTakesAnyNumberOfMembers(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	names := []string{"B1", "B2", "B3", "B4", "B5", "B6", "B7", "B8", "B9", "B10"}
	bots := newAgents(t, svc, names...)
	ids := make([]string, len(names))
	for i, n := range names {
		ids[i] = bots[n].ID
	}
	room, err := svc.CreateRoom(ctx, "Crowd", "", ids[:9])
	if err != nil || len(room.Members) != 9 {
		t.Fatalf("create a room with 9 members: members = %d, err = %v; want 9 members", len(room.Members), err)
	}
	room, err = svc.SetRoomMembers(ctx, room.ID, ids)
	if err != nil || len(room.Members) != 10 {
		t.Fatalf("set 10 members: members = %d, err = %v; want 10 members", len(room.Members), err)
	}
}

func TestHereMentionWakesEveryOtherMember(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo", "Cy")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID, bots["Cy"].ID})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := svc.SubmitUserMessage(ctx, room.ID, "@Bo and @here: status?", "n1"); err != nil {
		t.Fatal(err)
	}
	if got := queued(t, svc, bots); !equal(got, []string{"Ana@room", "Bo@room", "Cy@room"}) {
		t.Fatalf("user @here next to @Bo: queued = %v; want every member", got)
	}
	drainQueue(t, svc)

	woken, err := svc.PostToRoom(ctx, room.ID, bots["Ana"].ID, "", "Draft is ready. @HERE please review.", nil, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(woken, ",") != "Bo,Cy" {
		t.Fatalf("bot @here woke %v; want Bo and Cy, not the speaker", woken)
	}
	drainQueue(t, svc)

	for _, text := range []string{"Mail me@here.com", "We met in @hereford"} {
		if woken, err := svc.PostToRoom(ctx, room.ID, bots["Ana"].ID, "", text, nil, 0, false); err != nil || len(woken) != 0 {
			t.Fatalf("post %q woke %v (err %v); want nobody", text, woken, err)
		}
	}
}

func TestRoomHistoryReturnsTheLatestMessagesToMembersOnly(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo", "Outsider")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"one", "two", "three"} {
		if _, err := svc.PostToRoom(ctx, room.ID, bots["Ana"].ID, "", text, nil, 0, false); err != nil {
			t.Fatal(err)
		}
	}

	got, err := svc.RoomHistory(ctx, room.ID, bots["Bo"].ID, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, e := range got {
		var body struct{ Text string }
		_ = json.Unmarshal(e.Body, &body)
		texts = append(texts, body.Text)
	}
	if strings.Join(texts, ",") != "two,three" {
		t.Fatalf("history with limit 2 = %v; want the latest two, oldest first: [two three]", texts)
	}
	if u, _ := svc.PendingRoomUpdate(ctx, room.ID, bots["Bo"].ID); len(u.Messages) != 3 {
		t.Fatalf("pending messages after reading history = %d; want 3 (reading does not mark them seen)", len(u.Messages))
	}
	if _, err := svc.RoomHistory(ctx, room.ID, bots["Outsider"].ID, 2, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("history for a non-member: err = %v; want ErrInvalid", err)
	}
	if _, err := svc.RoomHistory(ctx, room.ID, bots["Bo"].ID, 51, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("history with limit 51: err = %v; want ErrInvalid", err)
	}
}

func TestMentionMatchesWholeNamesOnly(t *testing.T) {
	members := []Member{{ID: "bo", Name: "Bo"}, {ID: "bob", Name: "Bob"}, {ID: "forge", Name: "Forge"}, {ID: "fr", Name: "Forge Reviewer"}}
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"@Bob, look", []string{"bob"}},
		{"@bo: look", []string{"bo"}},
		{"(@Bo)", []string{"bo"}},
		{"write to ops@bo.dev", nil},
		{"@Bob_2 and @Bobby", nil},
		{"@Forge Reviewer, please check", []string{"fr"}},
		{"@Forge reviews it", []string{"forge"}},
		{"@Forge and @Forge Reviewer", []string{"forge", "fr"}},
		{"@Forge Reviewers", []string{"forge"}},
	} {
		var got []string
		for _, m := range mentioned(tc.text, members, "") {
			got = append(got, m.ID)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("mentioned(%q) = %v; want %v", tc.text, got, tc.want)
		}
	}
}

func TestABurstOfUserPostsQueuesOneTurnPerMember(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"First", "Second", "@bo third"} {
		if _, _, err := svc.SubmitUserMessage(ctx, room.ID, text, fmt.Sprint("n", i)); err != nil {
			t.Fatal(err)
		}
	}
	if got := queued(t, svc, bots); !equal(got, []string{"Ana@room", "Bo@room"}) {
		t.Fatalf("after three quick posts, queued = %v; want one turn per member", got)
	}
	// A turn already queued for a bot's mention is not a user post's turn,
	// and another room's turn is not this room's.
	other, err := svc.CreateRoom(ctx, "Other", "", []string{bots["Ana"].ID})
	if err != nil {
		t.Fatal(err)
	}
	drainQueue(t, svc)
	if _, err := svc.PostToRoom(ctx, room.ID, bots["Ana"].ID, "", "@Bo look", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SubmitUserMessage(ctx, other.ID, "Elsewhere", "o1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SubmitUserMessage(ctx, room.ID, "@bo and now?", "n9"); err != nil {
		t.Fatal(err)
	}
	if got := queued(t, svc, bots); !equal(got, []string{"Bo@room", "Ana@room", "Bo@room"}) {
		t.Fatalf("queued = %v; want Bo's mention turn, Ana's turn in the other room and Bo's turn for the user's post", got)
	}
}

func TestCancelQueuedTurnsOfStopsABotsOrARoomsQueuedTurnsOnly(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo", "Cy")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateRoom(ctx, "Other", "", []string{bots["Cy"].ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{bots["Ana"].ID, bots["Cy"].ID, room.ID, other.ID} {
		if _, _, err := svc.SubmitUserMessage(ctx, id, "hi", "n-"+id); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.CancelQueuedTurnsOf(ctx, bots["Ana"].ID); err != nil {
		t.Fatal(err)
	}
	if got := queued(t, svc, bots); !equal(got, []string{"Cy", "Bo@room", "Cy@room"}) {
		t.Fatalf("queued after cancelling Ana's turns = %v; want Cy's, and Bo's and Cy's in the rooms", got)
	}
	if err := svc.CancelQueuedTurnsOf(ctx, room.ID); err != nil {
		t.Fatal(err)
	}
	if got := queued(t, svc, bots); !equal(got, []string{"Cy", "Cy@room"}) {
		t.Fatalf("queued after cancelling the room's turns = %v; want Cy's two", got)
	}
}

// #21: a post whose room wake fails stores nothing and fails, so the same send again stores
// it once and wakes the members; a post is never saved without its wake.
func TestARoomPostAndItsWakeAreOneChange(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	drainQueue(t, svc)
	if _, err := svc.sqlDB.ExecContext(ctx, `CREATE TRIGGER wake_fails BEFORE INSERT ON turns
		WHEN NEW.source = 'room' AND NEW.status = 'queued'
		BEGIN SELECT RAISE(ABORT, 'wake failed'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SubmitUserMessage(ctx, room.ID, "Hello team", "n1"); err == nil || !strings.Contains(err.Error(), "wake failed") {
		t.Fatalf("post while the wake fails: err = %v; want the wake's error", err)
	}
	if n := userEntries(t, svc, room.ID); n != 0 {
		t.Fatalf("user entries after the failed post = %d; want 0", n)
	}
	if _, err := svc.sqlDB.ExecContext(ctx, `DROP TRIGGER wake_fails`); err != nil {
		t.Fatal(err)
	}
	if _, dup, err := svc.SubmitUserMessage(ctx, room.ID, "Hello team", "n1"); err != nil || dup {
		t.Fatalf("the same send again = %v, %v; want a new post", dup, err)
	}
	if n := userEntries(t, svc, room.ID); n != 1 {
		t.Fatalf("user entries = %d; want 1", n)
	}
	if got := queued(t, svc, bots); !equal(got, []string{"Ana@room", "Bo@room"}) {
		t.Fatalf("queued = %v; want both members woken", got)
	}
}
