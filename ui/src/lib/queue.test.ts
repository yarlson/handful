import { assert, test } from "vitest"
import {
  canWithdraw,
  grouped,
  GROUPS,
  intakeLabel,
  moved,
  confirms,
  GOAL_ACTIONS,
  goalBar,
  nextUp,
  onHold,
  openGoal,
  placeNote,
  reasonLine,
  requestGroup,
  showsPosition,
  slotHeader,
  slotsError,
  type RequestLike,
} from "./queue.ts"

const r = (intake: RequestLike["intake"], state = "todo", slotAt = 0): RequestLike => ({ intake, state, slotAt })

test("each request lands in one group of the serving board", () => {
  assert.equal(requestGroup(r("requested")), "triage")
  assert.equal(requestGroup(r("deferred")), "deferred")
  assert.equal(requestGroup(r("accepted")), "waiting")
  assert.equal(requestGroup(r("accepted", "doing", 5)), "progress")
  assert.equal(requestGroup(r("accepted", "blocked", 5)), "progress")
  assert.equal(requestGroup(r("accepted", "done", 5)), "closed")
  assert.equal(requestGroup(r("accepted", "dropped")), "closed")
  assert.equal(requestGroup(r("declined", "dropped")), "closed")
  assert.equal(requestGroup(r("withdrawn", "dropped")), "closed")
  assert.equal(requestGroup({ intake: "accepted", state: "todo" }), "waiting")
})

test("groups come in the board's order, keep the requests' order and skip empty ones", () => {
  assert.deepEqual(
    GROUPS.map((g) => g.label),
    ["In progress", "Waiting for a slot", "Awaiting triage", "Deferred", "Closed requests"],
  )
  const a = { ...r("requested"), id: "a" }
  const b = { ...r("accepted", "doing", 1), id: "b" }
  const c = { ...r("requested"), id: "c" }
  const groups = grouped([a, b, c])
  assert.deepEqual(
    groups.map((g) => [g.group, g.label, g.items.map((i) => i.id)]),
    [
      ["progress", "In progress", ["b"]],
      ["triage", "Awaiting triage", ["a", "c"]],
    ],
  )
  assert.deepEqual(grouped([]), [])
})

test("the slot header counts slots in use and accepted requests that wait", () => {
  const reqs = [r("accepted", "doing", 1), r("accepted"), r("accepted"), r("requested")]
  assert.equal(
    slotHeader({ limit: 1, used: 1, applies: true }, reqs),
    "Requests for other teams' goals · 1 of 1 slot in use · 2 waiting",
  )
  assert.equal(
    slotHeader({ limit: 2, used: 1, applies: true }, []),
    "Requests for other teams' goals · 1 of 2 slots in use · 0 waiting",
  )
  assert.equal(
    slotHeader({ limit: 1, used: 3, applies: false }, reqs),
    "Requests for other teams' goals · no slot limit while the team has no active goal",
  )
})

test("the asking team reads each request's state as a word", () => {
  assert.deepEqual(
    [
      r("requested"),
      r("accepted"),
      r("accepted", "doing", 1),
      r("deferred"),
      r("declined", "dropped"),
      r("withdrawn", "dropped"),
      r("accepted", "done", 1),
      r("accepted", "dropped"),
      { state: "todo" },
    ].map(intakeLabel),
    ["requested", "waiting", "in progress", "deferred", "declined", "withdrawn", "done", "dropped", "waiting"],
  )
})

test("only an open request can be withdrawn", () => {
  assert.isTrue(canWithdraw(r("requested")))
  assert.isTrue(canWithdraw(r("accepted", "doing", 1)))
  assert.isTrue(canWithdraw(r("deferred")))
  assert.isFalse(canWithdraw(r("accepted", "done", 1)))
  assert.isFalse(canWithdraw(r("accepted", "dropped")))
  assert.isFalse(canWithdraw(r("declined", "dropped")))
  assert.isFalse(canWithdraw(r("withdrawn", "dropped")))
  assert.isFalse(canWithdraw({ state: "todo" }))
})

test("a position chip shows only while a request waits", () => {
  assert.isTrue(showsPosition({ ...r("requested"), position: 2 }))
  assert.isTrue(showsPosition({ ...r("accepted"), position: 1 }))
  assert.isFalse(showsPosition({ ...r("accepted"), position: 0 }))
  assert.isFalse(showsPosition(r("accepted")))
  assert.isFalse(showsPosition({ ...r("accepted", "doing", 1), position: 1 }))
  assert.isFalse(showsPosition({ ...r("deferred"), position: 1 }))
})

test("the reason line adds what the work waits on", () => {
  assert.equal(reasonLine(undefined), "")
  assert.equal(reasonLine({ text: "In progress (Ana)." }), "In progress (Ana).")
  assert.equal(
    reasonLine({ text: "In progress (Ana).", waitsOn: "… waiting on team Ops." }),
    "In progress (Ana). … waiting on team Ops.",
  )
})

test("a queued goal moves up or down by one, never past the ends", () => {
  assert.deepEqual(moved(["a", "b", "c"], 1, -1), ["b", "a", "c"])
  assert.deepEqual(moved(["a", "b", "c"], 1, 1), ["a", "c", "b"])
  assert.deepEqual(moved(["a", "b"], 0, 1), ["b", "a"])
  assert.isNull(moved(["a", "b", "c"], 0, -1))
  assert.isNull(moved(["a", "b", "c"], 2, 1))
  assert.isNull(moved(["a"], -1, 1))
  assert.isNull(moved(["a"], 1, -1))
  const ids = ["a", "b"]
  moved(ids, 0, 1)
  assert.deepEqual(ids, ["a", "b"])
})

test("a move is announced with the goal's new place", () => {
  assert.equal(placeNote("Ship it", 2, 3), "Ship it is now 2 of 3.")
})

test("the queue holds when no goal is open and one is queued", () => {
  for (const s of ["active", "delivered", "paused"]) assert.isTrue(openGoal(s))
  for (const s of ["done", "ended", "queued", "incoming", "declined", "toString", undefined]) {
    assert.isFalse(openGoal(s))
  }
  assert.isTrue(onHold(undefined, 1))
  assert.isTrue(onHold("ended", 2))
  assert.isTrue(onHold("done", 1))
  assert.isFalse(onHold("active", 1))
  assert.isFalse(onHold("ended", 0))
})

test("the goal bar names the next goal and how many follow", () => {
  assert.isNull(nextUp([]))
  assert.equal(nextUp([{ text: "B" }]), "Next up: B")
  assert.equal(nextUp([{ text: "B" }, { text: "C" }, { text: "D" }]), "Next up: B (+2)")
})

test("requests in progress at once go from 1 to the number of members", () => {
  assert.isNull(slotsError("1", 3))
  assert.isNull(slotsError("3", 3))
  const msg = "Requests in progress at once must be from 1 to 3."
  assert.equal(slotsError("0", 3), msg)
  assert.equal(slotsError("4", 3), msg)
  assert.equal(slotsError("1.5", 3), msg)
  assert.equal(slotsError("", 3), msg)
  assert.equal(slotsError("  ", 3), msg)
  assert.equal(slotsError("x", 3), msg)
})

const g = (state: string, used = 3, budget = 30) => ({ state, used, budget })

test("an open goal shows its state's buttons in order", () => {
  assert.deepEqual(GOAL_ACTIONS, {
    active: ["extend", "end"],
    delivered: ["accept", "end"],
    paused: ["resume", "extend", "end"],
  })
  assert.deepEqual(goalBar(g("active"), 0), { mode: "open", state: "active", actions: ["extend", "end"] })
  assert.deepEqual(goalBar(g("active"), 2), { mode: "open", state: "active", actions: ["extend", "end"] })
  assert.deepEqual(goalBar(g("delivered"), 0), { mode: "open", state: "delivered", actions: ["accept", "end"] })
  assert.deepEqual(goalBar(g("paused"), 0), { mode: "open", state: "paused", actions: ["resume", "extend", "end"] })
})

test("a delivered goal with a goal queued asks about the next one", () => {
  assert.deepEqual(goalBar(g("delivered"), 1), { mode: "open", state: "delivered", actions: ["acceptNext", "end"] })
})

test("a paused goal resumes only while it has turns left", () => {
  assert.deepEqual(goalBar(g("paused", 29, 30), 0), {
    mode: "open",
    state: "paused",
    actions: ["resume", "extend", "end"],
  })
  assert.deepEqual(goalBar(g("paused", 30, 30), 0), { mode: "open", state: "paused", actions: ["extend", "end"] })
  assert.deepEqual(goalBar(g("paused", 31, 30), 1), { mode: "open", state: "paused", actions: ["extend", "end"] })
})

test("without an open goal the bar holds the queue or is idle", () => {
  for (const state of ["done", "ended", "queued", "incoming", "declined"]) {
    assert.deepEqual(goalBar(g(state), 1), { mode: "hold" }, state)
    assert.deepEqual(goalBar(g(state), 0), { mode: "idle" }, state)
  }
  assert.deepEqual(goalBar(undefined, 2), { mode: "hold" })
  assert.deepEqual(goalBar(null, 0), { mode: "idle" })
})

test("a confirm is open only for its own kind and goal", () => {
  assert.isTrue(confirms({ kind: "end", goalId: "g1" }, "end", "g1"))
  assert.isTrue(confirms({ kind: "accept", goalId: "g1" }, "accept", "g1"))
  assert.isFalse(confirms({ kind: "end", goalId: "g1" }, "accept", "g1"))
  assert.isFalse(confirms({ kind: "end", goalId: "g1" }, "end", "g2"))
  assert.isFalse(confirms(null, "end", "g1"))
})
