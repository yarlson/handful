import { assert, test } from "vitest"
import { emptyText, TRANSCRIPT, transcriptStep, type TranscriptEvent, type TranscriptStatus } from "./transcript.ts"

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
