import { assert, test } from "vitest"
import {
  drainAnswered,
  drainAsked,
  drainChanged,
  LINKS,
  linkLabel,
  linkStep,
  NO_DRAIN,
  type Link,
  type LinkEvent,
} from "./connection.ts"

const states: Link[] = ["connecting", "live", "reconnecting"]
const events: LinkEvent[] = ["hello", "error"]

test("every valid transition of the event stream", () => {
  const valid: [Link, LinkEvent, Link][] = [
    ["connecting", "hello", "live"],
    ["connecting", "error", "reconnecting"],
    ["live", "error", "reconnecting"],
    ["reconnecting", "hello", "live"],
    ["reconnecting", "error", "reconnecting"],
  ]
  for (const [from, event, to] of valid) assert.deepEqual(linkStep(from, event), { ok: true, state: to })
})

test("a second hello on a live stream is rejected and keeps the state", () => {
  assert.deepEqual(linkStep("live", "hello"), {
    ok: false,
    state: "live",
    error: "connection: hello is not valid while live",
  })
})

test("the table covers exactly the five valid transitions", () => {
  let n = 0
  for (const s of states) for (const e of events) if (LINKS[s][e] !== undefined) n++
  assert.equal(n, 5)
})

test("the sidebar tells a first connect from a reconnect", () => {
  assert.equal(linkLabel("connecting"), "Connecting…")
  assert.equal(linkLabel("live"), "Connected")
  assert.equal(linkLabel("reconnecting"), "Reconnecting…")
})

test("the answer to the latest question sets the drain flag", () => {
  const asked = drainAsked(NO_DRAIN)
  assert.deepEqual(asked, { draining: false, rev: 1 })
  assert.deepEqual(drainAnswered(asked, asked.rev, true), { draining: true, rev: 1 })
})

test("a question keeps the current flag until it is answered", () => {
  const on = drainChanged(NO_DRAIN, true)
  assert.deepEqual(drainAsked(on), { draining: true, rev: 2 })
})

test("a drain event always applies", () => {
  assert.deepEqual(drainChanged(NO_DRAIN, true), { draining: true, rev: 1 })
  assert.deepEqual(drainChanged({ draining: true, rev: 4 }, false), { draining: false, rev: 5 })
})

test("an answer that a drain event overtook is ignored", () => {
  const asked = drainAsked(NO_DRAIN)
  const event = drainChanged(asked, true)
  assert.deepEqual(drainAnswered(event, asked.rev, false), { draining: true, rev: 2 })
})

test("an answer to an older question is ignored", () => {
  const first = drainAsked(NO_DRAIN)
  const second = drainAsked(first)
  const late = drainAnswered(second, first.rev, true)
  assert.deepEqual(late, { draining: false, rev: 2 })
  assert.deepEqual(drainAnswered(late, second.rev, true), { draining: true, rev: 2 })
})
