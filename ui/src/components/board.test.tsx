import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Board, Bot, Goal, Request } from "@/lib/api"
import { BoardView } from "./board.tsx"

const team: Bot = {
  id: "t1",
  kind: "room",
  name: "Dev",
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
  team: { leadId: "a", agreement: "", goalBudget: 30, requestSlots: 1 },
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
const req = (id: string, title: string, r: Partial<Request>): Request => ({
  id,
  teamId: "t1",
  goalId: "g-other",
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
  fromTeamId: "t2",
  ...r,
})
const board = (b: Partial<Board>): Board => ({
  goal: goal("g1", "Ship CSV export", "active"),
  items: [],
  requests: [],
  queue: [],
  slots: { limit: 1, used: 1, applies: true },
  asked: [],
  teams: { t2: "Support", t3: "Ops" },
  ...b,
})
const render = (b: Board) =>
  renderToStaticMarkup(<BoardView team={team} data={b} error={null} act={async () => true} />)

test("queued goals are listed in order with keyboard controls and an announcer", () => {
  const html = render(
    board({ queue: [goal("q1", "Add PDF export", "queued", 1), goal("q2", "Fix login", "queued", 2)] }),
  )
  assert.include(html, "Next up (2)")
  assert.isBelow(html.indexOf("Add PDF export"), html.indexOf("Fix login"))
  assert.include(html, 'aria-label="Move Add PDF export up"')
  assert.include(html, 'aria-label="Move Fix login down"')
  assert.include(html, 'aria-label="Cancel goal Fix login"')
  assert.include(html, 'aria-live="polite"')
  assert.include(html, "Starts when you accept or end the current goal.")
  // The first can't go up and the last can't go down.
  assert.match(html, /id="queue-q1-up"[^>]*disabled/)
  assert.match(html, /id="queue-q2-down"[^>]*disabled/)
  assert.notMatch(html, /id="queue-q1-down"[^>]*disabled/)
})

test("with no open goal the queue is on hold and can start now", () => {
  const html = render(board({ goal: goal("g0", "Old", "done"), queue: [goal("q1", "Add PDF export", "queued", 1)] }))
  assert.include(html, "On hold.")
  assert.include(html, "Start now")
  assert.notInclude(html, "Starts when you accept")
})

test("no queue, no requests and no asks hide their sections", () => {
  const html = render(board({}))
  assert.notInclude(html, "Next up")
  assert.notInclude(html, "Requests ·")
  assert.notInclude(html, "Asked of other teams")
})

test("requests are grouped under the slot header with positions and reason lines", () => {
  const html = render(
    board({
      requests: [
        req("r1", "Translate docs", {
          intake: "accepted",
          state: "doing",
          slotAt: 5,
          ownerId: "a",
          reason: { code: "in_slot", text: "In progress (Ana)." },
        }),
        req("r2", "Check logs", {
          intake: "accepted",
          position: 1,
          total: 1,
          reason: { code: "own_goal_first", text: "Accepted, waiting for a slot." },
        }),
        req("r3", "New API", {
          intake: "requested",
          position: 1,
          total: 1,
          reason: { code: "triage", text: "Waiting for Ana to accept, decline or defer it (position 1 of 1)." },
        }),
        req("r4", "Later thing", {
          intake: "deferred",
          reason: { code: "deferred", text: "Deferred until our goal ends: busy." },
        }),
        req("r5", "Nope", {
          intake: "declined",
          state: "dropped",
          reason: { code: "declined", text: "Declined: no." },
        }),
      ],
    }),
  )
  assert.include(html, "Requests · 1 of 1 slot in use · 1 waiting")
  const order = [
    "In progress (1)",
    "Waiting for a slot (1)",
    "Awaiting triage (1)",
    "Deferred (1)",
    "Closed requests (1)",
  ]
  for (let i = 1; i < order.length; i++) assert.isBelow(html.indexOf(order[i - 1]), html.indexOf(order[i]), order[i])
  assert.include(html, "From Support")
  assert.include(html, "In progress (Ana).")
  assert.include(html, 'title="Position 1 of 1">#1')
  assert.include(html, "Bring back")
  // Closed requests are collapsed.
  assert.match(html, /<details[^>]*><summary[^>]*>Closed requests/)
})

test("the asking team sees each request's state as text, the reason, and can withdraw open ones", () => {
  const html = render(
    board({
      asked: [
        req("a1", "Need logs", {
          teamId: "t3",
          fromTeamId: "t1",
          intake: "accepted",
          position: 2,
          total: 3,
          reason: { code: "behind", text: "Accepted, position 2 of 3.", waitsOn: "… waiting on team Support." },
        }),
        req("a2", "Need design", {
          teamId: "t2",
          fromTeamId: "t1",
          intake: "declined",
          state: "dropped",
          reason: { code: "declined", text: "Declined: out of scope." },
        }),
      ],
    }),
  )
  assert.include(html, "Asked of other teams (2)")
  assert.include(html, "Asked of team Ops")
  assert.include(html, "Waiting")
  assert.include(html, "#2")
  assert.include(html, "Accepted, position 2 of 3. … waiting on team Support.")
  assert.include(html, "Declined")
  assert.include(html, "Declined: out of scope.")
  assert.include(html, "Items waiting on it are blocked.")
  assert.equal(html.split("Withdraw…").length - 1, 1)
})

test("items asked of other teams show only in the asked section", () => {
  const asked = req("a1", "Need logs", { teamId: "t3", fromTeamId: "t1", intake: "requested" })
  const own = { ...req("i1", "Write code", { goalId: "g1" }), teamId: "t1" }
  const html = render(board({ items: [own, asked], asked: [asked] }))
  assert.equal(html.split("Need logs").length - 1, 1)
  assert.include(html, "Write code")
})
