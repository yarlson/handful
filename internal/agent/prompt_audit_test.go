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

// Audit finding 8: in a team turn send_to_agent only posts to a room, waking no one.
func TestSendToAgentInATeamTurnSaysItOnlyPostsToARoom(t *testing.T) {
	t.Parallel()
	team := sendToAgentDescription(true)
	wantText(t, "send_to_agent in a team turn", team,
		[]string{"In a team turn you cannot message a bot: to ask a teammate to act, post in the team's channel with send_to_user and write @Name."},
		[]string{"message them directly", "Message one of the user's other bots"})
	if outside := sendToAgentDescription(false); !strings.Contains(outside, "To ask a teammate to act, message them directly.") {
		t.Errorf("send_to_agent outside a team = %q; want it to say to message a teammate", outside)
	}
}
