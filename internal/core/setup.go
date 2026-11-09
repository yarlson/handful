package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// ToolCreateSetup is Builder's tool that creates a setup. The user confirms
// every setup: an "always" decision is refused for it.
const ToolCreateSetup = "create_setup"

// Limits of one setup.
const (
	maxSetupBots    = 8
	maxSetupTeams   = 3
	maxSetupRooms   = 3
	maxSetupMembers = 8
)

// Setup is what Builder proposes and the user confirms: new bots, teams and
// rooms. Teams and rooms name their members and their lead by bot name: a
// bot of the setup or a bot that exists.
type Setup struct {
	Bots  []SetupBot
	Teams []SetupTeam
	Rooms []SetupRoom
}

type SetupBot struct {
	Name         string
	Instructions string
	Model        string // "" inherits
	Effort       string // "" inherits
}

type SetupTeam struct {
	Name       string
	Purpose    string
	Members    []string
	Lead       string // the first member leads when it is empty
	Agreement  string
	GoalBudget int64 // DefaultGoalBudget when 0
	// DefaultModel and DefaultEffort are the members' defaults in the team.
	DefaultModel  string
	DefaultEffort string
}

type SetupRoom struct {
	Name    string
	Purpose string
	Members []string
}

// plannedSetup is a checked setup, with the ids of its new bots and rooms.
type plannedSetup struct {
	bots  []Bot
	rooms []plannedRoom // the teams, then the rooms
}

type plannedRoom struct {
	id      string
	name    string
	purpose string
	members []Member
	team    *Team // nil for a room that is no team
}

// CheckSetup returns the setup as CreateSetup would create it: trimmed, with
// the members as the bots' own names, without repeats, and with each team's
// lead and goal budget. It reports why the setup cannot be created.
func (s *Service) CheckSetup(ctx context.Context, setup Setup) (Setup, error) {
	plan, err := s.planSetup(ctx, setup)
	if err != nil {
		return Setup{}, err
	}
	return plan.setup(), nil
}

// CreateSetup checks a setup again and creates all of it in one transaction,
// or nothing: the bots, the teams and rooms, and the member list that opens
// each channel. It returns the new bots, then the new teams and rooms. The new
// bots start without a turn.
func (s *Service) CreateSetup(ctx context.Context, setup Setup) ([]Bot, error) {
	plan, err := s.planSetup(ctx, setup)
	if err != nil {
		return nil, err
	}
	dirs, err := s.prepareSetupWorkspaces(plan)
	if err != nil {
		return nil, errors.Join(err, removeDirs(dirs))
	}
	var created []Bot
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		created, err = insertSetup(ctx, q, plan, s.nowMs())
		return err
	})
	if err != nil {
		return nil, errors.Join(err, removeDirs(dirs))
	}
	s.hub.Publish(Event{Type: "bots"})
	return created, nil
}

func (p plannedSetup) setup() Setup {
	var s Setup
	for _, b := range p.bots {
		s.Bots = append(s.Bots, SetupBot{Name: b.Name, Instructions: b.Instructions, Model: b.Model, Effort: b.Effort})
	}
	for _, r := range p.rooms {
		names := make([]string, len(r.members))
		lead := ""
		for i, m := range r.members {
			names[i] = m.Name
			if r.team != nil && m.ID == r.team.LeadID {
				lead = m.Name
			}
		}
		if r.team == nil {
			s.Rooms = append(s.Rooms, SetupRoom{Name: r.name, Purpose: r.purpose, Members: names})
			continue
		}
		s.Teams = append(s.Teams, SetupTeam{Name: r.name, Purpose: r.purpose, Members: names, Lead: lead, Agreement: r.team.Agreement, GoalBudget: r.team.GoalBudget,
			DefaultModel: r.team.DefaultModel, DefaultEffort: r.team.DefaultEffort})
	}
	return s
}

func (s *Service) planSetup(ctx context.Context, setup Setup) (plannedSetup, error) {
	if err := checkSetupSize(setup); err != nil {
		return plannedSetup{}, err
	}
	existing, err := s.ListBots(ctx)
	if err != nil {
		return plannedSetup{}, err
	}
	names := newSetupNames(existing)
	var plan plannedSetup
	for _, b := range setup.Bots {
		bot, err := names.newBot(b)
		if err != nil {
			return plannedSetup{}, fmt.Errorf("bot %q: %w", b.Name, err)
		}
		plan.bots = append(plan.bots, bot)
	}
	for _, t := range setup.Teams {
		room, err := names.newTeam(t)
		if err != nil {
			return plannedSetup{}, fmt.Errorf("team %q: %w", t.Name, err)
		}
		plan.rooms = append(plan.rooms, room)
	}
	for _, r := range setup.Rooms {
		room, err := names.newRoom(r.Name, r.Purpose, r.Members)
		if err != nil {
			return plannedSetup{}, fmt.Errorf("room %q: %w", r.Name, err)
		}
		plan.rooms = append(plan.rooms, room)
	}
	return plan, nil
}

func checkSetupSize(setup Setup) error {
	if len(setup.Bots)+len(setup.Teams)+len(setup.Rooms) == 0 {
		return invalid("the setup is empty")
	}
	if len(setup.Bots) > maxSetupBots || len(setup.Teams) > maxSetupTeams || len(setup.Rooms) > maxSetupRooms {
		return invalid("a setup has at most %d new bots, %d teams and %d rooms", maxSetupBots, maxSetupTeams, maxSetupRooms)
	}
	return nil
}

// setupNames resolves the names of a setup. A new name must differ, ignoring
// case, from every bot, team and room that exists or that the setup adds.
type setupNames struct {
	taken    map[string]bool
	bots     map[string][]Member // the agent bots that can be members, by folded name
	groups   map[string]bool     // the folded names of rooms and teams
	builders map[string]bool     // the folded names of Builder, which joins no room
}

func newSetupNames(existing []Bot) setupNames {
	n := setupNames{taken: map[string]bool{}, bots: map[string][]Member{}, groups: map[string]bool{}, builders: map[string]bool{}}
	for _, b := range existing {
		key := strings.ToLower(b.Name)
		n.taken[key] = true
		if b.IsRoom() {
			n.groups[key] = true
			continue
		}
		if b.Role == RoleBuilder {
			n.builders[key] = true
			continue
		}
		n.bots[key] = append(n.bots[key], Member{ID: b.ID, Name: b.Name, Model: b.Model, Effort: b.Effort})
	}
	return n
}

func (n setupNames) claim(name string) error {
	key := strings.ToLower(name)
	if n.taken[key] {
		return invalid("the name %q is in use; choose another", name)
	}
	n.taken[key] = true
	return nil
}

func (n setupNames) newBot(b SetupBot) (Bot, error) {
	if err := checkVisible(b.Name, b.Instructions); err != nil {
		return Bot{}, err
	}
	name, instructions, err := validateProfile(b.Name, b.Instructions)
	if err != nil {
		return Bot{}, err
	}
	model, effort, err := validModelEffort(b.Model, b.Effort)
	if err != nil {
		return Bot{}, err
	}
	if err := n.claim(name); err != nil {
		return Bot{}, err
	}
	bot := Bot{ID: uuid.NewString(), Kind: kindAgentBot, Name: name, Instructions: instructions, Model: model, Effort: effort}
	key := strings.ToLower(name)
	n.bots[key] = append(n.bots[key], Member{ID: bot.ID, Name: name, Model: model, Effort: effort})
	return bot, nil
}

func (n setupNames) newTeam(t SetupTeam) (plannedRoom, error) {
	if err := checkVisible(t.Lead, t.Agreement); err != nil {
		return plannedRoom{}, err
	}
	room, err := n.newRoom(t.Name, t.Purpose, t.Members)
	if err != nil {
		return plannedRoom{}, err
	}
	set := TeamSettings{Agreement: t.Agreement, GoalBudget: t.GoalBudget, DefaultModel: t.DefaultModel, DefaultEffort: t.DefaultEffort}
	if strings.TrimSpace(t.Lead) != "" {
		lead, err := n.member(t.Lead)
		if err != nil {
			return plannedRoom{}, err
		}
		set.LeadID = lead.ID
	}
	if room.team, err = validTeam(set, room.members); err != nil {
		return plannedRoom{}, err
	}
	return room, nil
}

func (n setupNames) newRoom(name, purpose string, memberNames []string) (plannedRoom, error) {
	if err := checkVisible(append([]string{name, purpose}, memberNames...)...); err != nil {
		return plannedRoom{}, err
	}
	name, purpose, err := validateProfile(name, purpose)
	if err != nil {
		return plannedRoom{}, err
	}
	if err := n.claim(name); err != nil {
		return plannedRoom{}, err
	}
	n.groups[strings.ToLower(name)] = true
	members, err := n.members(memberNames)
	if err != nil {
		return plannedRoom{}, err
	}
	return plannedRoom{id: uuid.NewString(), name: name, purpose: purpose, members: members}, nil
}

func (n setupNames) members(names []string) ([]Member, error) {
	if len(names) == 0 || len(names) > maxSetupMembers {
		return nil, invalid("name 1 to %d member bots", maxSetupMembers)
	}
	var members []Member
	seen := map[string]bool{}
	for _, name := range names {
		m, err := n.member(name)
		if err != nil {
			return nil, err
		}
		if !seen[m.ID] {
			seen[m.ID] = true
			members = append(members, m)
		}
	}
	return members, nil
}

// member finds the one agent bot with this name, ignoring case.
func (n setupNames) member(name string) (Member, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	found := n.bots[key]
	if len(found) > 1 {
		return Member{}, invalid("%d bots are named %q; ask the user to rename one first", len(found), name)
	}
	if len(found) == 1 {
		return found[0], nil
	}
	if n.groups[key] {
		return Member{}, invalid("%q is a room or a team, not a bot", name)
	}
	if n.builders[key] {
		return Member{}, invalid("%q sets up bots and joins no team or room", name)
	}
	return Member{}, invalid("no bot is named %q", name)
}

// blankLines matches two blank lines in a row.
var blankLines = regexp.MustCompile(`\n[ \t]*\n[ \t]*\n`)

// checkVisible reports text that the setup card would not show the user: a
// control or format character other than a newline or a tab, such as a
// zero-width space or a direction mark, or two blank lines in a row. Such text
// could hide instructions that the user confirms without seeing them.
func checkVisible(texts ...string) error {
	for _, text := range texts {
		if blankLines.MatchString(text) {
			return invalid("a text has two blank lines in a row; the card would not show what follows clearly")
		}
		for _, r := range text {
			if r != '\n' && r != '\t' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
				return invalid("a text has the invisible character %U; write it without", r)
			}
		}
	}
	return nil
}

// prepareSetupWorkspaces creates the folders of the new bots and teams. It
// returns the folders it created, also when it fails.
func (s *Service) prepareSetupWorkspaces(plan plannedSetup) ([]string, error) {
	var dirs []string
	for _, b := range plan.bots {
		dirs = append(dirs, filepath.Join(s.dataDir, "bots", b.ID))
		if err := s.prepareWorkspace(s.Workspace(b.ID)); err != nil {
			return dirs, err
		}
	}
	for _, r := range plan.rooms {
		if r.team == nil {
			continue
		}
		dirs = append(dirs, filepath.Join(s.dataDir, "teams", r.id))
		if err := s.prepareWorkspace(s.TeamWorkspace(r.id)); err != nil {
			return dirs, err
		}
	}
	return dirs, nil
}

func removeDirs(dirs []string) error {
	var errs []error
	for _, dir := range dirs {
		errs = append(errs, os.RemoveAll(dir))
	}
	return errors.Join(errs...)
}

func insertSetup(ctx context.Context, q *db.Queries, plan plannedSetup, now int64) ([]Bot, error) {
	var created []Bot
	for _, b := range plan.bots {
		row, err := q.CreateBot(ctx, db.CreateBotParams{ID: b.ID, Name: b.Name, Instructions: b.Instructions, Kind: kindAgentBot, Model: b.Model, Effort: b.Effort, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return nil, fmt.Errorf("create bot %q: %w", b.Name, err)
		}
		created = append(created, toBot(row))
	}
	for _, r := range plan.rooms {
		room, err := insertRoom(ctx, q, r.id, r.name, r.purpose, r.members, r.team, now)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(map[string]string{"type": "members", "text": membersText(room)})
		if err != nil {
			return nil, fmt.Errorf("encode the members of %q: %w", room.Name, err)
		}
		if _, err := q.AppendEntry(ctx, db.AppendEntryParams{BotID: room.ID, ID: uuid.NewString(), Kind: KindEvent, Body: string(body), CreatedAt: now}); err != nil {
			return nil, fmt.Errorf("list the members of %q: %w", room.Name, err)
		}
		created = append(created, room)
	}
	return created, nil
}
