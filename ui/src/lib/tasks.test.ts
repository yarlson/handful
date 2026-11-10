import { assert, test } from "vitest"
import {
  awaitsAnswer,
  budgetError,
  budgetLine,
  budgetShare,
  canWithdraw,
  clockLine,
  clockStop,
  countsLine,
  decisionError,
  firstLine,
  historyKey,
  fromUser,
  inboxError,
  leadName,
  memberName,
  needsYouLine,
  nextUp,
  sentState,
  sourceLabel,
  stuckReason,
  taskError,
  taskEventLabel,
  tasksKey,
  taskBar,
  teamOption,
  teamSummary,
  turnsLine,
  type TaskLike,
  type TaskState,
} from "./tasks.ts"

const task = (over: Partial<TaskLike> = {}): TaskLike => ({
  state: "incoming",
  text: "Ship CSV export",
  used: 0,
  budget: 30,
  queuePos: 0,
  ...over,
})
const at = (ms: number) => `t${ms}`
const names = { team: (id: string) => `Team ${id}`, member: (id: string) => `Member ${id}`, at }

test("a task names who sent it", () => {
  assert.equal(sourceLabel(task({ origin: "handoff", fromTeamId: "p" }), names), "from Team p")
  assert.equal(sourceLabel(task({ origin: "proposal", fromBotId: "b" }), names), "proposed by Member b")
  assert.equal(sourceLabel(task({ origin: "proposal" }), names), "proposed by Member ")
  assert.equal(sourceLabel(task({ origin: "handoff" }), names), "from you")
  assert.equal(sourceLabel(task({ origin: "user" }), names), "from you")
  assert.equal(sourceLabel(task({ origin: "user", fromTeamId: "p" }), names), "from you")
  assert.equal(sourceLabel(task(), names), "from you")
})

test("the user answers the questions about the tasks the user sent", () => {
  assert.isTrue(fromUser(task({ origin: "user" })))
  assert.isTrue(fromUser(task({ origin: "handoff" })))
  assert.isFalse(fromUser(task({ origin: "handoff", fromTeamId: "p" })))
  assert.isFalse(fromUser(task({ origin: "proposal" })))
})

test("the clock stands for a usage limit, a drain or a stopped team, in that order", () => {
  const stopped = { state: "paused" }
  assert.equal(clockStop(true, true, stopped), "usage limit")
  assert.equal(clockStop(false, true, stopped), "server restart")
  assert.equal(clockStop(false, false, stopped), "team stopped")
  assert.isNull(clockStop(false, false, { state: "active" }))
  assert.isNull(clockStop(false, false, null))
  assert.isNull(clockStop(false, false, undefined))
})

test("only the user decides on a missed, asked-out or passed-out task", () => {
  assert.isNull(stuckReason(task({ missedAt: 9 }), "Dee", 30))
  assert.equal(
    stuckReason(task({ needsUser: true, missedAt: 9, askRounds: 4 }), "Dee", 30),
    "Dee didn't decide within 30 min",
  )
  assert.equal(stuckReason(task({ needsUser: true, askRounds: 4 }), "Dee", 30), "asked 3 times")
  assert.equal(stuckReason(task({ needsUser: true, askRounds: 3, passes: 4 }), "Dee", 30), "passed on 3 times")
  assert.equal(stuckReason(task({ needsUser: true }), "Dee", 30), "passed on 3 times")
})

const clock = { minutes: 30, lead: "Dee", sender: "Product", stop: null, loadedAt: 1_000_000 }

test("the decision clock says when the lead decides by", () => {
  assert.equal(clockLine(task(), clock, at), "decide by t2800000")
  assert.equal(clockLine(task({ decisionMs: 600_000 }), clock, at), "decide by t2200000")
  assert.equal(clockLine(task({ decisionMs: 1_799_999 }), clock, at), "decide by t1000001")
  assert.equal(clockLine(task({ decisionMs: 1_800_000 }), clock, at), "Dee decides now")
  assert.equal(clockLine(task({ decisionMs: 2_000_000 }), clock, at), "Dee decides now")
})

test("the decision clock says why it stands", () => {
  assert.equal(clockLine(task(), { ...clock, stop: "usage limit" }, at), "clock paused: usage limit")
  assert.equal(
    clockLine(task({ askedAt: 5, askRounds: 2 }), { ...clock, stop: "usage limit" }, at),
    "asked Product · round 2 of 3 · clock paused",
  )
  assert.equal(clockLine(task({ askedAt: 5 }), clock, at), "asked Product · round 1 of 3 · clock paused")
  assert.equal(
    clockLine(task({ needsUser: true, missedAt: 7, askedAt: 5 }), clock, at),
    "needs you: Dee didn't decide within 30 min",
  )
  assert.equal(clockLine(task({ state: "later", laterUntil: 42, needsUser: true }), clock, at), "later, until t42")
  assert.equal(clockLine(task({ state: "later" }), clock, at), "later, until t0")
})

test("the sending team reads each state of a task it sent", () => {
  const cases: [Partial<TaskLike>, string, string][] = [
    [{ state: "incoming" }, "In Dev's inbox", "waiting"],
    [{ state: "incoming", askedAt: 3 }, "Dev asked", "attention"],
    [{ state: "incoming", askedAt: 3, needsUser: true }, "Needs you", "attention"],
    [{ state: "later", laterUntil: 9 }, "Later, until t9", "waiting"],
    [{ state: "later" }, "Later, until t0", "waiting"],
    [{ state: "queued", queuePos: 2 }, "#2 in Dev's plan", "waiting"],
    [{ state: "active" }, "Dev is on it", "progress"],
    [{ state: "paused" }, "Paused at Dev", "attention"],
    [{ state: "delivered" }, "Done, waiting for approval", "progress"],
    [{ state: "done" }, "Done", "progress"],
    [{ state: "declined" }, "Pushed back", "attention"],
    [{ state: "stopped" }, "Stopped", "over"],
    [{ state: "ended" }, "Withdrawn", "over"],
  ]
  for (const [over, label, tone] of cases) assert.deepEqual(sentState(task(over), "Dev", at), { label, tone }, label)
})

test("a task's turns name the team that pays", () => {
  assert.equal(turnsLine(0, "Dev"), "")
  assert.equal(turnsLine(1, "Dev"), "1 turn, paid by Dev")
  assert.equal(turnsLine(38, "Dev"), "38 turns, paid by Dev")
})

test("only a task that hasn't started can be withdrawn", () => {
  for (const s of ["incoming", "later", "queued"]) assert.isTrue(canWithdraw({ state: s }), s)
  for (const s of ["active", "paused", "delivered", "done", "declined", "stopped", "ended"])
    assert.isFalse(canWithdraw({ state: s }), s)
})

test("a lead waits for an answer while a question is open and the user doesn't decide", () => {
  assert.isTrue(awaitsAnswer(task({ askedAt: 3 })))
  assert.isFalse(awaitsAnswer(task({ askedAt: 3, needsUser: true })))
  assert.isFalse(awaitsAnswer(task({ askedAt: 0 })))
  assert.isFalse(awaitsAnswer(task()))
})

const bar = (state: TaskState | "x") => ({ state })

test("the task bar shows the buttons of the Now or done task, and a paused task always resumes", () => {
  assert.deepEqual(taskBar(bar("active"), 0), { mode: "now", actions: ["stop"] })
  assert.deepEqual(taskBar(bar("active"), 2), { mode: "now", actions: ["stop"] })
  assert.deepEqual(taskBar(bar("paused"), 0), { mode: "now", actions: ["resume", "stop"] })
  assert.deepEqual(taskBar(bar("delivered"), 1), { mode: "now", actions: ["approve", "changes"] })
})

test("without a Now task the bar offers to reopen, start the plan, or send a task", () => {
  assert.deepEqual(taskBar(bar("stopped"), 2), { mode: "stopped" })
  for (const state of ["done", "ended", "queued", "incoming", "later", "declined", "x"] as const) {
    assert.deepEqual(taskBar(bar(state), 1), { mode: "hold" }, state)
    assert.deepEqual(taskBar(bar(state), 0), { mode: "idle" }, state)
  }
  assert.deepEqual(taskBar(undefined, 2), { mode: "hold" })
  assert.deepEqual(taskBar(null, 0), { mode: "idle" })
})

test("the bar names the next task and how many follow", () => {
  assert.isNull(nextUp([]))
  assert.equal(nextUp([{ text: "B" }]), "Next: B")
  assert.equal(nextUp([{ text: "B" }, { text: "C" }, { text: "D" }]), "Next: B (+2)")
})

test("each Needs you kind has its own line", () => {
  assert.deepEqual(needsYouLine({ kind: "approve", text: "Spec it" }, "Product"), {
    lead: "Approve:",
    rest: "“Spec it” is done (Product)",
  })
  assert.deepEqual(needsYouLine({ kind: "missed", text: "Retry", lead: "Dee" }, "Dev"), {
    lead: "Lead didn't decide in time:",
    rest: "Dee left “Retry” in Dev's inbox",
  })
  assert.deepEqual(needsYouLine({ kind: "missed", text: "Retry" }, "Dev"), {
    lead: "Lead didn't decide in time:",
    rest: "The lead left “Retry” in Dev's inbox",
  })
  assert.deepEqual(needsYouLine({ kind: "stuck", text: "Retry", reason: "asked 3 times" }, "Dev"), {
    lead: "Needs a decision:",
    rest: "“Retry” was asked 3 times (Dev)",
  })
  assert.deepEqual(needsYouLine({ kind: "stuck", text: "Retry" }, "Dev"), {
    lead: "Needs a decision:",
    rest: "“Retry” was stuck (Dev)",
  })
  assert.deepEqual(needsYouLine({ kind: "question", text: "Which build?" }, "Support"), {
    lead: "Question:",
    rest: "Which build? (Support)",
  })
  assert.deepEqual(
    needsYouLine({ kind: "budget_warning", text: "Ship used 80% of today's turns (80 of 100)" }, "Ship"),
    { lead: "Budget:", rest: "Ship used 80% of today's turns (80 of 100)" },
  )
  assert.deepEqual(needsYouLine({ kind: "budget_reached", text: "Budget: Ship used 100 of 100 turns today" }, "Ship"), {
    lead: "Budget reached:",
    rest: "Ship used 100 of 100 turns today",
  })
  assert.deepEqual(needsYouLine({ kind: "budget_reached", text: "Team Budget: Ops used 5 of 5 turns today" }, "Ops"), {
    lead: "Budget reached:",
    rest: "Team Budget: Ops used 5 of 5 turns today",
  })
})

const day = { today: 212, limit: 400, suggested: 0, paused: false, warn: false }
const none = { offTask: 0, budget: null }

test("a team's card sums up Now, Next, the inbox, what it sent, turns on no task and today's budget", () => {
  assert.deepEqual(
    teamSummary({
      goal: { state: "active", text: "Build Settings" },
      plan: [1, 2],
      inbox: [1],
      handedOver: [
        { state: "incoming" },
        { state: "active" },
        { state: "done" },
        { state: "ended" },
        { state: "declined" },
        { state: "stopped" },
      ],
      offTask: { count: 6 },
      budget: day,
    }),
    { now: "Build Settings", paused: false, next: 2, inbox: 1, sent: 2, offTask: 6, budget: day },
  )
  assert.deepEqual(teamSummary({ goal: { state: "paused", text: "Answer issues" } }), {
    now: "Answer issues",
    paused: true,
    next: 0,
    inbox: 0,
    sent: 0,
    ...none,
  })
  const spent = { ...day, today: 400, paused: true }
  assert.deepEqual(teamSummary({ goal: { state: "active", text: "Answer issues" }, budget: spent }), {
    now: "Answer issues",
    paused: true,
    next: 0,
    inbox: 0,
    sent: 0,
    offTask: 0,
    budget: spent,
  })
  for (const state of ["delivered", "stopped", "done"])
    assert.deepEqual(teamSummary({ goal: { state, text: "Old" } }), {
      now: null,
      paused: false,
      next: 0,
      inbox: 0,
      sent: 0,
      ...none,
    })
  assert.deepEqual(teamSummary({ goal: null }), { now: null, paused: false, next: 0, inbox: 0, sent: 0, ...none })
})

test("the counts line adds what the team sent and turns on no task only when there are some", () => {
  const s = { now: null, paused: false, next: 2, inbox: 1, sent: 0, ...none }
  assert.equal(countsLine(s), "Next 2 · Inbox 1")
  assert.equal(countsLine({ ...s, next: 0, inbox: 0, sent: 4 }), "Next 0 · Inbox 0 · sent 4")
  assert.equal(countsLine({ ...s, offTask: 1 }), "Next 2 · Inbox 1 · not on a task: 1 turn")
  assert.equal(countsLine({ ...s, sent: 1, offTask: 6 }), "Next 2 · Inbox 1 · sent 1 · not on a task: 6 turns")
})

test("a day without a limit is watched; with one, it counts against the limit", () => {
  assert.equal(budgetLine({ ...day, limit: 0 }), "212 turns (no limit yet)")
  assert.equal(budgetLine({ ...day, today: 1, limit: 0 }), "1 turn (no limit yet)")
  assert.equal(budgetLine(day), "212 of 400 turns used")
  assert.equal(budgetLine({ ...day, today: 1 }), "1 of 400 turns used")
  assert.isNull(budgetShare({ ...day, limit: 0 }))
  assert.equal(budgetShare(day), 53)
  assert.equal(budgetShare({ ...day, today: 0 }), 0)
  assert.equal(budgetShare({ ...day, today: 400 }), 100)
  assert.equal(budgetShare({ ...day, today: 450 }), 100)
})

test("the turns per day are 0 (no limit) to 10000 whole turns", () => {
  for (const v of ["0", "", "1", "10000"]) assert.isNull(budgetError(v), v)
  for (const v of ["-1", "10001", "2.5", "x"])
    assert.equal(budgetError(v), "A team's turns per day must be 0 (no limit) to 10000.", v)
})

test("the decision time is 5 to 1440 whole minutes", () => {
  for (const v of ["5", "30", "1440"]) assert.isNull(decisionError(v), v)
  for (const v of ["4", "1441", "7.5", "", " ", "x"])
    assert.equal(decisionError(v), "The decision time must be 5 to 1440 minutes.", v)
})

test("the inbox holds 1 to 50 tasks, and 1 up to that from one team", () => {
  assert.isNull(inboxError("10", "3"))
  assert.isNull(inboxError("1", "1"))
  assert.isNull(inboxError("50", "50"))
  for (const m of ["0", "51", "2.5", "", "x"]) assert.equal(inboxError(m, "1"), "The inbox can hold 1 to 50 tasks.", m)
  for (const p of ["0", "11", "1.5", "", "x"]) assert.equal(inboxError("10", p), "From one team: 1 to 10 tasks.", p)
})

test("a card shows a brief's first line", () => {
  assert.equal(firstLine(undefined), "")
  assert.equal(firstLine(""), "")
  assert.equal(firstLine("\n  \n  Fix the scroll.  \nMore"), "Fix the scroll.")
})

test("the send dialog names each team with the tasks in its inbox", () => {
  assert.equal(teamOption({ name: "Dev" }), "Dev")
  assert.equal(teamOption({ name: "Dev", incomingHandoffs: 0 }), "Dev")
  assert.equal(teamOption({ name: "Dev", incomingHandoffs: 1 }), "Dev (1 in its inbox)")
  assert.equal(teamOption({ name: "Dev", incomingHandoffs: 4 }), "Dev (4 in its inbox)")
})

test("a task needs a team and a text of at most 4000 characters", () => {
  assert.equal(taskError("", "Do it"), "Choose the team to send it to.")
  assert.equal(taskError("d", "  "), "Write what the team should do.")
  assert.equal(taskError("d", "x".repeat(4001)), "The task can be at most 4000 characters; put the rest in the brief.")
  assert.isNull(taskError("d", "x".repeat(4000)))
})

test("the tasks key changes when a turn, a question, the attention or an inbox changes", () => {
  const bot = { id: "d", running: false }
  assert.equal(tasksKey([bot, { id: "p", running: true }]), "d:0::0:0,p:1::0:0")
  assert.equal(
    tasksKey([{ id: "d", running: false, attention: "question", openQuestions: 2, incomingHandoffs: 3 }]),
    "d:0:question:2:3",
  )
  assert.equal(tasksKey([]), "")
})

test("a team's lead and members are named, and a member who left says so", () => {
  const team = { members: [{ id: "a", name: "Dee" }], team: { leadId: "a" } }
  assert.equal(memberName(team, "a"), "Dee")
  assert.equal(memberName(team, "b"), "a former member")
  assert.equal(memberName({}, "a"), "a former member")
  assert.equal(leadName(team), "Dee")
  assert.equal(leadName({ ...team, team: { leadId: "b" } }), "a former member")
  assert.equal(leadName({ ...team, team: { leadId: "" } }), "The lead")
  assert.equal(leadName({ members: team.members }), "The lead")
})

test("a task's history names each decision, and each step event by its step", () => {
  const kinds: [string, string][] = [
    ["sent", "Sent"],
    ["proposed", "Proposed"],
    ["taken", "Taken"],
    ["ordered", "Moved in the plan"],
    ["pinned", "Pinned"],
    ["unpinned", "Unpinned"],
    ["asked", "Asked the sender"],
    ["answered", "Answered"],
    ["later", "Deferred"],
    ["due", "Back in the inbox"],
    ["passed", "Passed on"],
    ["pushed_back", "Pushed back"],
    ["closed", "Closed"],
    ["approved", "Approved"],
    ["changes", "Changes asked"],
    ["stopped", "Stopped"],
    ["reopened", "Reopened"],
    ["withdrawn", "Withdrawn"],
    ["missed", "Decision missed"],
    ["new_kind", "new_kind"],
  ]
  for (const [kind, label] of kinds) assert.equal(taskEventLabel({ kind, round: 0, goalId: "g1" }), label, kind)
  assert.equal(taskEventLabel({ kind: "passed", round: 0 }, "Settings API"), "Settings API: Passed")
  assert.equal(taskEventLabel({ kind: "changes", round: 2 }, "Settings API"), "Settings API: Changes asked · round 2")
  assert.equal(taskEventLabel({ kind: "created", round: 0 }), "A step: Created")
  assert.equal(taskEventLabel({ kind: "passed", round: 0, goalId: "" }), "A step: Passed")
})

test("an open history reloads with each load of its board, and per task", () => {
  assert.equal(historyKey("g1", 1), "g1:1")
  assert.notEqual(historyKey("g1", 2), historyKey("g1", 1))
  assert.notEqual(historyKey("g2", 1), historyKey("g1", 1))
})
