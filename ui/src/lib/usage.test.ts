import { assert, test } from "vitest"
import { contextLine, usageKey, type TurnUsage } from "./usage"

const turn = (p: Partial<TurnUsage>): TurnUsage => ({
  inputTokens: 4,
  cacheReadTokens: 9443,
  cacheWriteTokens: 3659,
  outputTokens: 306,
  calls: 3,
  contextTokens: 142_000,
  cold: false,
  ...p,
})

test("a warm last turn reads from cache, with the context in thousands of the window", () => {
  assert.equal(
    contextLine({ windowTokens: 200_000, last: turn({}) }),
    "Context: 142k of 200k · last turn: 3 calls, from cache",
  )
})

test("a cold last turn rebuilt the cache", () => {
  assert.equal(
    contextLine({ windowTokens: 200_000, last: turn({ cold: true, calls: 1, contextTokens: 51_499 }) }),
    "Context: 51k of 200k · last turn: 1 call, rebuilt the cache",
  )
})

test("thousands round to the nearest", () => {
  assert.equal(
    contextLine({ windowTokens: 400_000, last: turn({ contextTokens: 157_500, calls: 12 }) }),
    "Context: 158k of 400k · last turn: 12 calls, from cache",
  )
})

test("no line before a turn is recorded, or before the usage loads", () => {
  assert.isNull(contextLine({ windowTokens: 200_000, last: null }))
  assert.isNull(contextLine(null))
  assert.isNull(contextLine(undefined))
})

test("the usage reloads when a turn of the bot ends, with no new chat entry, and when the chat changes", () => {
  const running = usageKey({ id: "b1", running: true }, 7)
  const ended = usageKey({ id: "b1", running: false }, 7)
  assert.notEqual(ended, running)
  assert.notEqual(usageKey({ id: "b1", running: false }, 8), ended)
  assert.notEqual(usageKey({ id: "b2", running: false }, 7), ended)
  assert.equal(usageKey({ id: "b1", running: false }, 7), ended)
})
