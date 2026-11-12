package agent

import (
	"context"
	"regexp"
	"time"

	"handful/internal/core"
)

const (
	// usageLimitError is the api_error kind of Claude Code's usage limit refusal.
	usageLimitError = "usage_limit_reached"
	// usageLimitPrefix starts the text of every Claude Code usage limit
	// message, for example "You've hit your 5-hour limit · resets 8pm (Europe/Riga)".
	usageLimitPrefix  = "You've hit your "
	limitPauseDefault = 15 * time.Minute
	limitPauseMax     = 6 * time.Hour
	// A reset time in the message that passed less than limitResetGraceMinutes ago is the
	// reset that just happened, not the next one; then turns wait limitPauseShort and try again.
	limitResetGraceMinutes = 5
	limitPauseShort        = time.Minute
)

// limitResetText matches the reset in a usage limit message: "resets 8pm
// (Europe/Riga)", "resets 8:30pm (UTC)" or "resets Nov 13, 4pm (Europe/Riga)".
// A reset without a date is the next one at that hour.
var limitResetText = regexp.MustCompile(`resets (?:([A-Z][a-z]{2} \d{1,2}), )?(\d{1,2}(?::\d{2})?[ap]m) \(([^)]+)\)`)

// usageLimitReset returns when a usage limit ends: at resetsAt (Unix seconds)
// when it is after now, else at the reset in text, else limitPauseDefault after now.
// A reset in text that passed within limitResetGraceMinutes gives limitPauseShort.
func usageLimitReset(resetsAt int64, text string, now time.Time) time.Time {
	if reset := time.Unix(resetsAt, 0); resetsAt > 0 && reset.After(now) {
		return reset
	}
	m := limitResetText.FindStringSubmatch(text)
	if m == nil {
		return now.Add(limitPauseDefault)
	}
	date, clock, zone := m[1], m[2], m[3]
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return now.Add(limitPauseDefault)
	}
	local, dated := now.In(loc), date != ""
	if !dated {
		date = local.Format("Jan 2")
	}
	for _, layout := range []string{"Jan 2 3pm", "Jan 2 3:04pm"} {
		t, err := time.ParseInLocation(layout, date+" "+clock, loc)
		if err != nil {
			continue
		}
		return nextReset(time.Date(local.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc), now, dated)
	}
	return now.Add(limitPauseDefault)
}

// PausedUntil returns when the runner starts new turns again after a usage
// limit, or the zero time when it is not paused.
func (r *Runner) PausedUntil() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pausedUntil
}

// pauseForUsageLimit stops new turns until the usage limit in out resets, at
// most maxLimitPause from now. The limit belongs to the Claude account, so the
// pause holds for every bot. Only a new pause, not a longer one, adds an
// event to the chat of transcript.
func (r *Runner) pauseForUsageLimit(transcript, turnID string, out outcome) {
	now := time.Now()
	until := usageLimitReset(out.limitResetsAt, out.resultText, now)
	if bound := now.Add(r.maxLimitPause); until.After(bound) {
		until = bound
	}
	// Publishing under mu keeps the pause events in the order of the changes.
	r.mu.Lock()
	started, longer := r.pausedUntil.IsZero(), until.After(r.pausedUntil)
	if longer {
		r.pausedUntil = until
		r.resume.Reset(until.Sub(now))
		r.svc.PublishPause(until)
	}
	r.mu.Unlock()
	if !started {
		return
	}
	r.log.Warn("usage limit reached; new turns wait", "until", until)
	text := "Claude usage limit reached. New turns wait until " + until.In(r.svc.Location()).Format("Jan 2, 15:04") + "."
	if _, err := r.svc.AppendEntry(context.Background(), transcript, core.KindEvent, turnID, map[string]string{"type": "usage-limit", "text": text}); err != nil {
		r.log.Error("record usage limit", "turn", turnID, "err", err)
	}
}

// endPause ends the pause when its time has come; Run then dispatches the
// queued turns. Otherwise the pause grew meanwhile, and the timer waits again.
func (r *Runner) endPause() {
	r.mu.Lock()
	if left := time.Until(r.pausedUntil); left > 0 {
		r.resume.Reset(left)
		r.mu.Unlock()
		return
	}
	r.pausedUntil = time.Time{}
	r.svc.PublishPause(time.Time{})
	r.mu.Unlock()
}

// nextReset returns when a limit whose message names reset ends: reset itself when it is after
// now; limitPauseShort from now when it passed within the grace window; else the same time a
// year later, for a dated reset, or a day later.
func nextReset(reset, now time.Time, dated bool) time.Time {
	if reset.After(now) {
		return reset
	}
	if now.Sub(reset) < limitResetGraceMinutes*time.Minute {
		return now.Add(limitPauseShort)
	}
	if dated {
		return reset.AddDate(1, 0, 0)
	}
	return reset.AddDate(0, 0, 1)
}
