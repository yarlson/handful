import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Board, Bot, Goal, Item } from "@/lib/api"
import { BoardView, HistoryList, needsOf, NowChipView, StepDetails } from "./board.tsx"

const team: Bot = {
  id: "t1",
  kind: "room",
  name: "Dev",
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
  team: { leadId: "a", agreement: "", goalBudget: 30 },
  members: [
    { id: "a", name: "Ana" },
    { id: "b", name: "Bo" },
  ],
}
const other = (id: string, name: string): Bot => ({ ...team, id, name })
const goal = (id: string, text: string, state: Goal["state"], queuePos = 0): Goal => ({
  id,
  teamId: "t1",
  text,
  state,
  budget: 30,
  used: 0,
  queuePos,
  createdAt: 0,
})
// sent is a task team Dev (t1) sent to team Ops (t3).
const sent = (id: string, text: string, state: Goal["state"], x: Partial<Goal> = {}): Goal => ({
  ...goal(id, text, state),
  teamId: "t3",
  origin: "handoff",
  fromTeamId: "t1",
  ...x,
})
const step = (id: string, title: string, r: Partial<Item>): Item => ({
  id,
  teamId: "t1",
  goalId: "g1",
  title,
  brief: "",
  acceptance: "",
  state: "todo",
  ownerId: "",
  reviewerId: "",
  dependsOn: [],
  result: "",
  note: "",
  reviewRounds: 0,
  updatedAt: 0,
  ...r,
})
const budget = { today: 212, limit: 400, suggested: 0, paused: false, warn: false }
const board = (b: Partial<Board>): Board => ({
  goal: goal("g1", "Ship CSV export", "active"),
  items: [],
  teams: { t2: "Support", t3: "Ops" },
  ...b,
})
const render = (b: Board, x: { teams?: Bot[]; paused?: string | null; onOpenInbox?: () => void } = {}) =>
  renderToStaticMarkup(<BoardView team={team} data={b} error={null} act={async () => true} {...x} />)

const offTask = {
  count: 2,
  turns: [
    { turnId: "t1", botId: "b", botName: "Bo", startedAt: 0, firstLine: "Rebased the context-menu PR" },
    { turnId: "t2", botId: "b", botName: "Bo", startedAt: 0, firstLine: "" },
  ],
}

// busy has something in every section: one need of each kind, a plan and an inbox, open,
// finished and pushed-back sent tasks, a stopped task, turns on no task and the day's budget.
const busy = board({
  goal: { ...goal("g1", "Ship CSV export", "active"), used: 38 },
  items: [
    step("s1", "Write code", { state: "doing", ownerId: "a" }),
    step("s2", "Review", { state: "done", ownerId: "b" }),
    step("s3", "Spec", { state: "blocked", ownerId: "b" }),
  ],
  plan: [goal("q1", "Add PDF export", "queued", 1)],
  inbox: [goal("i1", "Fix a bug", "incoming"), { ...goal("i2", "Late call", "incoming"), needsUser: true }],
  done: [goal("d1", "Spec the updater", "delivered")],
  handedOver: [
    sent("h1", "Write docs", "queued", { queuePos: 2 }),
    sent("h2", "Asked one", "incoming", { askedAt: 5, intakeNote: "Which build?" }),
    sent("h3", "Delivered one", "delivered"),
    sent("h4", "Pushed one", "declined", { intakeNote: "GTM owns copy." }),
    sent("h5", "Put away one", "declined", { intakeNote: "No.", dismissedAt: 9 }),
    sent("h6", "Finished one", "done"),
    sent("h7", "Withdrawn one", "ended"),
  ],
  stopped: [{ ...goal("s9", "Older stopped", "stopped"), used: 7 }],
  offTask,
  budget: { ...budget, today: 400, paused: true },
})

// inOrder checks that every marker is in html, each after the one before it.
const inOrder = (html: string, markers: string[]) => {
  let at = -1
  for (const m of markers) {
    const i = html.indexOf(m, at + 1)
    assert.isAbove(i, at, `"${m}" after the marker before it`)
    at = i
  }
}

test("the sections come in order: Now, Needs you, Next, Waiting on other teams, Earlier, then the footer", () => {
  const html = render(busy, { teams: [team, other("t3", "Ops")] })
  inOrder(html, [
    'id="now-heading"',
    'id="needs-heading"',
    'id="next-heading"',
    'id="sent-heading"',
    "Earlier (",
    "<footer",
    'aria-label="Not on a task"',
    'id="budget-heading"',
  ])
  // Earlier starts collapsed.
  assert.match(html, /<details class="group\/earlier">/)
  for (const id of ["now", "needs", "next", "sent"]) {
    assert.match(html, new RegExp(`<section aria-labelledby="${id}-heading"`), id)
    assert.match(html, new RegExp(`<h3 id="${id}-heading"`), id)
  }
})

test("Needs you, Next and the off-task note show only when they have something", () => {
  const html = render(board({ budget }))
  assert.notInclude(html, "needs-heading")
  assert.notInclude(html, "next-heading")
  assert.notInclude(html, "Not on a task")
  assert.include(render(board({ inbox: [goal("i1", "Fix a bug", "incoming")] })), 'id="next-heading"')
  assert.include(render(board({ plan: [goal("q1", "Add PDF export", "queued", 1)] })), 'id="next-heading"')
})

test("the Now card shows its title, sender, progress, every step as one row, History and Stop", () => {
  const html = render(
    board({
      goal: { ...goal("g1", "Ship CSV export", "active"), used: 38, origin: "handoff", fromTeamId: "t2" },
      items: busy.items,
    }),
  )
  assert.include(html, "Ship CSV export")
  assert.include(html, "from Support")
  assert.match(html, /1 of 3 steps done · 38 turns · since \d\d:\d\d/)
  assert.include(html, 'aria-label="Steps of Ship CSV export"')
  assert.equal(html.match(/aria-controls="step-/g)?.length, 3)
  assert.include(html, 'aria-label="History: Ship CSV export"')
  assert.include(html, 'aria-label="Stop: Ship CSV export"')
  assert.include(render(board({})), "No steps yet · 0 turns")
  assert.include(render(board({ goal: { ...goal("g1", "One", "active"), used: 1 } })), "No steps yet · 1 turn ·")
})

test("each step row shows its state as an icon and a word, its owner and change requests, in state order", () => {
  const states: Item["state"][] = ["done", "todo", "dropped", "review", "blocked", "doing"]
  const html = render(
    board({
      items: states.map((s, i) =>
        step(`s${i}`, `Step ${s}`, {
          state: s,
          ownerId: "b",
          reviewRounds: s === "doing" ? 2 : s === "review" ? 1 : 0,
        }),
      ),
    }),
  )
  inOrder(html, ["Step blocked", "Step review", "Step doing", "Step todo", "Step done", "Step dropped"])
  for (const word of ["Blocked", "In review", "Doing", "To do", "Done", "Dropped"]) assert.include(html, `${word} · Bo`)
  assert.include(html, "Doing · Bo · 2 change requests")
  assert.include(html, "In review · Bo · 1 change request<")
  assert.notInclude(html, "Blocked · Bo · ")
  assert.equal(html.match(/<svg[^>]*class="[^"]*mt-0.5 size-4 shrink-0[^"]*" aria-hidden="true"/g)?.length, 6)
  assert.match(html, /<button type="button" aria-expanded="false" aria-controls="step-s0"/)
  assert.include(render(board({ items: [step("s1", "Lone", {})] })), "To do · No owner yet")
})

const item = (x: Partial<Item>) => step("i1", "Write code", { ownerId: "a", ...x })
const details = (x: Partial<Item>) => renderToStaticMarkup(<StepDetails team={team} item={item(x)} />)

test("an opened step shows its change request, history, brief, Done when and result", () => {
  const html = details({
    state: "doing",
    reviewerId: "b",
    reviewRounds: 2,
    eventCount: 5,
    brief: "Read the CSV.",
    acceptance: "Tests pass.",
    result: "PR #9.",
    latestChange: { round: 2, by: "Bo", at: 0, text: "Add a test for the empty file." },
  })
  assert.include(html, "Ana · reviewed by Bo")
  assert.match(html, /<button[^>]*aria-expanded="false"[^>]*aria-controls="history-i1"[^>]*>2 change requests</)
  assert.include(html, "Changes asked · round 2 · Bo")
  assert.include(html, "Add a test for the empty file.")
  assert.include(html, "Read the CSV.")
  assert.include(html, "Done when: Tests pass.")
  assert.include(html, "Result: PR #9.")
  assert.include(details({ eventCount: 3 }), "History (3)")
  assert.notInclude(details({ eventCount: 0 }), "History (")
  assert.include(details({ ownerId: "" }), "No owner yet")
})

test("a note shows only on a blocked or dropped step", () => {
  assert.include(details({ state: "blocked", note: "Which API?" }), "Which API?")
  assert.include(details({ state: "dropped", note: "Not needed." }), "Not needed.")
  assert.notInclude(details({ state: "doing", note: "old note" }), "old note")
})

test("Needs you lists the budget, missed decisions, questions, done tasks, sent tasks done, then pushed back", () => {
  assert.deepEqual(
    needsOf(busy).map((n) => (n.kind === "budget" ? "budget" : `${n.kind}:${n.goal.id}`)),
    ["budget", "decide:i2", "answer:h2", "approve:d1", "sent-done:h3", "pushed:h4"],
  )
  assert.deepEqual(needsOf(board({})), [])
  // A question the user must settle instead of the sender is no answer to give.
  assert.deepEqual(
    needsOf(board({ handedOver: [sent("h2", "Asked", "incoming", { askedAt: 5, needsUser: true })] })),
    [],
  )
  const html = render(busy, { onOpenInbox: () => {} })
  assert.include(html, "Needs you (6)")
  inOrder(html, [
    "Today&#x27;s turns are used.",
    "Late call",
    "Asked one",
    "Spec the updater",
    "Delivered one",
    "Pushed one",
  ])
})

test("each need has the buttons that settle it", () => {
  const html = render(busy, { onOpenInbox: () => {} })
  const needs = html.slice(html.indexOf('id="needs-heading"'), html.indexOf('id="next-heading"'))
  assert.include(needs, 'aria-label="Raise: Dev"')
  assert.include(needs, "Ana didn&#x27;t decide in time:")
  assert.include(needs, ">Open inbox</button>")
  assert.include(needs, "Ops asks:")
  assert.include(needs, 'aria-label="Answer: Asked one"')
  assert.include(needs, "Ops:</strong> Which build?")
  for (const t of ["Spec the updater", "Delivered one"]) {
    assert.include(needs, `aria-label="Approve: ${t}"`)
    assert.include(needs, `aria-label="Ask for changes: ${t}"`)
  }
  assert.include(needs, "Done, approve it:")
  assert.include(needs, "Done at Ops, approve it:")
  assert.include(needs, "Ops pushed back:")
  assert.include(needs, 'aria-label="Dismiss: Pushed one"')
  assert.include(needs, "Ops:</strong> GTM owns copy.")
  assert.include(needs, 'aria-label="History: Pushed one"')
  assert.notInclude(needs, 'aria-label="History: Spec the updater"')
  assert.notInclude(needs, "Put away one")
})

test("a pushed-back task that was put away, and finished sent tasks, show only under Earlier", () => {
  const html = render(busy)
  const start = html.indexOf("Earlier (")
  for (const t of ["Put away one", "Finished one", "Withdrawn one"]) {
    assert.isAbove(html.indexOf(t), start, `${t} only under Earlier`)
  }
  assert.include(html, "Earlier (4)")
})

test("open sent tasks wait on other teams with their state, and only those not started can be withdrawn", () => {
  const html = render(busy)
  const waiting = html.slice(html.indexOf('id="sent-heading"'), html.indexOf("Earlier ("))
  assert.include(waiting, "Waiting on other teams (1)")
  assert.include(waiting, "Write docs")
  assert.include(waiting, "#2 in Ops&#x27;s plan")
  assert.include(waiting, 'aria-label="Withdraw: Write docs"')
  assert.notInclude(waiting, "Asked one")
  assert.notInclude(waiting, "Delivered one")
  const started = render(board({ handedOver: [sent("h1", "Model chip", "active", { used: 12 })] }))
  assert.include(started, "Ops is on it")
  assert.include(started, "To Ops · 12 turns, paid by Ops")
  assert.notInclude(started, 'aria-label="Withdraw: Model chip"')
  const later = render(board({ handedOver: [sent("h1", "Later one", "later", { intakeNote: "After the release." })] }))
  assert.include(later, "Note: After the release.")
})

test("the team's stopped tasks show under Earlier with Reopen and their history", () => {
  const html = render(
    board({
      goal: goal("s0", "Latest stopped", "stopped"),
      stopped: [goal("s0", "Latest stopped", "stopped"), goal("s1", "Older stopped", "stopped")],
    }),
  )
  const earlier = html.slice(html.indexOf("Earlier ("))
  for (const t of ["Latest stopped", "Older stopped"]) {
    assert.include(earlier, `aria-label="Reopen: ${t}"`)
    assert.include(earlier, `aria-label="History: ${t}"`)
  }
  assert.include(html, "Earlier (2)")
  assert.include(html, "Nothing now.")
})

test("the inbox line counts what waits for the lead, and shows only when something does", () => {
  const html = render(board({ inbox: [goal("i1", "Fix a bug", "incoming"), goal("i2", "Fix another", "later")] }), {
    onOpenInbox: () => {},
  })
  assert.include(html, "Inbox: 2 tasks wait for Ana&#x27;s decision.")
  assert.include(html, ">Open inbox</button>")
  assert.include(render(board({ inbox: [goal("i1", "Fix a bug", "incoming")] })), "Inbox: 1 task waits for Ana")
  assert.notInclude(render(board({ plan: [goal("q1", "Add PDF export", "queued", 1)] })), "Inbox:")
})

test("Send a task shows only when the app has another team", () => {
  assert.include(render(board({}), { teams: [team, other("t3", "Ops")] }), "Send a task…")
  assert.notInclude(render(board({}), { teams: [team] }), "Send a task…")
})

test("an empty team shows only Nothing now, Waiting on other teams (0) and Today", () => {
  const html = render(board({ goal: null, budget }))
  assert.include(html, "Nothing now. To send the lead a task, type /task")
  assert.include(html, "Waiting on other teams (0)")
  assert.include(html, "Nothing sent is open.")
  assert.include(html, 'id="budget-heading"')
  for (const gone of ["needs-heading", "next-heading", "Earlier (", "Not on a task", "Steps of", "<li"])
    assert.notInclude(html, gone)
})

test("a drain shows on the Now card as Paused for update, or the restart's label", () => {
  assert.include(render(board({}), { paused: "Paused for update" }), "Paused for update")
  assert.include(render(board({}), { paused: "Paused for restart" }), "Paused for restart")
  assert.notInclude(render(board({})), "Paused for")
  assert.include(render(board({ goal: goal("g1", "Ship CSV export", "paused") })), ">Paused<")
})

test("planned tasks are listed in the lead's order, with their controls shown on hover or focus", () => {
  const html = render(
    board({
      plan: [{ ...goal("q1", "Add PDF export", "queued", 1), pinned: true }, goal("q2", "Fix login", "queued", 2)],
    }),
  )
  assert.include(html, "Next (2)")
  assert.include(html, "In Ana&#x27;s order. Move or pin to change it.")
  assert.isBelow(html.indexOf("Add PDF export"), html.indexOf("Fix login"))
  assert.include(html, 'aria-label="Move Add PDF export up"')
  assert.include(html, 'aria-label="Unpin Add PDF export"')
  assert.include(html, 'aria-label="Pin Fix login at #2"')
  assert.include(html, ">pinned<")
  assert.include(html, 'aria-label="Withdraw: Fix login"')
  assert.include(html, 'aria-label="History: Fix login"')
  assert.include(html, 'aria-live="polite"')
  assert.include(html, "The first one starts when the Now task is done.")
  assert.match(html, /id="plan-q1-up"[^>]*disabled/)
  assert.match(html, /id="plan-q2-down"[^>]*disabled/)
  assert.notMatch(html, /id="plan-q1-down"[^>]*disabled/)
  assert.equal(html.match(/group-hover\/plan:opacity-100 group-focus-within\/plan:opacity-100/g)?.length, 2)
})

test("with no Now task the plan is on hold and can start now", () => {
  const html = render(board({ goal: goal("g0", "Old", "done"), plan: [goal("q1", "Add PDF export", "queued", 1)] }))
  assert.include(html, "Nothing now. The plan is on hold: start the next task below.")
  assert.include(html, "On hold.")
  assert.include(html, "Start now")
  assert.notInclude(html, "The first one starts")
})

test("the history lists every event with its round, author and whole text", () => {
  const ev = (id: string, kind: string, round: number, actorName: string, text: string) => ({
    id,
    itemId: "i1",
    kind,
    actorId: actorName ? "b" : "",
    actorName,
    round,
    text,
    createdAt: 0,
  })
  const html = renderToStaticMarkup(
    <HistoryList
      id="history-i1"
      item={item({ historyFrom: 1000 })}
      events={[
        ev("1", "submitted", 0, "Ana", "Done."),
        ev("2", "changes", 1, "Bo", "Round one text."),
        ev("3", "changes", 2, "Bo", "Round two text."),
        ev("4", "passed", 0, "Bo", ""),
      ]}
    />,
  )
  assert.include(html, 'aria-label="History of Write code"')
  assert.isBelow(html.indexOf("Round one text."), html.indexOf("Round two text."))
  assert.include(html, "Changes asked · round 1")
  assert.include(html, "Changes asked · round 2")
  assert.include(html, "Passed")
  assert.include(html, "History starts on")
})

test("turns on no task show as a quiet note in the footer, with Make a task and Dismiss", () => {
  const html = render(board({ offTask }))
  const footer = html.slice(html.indexOf("<footer"))
  assert.include(footer, '<section aria-label="Not on a task"')
  assert.include(footer, "border-dashed")
  assert.include(footer, "<strong>2 turns in the last hour</strong> weren&#x27;t on any task: Bo.")
  assert.include(footer, 'aria-label="Make a task: 2 turns on no task"')
  assert.include(footer, 'aria-label="Dismiss: 2 turns on no task"')
  const one = render(board({ offTask: { count: 1, turns: offTask.turns.slice(0, 1) } }))
  assert.include(one, "<strong>1 turn in the last hour</strong> wasn&#x27;t on any task: Bo.")
  assert.notInclude(render(board({ offTask: { count: 0, turns: [] } })), "Not on a task")
})

test("today's budget shows the turns used against the limit, and the paused state", () => {
  const html = render(board({ budget }))
  assert.include(html, "212 of 400 turns used")
  assert.include(html, 'style="width:53%"')
  assert.include(html, 'aria-label="Change the limit: Dev"')
  assert.notInclude(html, "Paused: new turns wait")
  const paused = render(board({ budget: { ...budget, today: 400, paused: true } }))
  assert.include(paused, "Paused: new turns wait until midnight or a higher limit.")
})

test("a watched day shows its count with no limit, then the suggested limit to accept", () => {
  const watched = render(board({ budget: { ...budget, limit: 0 } }))
  assert.include(watched, "212 turns (no limit yet)")
  assert.notInclude(watched, 'style="width')
  assert.include(watched, 'aria-label="Set a limit: Dev"')
  const suggested = render(board({ budget: { ...budget, limit: 0, suggested: 350 } }))
  assert.include(suggested, "Suggested limit: 350 turns a day.")
  assert.include(suggested, ">Use 350</button>")
  assert.notInclude(render(board({ budget: { ...budget, suggested: 350 } })), "Suggested limit")
})

test("a long team name in a task's chip is cut short and leaves the title room", () => {
  const html = render(
    board({ plan: [{ ...goal("q1", "Build the spec", "queued", 1), origin: "handoff", fromTeamId: "p1" }] }),
    { teams: [other("p1", "Handful Customer Support and Success Operations")] },
  )
  assert.match(
    html,
    /<span[^>]*class="[^"]*\bmax-w-1\/2\b[^"]*\bshrink\b[^"]*"[^>]*><span class="min-w-0 truncate" title="from Handful Customer Support and Success Operations">/,
  )
  assert.match(html, /<span class="line-clamp-2 min-w-\[60%\] flex-1" title="Build the spec">/)
})

const chip = (b: Board) => renderToStaticMarkup(<NowChipView data={b} onOpen={() => {}} />)

test("the Now chip names the Now task and counts what needs the user, as Needs you does", () => {
  const html = chip(busy)
  assert.include(html, 'aria-label="Work: now, Ship CSV export; 6 need you. Opens the Work tab."')
  assert.include(html, ">Now<")
  assert.include(html, 'title="Ship CSV export"')
  assert.include(html, "6 need you")
  assert.include(html, "bg-emerald-500")
  const quiet = chip(board({ goal: null }))
  assert.include(quiet, 'aria-label="Work: nothing now. Opens the Work tab."')
  assert.include(quiet, ">Nothing now<")
  assert.notInclude(quiet, "need you")
  assert.include(quiet, "bg-muted-foreground/40")
  assert.include(chip(board({ goal: goal("g1", "Ship CSV export", "paused") })), "bg-amber-500")
  assert.include(chip(board({ goal: goal("g0", "Old", "stopped") })), ">Nothing now<")
})
