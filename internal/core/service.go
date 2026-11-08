// Package core holds the domain state of YarGPT: bots, transcripts, turns,
// memory, routines, skills and approvals. It owns the database and publishes
// every transcript change to the Hub.
package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid input")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func notFound(what, id string) error {
	return fmt.Errorf("%w: %s %q", ErrNotFound, what, id)
}

const (
	maxNameLen         = 80
	maxInstructionsLen = 8000
	maxMessageLen      = 32000
	maxNonceLen        = 100
)

// Bot is an agent or a room. A room ("group chat") has no Claude session of
// its own; its members take turns in it.
type Bot struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Name            string   `json:"name"`
	Instructions    string   `json:"instructions"`
	Role            string   `json:"role,omitempty"` // RoleBuilder for Builder
	ClaudeSessionID string   `json:"-"`
	Members         []Member `json:"members,omitempty"`
	Team            *Team    `json:"team,omitempty"`      // set for a room that is a team
	Attention       string   `json:"attention,omitempty"` // set by ListBots: AttentionApproval or AttentionQuestion
	CreatedAt       int64    `json:"createdAt"`
	UpdatedAt       int64    `json:"updatedAt"`
}

func (b Bot) IsRoom() bool { return b.Kind == KindRoom }

// What a bot or room needs from the user. An approval outranks a question,
// and a question outranks a paused goal.
const (
	AttentionApproval = "approval" // a tool approval card is pending
	AttentionQuestion = "question" // the latest message asks the user and is unanswered
	AttentionGoal     = "goal"     // the team's goal is paused
)

type Entry struct {
	ID        string          `json:"id"`
	BotID     string          `json:"botId"`
	Seq       int64           `json:"seq"`
	Rev       int64           `json:"rev"`
	Kind      string          `json:"kind"`
	TurnID    string          `json:"turnId"`
	Body      json.RawMessage `json:"body"`
	CreatedAt int64           `json:"createdAt"`
}

type Turn struct {
	ID     string
	BotID  string
	Source string
	Prompt string
	RoomID string // set when the bot takes a turn in a room
	Hops   int64  // agent messages that led to this turn
	// GoalID is the goal that a board turn works for; its budget counts the turn.
	GoalID    string
	ItemID    string // the item of a work or review turn
	BoardRole string // RoleLead, RoleWork, RoleReview or RoleRetro for a board turn
}

// FromUserPost reports whether a post of the user woke the turn in its room:
// only such a turn has no hops and no board role there.
func (t Turn) FromUserPost() bool {
	return t.Source == SourceRoom && t.Hops == 0 && t.BoardRole == ""
}

// Entry kinds and turn sources, as stored in the database.
const (
	KindUser     = "user"
	KindBot      = "bot"
	KindEvent    = "event"
	KindActivity = "activity"
	KindApproval = "approval"
	KindAgent    = "agent" // a direct message between bots

	KindRoom     = "room"
	kindAgentBot = "agent"

	SourceUser    = "user"
	SourceRoutine = "routine"
	SourceSystem  = "system"
	SourceAgent   = "agent"
	SourceRoom    = "room"
	SourceCompact = "compact" // compacts a session; runs through the queue like a turn
	SourceTask    = "task"    // the model's turn after a background task reported; never queued
)

// BackgroundTask is a background shell, monitor or subagent that a bot's
// claude process runs between turns.
type BackgroundTask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Bot         string `json:"bot,omitempty"` // set in a room: the member that runs it
}

type Service struct {
	sqlDB      *sql.DB
	q          *db.Queries
	hub        *Hub
	dataDir    string
	loc        *time.Location
	now        func() time.Time
	turnsReady chan struct{}
	approvals  *approvalWaiters
	// tempDirs are the temp folders a bot may attach files from, besides its
	// workspace and the media folder.
	tempDirs []string
}

// New creates the service. dataDir holds bot workspaces, shared skills and
// the media files of messages.
func New(sqlDB *sql.DB, dataDir string, loc *time.Location) (*Service, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "skills"), 0o755); err != nil {
		return nil, fmt.Errorf("create skills dir: %w", err)
	}
	// Before videos were accepted, the folder was "images".
	if err := os.Rename(filepath.Join(dataDir, "images"), filepath.Join(dataDir, "media")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("move the images dir to media: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "media"), 0o700); err != nil {
		return nil, fmt.Errorf("create media dir: %w", err)
	}
	return &Service{
		sqlDB:      sqlDB,
		q:          db.New(sqlDB),
		hub:        NewHub(),
		dataDir:    dataDir,
		loc:        loc,
		now:        time.Now,
		turnsReady: make(chan struct{}, 1),
		approvals:  newApprovalWaiters(),
		tempDirs:   []string{"/tmp", os.TempDir()},
	}, nil
}

func (s *Service) Hub() *Hub                   { return s.hub }
func (s *Service) Location() *time.Location    { return s.loc }
func (s *Service) TurnsReady() <-chan struct{} { return s.turnsReady }
func (s *Service) DataDir() string             { return s.dataDir }
func (s *Service) SkillsDir() string           { return filepath.Join(s.dataDir, "skills") }
func (s *Service) Workspace(botID string) string {
	return filepath.Join(s.dataDir, "bots", botID, "workspace")
}

func (s *Service) signalTurns() {
	select {
	case s.turnsReady <- struct{}{}:
	default:
	}
}

func (s *Service) nowMs() int64 { return s.now().UnixMilli() }

func toBot(b db.Bot) Bot {
	return Bot{ID: b.ID, Kind: b.Kind, Name: b.Name, Instructions: b.Instructions, Role: b.Role, ClaudeSessionID: b.ClaudeSessionID, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt}
}

func toEntry(e db.Entry) Entry {
	return Entry{ID: e.ID, BotID: e.BotID, Seq: e.Seq, Rev: e.Rev, Kind: e.Kind, TurnID: e.TurnID, Body: json.RawMessage(e.Body), CreatedAt: e.CreatedAt}
}

func validateProfile(name, instructions string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", invalid("name is required")
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return "", "", invalid("name is longer than %d characters", maxNameLen)
	}
	if utf8.RuneCountInString(instructions) > maxInstructionsLen {
		return "", "", invalid("instructions are longer than %d characters", maxInstructionsLen)
	}
	return name, strings.TrimSpace(instructions), nil
}

// CreateBot stores a new bot, prepares its workspace and queues its first-run turn.
func (s *Service) CreateBot(ctx context.Context, name, instructions string) (Bot, error) {
	name, instructions, err := validateProfile(name, instructions)
	if err != nil {
		return Bot{}, err
	}
	id := uuid.NewString()
	if err := s.prepareWorkspace(s.Workspace(id)); err != nil {
		return Bot{}, err
	}
	now := s.nowMs()
	row, err := s.q.CreateBot(ctx, db.CreateBotParams{ID: id, Name: name, Instructions: instructions, Kind: kindAgentBot, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return Bot{}, fmt.Errorf("create bot %q: %w", name, err)
	}
	s.hub.Publish(Event{Type: "bots"})
	if _, err := s.EnqueueTurn(ctx, id, SourceSystem, firstRunPrompt); err != nil {
		return Bot{}, err
	}
	return toBot(row), nil
}

// prepareWorkspace creates a working directory of a bot or a team. Its
// .claude/skills links to the shared skills directory, so every bot sees the
// same skills. The link is relative, so it survives a move of the data
// directory.
func (s *Service) prepareWorkspace(dir string) error {
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create workspace %s: %w", dir, err)
	}
	link := filepath.Join(claudeDir, "skills")
	if _, err := os.Lstat(link); err == nil {
		return nil
	}
	target, err := filepath.Rel(claudeDir, s.SkillsDir())
	if err != nil {
		return fmt.Errorf("locate skills from workspace %s: %w", dir, err)
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("link skills into workspace %s: %w", dir, err)
	}
	return nil
}

func (s *Service) ListBots(ctx context.Context) ([]Bot, error) {
	rows, err := s.q.ListBots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list bots: %w", err)
	}
	members, err := s.q.ListAllRoomMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list room members: %w", err)
	}
	teams, err := s.q.ListTeams(ctx)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	teamRows := map[string]db.Team{}
	for _, t := range teams {
		teamRows[t.TeamID] = t
	}
	byRoom := map[string][]Member{}
	for _, m := range members {
		byRoom[m.RoomID] = append(byRoom[m.RoomID], Member{ID: m.BotID, Name: m.Name})
	}
	attention, err := s.attention(ctx)
	if err != nil {
		return nil, err
	}
	bots := make([]Bot, 0, len(rows))
	for _, r := range rows {
		b := toBot(r)
		b.Members = byRoom[b.ID]
		if row, ok := teamRows[b.ID]; ok {
			b.Team = toTeam(row, b.Members)
		}
		b.Attention = attention[b.ID]
		bots = append(bots, b)
	}
	return bots, nil
}

// attention maps each bot or room that waits on the user to the reason.
func (s *Service) attention(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	paused, err := s.q.ListTeamsWithPausedGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list teams with paused goals: %w", err)
	}
	for _, id := range paused {
		out[id] = AttentionGoal
	}
	asking, err := s.q.ListBotsAwaitingReply(ctx)
	if err != nil {
		return nil, fmt.Errorf("list bots awaiting a reply: %w", err)
	}
	for _, id := range asking {
		out[id] = AttentionQuestion
	}
	pending, err := s.q.ListBotsWithPendingApprovals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending approvals: %w", err)
	}
	for _, id := range pending {
		out[id] = AttentionApproval
	}
	return out, nil
}

func (s *Service) GetBot(ctx context.Context, id string) (Bot, error) {
	row, err := s.q.GetBot(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Bot{}, notFound("bot", id)
	}
	if err != nil {
		return Bot{}, fmt.Errorf("get bot %s: %w", id, err)
	}
	b := toBot(row)
	if b.IsRoom() {
		if b.Members, err = s.memberList(ctx, id); err != nil {
			return Bot{}, err
		}
		if b.Team, err = s.teamOf(ctx, id, b.Members); err != nil {
			return Bot{}, err
		}
	}
	return b, nil
}

// UpdateBot changes the name and/or instructions. A nil field keeps its value.
func (s *Service) UpdateBot(ctx context.Context, id string, name, instructions *string) (Bot, error) {
	bot, err := s.GetBot(ctx, id)
	if err != nil {
		return Bot{}, err
	}
	newName, newInstructions := bot.Name, bot.Instructions
	if name != nil {
		newName = *name
	}
	if instructions != nil {
		newInstructions = *instructions
	}
	newName, newInstructions, err = validateProfile(newName, newInstructions)
	if err != nil {
		return Bot{}, err
	}
	row, err := s.q.UpdateBotProfile(ctx, db.UpdateBotProfileParams{Name: newName, Instructions: newInstructions, UpdatedAt: s.nowMs(), ID: id})
	if err != nil {
		return Bot{}, fmt.Errorf("update bot %s: %w", id, err)
	}
	if newName != bot.Name {
		if _, err := s.AppendEntry(ctx, id, KindEvent, "", map[string]string{"type": "renamed", "from": bot.Name, "to": newName}); err != nil {
			return Bot{}, err
		}
	}
	s.hub.Publish(Event{Type: "bots"})
	return toBot(row), nil
}

// DeleteBot removes a bot, its agent memory and its workspace directory, or
// a team with its team memory and its shared folder. The caller must stop any
// running turn of the bot first.
func (s *Service) DeleteBot(ctx context.Context, id string) error {
	bot, err := s.GetBot(ctx, id)
	if err != nil {
		return err
	}
	teams, err := s.RoomsOf(ctx, id)
	if err != nil {
		return err
	}
	for _, key := range []string{id, teamScopeKey + id} {
		if err := s.q.DeleteMemoriesInScope(ctx, key); err != nil {
			return fmt.Errorf("delete memories of %s: %w", id, err)
		}
	}
	if err := s.q.DeleteBot(ctx, id); err != nil {
		return fmt.Errorf("delete bot %s: %w", id, err)
	}
	for _, dir := range []string{filepath.Join(s.dataDir, "bots", id), filepath.Join(s.dataDir, "teams", id)} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove the folder of %s: %w", id, err)
		}
	}
	// The teams of a deleted bot no longer wait on it.
	for _, team := range slices.DeleteFunc(teams, func(r Bot) bool { return !r.IsTeam() }) {
		if err := s.releaseMember(ctx, team.ID, id, bot.Name); err != nil {
			return err
		}
	}
	s.hub.Publish(Event{Type: "bots"})
	return nil
}

func (s *Service) SetSession(ctx context.Context, botID, sessionID string) error {
	if err := s.q.SetBotSession(ctx, db.SetBotSessionParams{ClaudeSessionID: sessionID, UpdatedAt: s.nowMs(), ID: botID}); err != nil {
		return fmt.Errorf("set session of bot %s: %w", botID, err)
	}
	return nil
}

// AppendEntry adds an entry to the bot's transcript and publishes it.
func (s *Service) AppendEntry(ctx context.Context, botID, kind, turnID string, body any) (Entry, error) {
	return s.appendEntryWithID(ctx, uuid.NewString(), botID, kind, turnID, body)
}

func (s *Service) appendEntryWithID(ctx context.Context, id, botID, kind, turnID string, body any) (Entry, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return Entry{}, fmt.Errorf("encode %s entry body: %w", kind, err)
	}
	row, err := s.q.AppendEntry(ctx, db.AppendEntryParams{BotID: botID, ID: id, Kind: kind, TurnID: turnID, Body: string(raw), CreatedAt: s.nowMs()})
	if err != nil {
		return Entry{}, fmt.Errorf("append %s entry to bot %s: %w", kind, botID, err)
	}
	e := toEntry(row)
	s.hub.Publish(Event{Type: "entry", BotID: botID, Entry: &e})
	return e, nil
}

// UpdateEntry replaces an entry's body. The entry gets a new rev.
func (s *Service) UpdateEntry(ctx context.Context, id string, body any) (Entry, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return Entry{}, fmt.Errorf("encode entry %s body: %w", id, err)
	}
	row, err := s.q.UpdateEntryBody(ctx, db.UpdateEntryBodyParams{Body: string(raw), ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, notFound("entry", id)
	}
	if err != nil {
		return Entry{}, fmt.Errorf("update entry %s: %w", id, err)
	}
	e := toEntry(row)
	s.hub.Publish(Event{Type: "entry", BotID: e.BotID, Entry: &e})
	return e, nil
}

func (s *Service) GetEntry(ctx context.Context, id string) (Entry, error) {
	row, err := s.q.GetEntry(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, notFound("entry", id)
	}
	if err != nil {
		return Entry{}, fmt.Errorf("get entry %s: %w", id, err)
	}
	return toEntry(row), nil
}

// Entries returns entries changed after rev, oldest change first.
func (s *Service) Entries(ctx context.Context, botID string, afterRev int64, limit int) ([]Entry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.q.ListEntriesAfterRev(ctx, db.ListEntriesAfterRevParams{BotID: botID, Rev: afterRev, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("list entries of bot %s: %w", botID, err)
	}
	entries := make([]Entry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, toEntry(r))
	}
	return entries, nil
}

// AppendBotMessage delivers a message from the bot to the user. needsReply
// marks a question the bot cannot continue without; it shows as attention
// until the user writes again.
func (s *Service) AppendBotMessage(ctx context.Context, botID, turnID, text string, files []string, fallback, needsReply bool) (Entry, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(files) == 0 {
		return Entry{}, invalid("message text is empty")
	}
	bot, err := s.GetBot(ctx, botID)
	if err != nil {
		return Entry{}, err
	}
	media, err := s.saveBotFiles(s.Workspace(botID), files)
	if err != nil {
		return Entry{}, err
	}
	body := map[string]any{"text": withoutSelfIntro(text, bot.Name), "fallback": fallback, "needsReply": needsReply}
	if len(media) > 0 {
		body["media"] = media
	}
	e, err := s.AppendEntry(ctx, botID, KindBot, turnID, body)
	if err != nil {
		return Entry{}, errors.Join(err, s.removeMedia(media))
	}
	return e, nil
}

func (s *Service) TurnReplyCount(ctx context.Context, turnID string) (int64, error) {
	n, err := s.q.CountTurnReplies(ctx, turnID)
	if err != nil {
		return 0, fmt.Errorf("count replies of turn %s: %w", turnID, err)
	}
	return n, nil
}

// checkUserMessage checks a message that the user sends, with its number
// of attached files.
func checkUserMessage(text, clientNonce string, files int) error {
	if text == "" && files == 0 {
		return invalid("message text is empty")
	}
	if utf8.RuneCountInString(text) > maxMessageLen {
		return invalid("message is longer than %d characters", maxMessageLen)
	}
	if clientNonce == "" || len(clientNonce) > maxNonceLen {
		return invalid("clientNonce must be 1 to %d characters", maxNonceLen)
	}
	return nil
}

// SubmitUserMessage records a user message and queues a turn for it.
// A repeated clientNonce returns the original turn and duplicate=true.
func (s *Service) SubmitUserMessage(ctx context.Context, botID, text, clientNonce string, media ...[]byte) (turnID string, duplicate bool, err error) {
	text = strings.TrimSpace(text)
	if err := checkUserMessage(text, clientNonce, len(media)); err != nil {
		return "", false, err
	}
	bot, err := s.GetBot(ctx, botID)
	if err != nil {
		return "", false, err
	}
	nonce := sql.NullString{String: clientNonce, Valid: true}
	if existing, err := s.q.GetTurnByNonce(ctx, db.GetTurnByNonceParams{BotID: botID, ClientNonce: nonce}); err == nil {
		return existing.ID, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("look up turn by nonce for bot %s: %w", botID, err)
	}
	names, err := s.saveMedia(uploadedMedia(media))
	if err != nil {
		return "", false, err
	}
	turnID, e, err := s.insertUserMessage(ctx, bot, text, nonce, names)
	if err != nil {
		// Until the entry that refers to the files is committed, a failure must remove them.
		return "", false, errors.Join(err, s.removeMedia(names))
	}
	s.hub.Publish(Event{Type: "entry", BotID: botID, Entry: &e})
	if bot.IsRoom() {
		if err := s.wakeRoomMembers(ctx, bot, text); err != nil {
			return "", false, err
		}
	}
	s.signalTurns()
	return turnID, false, nil
}

// insertUserMessage stores the user's message to bot and its turn in one
// transaction. In a room the turn row only anchors the nonce; members get
// their own turns.
func (s *Service) insertUserMessage(ctx context.Context, bot Bot, text string, nonce sql.NullString, media []string) (string, Entry, error) {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return "", Entry{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	now := s.nowMs()
	source, status, roomID := SourceUser, "queued", ""
	if bot.IsRoom() {
		source, status, roomID = SourceRoom, "done", bot.ID
	}
	turn, err := qtx.InsertTurn(ctx, db.InsertTurnParams{ID: uuid.NewString(), BotID: bot.ID, Source: source, ClientNonce: nonce, Prompt: text, Status: status, RoomID: roomID, CreatedAt: now})
	if err != nil {
		return "", Entry{}, fmt.Errorf("insert turn for bot %s: %w", bot.ID, err)
	}
	body, err := json.Marshal(userBody{Text: text, Media: media})
	if err != nil {
		return "", Entry{}, fmt.Errorf("encode user entry: %w", err)
	}
	row, err := qtx.AppendEntry(ctx, db.AppendEntryParams{BotID: bot.ID, ID: uuid.NewString(), Kind: KindUser, TurnID: turn.ID, Body: string(body), CreatedAt: now})
	if err != nil {
		return "", Entry{}, fmt.Errorf("append user entry to bot %s: %w", bot.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return "", Entry{}, fmt.Errorf("commit user message for bot %s: %w", bot.ID, err)
	}
	return turn.ID, toEntry(row), nil
}

// EnqueueTurn queues a hidden turn (a routine run or a system cue) for the bot.
func (s *Service) EnqueueTurn(ctx context.Context, botID, source, prompt string) (string, error) {
	return s.enqueueTurn(ctx, Turn{BotID: botID, Source: source, Prompt: prompt})
}

func (s *Service) enqueueTurn(ctx context.Context, t Turn) (string, error) {
	id, err := s.queueTurn(ctx, s.q, t)
	if err != nil {
		return "", err
	}
	s.signalTurns()
	return id, nil
}

// queueTurn inserts a queued turn with q, which can be a transaction. The
// caller signals the runner after the turn is committed.
func (s *Service) queueTurn(ctx context.Context, q *db.Queries, t Turn) (string, error) {
	turn, err := q.InsertTurn(ctx, db.InsertTurnParams{
		ID: uuid.NewString(), BotID: t.BotID, Source: t.Source, Prompt: t.Prompt, Status: "queued", RoomID: t.RoomID, Hops: t.Hops,
		GoalID: t.GoalID, ItemID: t.ItemID, BoardRole: t.BoardRole, CreatedAt: s.nowMs(),
	})
	if err != nil {
		return "", fmt.Errorf("queue %s turn for bot %s: %w", t.Source, t.BotID, err)
	}
	return turn.ID, nil
}

// StartTaskTurn records a running task turn: Claude Code woke the bot because
// one of its background tasks reported. hops is the depth of the turn that
// started the task, so a chain of bots cannot restart at zero through a task.
func (s *Service) StartTaskTurn(ctx context.Context, botID, roomID, prompt string, hops int64) (Turn, error) {
	now := s.nowMs()
	row, err := s.q.InsertRunningTurn(ctx, db.InsertRunningTurnParams{ID: uuid.NewString(), BotID: botID, Source: SourceTask, Prompt: prompt, RoomID: roomID, Hops: hops, CreatedAt: now, StartedAt: sql.NullInt64{Int64: now, Valid: true}})
	if err != nil {
		return Turn{}, fmt.Errorf("record task turn for bot %s: %w", botID, err)
	}
	return Turn{ID: row.ID, BotID: row.BotID, Source: row.Source, Prompt: row.Prompt, RoomID: row.RoomID, Hops: row.Hops}, nil
}

// QueuedTurns returns queued turns, oldest first.
func (s *Service) QueuedTurns(ctx context.Context, limit int) ([]Turn, error) {
	rows, err := s.q.ListQueuedTurns(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list queued turns: %w", err)
	}
	turns := make([]Turn, 0, len(rows))
	for _, r := range rows {
		turns = append(turns, Turn{
			ID: r.ID, BotID: r.BotID, Source: r.Source, Prompt: r.Prompt, RoomID: r.RoomID, Hops: r.Hops,
			GoalID: r.GoalID, ItemID: r.ItemID, BoardRole: r.BoardRole,
		})
	}
	return turns, nil
}

// StartTurn marks a queued turn as running. It returns false when the turn
// is no longer queued, for example because a change on a board cancelled it.
func (s *Service) StartTurn(ctx context.Context, id string) (bool, error) {
	n, err := s.q.StartTurn(ctx, db.StartTurnParams{StartedAt: sql.NullInt64{Int64: s.nowMs(), Valid: true}, ID: id})
	if err != nil {
		return false, fmt.Errorf("start turn %s: %w", id, err)
	}
	return n == 1, nil
}

// FinishTurn records the final status ("done", "failed" or "interrupted").
func (s *Service) FinishTurn(ctx context.Context, id, status, errText string) error {
	if err := s.q.FinishTurn(ctx, db.FinishTurnParams{Status: status, Error: errText, FinishedAt: sql.NullInt64{Int64: s.nowMs(), Valid: true}, ID: id}); err != nil {
		return fmt.Errorf("finish turn %s: %w", id, err)
	}
	return nil
}

// RecoverAfterRestart cleans up after a server stop: turns that were running
// become interrupted, pending approval cards expire and tool calls without a
// result are closed. At startup no turn runs, so none of these can still change.
func (s *Service) RecoverAfterRestart(ctx context.Context) error {
	turns, err := s.q.AbandonRunningTurns(ctx, sql.NullInt64{Int64: s.nowMs(), Valid: true})
	if err != nil {
		return fmt.Errorf("abandon running turns: %w", err)
	}
	for _, t := range turns {
		if _, err := s.AppendEntry(ctx, t.BotID, KindEvent, t.ID, map[string]string{"type": "interrupted", "text": "The server restarted while this turn was running."}); err != nil {
			return err
		}
	}
	open, err := s.q.ListOpenEntries(ctx)
	if err != nil {
		return fmt.Errorf("list open entries: %w", err)
	}
	for _, e := range open {
		var body map[string]any
		if err := json.Unmarshal([]byte(e.Body), &body); err != nil {
			return fmt.Errorf("decode entry %s: %w", e.ID, err)
		}
		if e.Kind == KindApproval {
			body["status"] = "expired"
		} else {
			body["status"] = "error"
			body["output"] = "No result: the turn ended first."
		}
		if _, err := s.UpdateEntry(ctx, e.ID, body); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) PublishState(botID string, running bool) {
	s.hub.Publish(Event{Type: "state", BotID: botID, Running: &running})
}

// PublishTasks sends the background tasks shown in a bot's chat or a room.
func (s *Service) PublishTasks(id string, tasks []BackgroundTask) {
	s.hub.Publish(Event{Type: "tasks", BotID: id, Tasks: &tasks})
}

// PublishPause sends when new turns start again after a usage limit; the
// zero time means that the pause ended.
func (s *Service) PublishPause(until time.Time) {
	e := Event{Type: "pause"}
	if !until.IsZero() {
		e.PausedUntil = &until
	}
	s.hub.Publish(e)
}

const firstRunPrompt = `[first run] The user just created you and has not written anything yet. This cue is your signal to open the conversation; do not mention it.
Greet them in one or two short sentences in your own voice, then ask one question that helps you learn what they want help with. If your instructions already give you a concrete assignment, start on it instead of asking.`
