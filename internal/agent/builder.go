package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"handful/internal/core"
)

// setupInput is the input of create_setup and the content of its approval
// card, which the UI shows.
type setupInput struct {
	Bots  []setupBotInput  `json:"bots,omitempty" jsonschema:"the new bots"`
	Teams []setupTeamInput `json:"teams,omitempty" jsonschema:"the new teams"`
	Rooms []setupRoomInput `json:"rooms,omitempty" jsonschema:"the new rooms"`
}

type setupBotInput struct {
	Name         string `json:"name" jsonschema:"a short name that no bot, team or room of the user has"`
	Instructions string `json:"instructions" jsonschema:"the bot's standing instructions: a first sentence that names its role, then what it does and how"`
	Model        string `json:"model,omitempty" jsonschema:"the bot's model, only when the user asked for one: fable, opus, sonnet, haiku or a full model ID; empty inherits"`
	Effort       string `json:"effort,omitempty" jsonschema:"the bot's reasoning effort, only when the user asked for one: low, medium, high, xhigh or max; empty inherits"`
}

type setupTeamInput struct {
	Name      string   `json:"name" jsonschema:"a short name that no bot, team or room of the user has"`
	Purpose   string   `json:"purpose,omitempty" jsonschema:"what the team is for, with the facts every member needs, such as the folder of the code"`
	Members   []string `json:"members" jsonschema:"the names of 1 to 8 bots: new bots of this setup or bots the user has"`
	Lead      string   `json:"lead,omitempty" jsonschema:"the name of the member who leads; the first member when empty"`
	Agreement string   `json:"agreement,omitempty" jsonschema:"the working agreement: how the team works and when work is done"`
	// GoalBudget was a task's turn limit. A task has none now; the field stays so that a call that
	// still sends it is valid, and its value is dropped.
	GoalBudget int64 `json:"goal_budget,omitempty" jsonschema:"ignored: a task has no turn limit; leave it out"`
	// The defaults of members without their own model or effort, in this team.
	DefaultModel  string `json:"default_model,omitempty" jsonschema:"the model of members without their own, in this team, only when the user asked for one; empty inherits"`
	DefaultEffort string `json:"default_effort,omitempty" jsonschema:"the effort of members without their own, in this team, only when the user asked for one; empty inherits"`
}

type setupRoomInput struct {
	Name    string   `json:"name" jsonschema:"a short name that no bot, team or room of the user has"`
	Purpose string   `json:"purpose,omitempty" jsonschema:"what the room is for"`
	Members []string `json:"members" jsonschema:"the names of 1 to 8 bots: new bots of this setup or bots the user has"`
}

// inputOf is the input form of a checked setup, which the card shows.
func inputOf(s core.Setup) setupInput {
	var in setupInput
	for _, b := range s.Bots {
		in.Bots = append(in.Bots, setupBotInput{Name: b.Name, Instructions: b.Instructions, Model: b.Model, Effort: b.Effort})
	}
	for _, t := range s.Teams {
		in.Teams = append(in.Teams, setupTeamInput{Name: t.Name, Purpose: t.Purpose, Members: t.Members, Lead: t.Lead, Agreement: t.Agreement, DefaultModel: t.DefaultModel, DefaultEffort: t.DefaultEffort})
	}
	for _, r := range s.Rooms {
		in.Rooms = append(in.Rooms, setupRoomInput{Name: r.Name, Purpose: r.Purpose, Members: r.Members})
	}
	return in
}

func (in setupInput) setup() core.Setup {
	var s core.Setup
	for _, b := range in.Bots {
		s.Bots = append(s.Bots, core.SetupBot{Name: b.Name, Instructions: b.Instructions, Model: b.Model, Effort: b.Effort})
	}
	for _, t := range in.Teams {
		s.Teams = append(s.Teams, core.SetupTeam{Name: t.Name, Purpose: t.Purpose, Members: t.Members, Lead: t.Lead, Agreement: t.Agreement, DefaultModel: t.DefaultModel, DefaultEffort: t.DefaultEffort})
	}
	for _, r := range in.Rooms {
		s.Rooms = append(s.Rooms, core.SetupRoom{Name: r.Name, Purpose: r.Purpose, Members: r.Members})
	}
	return s
}

const createSetupDescription = "Create new bots, teams and rooms for the user in one step. Call it only after the user agreed to the setup that you proposed in the chat. The user sees the whole setup on a card and creates or cancels it; nothing is created otherwise. Members and leads are bot names: bots of this setup or bots the user has."

// isBuilder reports whether the run of a tool server's token is a turn of
// Builder, the only bot with create_setup. When the bot cannot be read, the
// tool is not there.
func isBuilder(svc *core.Service, current func() (runContext, bool)) bool {
	rc, ok := current()
	if !ok {
		return false
	}
	bot, err := svc.GetBot(context.Background(), rc.BotID)
	return err == nil && bot.Role == core.RoleBuilder
}

// createSetup checks the setup, shows the user the checked setup on a card,
// and creates exactly that setup when the user confirms it.
func (t tools) createSetup(ctx context.Context, rc runContext, in setupInput) (*mcp.CallToolResult, any, error) {
	setup, err := t.svc.CheckSetup(ctx, in.setup())
	if err != nil {
		return nil, nil, err
	}
	card, err := json.Marshal(inputOf(setup))
	if err != nil {
		return nil, nil, fmt.Errorf("encode the setup: %w", err)
	}
	allowed, reason, err := t.svc.RequestApproval(ctx, core.ApprovalRequest{BotID: rc.BotID, BotName: rc.BotName, TranscriptID: rc.transcript(), TurnID: rc.TurnID, Tool: core.ToolCreateSetup, Input: card})
	if err != nil {
		return nil, nil, err
	}
	if !allowed {
		return textResult("Created nothing: %s Do not create the setup another way.", reason), nil, nil
	}
	created, err := t.svc.CreateSetup(ctx, setup)
	if err != nil {
		return nil, nil, err
	}
	return textResult("%s", createdText(created)), nil, nil
}

// createdText lists what a setup created, for Builder's next message.
func createdText(created []core.Bot) string {
	var b strings.Builder
	b.WriteString("Created:\n")
	newBots := false
	for _, bot := range created {
		b.WriteString("- " + createdLine(bot) + "\n")
		newBots = newBots || !bot.IsRoom()
	}
	if newBots {
		b.WriteString("The new bots have not started yet. Do not message them; tell the user where to start.")
	} else {
		b.WriteString("Tell the user where to start.")
	}
	return b.String()
}

func createdLine(bot core.Bot) string {
	if bot.IsTeam() {
		lead, _ := bot.Lead()
		return fmt.Sprintf("team %q (id %s), led by %s, with %s", bot.Name, bot.ID, lead.Name, memberList(bot.Members))
	}
	if bot.IsRoom() {
		return fmt.Sprintf("room %q (id %s), with %s", bot.Name, bot.ID, memberList(bot.Members))
	}
	return fmt.Sprintf("bot %q (id %s)", bot.Name, bot.ID)
}

func memberList(members []core.Member) string {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.Name
	}
	return strings.Join(names, ", ")
}

// builderSection is Builder's part of the system prompt. It quotes Builder's
// welcome, which is not in Builder's Claude session, and names the user's
// rooms and teams, whose names a setup must not use.
func builderSection(pc promptContext) string {
	if pc.Bot.Role != core.RoleBuilder {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, builderGuide, core.BuilderWelcome)
	if len(pc.Groups) > 0 {
		names := make([]string, len(pc.Groups))
		for i, g := range pc.Groups {
			names[i] = fmt.Sprintf("%q", g.Name)
		}
		fmt.Fprintf(&b, "Names that the user's rooms and teams use: %s.\n", strings.Join(names, ", "))
	}
	return b.String()
}

const builderGuide = `
## Setting up the user's bots
You are Builder: you set up the user's bots, teams and rooms in Handful and explain how to use them.
Your chat opened with this message from you. It is not in your conversation history; the user's first message usually answers it, so do not greet the user again:
<welcome>
%s
</welcome>

What Handful offers:
- A bot is a persistent agent with its own chat, memory, routines (prompts that run on a schedule) and folder. It fits one ongoing need: an assistant, a specialist.
- A room is a group chat of the user and some bots. The user's post wakes the bots it @mentions, or every member when it mentions none; a bot's post wakes only the bots it @mentions. It fits discussions and hand-offs that the user steers.
- A team is a room with a lead, a working agreement and a shared folder. The user gives it a goal; the lead splits the goal into items on the team's board, members do them, another member reviews an item when the agreement asks for it, and the lead delivers the result. A team has no turn limit until the user sets a daily budget in the team's settings. It fits work with several steps and a clear result.
- All of the user's bots share skills, artifacts and memory facts of scope "user". The user runs a skill with /<skill> <request> in a bot's chat or a team's chat, where the lead runs it, or the members that the message @mentions; a plain room does not run skills.
- A bot reaches mail, calendars or chat apps only through a connector that the user adds under Connectors; say so when a setup needs one.
- Instructions do not run on their own: a bot works when the user, a routine, a teammate or its team's board starts a turn. For scheduled work, tell the user to ask the new bot for it, for example "every weekday at 8:10, brief me"; the bot then saves its own routine.

Starter setups; adapt their names, instructions and size to the user:
- Personal assistant: one bot that plans the day, keeps reminders, researches and drafts. Start with one bot; add a specialist only when a job needs its own instructions or schedule.
- Software team: a team of a lead who plans and splits the work, a developer who writes code with tests, and a reviewer who checks each change with fresh eyes. Agreement: "Tests pass, and another member reviews each change." Ask where the code is and put the folder in the team's purpose.
- Research desk: a team of a lead who frames the questions, a researcher who gathers sources, and a fact-checker who checks each claim. Agreement: "Every claim has a source that another member checked."
- Writing studio: a team of an editor who leads, plans and reviews, and a writer who drafts. Agreement: "The editor reviews every draft before it is done."
- Anything else: compose it from bots, a room or a team.

How to work:
1. Learn what the user wants in at most three short questions, and skip a question whose answer does not change the setup.
2. Propose one concrete setup in the chat, in plain words: each new bot with its name and its role in one line, and each team or room with its members and lead. Keep it small: one bot when one is enough, teams of 2 to 5 bots. A setup has at most 8 new bots, 3 teams and 3 rooms, and a team or room has 1 to 8 members. Give each new bot, team and room a name that no bot, room or team of the user has, ignoring case. Reuse a bot from your Teammates list only when its instructions fit the role, because you cannot change them. Never add yourself to a team or room. Mention that every turn of a bot uses the user's Claude plan, and that a team has no turn limit until the user sets a daily budget in the team's settings.
3. When the user agrees, or already asked for an exact setup, call create_setup yourself, never through a subagent, with the whole setup. Start each new bot's instructions with one sentence that names its role, because its teammates see only that start; then say what it does and how. For a member of a team, leave hand-offs and reviews to the board and the working agreement; for a member of a room, say whom it @mentions next. Never put passwords, keys or other secrets in a setup.
4. The user sees the setup on a card and creates it or cancels it. Create bots, teams and rooms only with create_setup, never with commands or the Handful API. When the user cancels, ask in one short question what to change. When the card expires, say that nothing was created and offer to show it again. When create_setup reports a problem, fix a name that is in use and tell the user the new name; propose any other change in the chat first. Call create_setup again only after the user agrees again.
5. When the setup is created, tell the user where to start: which bot to write to, or which team to open and give a goal with /goal <what the team should do> in its chat. Do not message the new bots yourself.
When the user asks you for other work, answer a quick question yourself, and offer a bot or a team for ongoing work. You cannot change or delete the user's other bots, their teams or their rooms; the user does that in the app.
`
