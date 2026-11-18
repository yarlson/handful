package core

import (
	"context"
	"database/sql"
	"fmt"

	"handful/internal/db"
)

// PlanWindow is the state of one usage window of the Claude plan, such as five_hour or
// seven_day, as Claude Code last reported it.
type PlanWindow struct {
	Window             string   `json:"window"`
	Status             string   `json:"status"`                       // allowed, allowed_warning or rejected
	Utilization        *float64 `json:"utilization,omitempty"`        // the fraction of the limit used
	ResetsAt           int64    `json:"resetsAt,omitempty"`           // Unix seconds; 0 when not reported
	SurpassedThreshold *float64 `json:"surpassedThreshold,omitempty"` // the fraction of the limit whose warning fired
	ReceivedAt         int64    `json:"receivedAt"`                   // ms
}

// RecordPlanWindows stores each of windows as the latest state of its window, received now, and
// sends them to the event stream. No windows send nothing.
func (s *Service) RecordPlanWindows(ctx context.Context, windows []PlanWindow) error {
	if len(windows) == 0 {
		return nil
	}
	now := s.nowMs()
	received := make([]PlanWindow, len(windows))
	err := s.inTx(ctx, func(q *db.Queries) error {
		for i, w := range windows {
			w.ReceivedAt = now
			received[i] = w
			err := q.SetPlanWindow(ctx, db.SetPlanWindowParams{
				Name: w.Window, Status: w.Status, Utilization: nullFloat(w.Utilization), ResetsAt: w.ResetsAt,
				SurpassedThreshold: nullFloat(w.SurpassedThreshold), ReceivedAt: now,
			})
			if err != nil {
				return fmt.Errorf("store plan window %s: %w", w.Window, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record the plan windows: %w", err)
	}
	s.hub.Publish(Event{Type: "planWindows", PlanWindows: received})
	return nil
}

// PlanWindows returns the latest state of each plan window that Claude Code has reported, by
// window name.
func (s *Service) PlanWindows(ctx context.Context) ([]PlanWindow, error) {
	rows, err := s.q.ListPlanWindows(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the plan windows: %w", err)
	}
	windows := make([]PlanWindow, len(rows))
	for i, r := range rows {
		windows[i] = PlanWindow{
			Window: r.Name, Status: r.Status, Utilization: floatOf(r.Utilization), ResetsAt: r.ResetsAt,
			SurpassedThreshold: floatOf(r.SurpassedThreshold), ReceivedAt: r.ReceivedAt,
		}
	}
	return windows, nil
}

func nullFloat(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

func floatOf(f sql.NullFloat64) *float64 {
	if !f.Valid {
		return nil
	}
	return &f.Float64
}
