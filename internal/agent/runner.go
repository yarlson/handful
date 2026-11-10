// Package agent runs bot turns as local Claude Code CLI processes and serves
// the MCP tools those processes call back into.
package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"yargpt/internal/core"
)

type Config struct {
	ClaudePath  string // claude executable
	MCPURL      string // URL of this server's MCP endpoint
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
// Conversation turns in one room also run one at a time, so each member sees
// the replies before its own; idle members go before members that are busy
// elsewhere. The work and the reviews of a team's items run beside them.
type Runner struct {
	svc    *core.Service
	cfg    Config
	tokens *tokenRegistry
	oauth  *signIns
	log    *slog.Logger

	outputTimeout time.Duration // turnOutputTimeout; tests shorten it
	// environ is the environment that claude processes start from:
	// os.Environ, which tests replace because they run in parallel.
	environ func() []string
	// publish sends a working state; tests watch it.
	publish func(id string, running bool)

	mu       sync.Mutex
	running  map[string]runningTurn // bot id -> its running turn
	deleting map[string]bool        // bots and rooms being deleted; none of their turns starts
	sessions map[sessionKey]*session
	models   map[sessionKey]modelReport // the model each session's process reported
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
	itemID string // set for a work or review turn on a team's item
	goalID string // set for a board turn: the goal it works for
	board  bool   // a board turn: it counts toward the board turns' cap
	since  time.Time
}

// leadDecision reports whether t is a lead's board turn: a wake to decide on
// or plan the team's work. It doesn't wait for the room's chat and goes
// ahead of other queued turns, so the lead's decisions don't queue behind
// conversation.
func leadDecision(t core.Turn) bool { return t.BoardRole == core.RoleLead }

// boardTurn reports whether t counts toward the cap on board turns: work for
// a goal, or a lead's decision.
func boardTurn(t core.Turn) bool { return t.GoalID != "" || leadDecision(t) }

// leadsTurn reports whether t is one of a lead's board turns, which dispatch
// tries first: a decision, or the retro of an accepted goal. The retro
// stays among them, so that it keeps its place before the planning of the
// next goal, which is queued after it.
func leadsTurn(t core.Turn) bool { return leadDecision(t) || t.BoardRole == core.RoleRetro }

// dispatchOrder puts the leads' board turns first and keeps the queue order
// otherwise.
func dispatchOrder(turns []core.Turn) []core.Turn {
	slices.SortStableFunc(turns, func(a, b core.Turn) int {
		if leadsTurn(a) == leadsTurn(b) {
			return 0
		}
		if leadsTurn(a) {
			return -1
		}
		return 1
	})
	return turns
}

func NewRunner(svc *core.Service, cfg Config, log *slog.Logger) *Runner {
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 3
	}
	resume := time.NewTimer(0)
	resume.Stop()
	return &Runner{
		svc: svc, cfg: cfg, tokens: newTokenRegistry(log), oauth: newSignIns(), log: log, outputTimeout: turnOutputTimeout, running: make(map[string]runningTurn), deleting: make(map[string]bool), sessions: make(map[sessionKey]*session), models: make(map[sessionKey]modelReport), done: make(chan struct{}, 1),
		resume: resume, maxLimitPause: limitPauseMax, publish: svc.PublishState, environ: os.Environ,
	}
}

// Run dispatches turns and fires due routines until ctx ends, then stops all
// running turns and claude processes and waits for them.
func (r *Runner) Run(ctx context.Context) {
	r.stopLeftoverProcesses()
	r.removeLeftoverRunFiles()
	ticker := time.NewTicker(routineTick)
	defer ticker.Stop()
	defer r.resume.Stop()
	r.fireRoutines(ctx)
	r.settleItems(ctx)
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
		r.settleItems(ctx)
		r.dispatch(ctx)
	}
}

// settleItems blocks the items whose owner or reviewer stopped without
// handing them on, so that the lead learns of them.
func (r *Runner) settleItems(ctx context.Context) {
	if err := r.svc.SettleItems(ctx, r.hasSession); err != nil && ctx.Err() == nil {
		r.log.Error("settle board items", "err", err)
	}
}

// hasSession reports whether a claude process runs for the bot's seat in a room.
func (r *Runner) hasSession(botID, roomID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.sessions[sessionKey{botID, roomID}]
	return ok
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

// chatBusyLocked reports whether a conversation turn runs in the room: a
// turn that works on no item. Work and reviews of items run beside it.
func (r *Runner) chatBusyLocked(roomID string) bool {
	for _, rt := range r.running {
		if rt.roomID == roomID && rt.itemID == "" {
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

// BeginDelete starts the deletion of a bot or a room: until done is called,
// none of its turns starts, and its queued turns are cancelled now.
func (r *Runner) BeginDelete(ctx context.Context, id string) (done func(), err error) {
	r.mu.Lock()
	r.deleting[id] = true
	r.mu.Unlock()
	done = func() {
		r.mu.Lock()
		delete(r.deleting, id)
		r.mu.Unlock()
	}
	if err := r.svc.CancelQueuedTurnsOf(ctx, id); err != nil {
		done()
		return nil, err
	}
	return done, nil
}

// FinishDelete deletes the bot or the room once it has stopped. Posts
// during the wait may have queued turns, for example of a room's members,
// so it cancels them again first.
func (r *Runner) FinishDelete(ctx context.Context, id string) error {
	if err := r.svc.CancelQueuedTurnsOf(ctx, id); err != nil {
		return err
	}
	return r.svc.DeleteBot(ctx, id)
}

// Interrupt stops the bot's running turn and ends its claude processes, with
// their background tasks. For a room it does the same for the members' turns
// and processes in the room, and drops the member turns still queued there.
// It returns false when nothing was running.
func (r *Runner) Interrupt(ctx context.Context, id string) (bool, error) {
	// A team's board stops first, so that no item counts as left behind
	// meanwhile; the turns stop even when that fails.
	stopErr := r.svc.StopTeam(ctx, id)
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
	return stopped, errors.Join(stopErr, r.svc.CancelQueuedRoomTurns(ctx, id))
}

// CancelGoal stops the running turns that work for a goal, in every team,
// and returns how many it stopped.
func (r *Runner) CancelGoal(goalID string) int {
	r.mu.Lock()
	var cancels []context.CancelFunc
	for _, rt := range r.running {
		if rt.goalID == goalID {
			cancels = append(cancels, rt.cancel)
		}
	}
	r.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	return len(cancels)
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
// It publishes under mu, as dispatch does, so a state that a turn which
// ended read cannot arrive after the next turn has published that it runs.
func (r *Runner) publishState(botID, roomID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publish(botID, r.botBusyLocked(botID) || r.roomBusyLocked(botID))
	if roomID != "" {
		r.publish(roomID, r.botBusyLocked(roomID) || r.roomBusyLocked(roomID))
	}
}

// signalDone makes Run dispatch again: a bot or a room may be free.
func (r *Runner) signalDone() {
	select {
	case r.done <- struct{}{}:
	default:
	}
}

// dispatch starts the queued turns that may start. It starts none after a
// usage limit, or while the server drains for an update; the turns stay
// queued, and leaving drain mode signals TurnsReady.
func (r *Runner) dispatch(ctx context.Context) {
	if draining, _ := r.svc.Draining(); draining || ctx.Err() != nil || !r.PausedUntil().IsZero() {
		return
	}
	turns, err := r.svc.QueuedTurns(ctx, 100)
	if err != nil {
		r.log.Error("list queued turns", "err", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range dispatchOrder(turns) {
		if len(r.running) >= r.cfg.MaxParallel {
			return
		}
		if !r.mayStartLocked(ctx, t) {
			continue
		}
		started, err := r.svc.StartTurn(ctx, t.ID)
		if err != nil {
			r.log.Error("start turn", "turn", t.ID, "err", err)
		}
		// A turn that a board change cancelled after the list was read does not start.
		if !started {
			continue
		}
		turnCtx, cancel := context.WithCancel(ctx)
		r.running[t.BotID] = runningTurn{cancel: cancel, roomID: t.RoomID, itemID: t.ItemID, goalID: t.GoalID, board: boardTurn(t), since: time.Now()}
		r.publish(t.BotID, true)
		if t.RoomID != "" {
			r.publish(t.RoomID, true)
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

// mayStartLocked reports whether turn t may start now. r.mu is held.
func (r *Runner) mayStartLocked(ctx context.Context, t core.Turn) bool {
	if r.deleting[t.BotID] || r.deleting[t.RoomID] {
		return false
	}
	// A bot runs one turn at a time. A member busy elsewhere does not hold
	// up the room: the next idle member in queue order goes first, and the
	// busy member answers when it is free.
	if r.botBusyLocked(t.BotID) {
		return false
	}
	// A room runs one conversation turn at a time, so each member sees the
	// replies before its own. A lead's board turn doesn't wait for it, so a
	// retro keeps its place ahead of the next goal's planning.
	if t.RoomID != "" && t.ItemID == "" && !leadsTurn(t) && r.chatBusyLocked(t.RoomID) {
		return false
	}
	if !boardTurn(t) {
		return true
	}
	// Board turns leave a slot for the user's other chats.
	if r.boardTurnsLocked() >= max(1, r.cfg.MaxParallel-1) {
		return false
	}
	if t.GoalID == "" {
		return true
	}
	ok, err := r.svc.GoalAllows(ctx, t)
	if err != nil {
		r.log.Error("check the goal of a turn", "turn", t.ID, "goal", t.GoalID, "err", err)
	}
	return ok
}

func (r *Runner) boardTurnsLocked() int {
	n := 0
	for _, rt := range r.running {
		if rt.board {
			n++
		}
	}
	return n
}

// outcome is what Claude Code reported for one message.
type outcome struct {
	compacted   *compactMetadata // set when the session was compacted
	resultText  string
	resultError bool
	exitErr     error
	stderr      string

	modelNotFound bool  // Claude Code could not run the requested model
	usageLimit    bool  // Claude Code reported the usage_limit_reached API error
	limitResetsAt int64 // Unix seconds when the usage limit resets; 0 when not reported
}

func (o outcome) failed() bool { return o.exitErr != nil || o.resultError }

// noteAPIError records a usage limit error of an assistant or result event,
// and a model that Claude Code could not run.
func (o *outcome) noteAPIError(ev streamEvent) {
	o.modelNotFound = o.modelNotFound || ev.Error == modelNotFound
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
	transcript := turnTranscript(t)
	fail := func(err error) {
		status, errText = "failed", err.Error()
		r.recordFailure(bg, t, transcript, err)
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

	out, err := r.runTurnOnce(ctx, bot, t, run)
	switch {
	case ctx.Err() != nil:
		status, errText = r.recordInterrupt(bg, t, transcript)
	case err != nil:
		fail(err)
	case out.failed():
		status, errText = r.turnFailed(bg, bot, t, run.Flags, out, fail)
	default:
		r.finishDelivery(bg, bot, t, run, delivered, out)
	}
}

// turnTranscript is where a turn shows its activity, approvals and
// failures: its room, or the bot's chat.
func turnTranscript(t core.Turn) string {
	if t.RoomID != "" {
		return t.RoomID
	}
	return t.BotID
}

// recordFailure logs a failed turn and writes its turn-failed entry.
func (r *Runner) recordFailure(ctx context.Context, t core.Turn, transcript string, err error) {
	r.log.Error("turn failed", "bot", t.BotID, "turn", t.ID, "err", err)
	if _, aerr := r.svc.AppendEntry(ctx, transcript, core.KindEvent, t.ID, map[string]string{"type": "turn-failed", "text": err.Error()}); aerr != nil {
		r.log.Error("record turn failure", "turn", t.ID, "err", aerr)
	}
}

// recordInterrupt writes the entry of a turn that the user stopped and
// returns its status and error.
func (r *Runner) recordInterrupt(ctx context.Context, t core.Turn, transcript string) (string, string) {
	if _, err := r.svc.AppendEntry(ctx, transcript, core.KindEvent, t.ID, map[string]string{"type": "interrupted", "text": "Stopped."}); err != nil {
		r.log.Error("record interrupt", "turn", t.ID, "err", err)
	}
	return "interrupted", "stopped by the user"
}

// runTurnOnce runs the turn, and once more on a new Claude session when the
// stored one is gone (for example, deleted on disk).
func (r *Runner) runTurnOnce(ctx context.Context, bot core.Bot, t core.Turn, run claudeRun) (outcome, error) {
	out, err := r.runTurn(ctx, bot, t, run)
	if err != nil || !out.failed() || !run.Resume || !out.sessionMissing() || ctx.Err() != nil {
		return out, err
	}
	run.Resume, run.SessionID = false, uuid.NewString()
	if err := r.saveSession(ctx, bot.ID, t.RoomID, run.SessionID); err != nil {
		return out, err
	}
	return r.runTurn(ctx, bot, t, run)
}

// finishDelivery marks what a successful turn delivered as handled, and
// delivers the final text when a user wrote and the bot sent nothing.
func (r *Runner) finishDelivery(ctx context.Context, bot core.Bot, t core.Turn, run claudeRun, delivered delivery, out outcome) {
	if err := r.svc.AckInbox(ctx, delivered.inbox); err != nil {
		r.log.Error("acknowledge inbox", "turn", t.ID, "err", err)
	}
	if t.RoomID != "" {
		if err := r.svc.MarkRoomSeen(ctx, t.RoomID, bot.ID, delivered.roomSeq); err != nil {
			r.log.Error("mark room seen", "turn", t.ID, "err", err)
		}
	}
	if t.Source == core.SourceUser && strings.TrimSpace(out.resultText) != "" {
		r.deliverFinalText(ctx, bot, t, run, out)
	}
}

// deliverFinalText posts the turn's final text when the bot sent nothing:
// a user who wrote something must get an answer even if the model forgot
// send_to_user.
func (r *Runner) deliverFinalText(ctx context.Context, bot core.Bot, t core.Turn, run claudeRun, out outcome) {
	n, err := r.svc.TurnReplyCount(ctx, t.ID)
	if err != nil {
		r.log.Error("count replies", "turn", t.ID, "err", err)
		return
	}
	if n > 0 {
		return
	}
	if _, err := r.svc.AppendBotMessageWith(ctx, bot.ID, t.ID, out.resultText, nil, true, false, r.postMeta(runContext{BotID: bot.ID, Post: run.Flags.post})); err != nil {
		r.log.Error("deliver final text", "turn", t.ID, "err", err)
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
	flags, err := r.runSettings(ctx, bot, t)
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	// The item is being done from now on, so the prompt shows it that way.
	if t.BoardRole == core.RoleWork {
		if err := r.svc.StartItemWork(ctx, t.ItemID, bot.ID); err != nil {
			return claudeRun{}, delivery{}, err
		}
	}
	prompt, update, err := r.turnPrompt(ctx, bot, t)
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	pc, err := r.loadPromptContext(ctx, bot, update.Room, now)
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	held, err := r.heldMessages(ctx, bot, update.Room)
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	inbox, more := capHeld(held)
	questions, err := r.questionNotes(ctx, bot, t)
	if err != nil {
		return claudeRun{}, delivery{}, err
	}
	delivered := delivery{inbox: inbox, roomSeq: update.LastSeq}
	sessionID := bot.ClaudeSessionID
	if t.RoomID != "" {
		sessionID = update.Seat.ClaudeSessionID
	}
	run := claudeRun{
		Prompt:       timestampLine(now) + questions + inboxBlock(inbox) + heldOverflow(more) + prompt,
		SystemPrompt: systemPrompt(pc),
		SessionID:    sessionID,
		Resume:       sessionID != "",
		BotName:      bot.Name,
		Flags:        flags,
	}
	if !run.Resume {
		run.SessionID = uuid.NewString()
		if err := r.saveSession(ctx, bot.ID, t.RoomID, run.SessionID); err != nil {
			return claudeRun{}, delivery{}, err
		}
	}
	return run, delivered, nil
}

// runSettings resolves and checks the model and effort of the turn. A value
// that fails the check marks the bot, and the run does not start.
func (r *Runner) runSettings(ctx context.Context, bot core.Bot, t core.Turn) (runFlags, error) {
	var room core.Bot
	if t.RoomID != "" {
		var err error
		if room, err = r.svc.GetBot(ctx, t.RoomID); err != nil {
			return runFlags{}, err
		}
	}
	flags, err := r.settings(bot, room)
	if err != nil {
		r.svc.SetModelAttention(bot.ID, true)
	}
	return flags, err
}

// turnPrompt returns the message of the turn, and for a turn in a room the
// room update that the message carries.
func (r *Runner) turnPrompt(ctx context.Context, bot core.Bot, t core.Turn) (string, core.RoomUpdate, error) {
	if t.RoomID != "" {
		update, err := r.svc.PendingRoomUpdate(ctx, t.RoomID, bot.ID)
		if err != nil {
			return "", core.RoomUpdate{}, err
		}
		board, err := r.teamBoard(ctx, update.Room, t)
		if err != nil {
			return "", core.RoomUpdate{}, err
		}
		changes, err := r.itemChanges(ctx, t)
		if err != nil {
			return "", core.RoomUpdate{}, err
		}
		prompt := roomPrompt(bot, update, t.Prompt, r.svc.MediaDir()) + boardPrompt(bot, update.Room, board, t, changes)
		if skill, args, ok := r.teamSkill(update, t); ok {
			prompt += "\n\n" + skillPrompt(skill, args)
		}
		return prompt, update, nil
	}
	prompt := t.Prompt
	if skill, args, ok := r.invokedSkill(t); ok {
		prompt = skillPrompt(skill, args)
	}
	if t.Source == core.SourceUser {
		media, err := r.svc.TurnMedia(ctx, bot.ID, t.ID)
		if err != nil {
			return "", core.RoomUpdate{}, err
		}
		prompt += mediaNote(r.svc.MediaDir(), media)
	}
	return prompt, core.RoomUpdate{}, nil
}

// itemChanges returns the change requests of the item of a work or review
// turn, oldest first.
func (r *Runner) itemChanges(ctx context.Context, t core.Turn) ([]core.ItemEvent, error) {
	if t.BoardRole != core.RoleWork && t.BoardRole != core.RoleReview {
		return nil, nil
	}
	return r.svc.ChangeRequests(ctx, t.ItemID)
}

// questionNotes says what the bot needs to know about its questions in the
// chat of turn t: what the user's message answers (in a direct chat), what
// the user dismissed since its previous turn there, and what is still open.
func (r *Runner) questionNotes(ctx context.Context, bot core.Bot, t core.Turn) (string, error) {
	chat := turnTranscript(t)
	var answered []core.OpenQuestion
	if t.RoomID == "" && t.Source == core.SourceUser {
		var err error
		if answered, err = r.svc.TurnAnswers(ctx, bot.ID, t.ID); err != nil {
			return "", err
		}
	}
	since, err := r.svc.PreviousTurnStart(ctx, bot.ID, t.RoomID, t.ID)
	if err != nil {
		return "", err
	}
	dismissed, err := r.svc.DismissalsSince(ctx, chat, bot.ID, since)
	if err != nil {
		return "", err
	}
	open, err := r.svc.OpenQuestionsOf(ctx, chat, bot.ID)
	if err != nil {
		return "", err
	}
	return questionNotes(answered, dismissed, open, r.svc.Location()), nil
}

// heldMessages returns the messages held for the bot that its turn delivers.
// A turn in a team delivers none, since the bot answers only its team there:
// they wait for its next turn outside the team.
func (r *Runner) heldMessages(ctx context.Context, bot, room core.Bot) ([]core.InboxMessage, error) {
	if room.IsTeam() {
		return nil, nil
	}
	return r.svc.Inbox(ctx, bot.ID)
}

// teamBoard returns the board that a turn in a team shows: the team's open
// goal, and the requests of other teams to it. A retro turn shows the goal
// that it looks back on; a turn for another team's goal shows no goal of the
// team.
func (r *Runner) teamBoard(ctx context.Context, room core.Bot, t core.Turn) (core.Board, error) {
	if !room.IsTeam() {
		return core.Board{}, nil
	}
	if t.BoardRole == core.RoleRetro {
		return r.svc.GoalBoard(ctx, t.GoalID)
	}
	board, err := r.svc.Board(ctx, room.ID)
	if err != nil || board.Goal == nil {
		return board, err
	}
	if !board.Goal.Open() || (t.GoalID != "" && t.GoalID != board.Goal.ID) {
		board.Goal, board.Items = nil, nil
	}
	return board, nil
}

// loadPromptContext gathers what the system prompt describes. room is the
// room of a turn in a room, else the zero Bot; in a team the bot works in the
// team's folder and sees the team's memory.
func (r *Runner) loadPromptContext(ctx context.Context, bot, room core.Bot, now time.Time) (promptContext, error) {
	pc := promptContext{Bot: bot, Workspace: r.svc.Workspace(bot.ID), Now: now}
	teamID := ""
	if room.IsTeam() {
		pc.Team, pc.OwnWorkspace, pc.Workspace = &room, pc.Workspace, r.svc.TeamWorkspace(room.ID)
		teamID = room.ID
	}
	var err error
	if pc.Profile, pc.Recent, err = r.svc.PromptMemory(ctx, bot.ID, teamID); err != nil {
		return promptContext{}, err
	}
	if pc.Routines, err = r.svc.ListRoutines(ctx, bot.ID); err != nil {
		return promptContext{}, err
	}
	if pc.Teammates, err = r.svc.Teammates(ctx, bot.ID); err != nil {
		return promptContext{}, err
	}
	if pc.Rooms, err = r.svc.RoomsOf(ctx, bot.ID); err != nil {
		return promptContext{}, err
	}
	if pc.Global, err = r.globalInstructions(); err != nil {
		return promptContext{}, err
	}
	if pc.OtherTeams, err = r.otherTeams(ctx, room); err != nil {
		return promptContext{}, err
	}
	if pc.Groups, err = r.builderGroups(ctx, bot); err != nil {
		return promptContext{}, err
	}
	return pc, nil
}

// builderGroups returns the user's rooms and teams when bot is Builder, whose
// new names must differ from theirs.
func (r *Runner) builderGroups(ctx context.Context, bot core.Bot) ([]core.Bot, error) {
	if bot.Role != core.RoleBuilder {
		return nil, nil
	}
	all, err := r.svc.ListBots(ctx)
	if err != nil {
		return nil, err
	}
	var groups []core.Bot
	for _, b := range all {
		if b.IsRoom() {
			groups = append(groups, b)
		}
	}
	return groups, nil
}

// otherTeams returns the user's teams other than room, when room is a team.
func (r *Runner) otherTeams(ctx context.Context, room core.Bot) ([]core.Bot, error) {
	if !room.IsTeam() {
		return nil, nil
	}
	all, err := r.svc.ListBots(ctx)
	if err != nil {
		return nil, err
	}
	var teams []core.Bot
	for _, b := range all {
		if b.IsTeam() && b.ID != room.ID {
			teams = append(teams, b)
		}
	}
	return teams, nil
}

// invokedSkill reports whether a user message starts with "/<skill-id>" for an
// existing skill, and returns the skill and the text after the id.
func (r *Runner) invokedSkill(t core.Turn) (core.Skill, string, bool) {
	if t.Source != core.SourceUser {
		return core.Skill{}, "", false
	}
	return r.slashSkill(t.Prompt)
}

// slashSkill reports whether text starts with "/<skill-id>" for an existing
// skill, and returns the skill and the text after the id.
func (r *Runner) slashSkill(text string) (core.Skill, string, bool) {
	if !strings.HasPrefix(text, "/") {
		return core.Skill{}, "", false
	}
	id, args, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
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
	who := ""
	if t.RoomID != "" {
		who = bot.Name + ": "
	}
	note := func(text string) {
		if _, err := r.svc.AppendEntry(bg, transcript, core.KindEvent, t.ID, map[string]string{"type": "context-compacted", "text": text}); err != nil {
			r.log.Error("record compaction", "turn", t.ID, "err", err)
		}
	}
	sessionID, err := r.compactSession(ctx, bot, t.RoomID)
	if err != nil {
		fail(err)
		return "failed", err.Error()
	}
	if sessionID == "" {
		note(who + "nothing to compact yet; the context is empty.")
		return "done", ""
	}
	// With background tasks running, the bot's process is still alive and compacts its own session.
	out, err := r.runCompact(ctx, bot, t, sessionID)
	if ctx.Err() != nil {
		note(who + "compaction stopped.")
		return "interrupted", "stopped by the user"
	}
	return r.compactResult(bg, bot, t, who, out, err, note, fail)
}

// compactSession is the Claude session that /compact runs on: the bot's
// own, or its seat's in a room; "" when there is none yet.
func (r *Runner) compactSession(ctx context.Context, bot core.Bot, roomID string) (string, error) {
	if roomID == "" {
		return bot.ClaudeSessionID, nil
	}
	seats, err := r.svc.RoomSeats(ctx, roomID)
	if err != nil {
		return "", err
	}
	for _, seat := range seats {
		if seat.ID == bot.ID {
			return seat.ClaudeSessionID, nil
		}
	}
	return "", nil
}

// compactResult records how /compact ended and returns the turn's status
// and error. A stored session that no longer exists is dropped.
func (r *Runner) compactResult(ctx context.Context, bot core.Bot, t core.Turn, who string, out outcome, err error, note func(string), fail func(error)) (string, string) {
	if err != nil {
		fail(err)
		return "failed", err.Error()
	}
	if out.failed() && out.sessionMissing() {
		if err := r.saveSession(ctx, bot.ID, t.RoomID, ""); err != nil {
			r.log.Error("drop missing session", "turn", t.ID, "err", err)
		}
		note(who + "nothing to compact; the stored session no longer exists, so the next turn starts fresh.")
		return "done", ""
	}
	if out.failed() {
		fail(errors.New(out.errorText()))
		return "failed", out.errorText()
	}
	text := who + compactedText(out.compacted)
	note(strings.ToUpper(text[:1]) + text[1:])
	return "done", ""
}

// compactedText tells the user that the context was compacted, with the
// token counts when Claude Code reported them.
func compactedText(c *compactMetadata) string {
	if c == nil {
		return "context compacted. Earlier messages stay visible here."
	}
	return fmt.Sprintf("context compacted from %s to %s tokens. Earlier messages stay visible here.", kTokens(c.PreTokens), kTokens(c.PostTokens))
}

// runCompact sends /compact to the session's process, which starts with the
// model and effort of the session's turns when none runs.
func (r *Runner) runCompact(ctx context.Context, bot core.Bot, t core.Turn, sessionID string) (outcome, error) {
	flags, err := r.runSettings(ctx, bot, t)
	if err != nil {
		return outcome{}, err
	}
	return r.sendCommand(ctx, sessionKey{bot.ID, t.RoomID}, bot, t, "/compact", flags, func(string) ([]string, []string, error) {
		return compactArgs(sessionID, flags.model, flags.effort), nil, nil
	})
}

// turnFailed records a turn that Claude Code failed and returns its status
// and error. A model that Claude Code can't run gets its own entry, which
// says what to change, and the turn isn't retried; any other failure goes
// to fail.
func (r *Runner) turnFailed(ctx context.Context, bot core.Bot, t core.Turn, flags runFlags, out outcome, fail func(error)) (string, string) {
	if out.modelRejected() {
		r.noteModelFailure(ctx, sessionKey{bot.ID, t.RoomID}, bot.Name, t.ID, flags, out)
	} else {
		fail(errors.New(out.errorText()))
	}
	return "failed", out.errorText()
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
	return r.sendCommand(ctx, sessionKey{bot.ID, t.RoomID}, bot, t, run.Prompt, run.Flags, func(token string) ([]string, []string, error) {
		path, signIn, err := r.writeMCPConfig(ctx, bot.ID, token)
		if err != nil {
			return nil, nil, err
		}
		promptFile, err := r.writeRunFile("prompt-*.md", []byte(run.SystemPrompt))
		if err != nil {
			r.removeRunFile(path)
			return nil, nil, fmt.Errorf("system prompt file: %w", err)
		}
		files := []string{path, promptFile}
		if len(signIn) > 0 {
			r.noteSignIn(ctx, t, signIn)
		}
		run.MCPConfig, run.SystemPromptFile = path, promptFile
		args, err := run.args()
		if err != nil {
			r.removeRunFiles(files)
			return nil, nil, err
		}
		return args, files, nil
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
