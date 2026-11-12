const assert = require("node:assert/strict")
const fs = require("node:fs")
const http = require("node:http")
const os = require("node:os")
const path = require("node:path")
const { test } = require("node:test")
const { DAY, HOUR, UpdaterModel } = require("./updater-state")
const { firstCheckDelay, fileStore, loadSettings, createScheduler, fetchFeedVersion, serverAPI, runningOf } = require("./updater-io")

function tempDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "handful-updater-"))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  return dir
}

// serve serves dir on a free port of 127.0.0.1 and logs each request.
async function serve(t, handler) {
  const log = []
  const server = http.createServer((req, res) => {
    log.push(`${req.method} ${req.url}`)
    handler(req, res)
  })
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve))
  t.after(() => new Promise((resolve) => server.close(resolve)))
  return { base: `http://127.0.0.1:${server.address().port}`, log }
}

function staticFeed(dir) {
  return (req, res) => {
    const file = path.join(dir, path.basename(new URL(req.url, "http://x").pathname))
    if (!fs.existsSync(file)) {
      res.writeHead(404).end()
      return
    }
    res.writeHead(200, { "Content-Type": "text/yaml" }).end(fs.readFileSync(file))
  }
}

// fakeTimers records the delays of setTimeout and runs the callbacks on demand.
function fakeTimers() {
  const pending = []
  return {
    pending,
    setTimeout(fn, ms) {
      const timer = { fn, ms }
      pending.push(timer)
      return timer
    },
    clearTimeout(timer) {
      const i = pending.indexOf(timer)
      if (i >= 0) pending.splice(i, 1)
    },
    async fire() {
      const timer = pending.shift()
      await timer.fn()
      return timer.ms
    },
  }
}

test("fileStore writes a file in a new folder and reads it back", (t) => {
  const file = path.join(tempDir(t), "user data", "update-state.json")
  const store = fileStore(file)
  assert.equal(store.read(), null)
  store.write('{"lastCheck":1}\n')
  assert.equal(store.read(), '{"lastCheck":1}\n')
  assert.equal(fs.existsSync(`${file}.tmp`), false)
  assert.throws(() => fileStore(path.dirname(file)).read(), { code: "EISDIR" })
})

test("loadSettings reads config.json each time", (t) => {
  const file = path.join(tempDir(t), "config.json")
  assert.deepEqual(loadSettings(file), { autoCheck: true, drainMinutes: 30 })
  fs.writeFileSync(file, '{"updates": {"autoCheck": false, "drainMinutes": 3}}')
  assert.deepEqual(loadSettings(file), { autoCheck: false, drainMinutes: 5 })
})

// A local generic feed, as GitHub's releases/latest/download/ serves it.
async function feedSetup(t, settingsJSON) {
  const dir = tempDir(t)
  const feedDir = path.join(dir, "feed")
  fs.mkdirSync(feedDir)
  fs.writeFileSync(
    path.join(feedDir, "handful-mac.yml"),
    "version: 1.4.0\nfiles:\n  - url: Handful-universal-mac.zip\n    sha512: abc\n    size: 3\npath: Handful-universal-mac.zip\nsha512: abc\n",
  )
  const configFile = path.join(dir, "config.json")
  fs.writeFileSync(configFile, settingsJSON)
  const { base, log } = await serve(t, staticFeed(feedDir))
  const clock = { t: Date.parse("2026-11-09T10:00:00Z") }
  const model = new UpdaterModel({
    current: "1.3.2",
    now: () => clock.t,
    store: fileStore(path.join(dir, "userData", "update-state.json")),
    settings: loadSettings(configFile),
    mode: "source",
  })
  const check = async (manual) => {
    if (!model.beginCheck(manual)) return
    try {
      model.checkDone(await fetchFeedVersion(`${base}/`))
    } catch (err) {
      model.checkFailed(err)
    }
  }
  const timers = fakeTimers()
  const scheduler = createScheduler({ model, settings: () => loadSettings(configFile), check, timers })
  return { model, clock, log, timers, scheduler, check, configFile }
}

test("with autoCheck off the scheduler makes no request; a manual check does", async (t) => {
  const { log, timers, scheduler, clock, check, model } = await feedSetup(t, '{"updates": {"autoCheck": false}}')
  scheduler.start()
  assert.equal(await timers.fire(), firstCheckDelay)
  for (let hour = 0; hour < 72; hour++) {
    clock.t += HOUR
    assert.equal(await timers.fire(), HOUR)
  }
  assert.deepEqual(log, [])
  await check(true)
  assert.deepEqual(log, ["GET /handful-mac.yml"])
  assert.equal(model.snapshot().state, "source")
  assert.equal(model.snapshot().version, "1.4.0")
  scheduler.stop()
  assert.equal(timers.pending.length, 0)
})

test("the scheduler checks a minute after launch, then at most once a day", async (t) => {
  const { log, timers, scheduler, clock, model } = await feedSetup(t, "{}")
  scheduler.start()
  assert.equal(timers.pending[0].ms, 60_000)
  await timers.fire()
  assert.equal(log.length, 1)
  assert.equal(model.snapshot().state, "source")
  // Hourly ticks for the rest of the day make no request.
  for (let hour = 1; hour < 24; hour++) {
    clock.t += HOUR
    assert.equal(await timers.fire(), HOUR)
  }
  assert.equal(log.length, 1, "the 24 h throttle holds")
  clock.t += HOUR
  await timers.fire()
  assert.equal(log.length, 2)
  assert.deepEqual(log, ["GET /handful-mac.yml", "GET /handful-mac.yml"])
  scheduler.stop()
})

test("turning autoCheck off in config.json stops the automatic checks without a restart", async (t) => {
  const { log, timers, scheduler, clock, configFile } = await feedSetup(t, "{}")
  scheduler.start()
  await timers.fire()
  assert.equal(log.length, 1)
  fs.writeFileSync(configFile, '{"updates": {"autoCheck": false}}')
  clock.t += 3 * DAY
  await timers.fire()
  assert.equal(log.length, 1)
  scheduler.stop()
})

test("an offline automatic check is retried at the next hourly tick", async (t) => {
  const dir = tempDir(t)
  const clock = { t: 0 }
  const model = new UpdaterModel({ current: "1.3.2", now: () => clock.t, mode: "auto" })
  let calls = 0
  const check = async (manual) => {
    calls++
    model.beginCheck(manual)
    model.checkFailed(Object.assign(new Error("getaddrinfo ENOTFOUND github.com"), { code: "ENOTFOUND" }))
  }
  const timers = fakeTimers()
  const scheduler = createScheduler({ model, settings: () => loadSettings(path.join(dir, "none.json")), check, timers })
  scheduler.start()
  await timers.fire()
  clock.t += HOUR
  await timers.fire()
  assert.equal(calls, 2)
  assert.equal(model.snapshot().state, "hidden")
  scheduler.stop()
})

test("a check that throws does not stop the schedule", async () => {
  const model = new UpdaterModel({ current: "1.3.2", now: () => 0 })
  const timers = fakeTimers()
  const scheduler = createScheduler({
    model,
    settings: () => ({ autoCheck: true, drainMinutes: 30 }),
    check: async () => {
      throw new Error("boom")
    },
    timers,
  })
  scheduler.start()
  await timers.fire()
  assert.equal(timers.pending.length, 1)
  scheduler.stop()
  scheduler.stop()
})

test("fetchFeedVersion fails on a missing feed or a feed without a version", async (t) => {
  const { base } = await serve(t, (req, res) => {
    if (req.url === "/empty/handful-mac.yml") res.writeHead(200).end("files: []\n")
    else res.writeHead(404).end()
  })
  await assert.rejects(fetchFeedVersion(`${base}/`), /404/)
  await assert.rejects(fetchFeedVersion(`${base}/empty/`), /no version/)
})

test("serverAPI calls /api/version and /api/drain, and tolerates an old or stopped server", async (t) => {
  const body = { version: "1.3.2", sourceBuild: false, draining: false, running: [{ botId: "a", name: "Dev Lead", kind: "turn" }] }
  const { base, log } = await serve(t, (req, res) => {
    // Like the API's CSRF guard: a change needs a JSON content type.
    if (req.method !== "GET" && req.headers["content-type"] !== "application/json") {
      res.writeHead(403).end()
    } else if (req.url === "/api/version" || req.url === "/api/drain") {
      res.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ ...body, draining: req.method === "POST" }))
    } else res.writeHead(404).end()
  })
  const api = serverAPI(base)
  const version = await api.version()
  assert.deepEqual(version, { status: 200, body })
  assert.deepEqual(runningOf(version), body.running)
  assert.equal((await api.drain()).body.draining, true)
  assert.equal((await api.undrain()).body.draining, false)
  assert.deepEqual(log, ["GET /api/version", "POST /api/drain", "DELETE /api/drain"])

  const old = await serve(t, (_req, res) => res.writeHead(404).end("404 page not found"))
  const missing = await serverAPI(old.base).version()
  assert.deepEqual(missing, { status: 404, body: null })
  assert.deepEqual(runningOf(missing), [])

  const broken = await serve(t, (_req, res) => res.writeHead(200).end("not json"))
  assert.deepEqual(await serverAPI(broken.base).version(), { status: 200, body: null })

  // A closed port: nothing answers.
  const closed = http.createServer()
  await new Promise((resolve) => closed.listen(0, "127.0.0.1", resolve))
  const port = closed.address().port
  await new Promise((resolve) => closed.close(resolve))
  const none = await serverAPI(`http://127.0.0.1:${port}`).version()
  assert.equal(none, null)
  assert.deepEqual(runningOf(none), [])
})

test("serverAPI sends the key with drain and undrain, reads it at each call, and sends none without a key", async (t) => {
  const seen = []
  const { base } = await serve(t, (req, res) => {
    seen.push(`${req.method} ${req.headers["x-handful-key"] ?? "-"}`)
    res.writeHead(200, { "Content-Type": "application/json" }).end("{}")
  })
  let key = "first"
  const api = serverAPI(base, fetch, () => key)
  await api.version()
  await api.drain()
  key = "rotated"
  await api.undrain()
  key = null
  await api.drain()
  assert.deepEqual(seen, ["GET -", "POST first", "DELETE rotated", "POST -"])
  await serverAPI(base).drain()
  assert.equal(seen.at(-1), "POST -")
})
