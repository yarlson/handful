package agent

import (
	"strings"
	"testing"
	"time"

	"handful/internal/core"
)

// wantText checks that text holds each of want and none of gone.
func wantText(t *testing.T, what, text string, want, gone []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s lacks %q", what, w)
		}
	}
	for _, g := range gone {
		if strings.Contains(text, g) {
			t.Errorf("%s still says %q", what, g)
		}
	}
}

// Audit finding 3: a task has no turn limit; only the team's daily budget limits turns.
func TestBuilderSaysATeamHasNoTurnLimitUntilADailyBudget(t *testing.T) {
	t.Parallel()
	builder := systemPrompt(promptContext{Bot: core.Bot{ID: "b", Name: "Builder", Role: core.RoleBuilder}, Now: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)})
	wantText(t, "Builder's prompt", builder,
		[]string{"A team has no turn limit until the user sets a daily budget in the team's settings."},
		[]string{"30 turns", "budget of turns"})
}
