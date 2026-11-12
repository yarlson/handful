const assert = require("node:assert/strict")
const { test } = require("node:test")
const { createPairBadge, createPairWatch, isPairEvent, retryDelay } = require("./pair-badge")

function setup({ focused = false } = {}) {
  const calls = []
  const state = { focused }
  const dock = {
    setBadge: (text) => calls.push(["setBadge", text]),
    bounce: (type) => calls.push(["bounce", type]),
  }
  const badge = createPairBadge({ dock, isFocused: () => state.focused })
  return { badge, calls, state }
}

test("a pair request in the background sets the badge and bounces once", () => {
  const { badge, calls } = setup()
  badge.change(true)
  assert.deepEqual(calls, [
    ["setBadge", "1"],
    ["bounce", "informational"],
  ])
})

test("the end of a pair request clears the badge", () => {
  const { badge, calls } = setup()
  badge.change(true)
  badge.change(false)
  assert.deepEqual(calls.at(-1), ["setBadge", ""])
})

test("focus clears the badge while the request still waits", () => {
  const { badge, calls } = setup()
  badge.change(true)
  badge.focus()
  assert.deepEqual(calls.at(-1), ["setBadge", ""])
})

test("a new page resets the badge, so a later request sets it again", () => {
  const { badge, calls } = setup()
  badge.change(true)
  badge.reset()
  assert.deepEqual(calls.at(-1), ["setBadge", ""])
  badge.change(true)
  assert.deepEqual(calls.slice(-2), [
    ["setBadge", "1"],
    ["bounce", "informational"],
  ])
})

test("a request while the window is in front sets no badge", () => {
  const { badge, calls } = setup({ focused: true })
  badge.change(true)
  badge.change(false)
  assert.deepEqual(calls, [["setBadge", ""]])
})

test("without a pair request the badge is never set", () => {
  const { badge, calls } = setup()
  badge.change(false)
  badge.change(0)
  badge.change("1")
  badge.focus()
  assert.ok(!calls.some(([name, text]) => name === "bounce" || text === "1"), JSON.stringify(calls))
})

test("a repeated start does not bounce again", () => {
  const { badge, calls } = setup()
  badge.change(true)
  badge.change(true)
  assert.equal(calls.filter(([name]) => name === "bounce").length, 1)
})

test("with no Dock nothing fails", () => {
  const badge = createPairBadge({ dock: null, isFocused: () => false })
  badge.change(true)
  badge.focus()
  badge.change(false)
  badge.reset()
})

// fakeServer answers /api/phone with the current request, and keeps
// /api/events open, sending each event that send() pushes.
function fakeServer() {
  const server = { request: null, phoneOff: false, reads: 0, streams: 0, controllers: [] }
  server.fetchImpl = async (url, init) => {
    if (url.endsWith("/api/phone")) {
      server.reads++
      if (server.phoneOff) return new Response("404 page not found", { status: 404 })
      return Response.json({ pairing: server.request ? { request: server.request } : {} })
    }
    if (url.endsWith("/api/events")) {
      server.streams++
      const body = new ReadableStream({
        start(controller) {
          server.controllers.push(controller)
          controller.enqueue(new TextEncoder().encode("event: hello\ndata: {}\n\n"))
          init?.signal?.addEventListener("abort", () => controller.error(new Error("aborted")))
        },
      })
      return new Response(body)
    }
    return new Response("", { status: 404 })
  }
  server.send = (event) => server.controllers.at(-1).enqueue(new TextEncoder().encode(`data: ${JSON.stringify(event)}\n\n`))
  server.end = () => server.controllers.at(-1).close()
  return server
}

function fakeClock() {
  const clock = { now: 1_000_000, pending: [] }
  clock.timers = {
    setTimeout(fn, ms) {
      const timer = { fn, at: clock.now + ms }
      clock.pending.push(timer)
      return timer
    },
    clearTimeout(timer) {
      clock.pending = clock.pending.filter((t) => t !== timer)
    },
  }
  clock.advance = async (ms) => {
    clock.now += ms
    for (const timer of clock.pending.filter((t) => t.at <= clock.now)) {
      clock.pending = clock.pending.filter((t) => t !== timer)
      timer.fn()
    }
    await settle()
  }
  return clock
}

// settle lets the fake streams and reads run; it waits for no clock.
const settle = async () => {
  for (let i = 0; i < 50; i++) await new Promise((resolve) => setImmediate(resolve))
}

function watch(server, clock) {
  const told = []
  const w = createPairWatch({ fetchImpl: server.fetchImpl, base: "http://x", onWaiting: (v) => told.push(v), timers: clock.timers, now: () => clock.now, log: () => {} })
  return { w, told }
}

test("with the window closed, a request that already waits is found at start", async (t) => {
  const server = fakeServer()
  const clock = fakeClock()
  server.request = { id: "r1", expiresAt: clock.now + 60_000 }
  const { w, told } = watch(server, clock)
  t.after(() => w.stop())
  w.start()
  await settle()
  assert.deepEqual(told, [true])
})

test("with the window closed, a pair event is read, and its end clears it", async (t) => {
  const server = fakeServer()
  const clock = fakeClock()
  const { w, told } = watch(server, clock)
  t.after(() => w.stop())
  w.start()
  await settle()
  assert.deepEqual(told, [false])
  server.request = { id: "r1", expiresAt: clock.now + 60_000 }
  server.send({ type: "bots" })
  await settle()
  assert.deepEqual(told, [false], "another event reads nothing")
  server.send({ type: "pair" })
  await settle()
  assert.deepEqual(told, [false, true])
  server.request = null
  server.send({ type: "pair" })
  await settle()
  assert.deepEqual(told, [false, true, false])
})

test("with the window closed, a request that runs out is cleared with no event", async (t) => {
  const server = fakeServer()
  const clock = fakeClock()
  server.request = { id: "r1", expiresAt: clock.now + 60_000 }
  const { w, told } = watch(server, clock)
  t.after(() => w.stop())
  w.start()
  await settle()
  assert.deepEqual(told, [true])
  server.request = { id: "r1", expiresAt: clock.now + 60_000 }
  await clock.advance(60_100)
  assert.deepEqual(told, [true, false])
})

test("the badge follows the watch: set while a request waits with the window closed, cleared when it ends", async (t) => {
  const { badge, calls } = setup()
  const server = fakeServer()
  const clock = fakeClock()
  const w = createPairWatch({ fetchImpl: server.fetchImpl, base: "http://x", onWaiting: (v) => badge.change(v), timers: clock.timers, now: () => clock.now, log: () => {} })
  t.after(() => w.stop())
  w.start()
  await settle()
  server.request = { id: "r1", expiresAt: clock.now + 60_000 }
  server.send({ type: "pair" })
  await settle()
  assert.deepEqual(calls, [
    ["setBadge", "1"],
    ["bounce", "informational"],
  ])
  server.request = null
  server.send({ type: "pair" })
  await settle()
  assert.deepEqual(calls.at(-1), ["setBadge", ""])
})

test("with phone access off, the watch never reports a request", async (t) => {
  const server = fakeServer()
  const clock = fakeClock()
  server.phoneOff = true
  const { w, told } = watch(server, clock)
  t.after(() => w.stop())
  w.start()
  await settle()
  server.send({ type: "pair" })
  await settle()
  assert.ok(told.every((v) => v === false), JSON.stringify(told))
})

test("a dropped event stream is read again and reopened", async (t) => {
  const server = fakeServer()
  const clock = fakeClock()
  const { w, told } = watch(server, clock)
  t.after(() => w.stop())
  w.start()
  await settle()
  server.request = { id: "r1", expiresAt: clock.now + 600_000 }
  server.end()
  await settle()
  assert.equal(server.streams, 1)
  await clock.advance(retryDelay)
  assert.equal(server.streams, 2)
  assert.deepEqual(told, [false, true], "the request that came while the stream was down is found")
})

test("a stopped watch reads and reports nothing more", async () => {
  const server = fakeServer()
  const clock = fakeClock()
  server.request = { id: "r1", expiresAt: clock.now + 60_000 }
  const { w, told } = watch(server, clock)
  w.start()
  await settle()
  w.stop()
  const reads = server.reads
  await clock.advance(120_000)
  assert.equal(server.reads, reads)
  assert.deepEqual(told, [true])
  assert.deepEqual(clock.pending, [])
})

test("isPairEvent reads only a data line whose type is pair", () => {
  assert.equal(isPairEvent('data: {"type":"pair"}'), true)
  assert.equal(isPairEvent('data: {"type":"bots"}'), false)
  assert.equal(isPairEvent("event: hello\ndata: {}"), false)
  assert.equal(isPairEvent(": ping"), false)
  assert.equal(isPairEvent("data: not json"), false)
})

test("when two reads answer out of order, only the later read counts", async (t) => {
  const clock = fakeClock()
  const answers = []
  let events = null
  // Each /api/phone read waits until the test answers it; /api/events stays open.
  const fetchImpl = async (url) => {
    if (url.endsWith("/api/phone")) return new Promise((resolve) => answers.push(resolve))
    return new Response(new ReadableStream({ start: (controller) => (events = controller) }))
  }
  const told = []
  const w = createPairWatch({ fetchImpl, base: "http://x", onWaiting: (v) => told.push(v), timers: clock.timers, now: () => clock.now, log: () => {} })
  t.after(() => w.stop())
  w.start()
  await settle()
  // A pair event starts a second read while the first still waits.
  events.enqueue(new TextEncoder().encode('data: {"type":"pair"}\n\n'))
  await settle()
  assert.equal(answers.length, 2)
  const reply = (request) => Response.json({ pairing: request ? { request } : {} })
  // The later read says the request ended; the earlier one, answering last, still saw it.
  answers[1](reply(null))
  await settle()
  answers[0](reply({ id: "r1", expiresAt: clock.now + 60_000 }))
  await settle()
  assert.deepEqual(told, [false])
})
