package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"handful/internal/db"
)

const (
	minRoutineInterval = 5 * time.Minute
	maxRoutinesPerBot  = 50
	maxRoutinePrompt   = 8000
)

type Routine struct {
	ID        string `json:"id"`
	BotID     string `json:"botId"`
	Name      string `json:"name"`
	Prompt    string `json:"prompt"`
	Schedule  string `json:"schedule"`
	Enabled   bool   `json:"enabled"`
	NextRunAt *int64 `json:"nextRunAt"`
	LastRunAt *int64 `json:"lastRunAt"`
	// LastRunBy says who started the last run: "the schedule", "the user"
	// or bot "<name>"; empty for a run recorded before it was tracked.
	LastRunBy string `json:"lastRunBy"`
	CreatedAt int64  `json:"createdAt"`
}

// RoutinePatch changes a routine. A nil field keeps its value.
type RoutinePatch struct {
	Name     *string
	Prompt   *string
	Schedule *string
	Enabled  *bool
}

func nullableMs(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func toRoutine(r db.Routine) Routine {
	return Routine{ID: r.ID, BotID: r.BotID, Name: r.Name, Prompt: r.Prompt, Schedule: r.Schedule, Enabled: r.Enabled == 1, NextRunAt: nullableMs(r.NextRunAt), LastRunAt: nullableMs(r.LastRunAt), LastRunBy: r.LastRunBy.String, CreatedAt: r.CreatedAt}
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// parseSchedule accepts a 5-field cron expression or a descriptor such as
// @daily or "@every 2h". Runs must be at least minRoutineInterval apart.
func (s *Service) parseSchedule(expr string) (cron.Schedule, error) {
	expr = strings.TrimSpace(expr)
	sched, err := cronParser.Parse(expr)
	if err != nil {
		return nil, invalid("schedule %q: %v", expr, err)
	}
	first := sched.Next(s.now().In(s.loc))
	if first.IsZero() {
		return nil, invalid("schedule %q never fires", expr)
	}
	// Check a day of consecutive runs; a fixed-step schedule repeats within that window.
	prev := first
	for range 288 {
		next := sched.Next(prev)
		if next.IsZero() || next.Sub(first) > 24*time.Hour {
			break
		}
		if next.Sub(prev) < minRoutineInterval {
			return nil, invalid("schedule %q runs more often than every %s", expr, minRoutineInterval)
		}
		prev = next
	}
	return sched, nil
}

func validateRoutineText(name, prompt string) (string, string, error) {
	name = strings.TrimSpace(name)
	prompt = strings.TrimSpace(prompt)
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		return "", "", invalid("routine name must be 1 to %d characters", maxNameLen)
	}
	if prompt == "" || utf8.RuneCountInString(prompt) > maxRoutinePrompt {
		return "", "", invalid("routine prompt must be 1 to %d characters", maxRoutinePrompt)
	}
	return name, prompt, nil
}

func (s *Service) CreateRoutine(ctx context.Context, botID, name, prompt, schedule string) (Routine, error) {
	bot, err := s.GetBot(ctx, botID)
	if err != nil {
		return Routine{}, err
	}
	if bot.IsRoom() {
		return Routine{}, invalid("a room cannot have routines; give the routine to one of its bots")
	}
	name, prompt, err = validateRoutineText(name, prompt)
	if err != nil {
		return Routine{}, err
	}
	sched, err := s.parseSchedule(schedule)
	if err != nil {
		return Routine{}, err
	}
	now := s.nowMs()
	next := sched.Next(s.now().In(s.loc)).UnixMilli()
	row, err := s.q.CreateRoutine(ctx, db.CreateRoutineParams{
		ID: uuid.NewString(), BotID: botID, Name: name, Prompt: prompt, Schedule: strings.TrimSpace(schedule),
		Enabled: 1, NextRunAt: sql.NullInt64{Int64: next, Valid: true}, CreatedAt: now, UpdatedAt: now, MaxRoutines: maxRoutinesPerBot,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Routine{}, invalid("a bot can have at most %d routines", maxRoutinesPerBot)
	}
	if err != nil {
		return Routine{}, fmt.Errorf("create routine %q for bot %s: %w", name, botID, err)
	}
	r := toRoutine(row)
	if _, err := s.AppendEntry(ctx, botID, KindEvent, "", map[string]string{"type": "routine-created", "text": fmt.Sprintf("Created routine %q (%s)", r.Name, r.Schedule)}); err != nil {
		return Routine{}, err
	}
	return r, nil
}

func (s *Service) GetRoutine(ctx context.Context, id string) (Routine, error) {
	row, err := s.q.GetRoutine(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Routine{}, notFound("routine", id)
	}
	if err != nil {
		return Routine{}, fmt.Errorf("get routine %s: %w", id, err)
	}
	return toRoutine(row), nil
}

func (s *Service) ListRoutines(ctx context.Context, botID string) ([]Routine, error) {
	rows, err := s.q.ListRoutines(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("list routines of bot %s: %w", botID, err)
	}
	out := make([]Routine, 0, len(rows))
	for _, r := range rows {
		out = append(out, toRoutine(r))
	}
	return out, nil
}

// UpdateRoutine changes a routine. byBot names the bot that made the change;
// empty means the user, and then the routine's bot gets a notice.
func (s *Service) UpdateRoutine(ctx context.Context, id string, p RoutinePatch, byBot string) (Routine, error) {
	cur, err := s.GetRoutine(ctx, id)
	if err != nil {
		return Routine{}, err
	}
	name, prompt, schedule, enabled := p.applyTo(cur)
	name, prompt, err = validateRoutineText(name, prompt)
	if err != nil {
		return Routine{}, err
	}
	sched, err := s.parseSchedule(schedule)
	if err != nil {
		return Routine{}, err
	}
	next := sql.NullInt64{}
	enabledInt := int64(0)
	if enabled {
		enabledInt = 1
		next = sql.NullInt64{Int64: sched.Next(s.now().In(s.loc)).UnixMilli(), Valid: true}
	}
	row, err := s.storeRoutine(ctx, db.UpdateRoutineParams{Name: name, Prompt: prompt, Schedule: schedule, Enabled: enabledInt, NextRunAt: next, UpdatedAt: s.nowMs(), ID: id})
	if err != nil {
		return Routine{}, err
	}
	if changed := routineChanges(cur, toRoutine(row)); byBot == "" && len(changed) > 0 {
		text := fmt.Sprintf("The user changed routine %q: %s.", cur.Name, strings.Join(changed, ", "))
		if err := s.noticeRoutine(ctx, cur.BotID, "routine-updated", text); err != nil {
			return Routine{}, err
		}
	}
	return toRoutine(row), nil
}

// applyTo returns cur's name, prompt, schedule and enabled state with the patch's values in
// place of the ones it sets.
func (p RoutinePatch) applyTo(cur Routine) (name, prompt, schedule string, enabled bool) {
	name, prompt, schedule, enabled = cur.Name, cur.Prompt, cur.Schedule, cur.Enabled
	if p.Name != nil {
		name = *p.Name
	}
	if p.Prompt != nil {
		prompt = *p.Prompt
	}
	if p.Schedule != nil {
		schedule = strings.TrimSpace(*p.Schedule)
	}
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	return name, prompt, schedule, enabled
}

// storeRoutine saves a changed routine and ends its queued runs, which hold its old version.
func (s *Service) storeRoutine(ctx context.Context, arg db.UpdateRoutineParams) (db.Routine, error) {
	var row db.Routine
	err := s.inTx(ctx, func(q *db.Queries) (err error) {
		if err := s.cancelRoutineRuns(ctx, q, arg.ID, "the routine changed"); err != nil {
			return err
		}
		if row, err = q.UpdateRoutine(ctx, arg); err != nil {
			return fmt.Errorf("update routine %s: %w", arg.ID, err)
		}
		return nil
	})
	return row, err
}

// routineChanges names what differs between two versions of a routine.
func routineChanges(old, cur Routine) []string {
	var changed []string
	if old.Name != cur.Name {
		changed = append(changed, fmt.Sprintf("renamed to %q", cur.Name))
	}
	if old.Prompt != cur.Prompt {
		changed = append(changed, "prompt")
	}
	if old.Schedule != cur.Schedule {
		changed = append(changed, "schedule")
	}
	if old.Enabled != cur.Enabled {
		changed = append(changed, map[bool]string{true: "resumed", false: "paused"}[cur.Enabled])
	}
	return changed
}

// noticeRoutine tells a bot that the user changed one of its routines: in
// its chat for the user, and in its inbox for its next turn, since a bot's
// session never sees chat events.
func (s *Service) noticeRoutine(ctx context.Context, botID, kind, text string) error {
	if _, err := s.AppendEntry(ctx, botID, KindEvent, "", map[string]string{"type": kind, "text": text}); err != nil {
		return err
	}
	if err := s.q.InsertInboxMessage(ctx, db.InsertInboxMessageParams{ID: uuid.NewString(), BotID: botID, FromBotID: LearnedByUser, FromName: "the user", Text: text, CreatedAt: s.nowMs()}); err != nil {
		return fmt.Errorf("hold routine notice for bot %s: %w", botID, err)
	}
	return nil
}

// DeleteRoutine deletes a routine. byBot names the bot that deleted it; empty
// means the user, and then the routine's bot gets a notice.
func (s *Service) DeleteRoutine(ctx context.Context, id, byBot string) error {
	cur, err := s.GetRoutine(ctx, id)
	if err != nil {
		return err
	}
	var n int64
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := s.cancelRoutineRuns(ctx, q, id, "the routine was deleted"); err != nil {
			return err
		}
		if n, err = q.DeleteRoutine(ctx, id); err != nil {
			return fmt.Errorf("delete routine %s: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound("routine", id)
	}
	if byBot == "" {
		return s.noticeRoutine(ctx, cur.BotID, "routine-deleted", fmt.Sprintf("The user deleted routine %q.", cur.Name))
	}
	return nil
}

// RunRoutineNow queues one on-demand run of the routine and records it as
// the routine's last run. byBot names the bot that asked for the run; empty
// means the user. A bot's run counts as a hop after hops, the depth of the
// bot's turn, so a routine that runs itself stops at the hop limit.
func (s *Service) RunRoutineNow(ctx context.Context, id, byBot string, hops int64) (string, error) {
	r, err := s.GetRoutine(ctx, id)
	if err != nil {
		return "", err
	}
	turnHops := int64(0)
	if byBot != "" {
		turnHops = hops + 1
	}
	if turnHops > maxAgentHops {
		return "", invalid("routine %s was not run: this turn comes from a chain of %d bot-to-bot steps, the most allowed", r.ID, maxAgentHops)
	}
	now := s.now().In(s.loc)
	by, trigger := "the user", "the user"
	if byBot != "" {
		by, trigger = byBot, fmt.Sprintf("bot %q", byBot)
	}
	if err := s.q.SetRoutineLastRun(ctx, db.SetRoutineLastRunParams{LastRunAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true}, LastRunBy: sql.NullString{String: trigger, Valid: true}, ID: r.ID}); err != nil {
		return "", fmt.Errorf("record the run of routine %s: %w", r.ID, err)
	}
	t := Turn{BotID: r.BotID, Source: SourceRoutine, Hops: turnHops, Prompt: routinePrompt(r, "was run on demand by "+by, now)}
	var turnID string
	err = s.inTx(ctx, func(q *db.Queries) (err error) {
		turnID, err = s.queueRoutineTurn(ctx, q, r.ID, t)
		return err
	})
	if err != nil {
		return "", err
	}
	s.signalTurns()
	return turnID, nil
}

// queueRoutineTurn queues turn t, with q, as a run of routine routineID, and returns its id.
func (s *Service) queueRoutineTurn(ctx context.Context, q *db.Queries, routineID string, t Turn) (string, error) {
	id, err := s.queueTurn(ctx, q, t)
	if err != nil {
		return "", err
	}
	if err := q.LinkRoutineTurn(ctx, db.LinkRoutineTurnParams{TurnID: id, RoutineID: routineID}); err != nil {
		return "", fmt.Errorf("link turn %s to routine %s: %w", id, routineID, err)
	}
	return id, nil
}

// cancelRoutineRuns ends the queued runs of routine id, which a change or a deletion made
// stale: they hold the routine's old text, or a routine that is gone.
func (s *Service) cancelRoutineRuns(ctx context.Context, q *db.Queries, id, reason string) error {
	if err := q.CancelQueuedRoutineTurns(ctx, db.CancelQueuedRoutineTurnsParams{Reason: reason, At: sql.NullInt64{Int64: s.nowMs(), Valid: true}, RoutineID: id}); err != nil {
		return fmt.Errorf("cancel the queued runs of routine %s: %w", id, err)
	}
	return nil
}

// FireDueRoutines queues a turn for each enabled routine whose time has come
// and moves its next run forward. A routine whose last run still waits in the
// queue, behind a busy bot or a usage limit, only moves its next run forward:
// one waiting run does its work. It returns the number of routines fired.
func (s *Service) FireDueRoutines(ctx context.Context) (int, error) {
	now := s.now().In(s.loc)
	rows, err := s.q.ListDueRoutines(ctx, sql.NullInt64{Int64: now.UnixMilli(), Valid: true})
	if err != nil {
		return 0, fmt.Errorf("list due routines: %w", err)
	}
	fired := 0
	for _, row := range rows {
		ok, err := s.fireRoutine(ctx, toRoutine(row), now)
		if err != nil {
			return fired, err
		}
		if ok {
			fired++
		}
	}
	if fired > 0 {
		s.signalTurns()
	}
	return fired, nil
}

// fireRoutine moves routine r's next run forward from now and, unless a run of r waits in the
// queue, records this run and queues it, in one transaction. It reports whether it queued one.
func (s *Service) fireRoutine(ctx context.Context, r Routine, now time.Time) (bool, error) {
	next := sql.NullInt64{}
	// The spacing check ran when the schedule was saved. Running it again
	// here can fail near a clock change, which would stop the routine.
	if sched, err := cronParser.Parse(strings.TrimSpace(r.Schedule)); err == nil {
		next = sql.NullInt64{Int64: sched.Next(now).UnixMilli(), Valid: true}
	}
	fired := false
	err := s.inTx(ctx, func(q *db.Queries) error {
		waiting, err := q.HasQueuedRoutineTurn(ctx, r.ID)
		if err != nil {
			return fmt.Errorf("look for a queued run of routine %s: %w", r.ID, err)
		}
		if waiting {
			if err := q.SetRoutineNextRun(ctx, db.SetRoutineNextRunParams{NextRunAt: next, ID: r.ID}); err != nil {
				return fmt.Errorf("advance routine %s: %w", r.ID, err)
			}
			return nil
		}
		if err := q.MarkRoutineFired(ctx, db.MarkRoutineFiredParams{LastRunAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true}, LastRunBy: sql.NullString{String: "the schedule", Valid: true}, NextRunAt: next, ID: r.ID}); err != nil {
			return fmt.Errorf("advance routine %s: %w", r.ID, err)
		}
		fired = true
		_, err = s.queueRoutineTurn(ctx, q, r.ID, Turn{BotID: r.BotID, Source: SourceRoutine, Prompt: routinePrompt(r, "is due", now)})
		return err
	})
	return fired && err == nil, err
}

func routinePrompt(r Routine, why string, at time.Time) string {
	return fmt.Sprintf(`[routine] %q (id %s, schedule %q) %s, at %s.
This is your own standing order, not a message the user just typed.
What you saved to do each time:
%s

Carry it out now. Use send_to_user only when there is something worth telling the user; if nothing changed and the instruction says to stay quiet, end without a message.`,
		r.Name, r.ID, r.Schedule, why, at.Format("Mon Jan 2 2006 15:04 MST"), r.Prompt)
}
