package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

const (
	// maxAgentHops bounds chains of bots waking bots (direct messages and
	// @mentions in rooms). A message past the bound is delivered without a wake.
	maxAgentHops = 6
	// firstRoomContext is how many earlier room messages a member sees on its
	// first turn in a room.
	firstRoomContext = 30
)

// Member is a bot in a room.
type Member struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RoomSeat is a member's state in a room: its Claude session for the room and
// the last room entry it has seen.
type RoomSeat struct {
	Member
	ClaudeSessionID string
	LastSeenSeq     int64
}

// CreateRoom creates a group chat of the user and the given bots.
func (s *Service) CreateRoom(ctx context.Context, name, instructions string, memberIDs []string) (Bot, error) {
	return s.createRoom(ctx, uuid.NewString(), name, instructions, memberIDs, nil)
}

// createRoom stores a room with its members, and makes it a team when set is
// not nil, in one transaction.
func (s *Service) createRoom(ctx context.Context, id, name, instructions string, memberIDs []string, set *TeamSettings) (Bot, error) {
	name, instructions, err := validateProfile(name, instructions)
	if err != nil {
		return Bot{}, err
	}
	members, err := s.validMembers(ctx, memberIDs)
	if err != nil {
		return Bot{}, err
	}
	var team *Team
	if set != nil {
		if team, err = validTeam(*set, members); err != nil {
			return Bot{}, err
		}
	}
	now := s.nowMs()
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return Bot{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	row, err := qtx.CreateBot(ctx, db.CreateBotParams{ID: id, Name: name, Instructions: instructions, Kind: KindRoom, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return Bot{}, fmt.Errorf("create room %q: %w", name, err)
	}
	for _, m := range members {
		if err := qtx.AddRoomMember(ctx, db.AddRoomMemberParams{RoomID: row.ID, BotID: m.ID}); err != nil {
			return Bot{}, fmt.Errorf("add %s to room %q: %w", m.Name, name, err)
		}
	}
	if team != nil {
		if err := qtx.CreateTeam(ctx, db.CreateTeamParams{TeamID: id, LeadBotID: team.LeadID, Agreement: team.Agreement, GoalBudget: team.GoalBudget}); err != nil {
			return Bot{}, fmt.Errorf("create team %q: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Bot{}, fmt.Errorf("commit room %q: %w", name, err)
	}
	room := toBot(row)
	room.Members = members
	room.Team = team
	if _, err := s.AppendEntry(ctx, room.ID, KindEvent, "", map[string]string{"type": "members", "text": membersText(room)}); err != nil {
		return Bot{}, err
	}
	s.hub.Publish(Event{Type: "bots"})
	return room, nil
}

// SetRoomMembers replaces the room's members. Members that stay keep their
// room session.
func (s *Service) SetRoomMembers(ctx context.Context, roomID string, memberIDs []string) (Bot, error) {
	room, err := s.GetBot(ctx, roomID)
	if err != nil {
		return Bot{}, err
	}
	if !room.IsRoom() {
		return Bot{}, invalid("%s is not a room", room.Name)
	}
	members, err := s.validMembers(ctx, memberIDs)
	if err != nil {
		return Bot{}, err
	}
	keep := map[string]bool{}
	for _, m := range members {
		keep[m.ID] = true
		if err := s.q.AddRoomMember(ctx, db.AddRoomMemberParams{RoomID: roomID, BotID: m.ID}); err != nil {
			return Bot{}, fmt.Errorf("add %s to room %s: %w", m.Name, roomID, err)
		}
	}
	for _, m := range room.Members {
		if !keep[m.ID] {
			if err := s.removeMember(ctx, room, m); err != nil {
				return Bot{}, err
			}
		}
	}
	if room, err = s.GetBot(ctx, roomID); err != nil {
		return Bot{}, err
	}
	if _, err := s.AppendEntry(ctx, roomID, KindEvent, "", map[string]string{"type": "members", "text": membersText(room)}); err != nil {
		return Bot{}, err
	}
	s.hub.Publish(Event{Type: "bots"})
	return room, nil
}

// removeMember takes a member out of a room. A team's items then no longer
// wait on it.
func (s *Service) removeMember(ctx context.Context, room Bot, m Member) error {
	if err := s.q.RemoveRoomMember(ctx, db.RemoveRoomMemberParams{RoomID: room.ID, BotID: m.ID}); err != nil {
		return fmt.Errorf("remove %s from room %s: %w", m.Name, room.ID, err)
	}
	if !room.IsTeam() {
		return nil
	}
	return s.releaseMember(ctx, room.ID, m.ID, m.Name)
}

func (s *Service) validMembers(ctx context.Context, ids []string) ([]Member, error) {
	seen := map[string]bool{}
	var members []Member
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		b, err := s.GetBot(ctx, id)
		if err != nil {
			return nil, err
		}
		if b.IsRoom() {
			return nil, invalid("a room cannot be a member of a room")
		}
		members = append(members, Member{ID: b.ID, Name: b.Name})
	}
	if len(members) == 0 {
		return nil, invalid("a room needs at least 1 member bot")
	}
	return members, nil
}

func memberNames(ms []Member) string {
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Name
	}
	return strings.Join(names, ", ")
}

func (s *Service) memberList(ctx context.Context, roomID string) ([]Member, error) {
	seats, err := s.RoomSeats(ctx, roomID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, len(seats))
	for i, seat := range seats {
		out[i] = seat.Member
	}
	return out, nil
}

// RoomSeats returns the members of a room with their room state.
func (s *Service) RoomSeats(ctx context.Context, roomID string) ([]RoomSeat, error) {
	rows, err := s.q.ListRoomMembers(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("list members of room %s: %w", roomID, err)
	}
	out := make([]RoomSeat, len(rows))
	for i, r := range rows {
		out[i] = RoomSeat{ID: r.BotID, Name: r.Name, ClaudeSessionID: r.ClaudeSessionID, LastSeenSeq: r.LastSeenSeq}
	}
	return out, nil
}

// RoomsOf returns the rooms the bot is a member of.
func (s *Service) RoomsOf(ctx context.Context, botID string) ([]Bot, error) {
	rows, err := s.q.ListRoomsOfBot(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("list rooms of bot %s: %w", botID, err)
	}
	out := make([]Bot, 0, len(rows))
	for _, r := range rows {
		b := toBot(r)
		if b.Members, err = s.memberList(ctx, b.ID); err != nil {
			return nil, err
		}
		if b.Team, err = s.teamOf(ctx, b.ID, b.Members); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func (s *Service) SetRoomSession(ctx context.Context, roomID, botID, sessionID string) error {
	if err := s.q.SetRoomMemberSession(ctx, db.SetRoomMemberSessionParams{ClaudeSessionID: sessionID, RoomID: roomID, BotID: botID}); err != nil {
		return fmt.Errorf("set room session of bot %s: %w", botID, err)
	}
	return nil
}

// hereMention matches "@here" as a word of its own, so "me@here.com" and
// "@hereford" do not count.
var hereMention = regexp.MustCompile(`(?i)(^|[^\w@])@here\b`)

// mentioned returns the members whose @name appears in text, or every member
// when text contains @here; never the speaker.
func mentioned(text string, members []Member, speakerID string) []Member {
	everyone := hereMention.MatchString(text)
	lower := strings.ToLower(text)
	var out []Member
	for _, m := range members {
		if m.ID != speakerID && (everyone || strings.Contains(lower, "@"+strings.ToLower(m.Name))) {
			out = append(out, m)
		}
	}
	return out
}

// wakeRoomMembers queues room turns after the user's post in the room. It
// wakes the members the post @mentions; when it mentions none, it wakes the
// lead of a team, or all members of a plain room.
// Members take their turns one at a time, in order (the runner runs one turn
// per room at a time), so each sees the replies before it.
func (s *Service) wakeRoomMembers(ctx context.Context, room Bot, text string) error {
	targets := mentioned(text, room.Members, "")
	if len(targets) == 0 {
		targets = room.Members
		if lead, ok := room.Lead(); ok {
			targets = []Member{lead}
		}
	}
	for _, m := range targets {
		if _, err := s.enqueueTurn(ctx, Turn{BotID: m.ID, Source: SourceRoom, RoomID: room.ID, Prompt: "The user posted a new message."}); err != nil {
			return err
		}
	}
	return nil
}

// PostToRoom adds a bot's message to a room and wakes the members it mentions.
// It returns the names of the members it woke.
func (s *Service) PostToRoom(ctx context.Context, roomID, botID, turnID, text string, files []string, hops int64, needsReply bool) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(files) == 0 {
		return nil, invalid("message text is empty")
	}
	room, err := s.GetBot(ctx, roomID)
	if err != nil {
		return nil, err
	}
	speaker, err := roomMember(room, botID)
	if err != nil {
		return nil, err
	}
	text = withoutSelfIntro(text, speaker.Name)
	workspace := s.Workspace(botID)
	if room.IsTeam() {
		workspace = s.TeamWorkspace(room.ID)
	}
	media, err := s.saveBotFiles(workspace, files)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"text": text, "fallback": false, "needsReply": needsReply, "speakerId": speaker.ID, "speakerName": speaker.Name}
	if len(media) > 0 {
		body["media"] = media
	}
	if _, err := s.AppendEntry(ctx, roomID, KindBot, turnID, body); err != nil {
		return nil, errors.Join(err, s.removeMedia(media))
	}
	if hops+1 > maxAgentHops {
		return nil, nil
	}
	targets := mentioned(text, room.Members, botID)
	var woken []string
	for _, m := range targets {
		if _, err := s.enqueueTurn(ctx, Turn{BotID: m.ID, Source: SourceRoom, RoomID: roomID, Hops: hops + 1, Prompt: speaker.Name + " mentioned you."}); err != nil {
			return woken, err
		}
		woken = append(woken, m.Name)
	}
	return woken, nil
}

// roomMember returns the member botID of room, or ErrInvalid when room is
// not a room or the bot is not in it.
func roomMember(room Bot, botID string) (Member, error) {
	if room.IsRoom() {
		for _, m := range room.Members {
			if m.ID == botID {
				return m, nil
			}
		}
	}
	return Member{}, invalid("the bot is not a member of %s", room.Name)
}

// MaxRoomHistory is the most room messages that RoomHistory returns at once.
const MaxRoomHistory = 50

// RoomHistory returns, oldest first, the latest limit messages of a room
// with a seq below beforeSeq (0 for the newest), to one of its members. It
// does not mark them seen.
func (s *Service) RoomHistory(ctx context.Context, roomID, botID string, limit int, beforeSeq int64) ([]Entry, error) {
	if limit < 1 || limit > MaxRoomHistory {
		return nil, invalid("limit must be 1 to %d", MaxRoomHistory)
	}
	room, err := s.GetBot(ctx, roomID)
	if err != nil {
		return nil, err
	}
	if _, err := roomMember(room, botID); err != nil {
		return nil, err
	}
	if beforeSeq < 0 {
		return nil, invalid("before_seq must not be negative")
	}
	if beforeSeq == 0 {
		beforeSeq = math.MaxInt64
	}
	rows, err := s.q.ListLatestConversation(ctx, db.ListLatestConversationParams{BotID: roomID, Seq: beforeSeq, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("read room %s: %w", roomID, err)
	}
	entries := make([]Entry, len(rows))
	for i, r := range rows {
		entries[len(rows)-1-i] = toEntry(r)
	}
	return entries, nil
}

// RoomUpdate is what a member needs at the start of its turn in a room.
type RoomUpdate struct {
	Room       Bot
	Seat       RoomSeat
	Messages   []Entry // room messages the member has not seen yet
	FirstVisit bool
	// LastSeq is the seq of the last message in Messages, or the member's
	// last seen seq when there are none. MarkRoomSeen records it.
	LastSeq int64
}

// PendingRoomUpdate returns the room messages the member has not seen. On the
// member's first turn it returns the latest messages. It does not mark them
// seen: MarkRoomSeen does that after the turn has handled them.
func (s *Service) PendingRoomUpdate(ctx context.Context, roomID, botID string) (RoomUpdate, error) {
	room, err := s.GetBot(ctx, roomID)
	if err != nil {
		return RoomUpdate{}, err
	}
	seats, err := s.RoomSeats(ctx, roomID)
	if err != nil {
		return RoomUpdate{}, err
	}
	var seat *RoomSeat
	for i := range seats {
		if seats[i].ID == botID {
			seat = &seats[i]
		}
	}
	if seat == nil {
		return RoomUpdate{}, invalid("the bot is no longer a member of %s", room.Name)
	}
	rows, err := s.q.ListConversationAfterSeq(ctx, db.ListConversationAfterSeqParams{BotID: roomID, Seq: seat.LastSeenSeq, Limit: 1000})
	if err != nil {
		return RoomUpdate{}, fmt.Errorf("read room %s: %w", roomID, err)
	}
	first := seat.LastSeenSeq == 0
	if first && len(rows) > firstRoomContext {
		rows = rows[len(rows)-firstRoomContext:]
	}
	u := RoomUpdate{Room: room, Seat: *seat, FirstVisit: first, LastSeq: seat.LastSeenSeq}
	for _, r := range rows {
		u.Messages = append(u.Messages, toEntry(r))
		u.LastSeq = r.Seq
	}
	return u, nil
}

// MarkRoomSeen records that the member has seen the room messages up to seq.
func (s *Service) MarkRoomSeen(ctx context.Context, roomID, botID string, seq int64) error {
	if err := s.q.SetRoomMemberSeen(ctx, db.SetRoomMemberSeenParams{LastSeenSeq: seq, RoomID: roomID, BotID: botID}); err != nil {
		return fmt.Errorf("mark room %s seen by %s: %w", roomID, botID, err)
	}
	return nil
}

// CancelQueuedRoomTurns stops member turns that have not started in the room.
func (s *Service) CancelQueuedRoomTurns(ctx context.Context, roomID string) error {
	if err := s.q.CancelQueuedRoomTurns(ctx, db.CancelQueuedRoomTurnsParams{FinishedAt: sql.NullInt64{Int64: s.nowMs(), Valid: true}, RoomID: roomID}); err != nil {
		return fmt.Errorf("cancel queued turns of room %s: %w", roomID, err)
	}
	return nil
}

// Teammates returns the user's other agent bots.
func (s *Service) Teammates(ctx context.Context, botID string) ([]Bot, error) {
	all, err := s.ListBots(ctx)
	if err != nil {
		return nil, err
	}
	var out []Bot
	for _, b := range all {
		if !b.IsRoom() && b.ID != botID {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}
