import { assert, test } from "vitest"
import {
  actorOf,
  answersFor,
  byChat,
  countBadge,
  eventLabel,
  eventTone,
  historyToggle,
  questionOf,
  stateLabel,
  stripCollapsed,
  UNDO_MS,
  waitingCount,
  waitingKey,
  type OpenQuestion,
} from "./questions.ts"

const q = (entryId: string, chatId: string, createdAt: number, seq = 1): OpenQuestion => ({
  entryId,
  seq,
  chatId,
  askerId: "b",
  askerName: "Bo",
  question: entryId,
  createdAt,
})

test("each question state reads as words", () => {
  assert.equal(stateLabel("open"), "Open question")
  assert.equal(stateLabel("answered"), "Answered")
  assert.equal(stateLabel("dismissed"), "Dismissed")
  assert.equal(stateLabel("withdrawn"), "No longer needed")
  assert.isNull(stateLabel(undefined))
})

test("Waiting for you counts questions, approvals and paused goals", () => {
  assert.equal(waitingCount({ questions: [q("a", "c", 1), q("b", "c", 2)], approvals: 3, pausedGoals: 1 }), 6)
  assert.equal(waitingCount({ questions: [], approvals: 0, pausedGoals: 0 }), 0)
})

test("questions group by chat, oldest first, groups by their oldest question", () => {
  // Questions of the same time keep their order in the chat.
  const groups = byChat([q("x2", "x", 30), q("y2", "y", 10, 3), q("x1", "x", 20), q("y1", "y", 10, 2)])
  assert.deepEqual(
    groups.map((g) => [g.chatId, g.questions.map((v) => v.entryId)]),
    [
      ["y", ["y1", "y2"]],
      ["x", ["x1", "x2"]],
    ],
  )
  assert.deepEqual(byChat([]), [])
  const input = [q("b", "c", 2), q("a", "c", 1)]
  byChat(input)
  assert.deepEqual(
    input.map((v) => v.entryId),
    ["b", "a"],
  )
})

test("the sidebar counts open questions only past one", () => {
  assert.isNull(countBadge(undefined))
  assert.isNull(countBadge(0))
  assert.isNull(countBadge(1))
  assert.equal(countBadge(2), "2")
})

test("the strip starts collapsed past three questions", () => {
  assert.isFalse(stripCollapsed(3))
  assert.isTrue(stripCollapsed(4))
})

test("a dismissal can be undone for 10 seconds", () => {
  assert.equal(UNDO_MS, 10_000)
})

test("history events read as words, with the round of a change request", () => {
  assert.equal(eventLabel({ kind: "changes", round: 2 }), "Changes asked · round 2")
  assert.deepEqual(
    [
      "created",
      "assigned",
      "submitted",
      "passed",
      "blocked",
      "unblocked",
      "dropped",
      "deferred",
      "undeferred",
      "withdrawn",
    ].map((kind) => eventLabel({ kind, round: 0 })),
    [
      "Created",
      "Assigned",
      "Submitted",
      "Passed",
      "Blocked",
      "Unblocked",
      "Dropped",
      "Deferred",
      "Brought back",
      "Withdrawn",
    ],
  )
  assert.equal(eventLabel({ kind: "renamed", round: 0 }), "renamed")
})

test("change requests and passes are marked, other events are plain", () => {
  assert.equal(eventTone("changes"), "changes")
  assert.equal(eventTone("passed"), "passed")
  assert.equal(eventTone("submitted"), "plain")
})

test("an event names its bot, the user, or a member that left", () => {
  assert.equal(actorOf({ actorId: "b", actorName: "Bo" }), "Bo")
  assert.equal(actorOf({ actorId: "", actorName: "" }), "You")
  assert.equal(actorOf({ actorId: "b", actorName: "" }), "a former member")
})

test("the history button names the change requests, else the history's length", () => {
  assert.equal(historyToggle(1, 5), "1 change request")
  assert.equal(historyToggle(3, 9), "3 change requests")
  assert.equal(historyToggle(0, 4), "History (4)")
})

test("a question is named by its summary, else its first line, cut to 140 characters", () => {
  assert.deepEqual(questionOf("e1", { question: "  Ship it?  ", text: "Long text" }), {
    entryId: "e1",
    question: "Ship it?",
  })
  assert.deepEqual(questionOf("e2", { question: "", text: "\n\n  Which CRM?  \nMore" }), {
    entryId: "e2",
    question: "Which CRM?",
  })
  assert.equal(questionOf("e3", { text: "" }).question, "")
  // A line of spaces is no line.
  assert.equal(questionOf("e6", { text: "   \nReal question?" }).question, "Real question?")
  const long = questionOf("e4", { text: "x".repeat(200) }).question
  assert.equal(long.length, 140)
  assert.isTrue(long.endsWith("…"))
  assert.equal(questionOf("e5", { text: "y".repeat(140) }).question, "y".repeat(140))
})

test("a send answers the chip's question, unless it is a command", () => {
  const a = { entryId: "e1", question: "Ship it?" }
  assert.deepEqual(answersFor(a, false), ["e1"])
  assert.deepEqual(answersFor(a, true), [])
  assert.deepEqual(answersFor(null, false), [])
})

test("the Waiting key changes with any chat's attention or open questions", () => {
  const k = waitingKey([{ id: "a" }, { id: "b", attention: "question", openQuestions: 2 }])
  assert.equal(k, "a::0,b:question:2")
  assert.notEqual(
    waitingKey([{ id: "b", attention: "question", openQuestions: 3 }]),
    waitingKey([{ id: "b", attention: "question", openQuestions: 2 }]),
  )
})
