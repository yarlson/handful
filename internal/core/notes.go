package core

import "context"

// noteTo is a board note for a team's channel.
type noteTo struct{ teamID, text string }

func (s *Service) postNotes(ctx context.Context, notes []noteTo) error {
	for _, n := range notes {
		if n.teamID == "" {
			continue // a handoff that the user made for no team
		}
		if err := s.boardNote(ctx, n.teamID, n.text); err != nil {
			return err
		}
	}
	return nil
}

// actorName names who acts for a channel note.
func actorName(team Bot, a Actor) string {
	if a.BotID != "" {
		return memberName(team, a.BotID)
	}
	if a.Role == RoleUser {
		return "The user"
	}
	return "The app"
}
