package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

const (
	maxAgreementLen = 8000
	// DefaultGoalBudget is how many turns a goal of a team may use, unless the
	// team sets another number.
	DefaultGoalBudget = 30
	maxGoalBudget     = 500
)

// Team is what a team adds to its room: a lead, the working agreement and
// the turn budget of a goal. A room with a Team is a team.
type Team struct {
	// LeadID is the lead the user chose while it is a member, else the first
	// member, or "" when the team has no members.
	LeadID     string `json:"leadId"`
	Agreement  string `json:"agreement"`
	GoalBudget int64  `json:"goalBudget"`
	// DefaultModel and DefaultEffort fill in the model and effort of members
	// without their own, for their turns in the team. "" inherits.
	DefaultModel  string `json:"defaultModel"`
	DefaultEffort string `json:"defaultEffort"`
	// RequestSlots is how many requests of other teams the team works on at
	// once while its own goal is active: 1 to the number of members.
	RequestSlots int64 `json:"requestSlots"`
	// StartHandoffs is what an accepted handoff does when the team is free:
	// StartAuto starts it, StartAsk holds the queue until the user starts it.
	StartHandoffs string `json:"startHandoffs"`
}

func (b Bot) IsTeam() bool { return b.Team != nil }

// Lead returns the lead of a team. It returns false when b is not a team or
// has no members.
func (b Bot) Lead() (Member, bool) {
	if b.Team == nil {
		return Member{}, false
	}
	i := slices.IndexFunc(b.Members, func(m Member) bool { return m.ID == b.Team.LeadID })
	if i < 0 {
		return Member{}, false
	}
	return b.Members[i], true
}

// TeamSettings are what the user sets on a team. An empty LeadID means the
// first member; a zero GoalBudget means DefaultGoalBudget; zero RequestSlots
// means 1.
type TeamSettings struct {
	LeadID        string
	Agreement     string
	GoalBudget    int64
	DefaultModel  string
	DefaultEffort string
	RequestSlots  int64
	StartHandoffs string // "" means StartAuto
}

// TeamPatch changes the settings of a team. A nil field keeps its value.
type TeamPatch struct {
	LeadID        *string
	Agreement     *string
	GoalBudget    *int64
	DefaultModel  *string
	DefaultEffort *string
	RequestSlots  *int64
	StartHandoffs *string
}

// TeamWorkspace is the shared folder of a team. Every turn in the team runs there.
func (s *Service) TeamWorkspace(teamID string) string {
	return filepath.Join(s.dataDir, "teams", teamID, "workspace")
}

// CreateTeam creates a team: a room of the given bots with a lead, a working
// agreement and a shared folder.
func (s *Service) CreateTeam(ctx context.Context, name, purpose string, memberIDs []string, set TeamSettings) (Bot, error) {
	id := uuid.NewString()
	if err := s.prepareWorkspace(s.TeamWorkspace(id)); err != nil {
		return Bot{}, err
	}
	team, err := s.createRoom(ctx, id, name, purpose, memberIDs, &set)
	if err != nil {
		return Bot{}, errors.Join(err, os.RemoveAll(filepath.Join(s.dataDir, "teams", id)))
	}
	return team, nil
}

// UpdateTeam changes the lead, the working agreement or the goal budget of a team.
func (s *Service) UpdateTeam(ctx context.Context, id string, p TeamPatch) (Bot, error) {
	team, err := s.GetBot(ctx, id)
	if err != nil {
		return Bot{}, err
	}
	if !team.IsTeam() {
		return Bot{}, invalid("%s is not a team", team.Name)
	}
	if p.RequestSlots != nil && *p.RequestSlots == 0 {
		return Bot{}, invalid("Requests in progress at once must be from 1 to %d.", len(team.Members))
	}
	updated, err := validTeam(p.apply(*team.Team), team.Members)
	if err != nil {
		return Bot{}, err
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := q.UpdateTeam(ctx, db.UpdateTeamParams{
			LeadBotID: updated.LeadID, Agreement: updated.Agreement, GoalBudget: updated.GoalBudget,
			DefaultModel: updated.DefaultModel, DefaultEffort: updated.DefaultEffort, RequestSlots: updated.RequestSlots, StartHandoffs: updated.StartHandoffs, TeamID: id,
		}); err != nil {
			return fmt.Errorf("update team %s: %w", id, err)
		}
		return s.grantSlots(ctx, q, id)
	})
	if err != nil {
		return Bot{}, err
	}
	s.signalTurns()
	newLead := updated.LeadID != team.Team.LeadID
	team.Team = updated
	if newLead {
		if _, err := s.AppendEntry(ctx, id, KindEvent, "", map[string]string{"type": "members", "text": membersText(team)}); err != nil {
			return Bot{}, err
		}
	}
	s.hub.Publish(Event{Type: "bots"})
	return team, nil
}

// apply returns the settings of team t with the patch's fields.
func (p TeamPatch) apply(t Team) TeamSettings {
	set := TeamSettings(t)
	setIf(&set.LeadID, p.LeadID)
	setIf(&set.Agreement, p.Agreement)
	setIf(&set.GoalBudget, p.GoalBudget)
	setIf(&set.DefaultModel, p.DefaultModel)
	setIf(&set.DefaultEffort, p.DefaultEffort)
	setIf(&set.RequestSlots, p.RequestSlots)
	setIf(&set.StartHandoffs, p.StartHandoffs)
	return set
}

// setIf sets *dst to *v unless v is nil.
func setIf[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

// validTeam checks team settings against the members and fills in the defaults.
func validTeam(set TeamSettings, members []Member) (*Team, error) {
	agreement := strings.TrimSpace(set.Agreement)
	if utf8.RuneCountInString(agreement) > maxAgreementLen {
		return nil, invalid("the working agreement is longer than %d characters", maxAgreementLen)
	}
	budget := set.GoalBudget
	if budget == 0 {
		budget = DefaultGoalBudget
	}
	if budget < 1 || budget > maxGoalBudget {
		return nil, invalid("the goal budget must be 1 to %d turns", maxGoalBudget)
	}
	lead := set.LeadID
	if lead == "" && len(members) > 0 {
		lead = members[0].ID
	}
	if !slices.ContainsFunc(members, func(m Member) bool { return m.ID == lead }) {
		return nil, invalid("the lead must be a member of the team")
	}
	model, effort, err := validModelEffort(set.DefaultModel, set.DefaultEffort)
	if err != nil {
		return nil, err
	}
	slots := set.RequestSlots
	if slots == 0 {
		slots = 1
	}
	if slots < 1 || slots > int64(len(members)) {
		return nil, invalid("Requests in progress at once must be from 1 to %d.", len(members))
	}
	start, err := validStartHandoffs(set.StartHandoffs)
	if err != nil {
		return nil, err
	}
	return &Team{LeadID: lead, Agreement: agreement, GoalBudget: budget, DefaultModel: model, DefaultEffort: effort, RequestSlots: slots, StartHandoffs: start}, nil
}

// validStartHandoffs checks what accepted handoffs do; "" means StartAuto.
func validStartHandoffs(start string) (string, error) {
	if start == "" {
		return StartAuto, nil
	}
	if start != StartAuto && start != StartAsk {
		return "", invalid("Accepted handoffs must start %q or wait for the user (%q).", StartAuto, StartAsk)
	}
	return start, nil
}

// teamOf returns the team settings of a room, or nil when the room is no team.
func (s *Service) teamOf(ctx context.Context, roomID string, members []Member) (*Team, error) {
	row, err := s.q.GetTeam(ctx, roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get team %s: %w", roomID, err)
	}
	return toTeam(row, members), nil
}

func toTeam(row db.Team, members []Member) *Team {
	lead := row.LeadBotID
	if !slices.ContainsFunc(members, func(m Member) bool { return m.ID == lead }) {
		lead = ""
		if len(members) > 0 {
			lead = members[0].ID
		}
	}
	return &Team{
		LeadID: lead, Agreement: row.Agreement, GoalBudget: row.GoalBudget, DefaultModel: row.DefaultModel, DefaultEffort: row.DefaultEffort,
		RequestSlots: clampSlots(row.RequestSlots, len(members)), StartHandoffs: row.StartHandoffs,
	}
}

// clampSlots keeps a team's request slots within its number of members.
func clampSlots(slots int64, members int) int64 {
	return max(1, min(slots, int64(members)))
}

// TurnTeam returns roomID when it is a team, and "" when it is a plain room
// or empty: the team that a turn in roomID works for.
func (s *Service) TurnTeam(ctx context.Context, roomID string) (string, error) {
	if roomID == "" {
		return "", nil
	}
	_, err := s.q.GetTeam(ctx, roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get team %s: %w", roomID, err)
	}
	return roomID, nil
}

// TurnWorkspace is the folder where a bot works in a turn: the team's shared
// folder in a team, else the bot's own workspace.
func (s *Service) TurnWorkspace(ctx context.Context, botID, roomID string) (string, error) {
	teamID, err := s.TurnTeam(ctx, roomID)
	if err != nil {
		return "", err
	}
	if teamID != "" {
		return s.TeamWorkspace(teamID), nil
	}
	return s.Workspace(botID), nil
}

// CheckTeamTurnTarget rejects a message from a turn in team roomID to anyone
// but the team itself, so that team work stays in the team channel. It
// allows every target outside a team.
func (s *Service) CheckTeamTurnTarget(ctx context.Context, roomID, targetID string) error {
	if roomID == targetID {
		return nil
	}
	teamID, err := s.TurnTeam(ctx, roomID)
	if err != nil || teamID == "" {
		return err
	}
	return invalid("in a turn in your team, talk to the team in its channel: post with send_to_user and write @Name to wake a member; a lead asks another team for work with update_board request")
}

// membersText is the event text that lists a room's members and a team's lead.
func membersText(room Bot) string {
	text := "Members: " + memberNames(room.Members)
	if lead, ok := room.Lead(); ok {
		text += ". Lead: " + lead.Name
	}
	return text
}
