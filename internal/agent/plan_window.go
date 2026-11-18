package agent

import (
	"context"
	"maps"
	"regexp"
	"slices"

	"handful/internal/core"
)

// rateLimitInfo is the plan usage that a rate_limit_event reports: each window in
// UnifiedWindows, or, when that is absent, the one window RateLimitType in the top-level fields.
type rateLimitInfo struct {
	planWindowInfo
	RateLimitType  string                    `json:"rateLimitType"`
	UnifiedWindows map[string]planWindowInfo `json:"unifiedWindows"`
}

type planWindowInfo struct {
	Status             string   `json:"status"`
	ResetsAt           int64    `json:"resetsAt"` // Unix seconds
	Utilization        *float64 `json:"utilization"`
	SurpassedThreshold *float64 `json:"surpassedThreshold"`
}

// planWindowName is the form of the window names that Claude Code sends, such as seven_day.
var planWindowName = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// maxPlanWindows bounds the windows that one event stores.
const maxPlanWindows = 16

// planWindows returns the windows of info in name order. It leaves out a window whose name is
// not of the form planWindowName.
func (info rateLimitInfo) planWindows() []core.PlanWindow {
	windows := info.UnifiedWindows
	if len(windows) == 0 {
		windows = map[string]planWindowInfo{info.RateLimitType: info.planWindowInfo}
	}
	var out []core.PlanWindow
	for _, name := range slices.Sorted(maps.Keys(windows)) {
		if !planWindowName.MatchString(name) || len(out) == maxPlanWindows {
			continue
		}
		w := windows[name]
		out = append(out, core.PlanWindow{
			Window: name, Status: w.Status, Utilization: w.Utilization, ResetsAt: w.ResetsAt,
			SurpassedThreshold: w.SurpassedThreshold,
		})
	}
	return out
}

// recordPlanWindows stores the plan windows of the rate_limit_event info that bot botID's
// stream reported.
func (r *Runner) recordPlanWindows(botID string, info *rateLimitInfo) {
	if info == nil {
		return
	}
	if err := r.svc.RecordPlanWindows(context.Background(), info.planWindows()); err != nil {
		r.log.Error("record plan windows", "bot", botID, "err", err)
	}
}
