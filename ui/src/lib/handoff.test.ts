import { assert, test } from "vitest"
import {
  awaitsSender,
  canWithdrawHandoff,
  firstLine,
  fromLabel,
  handedOverState,
  handoffError,
  incomingState,
  isHandoff,
  MAX_ASK_ROUNDS,
  teamOption,
  type HandoffGoal,
} from "./handoff.ts"

const g = (x: Partial<HandoffGoal>): HandoffGoal => ({ state: "incoming", queuePos: 0, used: 0, budget: 30, ...x })
const name = (id: string) => ({ s: "Support", d: "Dev" })[id] ?? "a deleted team"

test("a handed-over goal names who handed it over", () => {
  assert.isTrue(isHandoff({ origin: "handoff" }))
  assert.isFalse(isHandoff({ origin: "user" }))
  assert.isFalse(isHandoff({}))
  assert.equal(fromLabel({ origin: "handoff", fromTeamId: "s" }, name), "From Support")
  assert.equal(fromLabel({ origin: "handoff", fromTeamId: "" }, name), "From you")
  assert.equal(fromLabel({ origin: "handoff" }, name), "From you")
  assert.isNull(fromLabel({ origin: "user", fromTeamId: "s" }, name))
})

test("an incoming handoff says whether it's new, asked, answered or needs the user", () => {
  assert.equal(MAX_ASK_ROUNDS, 3)
  assert.equal(incomingState(g({})), "New")
  assert.equal(incomingState(g({ askedAt: 5, askRounds: 2 })), "Asked the sender (2 of 3)")
  assert.equal(incomingState(g({ askedAt: 5 })), "Asked the sender (0 of 3)")
  assert.equal(incomingState(g({ askRounds: 1 })), "Answered (1 of 3 asked)")
  assert.equal(incomingState(g({ askedAt: 5, askRounds: 3, needsUser: true })), "Needs you")
})

test("the sender sees each handoff's state with its place and turns", () => {
  assert.equal(handedOverState(g({}), "Dev"), "Waiting for Dev's lead")
  assert.equal(handedOverState(g({ askedAt: 1, askRounds: 1 }), "Dev"), "Asked you (1 of 3)")
  assert.equal(handedOverState(g({ askedAt: 1 }), "Dev"), "Asked you (0 of 3)")
  assert.equal(handedOverState(g({ needsUser: true, askedAt: 1 }), "Dev"), "Needs you at Dev")
  assert.equal(handedOverState(g({ state: "queued", queuePos: 2 }), "Dev"), "Queued #2")
  assert.equal(handedOverState(g({ state: "active", used: 4 }), "Dev"), "Active · 4/30 turns, paid by Dev")
  assert.equal(handedOverState(g({ state: "paused", used: 30 }), "Dev"), "Paused · 30/30 turns, paid by Dev")
  assert.deepEqual(
    ["delivered", "done", "declined", "ended", "odd"].map((state) => handedOverState(g({ state }), "Dev")),
    ["Delivered", "Done", "Declined", "Ended", "odd"],
  )
})

test("only an incoming or queued handoff can be withdrawn", () => {
  assert.isTrue(canWithdrawHandoff({ state: "incoming" }))
  assert.isTrue(canWithdrawHandoff({ state: "queued" }))
  for (const state of ["active", "paused", "delivered", "done", "declined", "ended"]) {
    assert.isFalse(canWithdrawHandoff({ state }), state)
  }
})

test("a handoff waits for the sender only while asked and not handed to the user", () => {
  assert.isTrue(awaitsSender(g({ askedAt: 1 })))
  assert.isFalse(awaitsSender(g({})))
  assert.isFalse(awaitsSender(g({ askedAt: 1, needsUser: true })))
})

test("a card shows a brief's first line", () => {
  assert.equal(firstLine(undefined), "")
  assert.equal(firstLine(""), "")
  assert.equal(firstLine("\n  \n  Fix the scroll.  \nMore"), "Fix the scroll.")
})

test("the dialog names each team with the handoffs that wait for it", () => {
  assert.equal(teamOption({ name: "Dev" }), "Dev")
  assert.equal(teamOption({ name: "Dev", incomingHandoffs: 0 }), "Dev")
  assert.equal(teamOption({ name: "Dev", incomingHandoffs: 1 }), "Dev (1 handoff waiting)")
  assert.equal(teamOption({ name: "Dev", incomingHandoffs: 4 }), "Dev (4 handoffs waiting)")
})

test("a hand-over needs a team and a goal of at most 4000 characters", () => {
  assert.equal(handoffError("", "Do it"), "Choose the team to hand it to.")
  assert.equal(handoffError("d", "  "), "Write what the team should do.")
  assert.equal(
    handoffError("d", "x".repeat(4001)),
    "The goal can be at most 4000 characters; put the rest in the brief.",
  )
  assert.isNull(handoffError("d", "x".repeat(4000)))
})
