package agent

import (
	"context"
	"encoding/json"
	"maps"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"handful/internal/core"
)

func TestTeamPromptsCarryTheGatekeeperAndMemberTexts(t *testing.T) {
	t.Parallel()
	members := []core.Member{{ID: "ana", Name: "Ana"}, {ID: "bo", Name: "Bo"}}
	team := &core.Bot{ID: "team", Name: "Ship", Members: members, Team: &core.Team{LeadID: "ana"}}
	for _, global := range []string{"", "Be brief."} {
		lead := systemPrompt(promptContext{Bot: core.Bot{ID: "ana", Name: "Ana"}, Team: team, Global: global})
		member := systemPrompt(promptContext{Bot: core.Bot{ID: "bo", Name: "Bo"}, Team: team, Global: global})
		solo := systemPrompt(promptContext{Bot: core.Bot{ID: "ana", Name: "Ana"}, Global: global})
		if !strings.Contains(lead, "- "+leadGatekeeper+"\n") || strings.Contains(member, leadGatekeeper) || strings.Contains(solo, leadGatekeeper) {
			t.Errorf("global %q: the gatekeeper text must be in the lead's prompt only", global)
		}
		if !strings.Contains(member, "- "+memberWork+"\n") || strings.Contains(lead, memberWork) || strings.Contains(solo, memberWork) {
			t.Errorf("global %q: the member text must be in a member's prompt only", global)
		}
		for _, old := range []string{"update_board hand_over", "update_board request", "start a goal with update_board start_goal"} {
			if strings.Contains(lead, old) || strings.Contains(member, old) {
				t.Errorf("global %q: a team prompt still points to %q", global, old)
			}
		}
	}
	// AC22.
	if !strings.Contains(leadGatekeeper, "Never ask the user to relay work to another team") {
		t.Error("the gatekeeper text lacks the relay rule")
	}
	if !strings.HasPrefix(leadGatekeeper, "You are the gatekeeper of this team's work. Every task from outside your team, the user's too, arrives in your inbox.") ||
		!strings.HasSuffix(memberWork, "Don't start other work on your own.") {
		t.Error("the §8 texts changed")
	}
	if !strings.Contains(leadBrief, "send that team a task with send_task and name the step in waits_for") || !strings.Contains(leadBrief, "call close with goal_id") {
		t.Errorf("lead brief = %q", leadBrief)
	}
}

func TestTheBoardShowsThePlanTheInboxDoneAndSentTasks(t *testing.T) {
	t.Parallel()
	dev := core.Bot{ID: "dev", Name: "Dev", Members: []core.Member{{ID: "dl", Name: "Dee"}}, Team: &core.Team{LeadID: "dl"}}
	task := func(id, text, state string, h core.Handoff) core.Goal {
		return core.Goal{ID: id, Text: text, TeamID: "dev", State: state, Handoff: h}
	}
	board := core.Board{
		Plan: []core.Goal{{ID: "p1", Text: "Fix login.", QueuePos: 1, Pinned: true}, {ID: "p2", Text: "Tidy.", QueuePos: 2}},
		Inbox: []core.Goal{
			task("g1", "Fix the scroll bug.", core.GoalIncoming, core.Handoff{FromTeamID: "sup", Brief: "See artifact.", ArtifactIDs: []string{"a1"}, WaitsOnItemID: "s1"}),
			task("g2", "Build the queue.", core.GoalIncoming, core.Handoff{FromTeamID: "prod", AskedAt: 5, AskRounds: 2, IntakeNote: "Which spec rev?"}),
			task("g3", "Tidy docs.", core.GoalIncoming, core.Handoff{AskRounds: 3, NeedsUser: true}),
			task("g4", "Refactor.", core.GoalLater, core.Handoff{Origin: core.OriginProposal, IntakeNote: "After the release"}),
		},
		Done:  []core.Goal{task("d1", "Ship it.", core.GoalDelivered, core.Handoff{FromTeamID: "sup"})},
		Sent:  []core.Goal{{ID: "s9", Text: "Write the guide.", TeamID: "docs", State: core.GoalQueued}},
		Teams: map[string]string{"sup": "Support", "prod": "Product", "docs": "Docs"},
	}
	got := boardPrompt(core.Bot{ID: "dl"}, dev, board, core.Turn{}, nil, time.Time{})
	for _, want := range []string{
		"Plan, next tasks in order (don't plan them until they are Now):\n- #1 \"Fix login.\" (task id p1), pinned by the user\n- #2 \"Tidy.\" (task id p2)\n",
		"Inbox, waiting for your decision.",
		`- "Fix the scroll bug." (task id g1), from team "Support" — decide by 00:00 — their work waits on this` + "\n  Brief: See artifact.\n  Artifacts: a1\n",
		`- "Build the queue." (task id g2), from team "Product" — you asked (2 of 3): Which spec rev?; wait for the answer; clock paused: waiting for the sender's answer` + "\n",
		`- "Tidy docs." (task id g3), from the user — the leads couldn't settle it; only the user decides now` + "\n",
		`- "Refactor." (task id g4), from a proposal of your team — deferred: After the release; it comes back when due` + "\n",
		"Done, waiting for approval by whoever asked:\n" + `- "Ship it." (task id d1), from team "Support"` + "\n",
		"Sent to other teams:\n" + `- "Write the guide." (task id s9) to team "Docs" [queued]` + "\n",
		inboxBrief,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("board lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "Brief: ") != 1 || strings.Count(got, "Artifacts: ") != 1 {
		t.Errorf("only the task with a brief and artifacts shows those lines:\n%s", got)
	}
	if member := boardPrompt(core.Bot{ID: "other"}, dev, board, core.Turn{}, nil, time.Time{}); strings.Contains(member, inboxBrief) {
		t.Error("a member's board has the lead's inbox brief")
	}
	if sender := boardPrompt(core.Bot{ID: "dl"}, dev, core.Board{Sent: board.Sent, Teams: board.Teams}, core.Turn{}, nil, time.Time{}); !strings.Contains(sender, "Sent to other teams:") || strings.Contains(sender, inboxBrief) {
		t.Errorf("sender-only board = %q", sender)
	}
	if done := boardPrompt(core.Bot{ID: "dl"}, dev, core.Board{Done: board.Done, Teams: board.Teams}, core.Turn{}, nil, time.Time{}); !strings.Contains(done, "Done, waiting for approval") {
		t.Errorf("done-only board = %q", done)
	}
}

// The lead's board says by when to decide on each inbox task, in the user's
// time zone, or why its clock stands, or that the time ran out.
func TestTheLeadsBoardShowsEachInboxTasksDecisionClock(t *testing.T) {
	t.Parallel()
	dev := core.Bot{ID: "dev", Name: "Dev", Members: []core.Member{{ID: "dl", Name: "Dee"}}, Team: &core.Team{LeadID: "dl", DecisionMinutes: 30}}
	eet := time.FixedZone("EET", 2*60*60)
	afternoon := time.Date(2026, 11, 10, 14, 0, 0, 0, eet)
	inbox := func(h core.Handoff, decisionMs int64) core.Board {
		h.DecisionMs = decisionMs
		return core.Board{Inbox: []core.Goal{{ID: "g1", Text: "Fix it.", TeamID: "dev", State: core.GoalIncoming, Handoff: h}}}
	}
	running := core.Goal{ID: "now", Text: "Ship.", State: core.GoalActive, Used: 3, Budget: 10}
	stopped := core.Goal{ID: "now", Text: "Ship.", State: core.GoalPaused, Used: 3, Budget: 10}
	pastBudget := core.Goal{ID: "now", Text: "Ship.", State: core.GoalPaused, Used: 12, Budget: 10}
	for _, c := range []struct {
		name  string
		board core.Board
		now   time.Time
		want  string
	}{
		{"time left", inbox(core.Handoff{}, (10 * time.Minute).Milliseconds()), afternoon, " — decide by 14:20\n"},
		{"time used up while the lead runs a turn", inbox(core.Handoff{}, (40 * time.Minute).Milliseconds()), afternoon, " — decide by 14:00\n"},
		{"after midnight", inbox(core.Handoff{}, 0), time.Date(2026, 11, 10, 23, 50, 0, 0, eet), " — decide by Wed 00:20\n"},
		{"the user stopped the team", func() core.Board { b := inbox(core.Handoff{}, 0); b.Goal = &stopped; return b }(), afternoon, " — clock paused: the user stopped the team's task\n"},
		{"the Now task runs", func() core.Board { b := inbox(core.Handoff{}, 0); b.Goal = &running; return b }(), afternoon, " — decide by 14:30\n"},
		{"a stopped task past its turns so far", func() core.Board { b := inbox(core.Handoff{}, 0); b.Goal = &pastBudget; return b }(), afternoon, " — clock paused: the user stopped the team's task\n"},
		{"a question to the sender waits", inbox(core.Handoff{AskedAt: 5, AskRounds: 1, IntakeNote: "Which page?"}, 0), afternoon, " — you asked (1 of 3): Which page?; wait for the answer; clock paused: waiting for the sender's answer\n"},
		{"missed", inbox(core.Handoff{MissedAt: 9, NeedsUser: true}, (30 * time.Minute).Milliseconds()), afternoon, " — missed — waiting for the user\n"},
	} {
		got := boardTable(dev, c.board, c.now)
		if want := `- "Fix it." (task id g1), from the user` + c.want; !strings.Contains(got, want) {
			t.Errorf("%s: board lacks %q:\n%s", c.name, want, got)
		}
	}
}

// The board shows a task's turns so far with no limit, and the team's use of
// its daily budget today, the team's only turn limit.
func TestTheBoardShowsTheTeamsDailyBudgetAndATasksTurnsSoFar(t *testing.T) {
	t.Parallel()
	dev := core.Bot{ID: "dev", Name: "Dev", Members: []core.Member{{ID: "dl", Name: "Dee"}}, Team: &core.Team{LeadID: "dl"}}
	task := &core.Goal{ID: "g1", Text: "Ship.", State: core.GoalActive, Used: 42, Budget: 30}
	for _, c := range []struct {
		name   string
		budget core.BudgetView
		want   string
	}{
		{"no limit", core.BudgetView{Today: 7}, "Team's turns today: 7 (no daily limit).\n"},
		{"under the limit", core.BudgetView{Today: 12, Limit: 100}, "Team's daily budget: 12 of 100 turns used today.\n"},
		{"near the limit", core.BudgetView{Today: 85, Limit: 100, Warn: true}, "Team's daily budget: 85 of 100 turns used today; keep the rest for the work that matters most.\n"},
		{"used up", core.BudgetView{Today: 100, Limit: 100, Paused: true}, "Team's daily budget: 100 of 100 turns used today; it is used up, so only the lead's decisions start until midnight or until the user raises it.\n"},
	} {
		got := boardTable(dev, core.Board{Goal: task, Budget: c.budget}, time.Time{})
		if want := "<board>\n" + c.want + "Goal (id g1, active, 42 turns so far): Ship.\n"; !strings.HasPrefix(got, want) {
			t.Errorf("%s: board = %q; want it to start with %q", c.name, got, want)
		}
		if strings.Contains(got, "of 30") {
			t.Errorf("%s: board = %q; want no limit on the task's turns", c.name, got)
		}
	}
}

// The lead's board lists the team's turns in the last hour that were on no
// task, with the ids make_task takes, so the lead can make tasks of that
// work; a member's board and a team with none say nothing of it.
func TestTheLeadsBoardListsTheTeamsTurnsOnNoTask(t *testing.T) {
	t.Parallel()
	dev := core.Bot{ID: "dev", Name: "Dev", Members: []core.Member{{ID: "dl", Name: "Dee"}, {ID: "bo", Name: "Bo"}}, Team: &core.Team{LeadID: "dl", DecisionMinutes: 30}}
	two := core.OffTask{Count: 2, Turns: []core.OffTaskTurn{{TurnID: "t1", BotName: "Bo", FirstLine: "Fixed the flaky test."}, {TurnID: "t2", BotName: "Dee", FirstLine: "Asked Bo about it."}}}
	one := core.OffTask{Count: 1, Turns: []core.OffTaskTurn{{TurnID: "t1", BotName: "Bo", FirstLine: "Fixed the flaky test."}}}
	running := &core.Goal{ID: "now", Text: "Ship.", State: core.GoalActive, Budget: 10}
	const pointer = "Turn work that takes more than one turn into a task: update_board make_task with the task in a line (text), a brief and the turn_ids of that work takes it into your plan and attaches the turns.\n"

	if got := boardPrompt(core.Bot{ID: "dl"}, dev, core.Board{Goal: running, OffTask: two}, core.Turn{}, nil, time.Time{}); !strings.HasSuffix(got, "\n2 turns of your team in the last hour were on no task:\n- turn t1, Bo: Fixed the flaky test.\n- turn t2, Dee: Asked Bo about it.\n"+pointer) {
		t.Errorf("lead's board with 2 turns on no task = %q; want the count, each turn with its id and the pointer at the end", got)
	}
	if got := boardPrompt(core.Bot{ID: "dl"}, dev, core.Board{OffTask: one}, core.Turn{}, nil, time.Time{}); !strings.Contains(got, "<board>") || !strings.HasSuffix(got, "\n1 turn of your team in the last hour was on no task:\n- turn t1, Bo: Fixed the flaky test.\n"+pointer) {
		t.Errorf("lead's board with no task and 1 turn on no task = %q; want the board and the list", got)
	}
	if got := boardPrompt(core.Bot{ID: "dl"}, dev, core.Board{Goal: running}, core.Turn{}, nil, time.Time{}); strings.Contains(got, "on no task") {
		t.Errorf("lead's board with no turn on no task = %q; want no list", got)
	}
	if got := boardPrompt(core.Bot{ID: "dl"}, dev, core.Board{}, core.Turn{}, nil, time.Time{}); got != "" {
		t.Errorf("lead's empty board = %q; want none", got)
	}
	if got := boardPrompt(core.Bot{ID: "bo"}, dev, core.Board{Goal: running, OffTask: two}, core.Turn{}, nil, time.Time{}); strings.Contains(got, "on no task") {
		t.Errorf("member's board = %q; want no off-task list", got)
	}
	if got := boardPrompt(core.Bot{ID: "bo"}, dev, core.Board{OffTask: two}, core.Turn{}, nil, time.Time{}); got != "" {
		t.Errorf("member's board with only turns on no task = %q; want none", got)
	}
}

// The lead makes a task of the team's turns on no task with make_task: the
// task is taken into the plan with the turns attached, and the board lists
// none of them any more. A member is refused.
func TestTheLeadMakesATaskOfTurnsOnNoTask(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	ship, ana, bo, _ := h.newTeam(t)
	h.newGoal(t, ship, boardItem("Publish", "Publish the guide.", bo.ID, ""))
	finishQueuedTurns(t, h)
	started := time.Now().Add(-5 * time.Minute).UnixMilli()
	for _, id := range []string{"chat-1", "chat-2"} {
		if _, err := h.conn.ExecContext(ctx, `INSERT INTO turns (id, bot_id, source, prompt, status, room_id, hops, created_at, started_at) VALUES (?, ?, 'room', 'Bo mentioned you.', 'done', ?, 1, ?, ?)`, id, bo.ID, ship.ID, started, started); err != nil {
			t.Fatal(err)
		}
	}
	offTask := func() int64 {
		t.Helper()
		board, err := h.svc.Board(ctx, ship.ID)
		if err != nil {
			t.Fatal(err)
		}
		return board.OffTask.Count
	}
	if n := offTask(); n != 2 {
		t.Fatalf("turns on no task before = %d; want 2", n)
	}
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	token, err := h.runner.tokens.issue(runContext{BotID: bo.ID, BotName: "Bo", TurnID: "t-bo", RoomID: ship.ID})
	if err != nil {
		t.Fatal(err)
	}
	member, err := mcpSession(t, srv.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { member.Close() })
	args := map[string]any{"action": "make_task", "text": "Fix the flaky test.", "brief": "Bo started it in chat.", "turn_ids": []string{"chat-1", "chat-2"}}
	if got, isErr := callText(t, member, "update_board", args); !isErr || !strings.Contains(got, "only the lead of Ship, or the user, makes a task from turns") {
		t.Fatalf("a member's make_task = %q (error %v); want it refused", got, isErr)
	}
	lead := boardSession(t, h, srv, ana, ship, false)
	got, isErr := callText(t, lead, "update_board", args)
	if isErr || !strings.HasPrefix(got, `Made "Fix the flaky test." (task id `) || !strings.HasSuffix(got, ` of those turns, as #1 in your plan; it starts when the tasks before it are closed. Don't plan it now.`) {
		t.Fatalf("the lead's make_task = %q (error %v); want the task taken as #1 in the plan", got, isErr)
	}
	if n := offTask(); n != 0 {
		t.Errorf("turns on no task after make_task = %d; want 0", n)
	}
	board, err := h.svc.Board(ctx, ship.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Plan) != 1 || board.Plan[0].Text != "Fix the flaky test." {
		t.Fatalf("plan = %+v; want the made task", board.Plan)
	}
	var attached int
	if err := h.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns WHERE goal_id = ? AND id IN ('chat-1', 'chat-2')`, board.Plan[0].ID).Scan(&attached); err != nil || attached != 2 {
		t.Fatalf("turns attached to the made task = %d, %v; want 2", attached, err)
	}

	// A team with no Now task starts the made task at once.
	cy := h.newNamedBot(t, "Cy")
	docs, err := h.svc.CreateTeam(ctx, "Docs", "Write docs.", []string{cy.ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.conn.ExecContext(ctx, `INSERT INTO turns (id, bot_id, source, prompt, status, room_id, hops, created_at, started_at) VALUES ('docs-chat', ?, 'room', 'Cy mentioned you.', 'done', ?, 1, ?, ?)`, cy.ID, docs.ID, started, started); err != nil {
		t.Fatal(err)
	}
	docsLead := boardSession(t, h, srv, cy, docs, false)
	got, isErr = callText(t, docsLead, "update_board", map[string]any{"action": "make_task", "text": "Write the guide.", "turn_ids": []string{"docs-chat"}})
	if isErr || !strings.HasSuffix(got, ` of those turns; it is your Now task: plan it into steps with create.`) {
		t.Fatalf("make_task with nothing Now = %q (error %v); want it started", got, isErr)
	}
}

// A lead's decision on an inbox task shows the team's Now task too; another
// goal's turn still doesn't.
func TestADecisionTurnSeesTheNowTask(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, _, _, _ := h.newTeam(t)
	h.newGoal(t, team)
	now, err := h.svc.Board(ctx, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		turn core.Turn
		want bool
	}{
		{core.Turn{GoalID: "inbox-task", BoardRole: core.RoleLead, Decision: true}, true},
		{core.Turn{GoalID: "other-goal", BoardRole: core.RoleLead}, false},
		{core.Turn{GoalID: now.Goal.ID, BoardRole: core.RoleLead}, true},
	} {
		board, err := h.runner.teamBoard(ctx, team, c.turn)
		if err != nil {
			t.Fatal(err)
		}
		if got := board.Goal != nil; got != c.want {
			t.Errorf("turn %+v sees the Now task = %v; want %v", c.turn, got, c.want)
		}
	}
}

// Every task action of update_board reaches its core call: two leads send,
// ask, answer, take, defer, pass on, push back, withdraw, propose, order,
// close, approve and stop tasks through MCP.
func TestEveryTaskActionWorksThroughUpdateBoard(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	ship, ana, bo, cy := h.newTeam(t)
	dee := h.newNamedBot(t, "Dee")
	docs, err := h.svc.CreateTeam(ctx, "Docs", "Write docs.", []string{cy.ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	ops, err := h.svc.CreateTeam(ctx, "Ops", "Run things.", []string{dee.ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	items := h.newGoal(t, ship, boardItem("Publish", "Publish the guide.", bo.ID, ""))
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	shipLead := boardSession(t, h, srv, ana, ship, false)
	docsLead := boardSession(t, h, srv, cy, docs, false)
	call := func(name string, args map[string]any) string {
		t.Helper()
		cs := shipLead
		if name == "docs" {
			cs = docsLead
		}
		got, isErr := callText(t, cs, "update_board", args)
		if isErr {
			t.Fatalf("%s %v = error %q", name, args["action"], got)
		}
		return got
	}
	state := func(id string) string {
		t.Helper()
		g, err := h.svc.GoalBoard(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return g.Goal.State
	}
	wakeOfAna := func(want string) {
		t.Helper()
		queued, err := h.svc.QueuedTurns(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range queued {
			if q.BotID == ana.ID && q.Prompt == want {
				return
			}
		}
		t.Errorf("no wake of Ship's lead with %q", want)
	}
	send := func(text string, extra map[string]any) string {
		t.Helper()
		args := map[string]any{"action": "send_task", "team_id": docs.ID, "text": text, "brief": "Context."}
		maps.Copy(args, extra)
		got := call("ship", args)
		if !strings.HasPrefix(got, "Sent \""+text+"\" (task id ") {
			t.Fatalf("send_task = %q", got)
		}
		id, _, _ := strings.Cut(strings.TrimPrefix(got, "Sent \""+text+"\" (task id "), ")")
		return id
	}

	guide := send("Write the guide.", map[string]any{"waits_for": items[0].ID})
	if got := call("docs", map[string]any{"action": "ask", "goal_id": guide, "text": "For which release?"}); got != `"Write the guide." is incoming now.` {
		t.Fatalf("ask = %q", got)
	}
	wakeOfAna("Team Docs asked about your task \"Write the guide.\": For which release? Answer a question with answer_task. If a task of yours waits on it, re-plan when it is pushed back.")
	call("ship", map[string]any{"action": "answer_task", "goal_id": guide, "text": "2.0."})
	if got := call("docs", map[string]any{"action": "take", "goal_id": guide}); got != `Took "Write the guide."; it is your Now task: plan it into steps with create.` {
		t.Fatalf("take = %q", got)
	}

	faq := send("Write the FAQ.", nil)
	until := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	if got := call("docs", map[string]any{"action": "later", "goal_id": faq, "text": "After 2.0.", "until": until}); !strings.HasPrefix(got, `"Write the FAQ." waits until `) || state(faq) != core.GoalLater {
		t.Fatalf("later = %q, state %s", got, state(faq))
	}
	if got, isErr := callText(t, docsLead, "update_board", map[string]any{"action": "later", "goal_id": faq, "until": "tomorrow"}); !isErr || !strings.Contains(got, "RFC 3339") {
		t.Fatalf("later with a bad time = %q (error %v)", got, isErr)
	}

	runbook := send("Write the runbook.", nil)
	if got, isErr := callText(t, docsLead, "update_board", map[string]any{"action": "pass_on", "goal_id": runbook, "team_id": "no-such-team", "text": "x"}); !isErr || strings.HasPrefix(got, "Passed") {
		t.Fatalf("pass_on to an unknown team = %q (error %v); want core's error", got, isErr)
	}
	if got := call("docs", map[string]any{"action": "pass_on", "goal_id": runbook, "team_id": ops.ID, "text": "Ops owns runbooks."}); got != `Passed "Write the runbook." on; it is in that team's inbox now.` {
		t.Fatalf("pass_on = %q", got)
	}
	if g, err := h.svc.GoalBoard(ctx, runbook); err != nil || g.Goal.TeamID != ops.ID {
		t.Fatalf("passed task = %+v, %v; want it at Ops", g.Goal, err)
	}

	logo := send("Draw a logo.", map[string]any{"waits_for": items[0].ID})
	finishQueuedTurns(t, h) // a lead has one queued wake at a time; clear the earlier ones
	if got := call("docs", map[string]any{"action": "push_back", "goal_id": logo, "text": "We don't draw"}); got != `"Draw a logo." is declined now.` {
		t.Fatalf("push_back = %q", got)
	}
	wakeOfAna("Team Docs pushed back your task \"Draw a logo.\": We don't draw. Answer a question with answer_task. If a task of yours waits on it, re-plan when it is pushed back.")

	notes := send("Write release notes.", nil)
	if got := call("ship", map[string]any{"action": "withdraw", "goal_id": notes}); got != `"Write release notes." is ended now.` {
		t.Fatalf("withdraw = %q", got)
	}

	if got := call("ship", map[string]any{"action": "propose", "text": "Add CSV import.", "brief": "Users ask for it."}); !strings.HasPrefix(got, `Proposed "Add CSV import." (task id `) {
		t.Fatalf("propose = %q", got)
	}
	board, err := h.svc.Board(ctx, ship.ID)
	if err != nil || len(board.Inbox) != 1 {
		t.Fatalf("Ship's inbox = %+v, %v; want the proposal", board.Inbox, err)
	}
	if got := call("ship", map[string]any{"action": "take", "goal_id": board.Inbox[0].ID}); got != `Took "Add CSV import." as #1 in your plan; it starts when the tasks before it are closed. Don't plan it now.` {
		t.Fatalf("take into the plan = %q", got)
	}
	call("ship", map[string]any{"action": "propose", "text": "Add JSON import."})
	board, _ = h.svc.Board(ctx, ship.ID)
	call("ship", map[string]any{"action": "take", "goal_id": board.Inbox[0].ID, "position": 1})
	board, _ = h.svc.Board(ctx, ship.ID)
	if board.Plan[0].Text != "Add JSON import." {
		t.Fatalf("plan after take at 1 = %q, %q", board.Plan[0].Text, board.Plan[1].Text)
	}
	if got := call("ship", map[string]any{"action": "order", "order": []string{board.Plan[1].ID, board.Plan[0].ID}}); got != "The plan is in the new order." {
		t.Fatalf("order = %q", got)
	}
	board, _ = h.svc.Board(ctx, ship.ID)
	if board.Plan[0].Text != "Add CSV import." {
		t.Fatalf("plan after order = %q first", board.Plan[0].Text)
	}

	// Docs closes the guide once its step is done; Ship's lead, who asked, approves it.
	doStep(t, h, docs, cy)
	if got := call("docs", map[string]any{"action": "close", "goal_id": guide, "text": "guide.md is written."}); got != `"Write the guide." is delivered now.` {
		t.Fatalf("close = %q", got)
	}
	if got := call("ship", map[string]any{"action": "approve", "goal_id": guide}); got != `"Write the guide." is done now.` {
		t.Fatalf("approve = %q", got)
	}

	// Stopping Ship's Now task starts the next one in its plan.
	if got := call("ship", map[string]any{"action": "stop", "goal_id": board.Goal.ID}); got != `"Ship CSV export." is stopped now.` {
		t.Fatalf("stop = %q", got)
	}
	board, _ = h.svc.Board(ctx, ship.ID)
	if board.Goal == nil || board.Goal.Text != "Add CSV import." {
		t.Fatalf("Now after stop = %+v; want the first planned task", board.Goal)
	}
}

func TestChangesGoToATaskOrAStepAndWithdrawToATask(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	ship, ana, _, cy := h.newTeam(t)
	docs, err := h.svc.CreateTeam(ctx, "Docs", "Write docs.", []string{cy.ID}, core.TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	shipLead := boardSession(t, h, srv, ana, ship, false)
	docsLead := boardSession(t, h, srv, cy, docs, false)
	g, err := h.svc.SendTask(ctx, core.Actor{BotID: ana.ID, TeamID: ship.ID, Role: core.RoleLead}, docs.ID, core.NewTask{Text: "Guide."})
	if err != nil {
		t.Fatal(err)
	}
	if got, isErr := callText(t, docsLead, "update_board", map[string]any{"action": "take", "goal_id": g.ID}); isErr {
		t.Fatal(got)
	}
	doStep(t, h, docs, cy)
	if got, isErr := callText(t, docsLead, "update_board", map[string]any{"action": "close", "goal_id": g.ID, "text": "Done."}); isErr {
		t.Fatal(got)
	}
	if got, isErr := callText(t, shipLead, "update_board", map[string]any{"action": "changes", "goal_id": g.ID, "text": "Add examples."}); isErr || got != `"Guide." is active now.` {
		t.Fatalf("changes to a done task = %q (error %v)", got, isErr)
	}
	// Without goal_id, changes acts on a step.
	if got, isErr := callText(t, shipLead, "update_board", map[string]any{"action": "changes", "item_id": "no-such-step", "text": "x"}); !isErr || !strings.Contains(got, "not found") {
		t.Fatalf("changes to a step = %q (error %v); want the step path's not-found error", got, isErr)
	}
	// withdraw acts only on a task: without goal_id there is none to find.
	if got, isErr := callText(t, shipLead, "update_board", map[string]any{"action": "withdraw", "item_id": "no-such-request"}); !isErr || got != `not found: goal ""` {
		t.Fatalf("withdraw without goal_id = %q (error %v); want the task path's not-found error", got, isErr)
	}
}

// update_board, with send_task, is in every team turn, the lead's and a
// member's, and not outside a team.
func TestSendTaskIsInEveryTeamTurn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	team, ana, bo, _ := h.newTeam(t)
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	for _, rc := range []runContext{
		{BotID: ana.ID, BotName: "Ana", TurnID: "t1", RoomID: team.ID, BoardRole: core.RoleLead},
		{BotID: bo.ID, BotName: "Bo", TurnID: "t2", RoomID: team.ID},
		{BotID: bo.ID, BotName: "Bo", TurnID: "t3"},
	} {
		token, err := h.runner.tokens.issue(rc)
		if err != nil {
			t.Fatal(err)
		}
		cs, err := mcpSession(t, srv.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		listed, err := cs.ListTools(context.Background(), nil)
		cs.Close()
		if err != nil {
			t.Fatal(err)
		}
		has := false
		for _, tool := range listed.Tools {
			if tool.Name == "update_board" {
				schema, _ := json.Marshal(tool.InputSchema)
				has = strings.Contains(string(schema), "send_task")
			}
		}
		if inTeam := rc.RoomID != ""; has != inTeam {
			t.Errorf("turn %+v has update_board with send_task = %v; want %v", rc, has, inTeam)
		}
	}
}

func finishQueuedTurns(t *testing.T, h *harness) {
	t.Helper()
	queued, err := h.svc.QueuedTurns(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range queued {
		if err := h.svc.FinishTurn(context.Background(), q.ID, "done", ""); err != nil {
			t.Fatal(err)
		}
	}
}

// doStep adds a step to team's Now task and has owner finish it.
func doStep(t *testing.T, h *harness, team, owner core.Bot) {
	t.Helper()
	ctx := context.Background()
	lead, _ := team.Lead()
	it, err := h.svc.CreateItem(ctx, core.Actor{BotID: lead.ID, TeamID: team.ID, Role: core.RoleLead}, boardItem("Write", "Write it.", owner.ID, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartItemWork(ctx, it.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.SubmitItem(ctx, core.Actor{BotID: owner.ID, TeamID: team.ID, Role: core.RoleWork}, it.ID, "done"); err != nil {
		t.Fatal(err)
	}
}
