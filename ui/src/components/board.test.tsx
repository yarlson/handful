import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Board, Bot, Goal, Item } from "@/lib/api"
import { BoardView, HistoryList } from "./board.tsx"

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
const board = (b: Partial<Board>): Board => ({
  goal: goal("g1", "Ship CSV export", "active"),
  items: [],
  teams: { t2: "Support", t3: "Ops" },
  ...b,
})
const render = (b: Board) =>
  renderToStaticMarkup(<BoardView team={team} data={b} error={null} act={async () => true} />)

test("planned tasks are listed in the lead's order with move, pin and withdraw controls and an announcer", () => {
  const html = render(
    board({
      plan: [{ ...goal("q1", "Add PDF export", "queued", 1), pinned: true }, goal("q2", "Fix login", "queued", 2)],
    }),
  )
  assert.include(html, "Next, in Ana&#x27;s order (2)")
  assert.isBelow(html.indexOf("Add PDF export"), html.indexOf("Fix login"))
  assert.include(html, 'aria-label="Move Add PDF export up"')
  assert.include(html, 'aria-label="Move Fix login down"')
  assert.include(html, 'aria-label="Unpin Add PDF export"')
  assert.include(html, 'aria-label="Pin Fix login at #2"')
  assert.include(html, ">pinned<")
  assert.include(html, 'aria-label="Withdraw: Fix login"')
  assert.include(html, 'aria-live="polite"')
  assert.include(html, "The first one starts when the Now task is done or stopped.")
  // The first can't go up and the last can't go down.
  assert.match(html, /id="plan-q1-up"[^>]*disabled/)
  assert.match(html, /id="plan-q2-down"[^>]*disabled/)
  assert.notMatch(html, /id="plan-q1-down"[^>]*disabled/)
})

test("with no Now task the plan is on hold and can start now", () => {
  const html = render(board({ goal: goal("g0", "Old", "done"), plan: [goal("q1", "Add PDF export", "queued", 1)] }))
  assert.include(html, "On hold.")
  assert.include(html, "Start now")
  assert.include(html, "Latest task")
  assert.include(html, ">Approved<")
  assert.notInclude(html, "The first one starts")
})

test("an empty plan hides its section, the old request lanes are gone, and the inbox says it's empty", () => {
  const html = render(board({}))
  assert.notInclude(html, "Next, in")
  assert.notInclude(html, "Requests for other teams")
  assert.notInclude(html, "Asked of other teams")
  assert.notInclude(html, "Sent to other teams")
  assert.notInclude(html, "Done, waiting for approval")
  assert.include(html, "Inbox (0)")
  assert.include(html, "Nothing waits for a decision.")
})

test("the Now task shows its turns, and the inbox counts what waits for the lead", () => {
  const html = renderToStaticMarkup(
    <BoardView
      team={team}
      data={board({
        goal: { ...goal("g1", "Ship CSV export", "active"), used: 38 },
        inbox: [goal("i1", "Fix a bug", "incoming"), goal("i2", "Fix another", "later")],
      })}
      error={null}
      act={async () => true}
      onOpenInbox={() => {}}
    />,
  )
  assert.include(html, ">Now<")
  assert.include(html, "38 turns so far")
  assert.include(html, "Inbox (2)")
  assert.include(html, "2 tasks wait for Ana&#x27;s decision.")
  assert.include(html, "Open inbox")
})

test("done tasks wait for approval with Approve and Ask for changes", () => {
  const html = render(board({ done: [goal("d1", "Spec the updater", "delivered")] }))
  assert.include(html, "Done, waiting for approval (1)")
  assert.include(html, 'aria-label="Approve: Spec the updater"')
  assert.include(html, 'aria-label="Ask for changes: Spec the updater"')
})

const item = (x: Partial<Item>) => ({
  ...step("i1", "Write code", { ownerId: "a" }),
  ...x,
})

test("a card with change requests opens its history, and shows the request being worked on", () => {
  const html = render(
    board({
      items: [
        item({
          state: "doing",
          reviewRounds: 2,
          eventCount: 5,
          latestChange: { round: 2, by: "Bo", at: 0, text: "Add a test for the empty file." },
        }),
      ],
    }),
  )
  assert.match(html, /<button[^>]*aria-expanded="false"[^>]*aria-controls="history-i1"[^>]*>2 change requests</)
  assert.include(html, "Changes asked · round 2 · Bo")
  assert.include(html, "Add a test for the empty file.")
})

test("a card without change requests links its history, and none without events", () => {
  assert.include(render(board({ items: [item({ eventCount: 3 })] })), "History (3)")
  assert.notInclude(render(board({ items: [item({ eventCount: 0 })] })), "History (")
})

test("a note shows only on blocked and dropped cards", () => {
  assert.include(render(board({ items: [item({ state: "blocked", note: "Which API?" })] })), "Which API?")
  assert.notInclude(render(board({ items: [item({ state: "doing", note: "old note" })] })), "old note")
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
  assert.include(html, "<ol")
  assert.isBelow(html.indexOf("Round one text."), html.indexOf("Round two text."))
  assert.include(html, "Changes asked · round 1")
  assert.include(html, "Changes asked · round 2")
  assert.include(html, "Passed")
  assert.include(html, "History starts on")
})

test("a task from another team says where it came from, and the sent section follows what the team sent", () => {
  const html = renderToStaticMarkup(
    <BoardView
      team={team}
      data={board({
        plan: [{ ...goal("q1", "Build the spec", "queued", 1), origin: "handoff", fromTeamId: "p1" }],
        handedOver: [
          {
            ...goal("h1", "Write docs", "queued", 2),
            teamId: "t3",
            origin: "handoff",
            fromTeamId: "t1",
          },
          {
            ...goal("h2", "Copy review", "declined"),
            teamId: "t2",
            origin: "handoff",
            fromTeamId: "t1",
            intakeNote: "GTM owns copy.",
          },
          {
            ...goal("h3", "Model chip", "active"),
            teamId: "t3",
            origin: "handoff",
            fromTeamId: "t1",
            used: 12,
          },
        ],
      })}
      error={null}
      act={async () => true}
      teams={[{ ...team, id: "p1", name: "Product" }]}
    />,
  )
  assert.include(html, "from Product")
  assert.include(html, "Sent to other teams (3)")
  assert.include(html, "#2 in Ops&#x27;s plan")
  assert.include(html, 'aria-label="Withdraw: Write docs"')
  assert.include(html, "Pushed back")
  assert.include(html, "GTM owns copy.")
  assert.notInclude(html, 'aria-label="Withdraw: Copy review"')
  assert.include(html, "Ops is on it")
  assert.include(html, "To Ops · 12 turns, paid by Ops")
})

test("every stopped task but the latest is listed with Reopen and its history", () => {
  const html = render(
    board({
      goal: goal("s0", "Latest stopped", "stopped"),
      stopped: [goal("s0", "Latest stopped", "stopped"), { ...goal("s1", "Older stopped", "stopped"), used: 7 }],
    }),
  )
  assert.include(html, "Stopped (1)")
  assert.include(html, 'aria-label="Reopen: Older stopped"')
  assert.notInclude(html, 'aria-label="Reopen: Latest stopped"')
  assert.include(html, "7 turns used · Reopen puts it back in the inbox.")
  assert.include(html, 'aria-label="History: Older stopped"')
  assert.include(html, 'aria-label="History: Latest stopped"')
})

test("no stopped tasks hide the section", () => {
  assert.notInclude(render(board({ stopped: [] })), "Stopped (")
  assert.notInclude(render(board({})), "Stopped (")
})

test("the Now task, planned and done tasks each open their history", () => {
  const html = render(
    board({ plan: [goal("q1", "Add PDF export", "queued", 1)], done: [goal("d1", "Spec the updater", "delivered")] }),
  )
  for (const t of ["Ship CSV export", "Add PDF export", "Spec the updater"])
    assert.include(html, `aria-label="History: ${t}"`)
})

const offTask = {
  count: 2,
  turns: [
    { turnId: "t1", botId: "b", botName: "Bo", startedAt: 0, firstLine: "Rebased the context-menu PR" },
    { turnId: "t2", botId: "b", botName: "Bo", startedAt: 0, firstLine: "" },
  ],
}

test("turns on no task show in a box with who, and Make a task; none hide it", () => {
  const html = render(board({ offTask }))
  assert.include(html, 'aria-labelledby="off-task-heading"')
  assert.include(html, ">Not on a task</h3>")
  assert.include(html, "<strong>2 turns in the last hour</strong> weren&#x27;t on any task: Bo.")
  assert.include(html, 'aria-label="Make a task: 2 turns on no task"')
  assert.include(html, 'aria-expanded="false" aria-controls="off-task-turns"')
  assert.notInclude(html, "Rebased the context-menu PR")
  assert.notInclude(render(board({ offTask: { count: 0, turns: [] } })), "Not on a task")
  assert.notInclude(render(board({})), "Not on a task")
})

const budget = { today: 212, limit: 400, suggested: 0, paused: false, warn: false }

test("today's budget shows the turns used against the limit, and the paused state", () => {
  const html = render(board({ budget }))
  assert.include(html, 'aria-labelledby="budget-heading"')
  assert.include(html, "212 of 400 turns used")
  assert.include(html, 'style="width:53%"')
  assert.include(html, 'aria-label="Change the limit: Dev"')
  assert.notInclude(html, "Paused: new turns wait")
  const paused = render(board({ budget: { ...budget, today: 400, paused: true } }))
  assert.include(paused, "Paused: new turns wait until midnight or a higher limit.")
  assert.include(paused, "bg-amber-500")
})

test("a watched day shows its count with no limit, then the suggested limit to accept", () => {
  const watched = render(board({ budget: { ...budget, limit: 0 } }))
  assert.include(watched, "212 turns (no limit yet)")
  assert.notInclude(watched, 'style="width')
  assert.notInclude(watched, "Suggested limit")
  assert.include(watched, 'aria-label="Set a limit: Dev"')
  const suggested = render(board({ budget: { ...budget, limit: 0, suggested: 350 } }))
  assert.include(suggested, "Suggested limit: 350 turns a day.")
  assert.include(suggested, ">Use 350</button>")
  assert.notInclude(render(board({ budget: { ...budget, suggested: 350 } })), "Suggested limit")
})
