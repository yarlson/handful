import { assert, test } from "vitest"
import type { Entry } from "./api.ts"
import {
  emptyText,
  expiryText,
  retryOf,
  TRANSCRIPT,
  transcriptStep,
  type TranscriptEvent,
  type TranscriptStatus,
} from "./transcript.ts"

const statuses: TranscriptStatus[] = ["unloaded", "loading", "ready", "failed"]
const events: TranscriptEvent[] = ["fetch", "loaded", "failed"]

const valid: [TranscriptStatus, TranscriptEvent, TranscriptStatus][] = [
  ["unloaded", "fetch", "loading"],
  ["loading", "fetch", "loading"],
  ["loading", "loaded", "ready"],
  ["loading", "failed", "failed"],
  ["ready", "fetch", "ready"],
  ["ready", "loaded", "ready"],
  ["ready", "failed", "ready"],
  ["failed", "fetch", "loading"],
  ["failed", "loaded", "ready"],
  ["failed", "failed", "failed"],
]

test("every valid transition of a transcript's load", () => {
  for (const [from, event, to] of valid) assert.deepEqual(transcriptStep(from, event), { ok: true, status: to })
})

test("a result for a chat that was never fetched is rejected", () => {
  assert.deepEqual(transcriptStep("unloaded", "loaded"), {
    ok: false,
    status: "unloaded",
    error: "transcript: loaded is not valid while unloaded",
  })
  assert.deepEqual(transcriptStep("unloaded", "failed"), {
    ok: false,
    status: "unloaded",
    error: "transcript: failed is not valid while unloaded",
  })
})

test("the table has exactly the valid transitions", () => {
  let n = 0
  for (const s of statuses) for (const e of events) if (TRANSCRIPT[s][e] !== undefined) n++
  assert.equal(n, valid.length)
})

test("an empty chat says why it is empty", () => {
  assert.equal(emptyText("ready"), "No messages yet.")
  assert.equal(emptyText("loading"), "Loading messages…")
  assert.equal(emptyText("unloaded"), "Loading messages…")
  assert.equal(emptyText("failed"), "Couldn't load the messages.")
})

const at = { botId: "b", seq: 1, rev: 1, createdAt: 0 }
const said = (id: string, turnId: string): Entry => ({ ...at, id, turnId, kind: "user", body: { text: id } })
const failed = (id: string, turnId: string): Entry => ({
  ...at,
  id,
  turnId,
  kind: "event",
  body: { type: "turn-failed", text: "Claude Code exited: status 1" },
})
const posted = (id: string, turnId: string): Entry => ({
  ...at,
  id,
  turnId,
  kind: "bot",
  body: { text: id, fallback: false },
})

test("Retry is on the latest failed turn that a user message started, and sends that message", () => {
  const ask = said("ask", "t1")
  const retry = retryOf([said("old", "t0"), failed("f0", "t0"), ask, posted("p", "t1"), failed("f1", "t1")])
  assert.deepEqual(retry, { failureId: "f1", message: ask })
})

test("a failed turn that a routine started has no Retry; the user's failed turn before it keeps it", () => {
  const ask = said("ask", "t1")
  assert.deepEqual(retryOf([ask, failed("f1", "t1"), failed("routine", "t2")]), { failureId: "f1", message: ask })
  assert.isNull(retryOf([posted("p", "t0"), failed("routine", "t2")]))
  assert.isNull(retryOf([]))
})

test("no failure offers Retry once the user wrote again", () => {
  assert.isNull(retryOf([said("ask", "t1"), failed("f1", "t1"), said("again", "t2")]))
})

test("an event that is not a failure never offers Retry", () => {
  const renamed: Entry = { ...at, id: "r", turnId: "t1", kind: "event", body: { type: "renamed", from: "A", to: "B" } }
  assert.isNull(retryOf([said("ask", "t1"), renamed]))
})

test("an expired approval says why it expired, and an old card says only Expired", () => {
  assert.equal(expiryText("limit"), "Expired: no answer within 4.5 minutes. Ask the bot to try again.")
  assert.equal(expiryText("turn-ended"), "Expired: the turn ended before you answered.")
  assert.equal(expiryText("restart"), "Expired: Handful restarted before you answered. Send your message again.")
  assert.equal(expiryText(undefined), "Expired")
})
