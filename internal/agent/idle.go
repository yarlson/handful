package agent

import (
	"context"
	"errors"
	"slices"
	"time"

	"handful/internal/core"
)

// An idle compaction runs Claude Code's /compact on a large session that waits for its next turn,
// before the prompt cache of its last request expires. It resumes the session with the arguments
// of the turn that started the session's last process, so that its call reads the cached prefix;
// the next turn then starts from the summary instead of writing the whole context to the cache
// again.

// idleOrigin is the turn that started a session's process, and the run it started with.
type idleOrigin struct {
	turn core.Turn
	run  claudeRun
}

// lastRequest is the latest model call of a session's main thread.
type lastRequest struct {
	id    string // the call's message id
	usage tokenUsage
	at    time.Time // when its first event came
}

// idleState is the state of a planned idle compaction. A plan starts planned and ends in one of
// the states that idleMoves lists no move from.
type idleState string

const (
	idlePlanned    idleState = "planned"
	idleCompacting idleState = "compacting"
	idleSkipped    idleState = "skipped"   // a check at the due time failed; the log says which
	idleCancelled  idleState = "cancelled" // a new turn of the session, Clear context or the server's stop
	idleDone       idleState = "done"
	idleFailed     idleState = "failed"
)

var idleMoves = map[idleState][]idleState{
	idlePlanned:    {idleCompacting, idleSkipped, idleCancelled, idleFailed},
	idleCompacting: {idleDone, idleFailed},
}

// The reasons to skip a due compaction, in the order of the checks.
const (
	skipSettingOff    = "idle_compaction_off"
	skipPrefixChanged = "prefix_changed" // the last call wrote much of its context to the cache
	skipLapsed        = "lapsed"         // the due time passed long ago, as after the computer slept
	skipNewerRequest  = "newer_request"  // a turn of the session waits or runs
	skipNearLimit     = "near_limit"     // turns wait for a usage limit
	skipNotResumed    = "not_resumed"    // no turn would resume the session
)

// idlePlan is the idle compaction of one session, owned by the goroutine of awaitIdle.
type idlePlan struct {
	key       sessionKey
	bot       core.Bot
	origin    idleOrigin
	sessionID string
	last      tokenUsage
	due       time.Time
	state     idleState // guarded by Runner.mu
	cancel    context.CancelFunc
}

// noteRequest records the model call of an assistant event of the session's main thread.
func (r *Runner) noteRequest(s *session, ev streamEvent) {
	m := ev.Message
	if ev.Type != "assistant" || ev.ParentToolUseID != nil || m == nil || m.Usage == nil {
		return
	}
	if m.ID != s.last.id {
		s.last.id, s.last.at = m.ID, r.now()
	}
	s.last.usage = *m.Usage
}

// planIdleCompaction plans the idle compaction of s's session after its process exited by
// itself, when its last call's context was large enough. r.mu is not held.
func (r *Runner) planIdleCompaction(s *session, exitErr error) {
	if s.origin == nil || exitErr != nil || s.last.usage.context() < r.idleMinContext {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.stopped || r.sessions[s.key] != nil {
		return
	}
	done := r.runDone
	select {
	case <-done:
		return
	default:
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &idlePlan{key: s.key, bot: s.bot, origin: *s.origin, sessionID: s.claudeID, last: s.last.usage, due: s.last.at.Add(r.idleDelay), state: idlePlanned, cancel: cancel}
	r.idle[s.key] = p
	r.wg.Go(func() { r.awaitIdle(ctx, p, done) })
}

// awaitIdle waits until p is due and then compacts the session, unless a check skips it.
// Cancelling ctx, or Run's end (done), ends the wait.
func (r *Runner) awaitIdle(ctx context.Context, p *idlePlan, done <-chan struct{}) {
	select {
	case <-ctx.Done():
		return
	case <-done:
		r.moveIdle(p, idleCancelled)
		return
	case <-r.after(p.due.Sub(r.now())):
	}
	if !r.beginIdle(p) {
		return
	}
	r.publishState(p.key.botID, p.key.roomID)
	r.moveIdle(p, r.compactIdle(p))
	r.publishState(p.key.botID, p.key.roomID)
	r.signalDone()
}

// beginIdle moves p to compacting, or to skipped with the reason, which it logs. It returns
// whether the compaction starts.
func (r *Runner) beginIdle(p *idlePlan) bool {
	reason := r.idleSkip(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	if reason == "" {
		var err error
		if reason, err = r.idleSkipLocked(p); err != nil {
			r.log.Error("check an idle compaction", "bot", p.key.botID, "room", p.key.roomID, "err", err)
			r.moveIdleLocked(p, idleFailed)
			return false
		}
	}
	if reason == "" {
		return r.moveIdleLocked(p, idleCompacting)
	}
	if r.moveIdleLocked(p, idleSkipped) {
		r.log.Info("skip idle compaction", "bot", p.key.botID, "room", p.key.roomID, "reason", reason)
	}
	return false
}

// idleSkip returns why p must not run, from the setting, its last call and the clock; "" when
// none of them stops it.
func (r *Runner) idleSkip(p *idlePlan) string {
	if !r.cfg.IdleCompaction() {
		return skipSettingOff
	}
	// The compaction call reads only the prefix of the last call, not what that call wrote to
	// the cache; 2k tokens cover the compaction's own instructions.
	u := p.last
	if (u.InputTokens+u.CacheCreationInputTokens+u.OutputTokens+2000)*10 > u.context() {
		return skipPrefixChanged
	}
	// Wall clock times: the monotonic clock stops while the computer sleeps, and the cache does not.
	if r.now().Round(0).After(p.due.Round(0).Add(r.idleGrace)) {
		return skipLapsed
	}
	return ""
}

// idleSkipLocked returns why p must not run, from the turns and sessions that Handful keeps; ""
// when none of them stops it. The checks only read, so they may run under r.mu, which keeps a
// turn from starting before p moves to compacting.
func (r *Runner) idleSkipLocked(p *idlePlan) (string, error) {
	ctx, k := context.Background(), p.key
	open, err := r.svc.OpenSessionTurns(ctx, k.botID, k.roomID, k.step)
	if err != nil {
		return "", err
	}
	if open > 0 {
		return skipNewerRequest, nil
	}
	if !r.pausedUntil.IsZero() {
		return skipNearLimit, nil
	}
	if r.deleting[k.botID] || r.deleting[k.roomID] {
		return skipNotResumed, nil
	}
	id, err := r.svc.ResumedSession(ctx, k.botID, k.roomID, k.step)
	if err != nil {
		return "", err
	}
	if id != p.sessionID {
		return skipNotResumed, nil
	}
	return "", nil
}

// compactIdle sends /compact to a process that resumes p's session as its origin turn did, and
// records the compaction as a turn of its own. It returns the plan's end state.
func (r *Runner) compactIdle(p *idlePlan) idleState {
	ctx := context.Background()
	id, err := r.svc.StartCompactionTurn(ctx, p.origin.turn)
	if err != nil {
		r.log.Error("idle compaction failed", "bot", p.key.botID, "room", p.key.roomID, "err", err)
		return idleFailed
	}
	// The turn keeps its origin's fields, so the process gets the same tools.
	t, run := p.origin.turn, p.origin.run
	t.ID = id
	run.Resume, run.SessionID = true, p.sessionID
	out, err := r.sendCommand(ctx, p.key, p.bot, t, "/compact", "", run.Flags, r.turnStart(ctx, p.bot, t, run, nil))
	if err == nil && out.failed() {
		err = errors.New(out.errorText())
	}
	if err != nil {
		r.log.Error("idle compaction failed", "bot", p.key.botID, "room", p.key.roomID, "turn", id, "err", err)
		r.endTurn(ctx, id, "failed", err.Error())
		return idleFailed
	}
	text := "Compacted while idle, before the prompt cache expired."
	if p.key.roomID != "" {
		text = p.bot.Name + ": compacted while idle, before the prompt cache expired."
	}
	if _, err := r.svc.AppendEntry(ctx, p.key.transcript(), core.KindEvent, id, map[string]string{"type": "context-compacted", "text": text}); err != nil {
		r.log.Error("record compaction", "turn", id, "err", err)
	}
	r.endTurn(ctx, id, "done", "")
	return idleDone
}

// moveIdle moves p to state next; see moveIdleLocked.
func (r *Runner) moveIdle(p *idlePlan, next idleState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.moveIdleLocked(p, next)
}

// moveIdleLocked moves p to state next when idleMoves allows it, and reports whether it did. A
// plan that ends leaves r.idle, and its wait ends. r.mu is held.
func (r *Runner) moveIdleLocked(p *idlePlan, next idleState) bool {
	if !slices.Contains(idleMoves[p.state], next) {
		return false
	}
	p.state = next
	if _, more := idleMoves[next]; more {
		return true
	}
	if r.idle[p.key] == p {
		delete(r.idle, p.key)
	}
	p.cancel()
	return true
}

// cancelIdleLocked cancels the planned idle compactions of the sessions that match. A running one
// goes on: a new turn of its session waits for it, as for any turn of a busy bot. r.mu is held.
func (r *Runner) cancelIdleLocked(match func(sessionKey) bool) {
	for k, p := range r.idle {
		if match(k) {
			r.moveIdleLocked(p, idleCancelled)
		}
	}
}

// compactingLocked reports whether an idle compaction runs for a session that matches. r.mu is held.
func (r *Runner) compactingLocked(match func(sessionKey) bool) bool {
	for k, p := range r.idle {
		if p.state == idleCompacting && match(k) {
			return true
		}
	}
	return false
}

// CancelIdleCompaction cancels the planned idle compactions of the chat of a bot or a room, whose
// sessions Clear context drops.
func (r *Runner) CancelIdleCompaction(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelIdleLocked(func(k sessionKey) bool { return k.transcript() == id })
}
