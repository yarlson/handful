// Package agent runs bot turns as local Claude Code CLI processes and serves
// the MCP tools those processes call back into.
package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"yargpt/internal/core"
)

type Config struct {
	ClaudePath  string // claude executable
	MCPURL      string // URL of this server's MCP endpoint
	Model       string // optional model alias for every bot
	MaxParallel int    // turns that may run at once across all bots
	// OAuthCallbackURL is the redirect URI of connector sign-ins. It must
	// match the address the server listens on.
	OAuthCallbackURL string
}

const (
	routineTick      = 20 * time.Second
	maxStreamLine    = 16 << 20
	maxStderr        = 64 << 10
	maxActivityText  = 2000
	processWaitDelay = 5 * time.Second
	// MCP calls may wait for a user's approval; give them longer than the approval timeout.
	mcpToolTimeoutMs = "900000"
	// Longer than any silence of a working turn: an approval wait, an MCP call, a Bash command.
	turnOutputTimeout = 30 * time.Minute
)

// Runner takes queued turns from the service and runs each one in the claude
// process of its Claude session. Turns of one bot run one at a time, in order.
// Turns in one room also run one at a time, so each member sees the replies
// before its own; idle members go before members that are busy elsewhere.
type Runner struct {
	svc    *core.Service
	cfg    Config
	tokens *tokenRegistry
	oauth  *signIns
	log    *slog.Logger

	outputTimeout time.Duration // turnOutputTimeout; tests shorten it

	mu       sync.Mutex
	running  map[string]runningTurn // bot id -> its running turn
	sessions map[sessionKey]*session
	wg       sync.WaitGroup
	done     chan struct{}

	// After a usage limit, dispatch starts no turn until resume fires.
	pausedUntil   time.Time   // guarded by mu; zero when not paused
	resume        *time.Timer // Run receives from it and ends the pause
	maxLimitPause time.Duration
}

type runningTurn struct {
	cancel context.CancelFunc
	roomID string
}

func NewRunner(svc *core.Service, cfg Config, log *slog.Logger) *Runner {
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 3
	}
	resume := time.NewTimer(0)
	resume.Stop()
	return &Runner{
		svc: svc, cfg: cfg, tokens: newTokenRegistry(), oauth: newSignIns(), log: log, outputTimeout: turnOutputTimeout, running: make(map[string]runningTurn), sessions: make(map[sessionKey]*session), done: make(chan struct{}, 1),
		resume: resume, maxLimitPause: limitPauseMax,
	}
}

// Run dispatches turns and fires due routines until ctx ends, then stops all
// running turns and claude processes and waits for them.
func (r *Runner) Run(ctx context.Context) {
	r.removeLeftoverMCPConfigs()
	ticker := time.NewTicker(routineTick)
	defer ticker.Stop()
	defer r.resume.Stop()
	r.fireRoutines(ctx)
	r.dispatch(ctx)
	for {
		select {
		case <-ctx.Done():
			r.stopSessions()
			r.wg.Wait()
			return
		case <-r.svc.TurnsReady():
		case <-r.done:
		case <-r.resume.C:
			r.endPause()
		case <-ticker.C:
			r.fireRoutines(ctx)
		}
		r.dispatch(ctx)
	}
}

func (r *Runner) fireRoutines(ctx context.Context) {
	if _, err := r.svc.FireDueRoutines(ctx); err != nil && ctx.Err() == nil {
		r.log.Error("fire due routines", "err", err)
	}
	if err := r.svc.PruneNotes(ctx); err != nil && ctx.Err() == nil {
		r.log.Error("prune notes", "err", err)
	}
}

// Running reports whether the bot, or a member in the room, has a turn in
// progress, including a task turn.
func (r *Runner) Running(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.botBusyLocked(id) || r.roomBusyLocked(id)
}

func (r *Runner) botBusyLocked(botID string) bool {
	if _, ok := r.running[botID]; ok {
		return true
	}
	for _, s := range r.sessions {
		if s.waking && s.key.botID == botID {
			return true
		}
	}
	return false
}

func (r *Runner) roomBusyLocked(roomID string) bool {
	for _, rt := range r.running {
		if rt.roomID == roomID {
			return true
		}
	}
	for _, s := range r.sessions {
		if s.waking && s.key.roomID == roomID {
			return true
		}
	}
	return false
}

// Active reports whether a turn or a claude process of the bot or room still runs.
func (r *Runner) Active(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if s.key.botID == id || s.key.roomID == id {
			return true
		}
	}
	return r.botBusyLocked(id) || r.roomBusyLocked(id)
}

// Interrupt stops the bot's running turn and ends its claude processes, with
// their background tasks. For a room it does the same for the members' turns
// and processes in the room, and drops the member turns still queued there.
// It returns false when nothing was running.
func (r *Runner) Interrupt(ctx context.Context, id string) (bool, error) {
	r.mu.Lock()
	var cancels []context.CancelFunc
	for botID, rt := range r.running {
		if botID == id || rt.roomID == id {
			cancels = append(cancels, rt.cancel)
		}
	}
	var sessions []*session
	for _, s := range r.sessions {
		if s.key.botID == id || s.key.roomID == id {
			sessions = append(sessions, s)
		}
	}
	r.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	for _, s := range sessions {
		r.stopSession(s)
	}
	stopped := len(cancels) > 0 || len(sessions) > 0
	if err := r.svc.CancelQueuedRoomTurns(ctx, id); err != nil {
		return stopped, err
	}
	return stopped, nil
}

// stopSessions ends every claude process. Run calls it at shutdown; the
// processes of running turns also end when their turns are cancelled.
func (r *Runner) stopSessions() {
	r.mu.Lock()
	var sessions []*session
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()
	for _, s := range sessions {
		r.stopSession(s)
	}
}

// publishState sends the working state of the bot and of the room it works in.
func (r *Runner) publishState(botID, roomID string) {
	r.svc.PublishState(botID, r.Running(botID))
	if roomID != "" {
		r.svc.PublishState(roomID, r.Running(roomID))
	}
}

// signalDone makes Run dispatch again: a bot or a room may be free.
func (r *Runner) signalDone() {
	select {
	case r.done <- struct{}{}:
	default:
	}
}

func (r *Runner) dispatch(ctx context.Context) {
	if ctx.Err() != nil || !r.PausedUntil().IsZero() {
		return
	}
	turns, err := r.svc.QueuedTurns(ctx, 100)
	if err != nil {
		r.log.Error("list queued turns", "err", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range turns {
		if len(r.running) >= r.cfg.MaxParallel {
			return
		}
		// A bot runs one turn at a time. A member busy elsewhere does not hold
		// up the room: the next idle member in queue order goes first, and the
		// busy member answers when it is free.
		if r.botBusyLocked(t.BotID) {
			continue
		}
		// A room runs one member turn at a time, so each member sees the
		// replies before its own.
		if t.RoomID != "" && r.roomBusyLocked(t.RoomID) {
			continue
		}
		if err := r.svc.StartTurn(ctx, t.ID); err != nil {
			r.log.Error("start turn", "turn", t.ID, "err", err)
			continue
		}
		turnCtx, cancel := context.WithCancel(ctx)
		r.running[t.BotID] = runningTurn{cancel: cancel, roomID: t.RoomID}
		r.svc.PublishState(t.BotID, true)
		if t.RoomID != "" {
			r.svc.PublishState(t.RoomID, true)
		}
		r.wg.Add(1)
		go func(t core.Turn) {
			defer r.wg.Done()
			defer cancel()
			r.execute(turnCtx, t)
			r.mu.Lock()
			delete(r.running, t.BotID)
			r.mu.Unlock()
			r.publishState(t.BotID, t.RoomID)
			r.signalDone()
		}(t)
	}
}

// outcome is what Claude Code reported for one message.
type outcome struct {
	compacted   *compactMetadata // set when the session was compacted
	resultText  string
	resultError bool
	exitErr     error
	stderr      string

	usageLimit    bool  // Claude Code reported the usage_limit_reached API error
	limitResetsAt int64 // Unix seconds when the usage limit resets; 0 when not reported
}

func (o outcome) failed() bool { return o.exitErr != nil || o.resultError }

// noteAPIError records a usage limit error of an assistant or result event.
func (o *outcome) noteAPIError(ev streamEvent) {
	if ev.APIError != usageLimitError {
		return
	}
	o.usageLimit = true
	if p := ev.APIErrorParams; p != nil && p.RateLimitInfo != nil {
		o.limitResetsAt = p.RateLimitInfo.ResetsAt
	}
}

// hitUsageLimit reports whether the result failed on a usage limit. The
// message text is the check when Claude Code sends no api_error, which it
// marks as internal.
func (o outcome) hitUsageLimit() bool {
	return o.resultError && (o.usageLimit || strings.HasPrefix(o.resultText, usageLimitPrefix))
}

func (o outcome) errorText() string {
	for _, s := range []string{o.resultText, o.stderr} {
		if s = strings.TrimSpace(s); s != "" {
			return truncate(s, 1000)
		}
	}
	if o.exitErr != nil {
		return o.exitErr.Error()
	}
	return "unknown error"
}

func (o outcome) sessionMissing() bool {
	return strings.Contains(o.resultText+o.stderr, "No conversation found")
}

func (r *Runner) execute(ctx context.Context, t core.Turn) {
	// Bookkeeping after the turn uses its own context: an interrupt cancels ctx.
	bg := context.WithoutCancel(ctx)
	status, errText := "done", ""
	defer func() {
		if err := r.svc.FinishTurn(bg, t.ID, status, errText); err != nil {
			r.log.Error("finish turn", "turn", t.ID, "err", err)
		}
	}()
	// A turn in a room shows its activity, approvals and failures in the room.
	transcript := t.BotID
	if t.RoomID != "" {
		transcript = t.RoomID
	}
	fail := func(err error) {
		status, errText = "failed", err.Error()
		r.log.Error("turn failed", "bot", t.BotID, "turn", t.ID, "err", err)
		if _, aerr := r.svc.AppendEntry(bg, transcript, core.KindEvent, t.ID, map[string]string{"type": "turn-failed", "text": errText}); aerr != nil {
			r.log.Error("record turn failure", "turn", t.ID, "err", aerr)
		}
	}

	bot, err := r.svc.GetBot(ctx, t.BotID)
	if err != nil {
		fail(err)
		return
	}
	if t.Source == core.SourceCompact {
		status, errText = r.compact(ctx, bot, t, transcript, fail)
		return
	}
	run, delivered, err := r.prepare(ctx, bot, t)
	if err != nil {
		fail(err)
		return
	}
	// Approval waits run in the MCP session, which outlives the turn; end them with it.
	defer r.svc.ExpireTurnApprovals(t.ID)

	out, err := r.runTurn(ctx, bot, t, run)
	if err == nil && out.failed() && run.Resume && out.sessionMissing() && ctx.Err() == nil {
		// The stored Claude session is gone (for example, deleted on disk): start a new one.
		run.Resume, run.SessionID = false, uuid.NewString()
		if err = r.saveSession(ctx, bot.ID, t.RoomID, run.SessionID); err == nil {
			out, err = r.runTurn(ctx, bot, t, run)
		}
	}
	switch {
	case ctx.Err() != nil:
		status, errText = "interrupted", "stopped by the user"
		if _, aerr := r.svc.AppendEntry(bg, transcript, core.KindEvent, t.ID, map[string]string{"type": "interrupted", "text": "Stopped."}); aerr != nil {
			r.log.Error("record interrupt", "turn", t.ID, "err", aerr)
		}
		return
	case err != nil:
		fail(err)
		return
	case out.failed():
		fail(errors.New(out.errorText()))
		return
	}
	if err := r.svc.AckInbox(bg, delivered.inbox); err != nil {
		r.log.Error("acknowledge inbox", "turn", t.ID, "err", err)
	}
	if t.RoomID != "" {
		if err := r.svc.MarkRoomSeen(bg, t.RoomID, bot.ID, delivered.roomSeq); err != nil {
			r.log.Error("mark room seen", "turn", t.ID, "err", err)
		}
	}

	// A user who wrote something must get an answer even if the model forgot send_to_user.
	if t.Source == core.SourceUser && strings.TrimSpace(out.resultText) != "" {
		n, err := r.svc.TurnReplyCount(bg, t.ID)
		if err != nil {
			r.log.Error("count replies", "turn", t.ID, "err", err)
		} else if n == 0 {
			if _, err := r.svc.AppendBotMessage(bg, bot.ID, t.ID, out.resultText, nil, true, false); err != nil {
				r.log.Error("deliver final text", "turn", t.ID, "err", err)
			}
		}
	}
}

// delivery is what a turn delivers to the bot. The runner marks it handled
// only after the turn succeeds, so a failed turn delivers it again.
type delivery struct {
	inbox   []core.InboxMessage // held direct messages
	roomSeq int64               // seq of the last room message, for a room turn
}

// prepare builds the claude invocation for the turn. It starts a new Claude
// session on the bot's first turn, and on its first turn in each room. It
// also returns what the turn delivers.
func (r *Runner) prepare(ctx context.Context, bot core.Bot, t core.Turn) (claudeRun, delivery, error) {
	now := time.Now().In(r.svc.Location())
	profile, recent, err := r.svc.PromptMemory(ctx, bot.ID)
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	routines, err := r.svc.ListRoutines(ctx, bot.ID)
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	teammates, err := r.svc.Teammates(ctx, bot.ID)
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	rooms, err := r.svc.RoomsOf(ctx, bot.ID)
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	inbox, err := r.svc.Inbox(ctx, bot.ID)
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	global, err := r.globalInstructions()
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	delivered := delivery{inbox: inbox}

	prompt, sessionID := t.Prompt, bot.ClaudeSessionID
	if skill, args, ok := r.invokedSkill(t); ok {
		prompt = skillPrompt(skill, args)
	}
	if t.Source == core.SourceUser {
		media, err := r.svc.TurnMedia(ctx, bot.ID, t.ID)
		if err != nil {
			return claudeRun{}, delivery{}, err
		}
		prompt += mediaNote(r.svc.MediaDir(), media)
	}
	if t.RoomID != "" {
		update, err := r.svc.PendingRoomUpdate(ctx, t.RoomID, bot.ID)
		if err != nil {
			return claudeRun{}, delivery{}, err
		}
		prompt, sessionID = roomPrompt(bot, update, t.Prompt, r.svc.MediaDir()), update.Seat.ClaudeSessionID
		delivered.roomSeq = update.LastSeq
	}
	run := claudeRun{
		Prompt: timestampLine(now) + inboxBlock(inbox) + prompt,
		SystemPrompt: systemPrompt(promptContext{
			Bot: bot, Workspace: r.svc.Workspace(bot.ID), Profile: profile, Recent: recent, Routines: routines,
			Teammates: teammates, Rooms: rooms, Now: now, Global: global,
		}),
		SessionID: sessionID,
		Resume:    sessionID != "",
		BotName:   bot.Name,
		Model:     r.cfg.Model,
	}
	if !run.Resume {
		run.SessionID = uuid.NewString()
		if err := r.saveSession(ctx, bot.ID, t.RoomID, run.SessionID); err != nil {
			return claudeRun{}, delivery{}, err
		}
	}
	return run, delivered, nil
}

// invokedSkill reports whether a user message starts with "/<skill-id>" for an
// existing skill, and returns the skill and the text after the id.
func (r *Runner) invokedSkill(t core.Turn) (core.Skill, string, bool) {
	if t.Source != core.SourceUser || !strings.HasPrefix(t.Prompt, "/") {
		return core.Skill{}, "", false
	}
	id, args, _ := strings.Cut(strings.TrimPrefix(t.Prompt, "/"), " ")
	skill, err := r.svc.GetSkill(id)
	if err != nil {
		return core.Skill{}, "", false
	}
	return skill, strings.TrimSpace(args), true
}

// compact runs Claude Code's /compact on the bot's session (its seat's session
// for a room turn) and records the result. It returns the turn status and error.
func (r *Runner) compact(ctx context.Context, bot core.Bot, t core.Turn, transcript string, fail func(error)) (string, string) {
	bg := context.WithoutCancel(ctx)
	note := func(text string) {
		if _, err := r.svc.AppendEntry(bg, transcript, core.KindEvent, t.ID, map[string]string{"type": "context-compacted", "text": text}); err != nil {
			r.log.Error("record compaction", "turn", t.ID, "err", err)
		}
	}
	who := ""
	if t.RoomID != "" {
		who = bot.Name + ": "
	}
	sessionID := bot.ClaudeSessionID
	if t.RoomID != "" {
		seats, err := r.svc.RoomSeats(ctx, t.RoomID)
		if err != nil {
			fail(err)
			return "failed", err.Error()
		}
		sessionID = ""
		for _, seat := range seats {
			if seat.ID == bot.ID {
				sessionID = seat.ClaudeSessionID
			}
		}
	}
	if sessionID == "" {
		note(who + "nothing to compact yet; the context is empty.")
		return "done", ""
	}
	// With background tasks running, the bot's process is still alive and compacts its own session.
	out, err := r.sendCommand(ctx, sessionKey{bot.ID, t.RoomID}, bot, t, "/compact", func(string) ([]string, string, error) {
		return compactArgs(sessionID, r.cfg.Model), "", nil
	})
	switch {
	case ctx.Err() != nil:
		note(who + "compaction stopped.")
		return "interrupted", "stopped by the user"
	case err != nil:
		fail(err)
		return "failed", err.Error()
	case out.failed() && out.sessionMissing():
		if err := r.saveSession(bg, bot.ID, t.RoomID, ""); err != nil {
			r.log.Error("drop missing session", "turn", t.ID, "err", err)
		}
		note(who + "nothing to compact; the stored session no longer exists, so the next turn starts fresh.")
		return "done", ""
	case out.failed():
		fail(errors.New(out.errorText()))
		return "failed", out.errorText()
	}
	text := who + "context compacted. Earlier messages stay visible here."
	if c := out.compacted; c != nil {
		text = fmt.Sprintf("%scontext compacted from %s to %s tokens. Earlier messages stay visible here.", who, kTokens(c.PreTokens), kTokens(c.PostTokens))
	}
	note(strings.ToUpper(text[:1]) + text[1:])
	return "done", ""
}

func kTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// saveSession stores the Claude session of the bot's main chat, or of its seat in a room.
func (r *Runner) saveSession(ctx context.Context, botID, roomID, sessionID string) error {
	if roomID != "" {
		return r.svc.SetRoomSession(ctx, roomID, botID, sessionID)
	}
	return r.svc.SetSession(ctx, botID, sessionID)
}

// runTurn sends the turn's prompt to the claude process of its Claude
// session, which starts with run's arguments and a new MCP config file when
// none runs.
func (r *Runner) runTurn(ctx context.Context, bot core.Bot, t core.Turn, run claudeRun) (outcome, error) {
	return r.sendCommand(ctx, sessionKey{bot.ID, t.RoomID}, bot, t, run.Prompt, func(token string) ([]string, string, error) {
		path, signIn, err := r.writeMCPConfig(ctx, bot.ID, token)
		if err != nil {
			return nil, "", err
		}
		if len(signIn) > 0 {
			r.noteSignIn(ctx, t, signIn)
		}
		run.MCPConfig = path
		args, err := run.args()
		if err != nil {
			r.removeMCPConfig(path)
			return nil, "", err
		}
		return args, path, nil
	})
}

// noteSignIn tells the user in the turn's chat that the connectors in names
// need sign-in, so this turn runs without them.
func (r *Runner) noteSignIn(ctx context.Context, t core.Turn, names []string) {
	transcript := t.BotID
	if t.RoomID != "" {
		transcript = t.RoomID
	}
	verb := "needs"
	if len(names) > 1 {
		verb = "need"
	}
	text := fmt.Sprintf("%s %s sign-in: open Connectors in the sidebar", strings.Join(names, ", "), verb)
	if _, err := r.svc.AppendEntry(context.WithoutCancel(ctx), transcript, core.KindEvent, t.ID, map[string]string{"type": "connector-sign-in", "text": text}); err != nil {
		r.log.Error("record connector sign-in", "turn", t.ID, "err", err)
	}
}

// activityRecorder mirrors tool calls from the stream as activity entries.
type activityRecorder struct {
	svc     *core.Service
	botID   string // the transcript the entries go to: the bot's chat or a room
	botName string // set in a room, to show which member made the call
	turnID  string
	log     *slog.Logger
	entries map[string]activityBody // tool_use id -> body
	ids     map[string]string       // tool_use id -> entry id
	parents map[string]string       // tool_use id of a subagent's call -> tool_use id of the call that started the subagent
}

type activityBody struct {
	Bot      string `json:"bot,omitempty"`
	Tool     string `json:"tool"`
	Summary  string `json:"summary"`
	Status   string `json:"status"` // running, done, error
	Subagent bool   `json:"subagent"`
	Output   string `json:"output,omitempty"`
}

func newActivityRecorder(svc *core.Service, botID, turnID string, log *slog.Logger) *activityRecorder {
	return &activityRecorder{svc: svc, botID: botID, turnID: turnID, log: log, entries: map[string]activityBody{}, ids: map[string]string{}, parents: map[string]string{}}
}

func (a *activityRecorder) toolUses(ctx context.Context, ev streamEvent) {
	if ev.Message == nil {
		return
	}
	for _, b := range ev.Message.Content {
		// YarGPT's own tools create their own entries (messages, approval cards).
		if b.Type != "tool_use" || strings.HasPrefix(b.Name, "mcp__"+mcpServerName+"__") {
			continue
		}
		body := activityBody{Bot: a.botName, Tool: b.Name, Summary: summarizeToolUse(b.Name, b.Input), Status: "running", Subagent: ev.ParentToolUseID != nil}
		e, err := a.svc.AppendEntry(ctx, a.botID, core.KindActivity, a.turnID, body)
		if err != nil {
			a.log.Error("record tool use", "turn", a.turnID, "err", err)
			continue
		}
		a.entries[b.ID], a.ids[b.ID] = body, e.ID
		if ev.ParentToolUseID != nil {
			a.parents[b.ID] = *ev.ParentToolUseID
		}
	}
}

// owns reports whether the recorder recorded the tool call toolUseID.
func (a *activityRecorder) owns(toolUseID string) bool {
	_, ok := a.ids[toolUseID]
	return ok
}

// root returns the main-thread call that started the subagent that made the
// call toolUseID, or "" for a call of the main thread.
func (a *activityRecorder) root(toolUseID string) string {
	root := ""
	for range len(a.parents) {
		parent, ok := a.parents[toolUseID]
		if !ok {
			break
		}
		root, toolUseID = parent, parent
	}
	return root
}

func (a *activityRecorder) toolResults(ctx context.Context, ev streamEvent) {
	if ev.Message == nil {
		return
	}
	for _, b := range ev.Message.Content {
		body, ok := a.entries[b.ToolUseID]
		if b.Type != "tool_result" || !ok {
			continue
		}
		body.Status, body.Output = "done", truncate(toolResultText(b.Content), maxActivityText)
		if b.IsError {
			body.Status = "error"
		}
		a.update(ctx, b.ToolUseID, body)
	}
}

// closeOpen marks tool calls that never got a result as errors. It skips a
// call when keep returns true for the call's root (see root); a nil keep
// skips none.
func (a *activityRecorder) closeOpen(ctx context.Context, keep func(root string) bool) {
	for id, body := range a.entries {
		if body.Status == "running" && (keep == nil || !keep(a.root(id))) {
			body.Status = "error"
			body.Output = "No result: the turn ended first."
			a.update(ctx, id, body)
		}
	}
}

func (a *activityRecorder) update(ctx context.Context, toolUseID string, body activityBody) {
	a.entries[toolUseID] = body
	if _, err := a.svc.UpdateEntry(ctx, a.ids[toolUseID], body); err != nil {
		a.log.Error("record tool result", "turn", a.turnID, "err", err)
	}
}

// limitedBuffer keeps the last maxStderr bytes written to it.
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Write(p)
	if over := l.buf.Len() - maxStderr; over > 0 {
		l.buf.Next(over)
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}
