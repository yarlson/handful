package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"yargpt/internal/core"
)

// taskTurnPrompt is stored as the prompt of a task turn; Claude Code writes
// the real one (the task's notification) itself.
const taskTurnPrompt = "[task] A background task reported."

// sessionKey names a Claude session: a bot's main chat, or its seat in a room.
type sessionKey struct{ botID, roomID string }

// transcript is the chat that shows the session's activity.
func (k sessionKey) transcript() string {
	if k.roomID != "" {
		return k.roomID
	}
	return k.botID
}

// session is the claude process of one Claude session. Turns reach it as
// stream-json messages on stdin, and Claude Code runs them one at a time. The
// process stays alive while background tasks that the model started (Bash
// with run_in_background, Monitor, background subagents) run, and Claude Code
// wakes the model when one of them reports. YarGPT keeps stdin open while
// tasks run, so later turns go to the same process, and closes it when no
// task is left; the process then exits.
type session struct {
	key    sessionKey
	bot    core.Bot
	token  string
	files  []string // MCP config and system prompt files of the process, removed when it exits
	flags  runFlags // the model and effort the process started with
	stdin  io.WriteCloser
	kill   context.CancelFunc
	exited chan struct{} // closed after the process exits and its turns are finished

	// Guarded by Runner.mu.
	open    bool                // stdin is open, so the session takes turns
	stopped bool                // stopSession ended the process
	silent  bool                // stopIfSilent ended the process
	waking  bool                // a task turn runs
	pending map[string]*command // turn id -> message on stdin that has not completed
	tasks   []core.BackgroundTask
	// busySince is when the process last became busy with a task turn or
	// background tasks, for the update's list of running work.
	busySince time.Time
	// taskHops is the most hops of a turn that changed the background tasks.
	// A task turn starts at it, as the turn that started the task.
	taskHops int64

	// lastOutput is when the process last printed a stream line, or when a
	// command came to a session that had none pending. Guarded by Runner.mu.
	lastOutput time.Time
}

// command is a turn's message to a session.
type command struct {
	turn  core.Turn
	rc    runContext
	flags runFlags     // used when the command starts a process
	done  chan outcome // receives the outcome once
}

// activeTurn is the turn that the session's events belong to: a command, or
// a task turn when cmd is nil.
type activeTurn struct {
	turn core.Turn
	cmd  *command
	act  *activityRecorder
	out  outcome
}

// onInit handles a process's init event: output outside any message starts
// a task turn, and the reported model is recorded. It returns the active turn.
func (r *Runner) onInit(ctx context.Context, s *session, cur *activeTurn, model string) *activeTurn {
	if cur == nil {
		cur = r.startTask(s)
	}
	r.noteInitModel(ctx, s, cur.turnID(), model)
	return cur
}

// turnID is the id of the active turn; "" when there is none.
func (a *activeTurn) turnID() string {
	if a == nil {
		return ""
	}
	return a.turn.ID
}

// sendCommand writes prompt to the claude process of key as the message of
// turn t and waits until Claude Code completes it. When no process runs for
// key, it starts one with startArgs, which receive the MCP token and return
// the arguments and the MCP config file, if any. Cancelling ctx stops the
// process and its background tasks.
func (r *Runner) sendCommand(ctx context.Context, key sessionKey, bot core.Bot, t core.Turn, prompt string, flags runFlags, startArgs func(token string) (args []string, files []string, err error)) (outcome, error) {
	msg, err := userMessage(t.ID, prompt)
	if err != nil {
		return outcome{}, err
	}
	c := &command{turn: t, rc: runContext{BotID: bot.ID, BotName: bot.Name, TurnID: t.ID, RoomID: key.roomID, Hops: t.Hops, BoardRole: t.BoardRole, UserPost: t.FromUserPost(), Post: flags.post}, flags: flags, done: make(chan outcome, 1)}
	s, err := r.claimSession(ctx, key, bot, c, startArgs)
	if err != nil {
		return outcome{}, err
	}
	if _, err := s.stdin.Write(msg); err != nil {
		r.stopSession(s)
		<-c.done
		return outcome{}, fmt.Errorf("send turn %s to claude: %w", t.ID, err)
	}
	select {
	case out := <-c.done:
		return out, nil
	case <-ctx.Done():
		r.stopSession(s)
		return <-c.done, nil
	}
}

// claimSession puts c on the open session of key, or starts a session for it.
// A closed session can still run a last task turn, and a second process must
// not resume the same Claude session meanwhile, so it waits for that one to exit.
func (r *Runner) claimSession(ctx context.Context, key sessionKey, bot core.Bot, c *command, startArgs func(string) ([]string, []string, error)) (*session, error) {
	for {
		r.mu.Lock()
		s := r.sessions[key]
		if s == nil {
			r.mu.Unlock()
			return r.startSession(key, bot, c, startArgs)
		}
		if s.open {
			// The running process keeps the model and effort it started with.
			c.flags, c.rc.Post = s.flags, s.flags.post
			if len(s.pending) == 0 {
				s.lastOutput = time.Now()
			}
			s.pending[c.turn.ID] = c
			r.mu.Unlock()
			return s, nil
		}
		r.mu.Unlock()
		select {
		case <-s.exited:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (r *Runner) startSession(key sessionKey, bot core.Bot, c *command, startArgs func(string) ([]string, []string, error)) (*session, error) {
	dir, err := r.svc.TurnWorkspace(context.Background(), key.botID, key.roomID)
	if err != nil {
		return nil, err
	}
	token, err := r.tokens.issue(c.rc)
	if err != nil {
		return nil, err
	}
	args, files, err := startArgs(token)
	if err != nil {
		r.tokens.revoke(token)
		return nil, err
	}
	// The process outlives the turn that starts it. stopSession, and
	// stopSessions at shutdown, end it.
	ctx, kill := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, r.cfg.ClaudePath, args...)
	cmd.Dir = dir
	// Bundled skills such as "schedule" create cloud routines, which compete with YarGPT's own routines.
	// The silent-turn reminder tells the model that the user waits for news,
	// which is wrong for hidden routine and agent turns.
	// Claude Code finds CLAUDE.md files by walking up from the workspace, which
	// lies under the user's home, so ~/.claude/CLAUDE.md would reach every bot
	// even with --setting-sources project.
	// The child also loses the variables that would override the model or
	// effort that YarGPT picked (childEnv).
	cmd.Env = append(childEnv(r.environ(), c.flags.effort != ""), "MCP_TOOL_TIMEOUT="+mcpToolTimeoutMs, "CLAUDE_CODE_DISABLE_BUNDLED_SKILLS=1", "CLAUDE_CODE_SILENT_TURN_REMINDER=0", "CLAUDE_CODE_DISABLE_CLAUDE_MDS=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = processWaitDelay
	fail := func(err error) (*session, error) {
		kill()
		r.tokens.revoke(token)
		r.removeRunFiles(files)
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail(fmt.Errorf("open claude stdin: %w", err))
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(fmt.Errorf("open claude stdout: %w", err))
	}
	stderr := &limitedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return fail(fmt.Errorf("start %s: %w", r.cfg.ClaudePath, err))
	}
	s := &session{key: key, bot: bot, token: token, files: files, flags: c.flags, stdin: stdin, kill: kill, exited: make(chan struct{}), open: true, lastOutput: time.Now(), pending: map[string]*command{c.turn.ID: c}}
	r.mu.Lock()
	r.sessions[key] = s
	r.mu.Unlock()
	r.wg.Go(func() {
		r.serve(s, cmd, stdout, stderr)
	})
	return s, nil
}

// serve reads the session's event stream until the process exits. Events
// between a message's "started" and "completed" lifecycle events belong to
// that message's turn. Any other model run is Claude Code waking the model
// for a background task; it becomes a task turn.
func (r *Runner) serve(s *session, cmd *exec.Cmd, stdout io.Reader, stderr *limitedBuffer) {
	ctx := context.Background()
	var cur *activeTurn
	var orphan outcome // a result outside any turn: claude failed before it took the message
	// A resumed session also reports the tasks of an earlier process that
	// ended; the chat showed those already.
	listed := map[string]bool{}
	liveTasks := 0
	// The recorders of ended turns, while background subagents that those
	// turns started may still report calls.
	var bg []*activityRecorder
	endActivity := func(act *activityRecorder) {
		if liveTasks == 0 {
			act.closeOpen(ctx, nil)
			return
		}
		act.closeOpen(ctx, func(root string) bool { return root != "" })
		if len(act.ids) > 0 {
			bg = append(bg, act)
		}
	}
	recorderOf := func(ev streamEvent) *activityRecorder {
		if ev.ParentToolUseID != nil {
			for _, act := range bg {
				if act.owns(*ev.ParentToolUseID) {
					return act
				}
			}
		}
		if cur == nil {
			return nil
		}
		return cur.act
	}
	quit := make(chan struct{})
	var watcher sync.WaitGroup
	watcher.Go(func() { r.stopIfSilent(s, quit) })
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), maxStreamLine)
	for sc.Scan() {
		r.mu.Lock()
		s.lastOutput = time.Now()
		r.mu.Unlock()
		var ev streamEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			r.log.Warn("skip unreadable stream line", "bot", s.key.botID, "err", err)
			continue
		}
		switch ev.Type {
		case "command_lifecycle":
			r.mu.Lock()
			c := s.pending[ev.CommandUUID]
			terminal := ev.State != "queued" && ev.State != "started"
			if terminal {
				delete(s.pending, ev.CommandUUID)
			}
			r.mu.Unlock()
			if c == nil {
				continue
			}
			switch ev.State {
			case "queued":
			case "started":
				if cur != nil && cur.cmd == nil {
					endActivity(cur.act)
					r.finishTask(s, cur, "done", "")
				}
				r.tokens.set(s.token, c.rc)
				cur = &activeTurn{turn: c.turn, cmd: c, act: r.recorder(s, c.turn.ID)}
			default:
				// Claude Code ends a command with "completed", or with
				// "cancelled" after an API error such as a usage limit, and
				// then waits for more input. Any other state also ends the
				// command, because waiting for "completed" would hang the turn.
				if ev.State != "completed" && ev.State != "cancelled" {
					r.log.Warn("unknown command state ends the turn", "bot", s.key.botID, "state", ev.State)
				}
				out := orphan
				if cur != nil && cur.cmd == c {
					endActivity(cur.act)
					out, cur = cur.out, nil
				}
				c.done <- out
				r.closeIfIdle(s)
			}
		case "system":
			switch ev.Subtype {
			case "init":
				cur = r.onInit(ctx, s, cur, ev.Model)
			case "compact_boundary":
				if cur != nil && ev.CompactMetadata != nil {
					cur.out.compacted = ev.CompactMetadata
				}
			case "task_notification":
				if listed[ev.TaskID] {
					r.noteTask(s, cur, ev.Status, ev.Summary)
				}
				if ev.ToolUseID != "" {
					for _, act := range bg {
						act.closeOpen(ctx, func(root string) bool { return root != ev.ToolUseID })
					}
				}
			case "background_tasks_changed":
				for _, t := range ev.Tasks {
					listed[t.TaskID] = true
				}
				liveTasks = len(ev.Tasks)
				if liveTasks == 0 {
					for _, act := range bg {
						act.closeOpen(ctx, nil)
					}
					bg = nil
				}
				r.setTasks(s, ev.Tasks)
			}
		case "assistant":
			if cur != nil {
				cur.out.noteAPIError(ev)
			}
			if act := recorderOf(ev); act != nil {
				act.toolUses(ctx, ev)
			}
		case "user":
			if act := recorderOf(ev); act != nil {
				act.toolResults(ctx, ev)
			}
		case "result":
			text, isErr := ev.Result, ev.IsError
			if isErr && text == "" {
				text = fmt.Sprint(ev.Subtype, " ", string(ev.Errors))
			}
			out, turnID := &orphan, ""
			if cur != nil {
				out, turnID = &cur.out, cur.turn.ID
			}
			out.noteAPIError(ev)
			out.resultText, out.resultError = text, isErr
			// The pause starts before the turn ends, so no queued turn starts in between.
			if out.hitUsageLimit() {
				r.pauseForUsageLimit(s.key.transcript(), turnID, *out)
			}
			if cur == nil {
				continue
			}
			if cur.cmd == nil {
				status, errText := "done", ""
				if isErr {
					status, errText = "failed", cur.out.errorText()
				}
				endActivity(cur.act)
				r.finishTask(s, cur, status, errText)
				cur = nil
				r.closeIfIdle(s)
			}
		}
	}
	if err := sc.Err(); err != nil {
		r.log.Warn("read claude stream", "bot", s.key.botID, "err", err)
		_, _ = io.Copy(io.Discard, stdout)
	}
	close(quit)
	watcher.Wait()
	exitErr := cmd.Wait()
	s.kill()
	r.removeRunFiles(s.files)

	r.mu.Lock()
	if r.sessions[s.key] == s {
		delete(r.sessions, s.key)
	}
	pending, stopped, silent, hadTasks := s.pending, s.stopped, s.silent, len(s.tasks) > 0
	s.open, s.pending, s.tasks = false, nil, nil
	r.mu.Unlock()
	r.tokens.revoke(s.token)
	stderrText := stderr.String()
	stderrText, exitErr = r.exitReason(s, exitErr, stderrText, stopped, silent)
	if cur != nil && cur.cmd == nil {
		status, errText := "failed", outcome{exitErr: exitErr, stderr: stderrText}.errorText()
		if stopped && !silent {
			status, errText = "interrupted", "stopped by the user"
		}
		cur.act.closeOpen(ctx, nil)
		r.finishTask(s, cur, status, errText)
	}
	for _, c := range pending {
		out := orphan
		if cur != nil && cur.cmd == c {
			cur.act.closeOpen(ctx, nil)
			out = cur.out
		}
		out.exitErr, out.stderr = exitErr, stderrText
		c.done <- out
	}
	for _, act := range bg {
		act.closeOpen(ctx, nil)
	}
	if hadTasks {
		r.svc.PublishTasks(s.key.transcript(), r.Tasks(s.key.transcript()))
	}
	close(s.exited)
	r.signalDone()
}

func (r *Runner) recorder(s *session, turnID string) *activityRecorder {
	act := newActivityRecorder(r.svc, s.key.transcript(), turnID, r.log)
	if s.key.roomID != "" {
		act.botName = s.bot.Name
	}
	return act
}

// startTask records the task turn that Claude Code started on its own. It
// returns nil when the turn cannot be recorded; its events are then dropped.
func (r *Runner) startTask(s *session) *activeTurn {
	r.mu.Lock()
	hops := s.taskHops
	r.mu.Unlock()
	t, err := r.svc.StartTaskTurn(context.Background(), s.key.botID, s.key.roomID, taskTurnPrompt, hops)
	if err != nil {
		r.log.Error("record task turn", "bot", s.key.botID, "err", err)
		return nil
	}
	r.tokens.set(s.token, runContext{BotID: s.bot.ID, BotName: s.bot.Name, TurnID: t.ID, RoomID: s.key.roomID, Hops: t.Hops, Post: s.flags.post})
	r.mu.Lock()
	markBusy(s)
	s.waking = true
	r.mu.Unlock()
	r.publishState(s.key.botID, s.key.roomID)
	return &activeTurn{turn: t, act: r.recorder(s, t.ID)}
}

// finishTask records the end of a task turn: "done", "failed" or "interrupted".
func (r *Runner) finishTask(s *session, a *activeTurn, status, errText string) {
	ctx := context.Background()
	switch status {
	case "failed":
		r.log.Error("task turn failed", "bot", s.key.botID, "turn", a.turn.ID, "err", errText)
		if _, err := r.svc.AppendEntry(ctx, s.key.transcript(), core.KindEvent, a.turn.ID, map[string]string{"type": "turn-failed", "text": errText}); err != nil {
			r.log.Error("record task turn failure", "turn", a.turn.ID, "err", err)
		}
	case "interrupted":
		if _, err := r.svc.AppendEntry(ctx, s.key.transcript(), core.KindEvent, a.turn.ID, map[string]string{"type": "interrupted", "text": "Stopped."}); err != nil {
			r.log.Error("record interrupt", "turn", a.turn.ID, "err", err)
		}
	}
	if err := r.svc.FinishTurn(ctx, a.turn.ID, status, errText); err != nil {
		r.log.Error("finish task turn", "turn", a.turn.ID, "err", err)
	}
	r.svc.ExpireTurnApprovals(a.turn.ID)
	r.mu.Lock()
	s.waking = false
	r.mu.Unlock()
	r.publishState(s.key.botID, s.key.roomID)
	r.signalDone()
}

// noteTask shows a background task's notification ("... completed (exit
// code 0)") in the chat. The summary of a stopped task is only its description.
func (r *Runner) noteTask(s *session, cur *activeTurn, status, summary string) {
	if summary == "" {
		return
	}
	if status == "stopped" {
		summary = "Background task stopped: " + summary
	}
	if s.key.roomID != "" {
		summary = s.bot.Name + ": " + summary
	}
	turnID := ""
	if cur != nil {
		turnID = cur.turn.ID
	}
	if _, err := r.svc.AppendEntry(context.Background(), s.key.transcript(), core.KindEvent, turnID, map[string]string{"type": "task", "text": summary}); err != nil {
		r.log.Error("record task notification", "bot", s.key.botID, "err", err)
	}
}

func (r *Runner) setTasks(s *session, list []streamTask) {
	tasks := make([]core.BackgroundTask, 0, len(list))
	for _, t := range list {
		task := core.BackgroundTask{ID: t.TaskID, Description: t.Description}
		if s.key.roomID != "" {
			task.Bot = s.bot.Name
		}
		tasks = append(tasks, task)
	}
	// The token holds the turn whose run reported the change.
	rc, _ := r.tokens.get(s.token)
	r.mu.Lock()
	if len(tasks) > 0 {
		markBusy(s)
	}
	s.tasks = tasks
	s.taskHops = max(s.taskHops, rc.Hops)
	r.mu.Unlock()
	r.svc.PublishTasks(s.key.transcript(), r.Tasks(s.key.transcript()))
	r.closeIfIdle(s)
}

// closeIfIdle closes the session's stdin when no turn and no task needs the
// process any more. The process then finishes what it still has and exits.
func (r *Runner) closeIfIdle(s *session) {
	r.mu.Lock()
	idle := s.open && len(s.pending) == 0 && !s.waking && len(s.tasks) == 0
	if idle {
		s.open, s.lastOutput = false, time.Now()
	}
	r.mu.Unlock()
	if !idle {
		return
	}
	if err := s.stdin.Close(); err != nil {
		r.log.Warn("close claude stdin", "bot", s.key.botID, "err", err)
	}
}

// awaited reports whether something waits for the process to print: a
// pending command, a task turn, or, once stdin is closed, its exit, which
// the next turn of the bot waits for. The caller holds r.mu.
func (s *session) awaited() bool {
	return len(s.pending) > 0 || s.waking || !s.open
}

// stopIfSilent stops the session when something waits for the process and it
// prints nothing for outputTimeout. It returns when quit closes.
func (r *Runner) stopIfSilent(s *session, quit <-chan struct{}) {
	timer := time.NewTimer(r.outputTimeout)
	defer timer.Stop()
	for {
		select {
		case <-quit:
			return
		case <-timer.C:
		}
		r.mu.Lock()
		next := r.outputTimeout
		if s.awaited() {
			next -= time.Since(s.lastOutput)
		}
		s.silent = next <= 0
		r.mu.Unlock()
		if next > 0 {
			timer.Reset(next)
			continue
		}
		r.log.Warn("stop claude after no output", "bot", s.key.botID, "room", s.key.roomID, "timeout", r.outputTimeout)
		r.stopSession(s)
		return
	}
}

// stopSession ends the process and its background tasks.
func (r *Runner) stopSession(s *session) {
	r.mu.Lock()
	s.stopped = true
	r.mu.Unlock()
	s.kill()
}

// Tasks returns the background tasks shown in the chat of a bot or a room.
func (r *Runner) Tasks(id string) []core.BackgroundTask {
	r.mu.Lock()
	defer r.mu.Unlock()
	tasks := []core.BackgroundTask{}
	for _, s := range r.sessions {
		if s.key.transcript() == id {
			tasks = append(tasks, s.tasks...)
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Bot < tasks[j].Bot })
	return tasks
}

// HasSession reports whether a claude process runs for the chat of a bot or
// a room, because a turn or background tasks need it.
func (r *Runner) HasSession(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if s.key.transcript() == id {
			return true
		}
	}
	return false
}

// exitReason is how a process's end reads in its turns: a stop for silence,
// a signal from outside YarGPT, such as a pattern kill, or the exit error and
// stderr as they are.
func (r *Runner) exitReason(s *session, exitErr error, stderr string, stopped, silent bool) (string, error) {
	if silent {
		return "", fmt.Errorf("no output from Claude for %.0f minutes", r.outputTimeout.Minutes())
	}
	if sig, ok := killedBy(exitErr); ok && !stopped {
		r.log.Warn("claude stopped by a signal", "bot", s.key.botID, "signal", sig, "stderr", truncate(stderr, 500))
		return "", fmt.Errorf("stopped by %s from outside YarGPT", sig)
	}
	return stderr, exitErr
}

// killedBy reports the signal that ended a process, by its name, when a
// signal ended it.
func killedBy(exitErr error) (string, bool) {
	var ee *exec.ExitError
	if !errors.As(exitErr, &ee) {
		return "", false
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		return "", false
	}
	if name := unix.SignalName(ws.Signal()); name != "" {
		return name, true
	}
	return fmt.Sprintf("signal %d", int(ws.Signal())), true
}

// markBusy notes, under Runner.mu, when an idle process becomes busy with a
// task turn or background tasks.
func markBusy(s *session) {
	if !s.waking && len(s.tasks) == 0 {
		s.busySince = time.Now()
	}
}
