// The parts of the updater that talk to the network, the disk and the clock,
// without Electron: the check schedule, the feed for builds that cannot
// update themselves, and the server's /api/version and /api/drain.

const fs = require("node:fs")
const path = require("node:path")
const { HOUR, feedVersion, readSettings } = require("./updater-state")

const firstCheckDelay = 60_000

// fileStore reads and writes a small file; it writes a temporary file and
// renames it, so a crash leaves the old content.
function fileStore(file, fsys = fs) {
  return {
    read() {
      try {
        return fsys.readFileSync(file, "utf8")
      } catch (err) {
        if (err.code === "ENOENT") return null
        throw err
      }
    },
    write(text) {
      fsys.mkdirSync(path.dirname(file), { recursive: true })
      const tmp = `${file}.tmp`
      fsys.writeFileSync(tmp, text)
      fsys.renameSync(tmp, file)
    },
  }
}

// loadSettings reads the updates settings from config.json, every time, so a
// change applies without a restart.
function loadSettings(configFile, fsys = fs) {
  return readSettings(fileStore(configFile, fsys).read())
}

// createScheduler runs the automatic checks: the first one a minute after
// launch, then a tick every hour. A tick checks only when the model says that
// a check is due, so at most once a day, and never with autoCheck off.
function createScheduler({ model, settings, check, timers = globalThis, delay = firstCheckDelay, interval = HOUR }) {
  let timer = null
  let stopped = false
  const schedule = (ms) => {
    if (!stopped) timer = timers.setTimeout(run, ms)
  }
  async function tick() {
    model.setSettings(settings())
    if (!model.autoCheckDue()) return false
    try {
      await check(false)
    } catch {
      // check reports its errors to the model.
    }
    return true
  }
  async function run() {
    await tick()
    schedule(interval)
  }
  return {
    tick,
    start() {
      stopped = false
      schedule(delay)
    },
    stop() {
      stopped = true
      if (timer !== null) timers.clearTimeout(timer)
      timer = null
    },
  }
}

// fetchFeedVersion reads the version of latest-mac.yml in the feed. It
// throws on a network error or an HTTP status other than 200.
async function fetchFeedVersion(feed, fetchImpl = fetch) {
  const res = await fetchImpl(new URL("latest-mac.yml", feed), { signal: AbortSignal.timeout(15_000), redirect: "follow" })
  if (!res.ok) throw new Error(`${res.status} from ${feed}latest-mac.yml`)
  const version = feedVersion(await res.text())
  if (!version) throw new Error(`latest-mac.yml in ${feed} has no version`)
  return version
}

// serverAPI calls the server's /api/version and /api/drain. Each call returns
// {status, body}, or null when the server does not answer.
function serverAPI(base, fetchImpl = fetch) {
  async function call(method, endpoint, timeout) {
    try {
      // The API's CSRF guard requires a JSON content type on every change.
      const init = method === "GET" ? {} : { headers: { "Content-Type": "application/json" }, body: "{}" }
      const res = await fetchImpl(`${base}${endpoint}`, { method, ...init, signal: AbortSignal.timeout(timeout) })
      let body = null
      try {
        body = res.status === 200 ? await res.json() : null
      } catch {
        body = null
      }
      return { status: res.status, body }
    } catch {
      return null
    }
  }
  return {
    version: (timeout = 2000) => call("GET", "/api/version", timeout),
    drain: () => call("POST", "/api/drain", 5000),
    undrain: () => call("DELETE", "/api/drain", 5000),
  }
}

// sharedDrain gives api with an undrain that leaves the server draining
// while another part of the app still waits on the drain: the updater and
// the bridge to Handful share the server's one drain hold.
function sharedDrain(api, othersHold) {
  return { ...api, undrain: () => (othersHold() ? Promise.resolve(null) : api.undrain()) }
}

// runningOf gives the running turns of an answer of /api/version. A server
// that does not answer, or does not have the endpoint, counts as idle.
function runningOf(response) {
  return response?.status === 200 && Array.isArray(response.body?.running) ? response.body.running : []
}

module.exports = { firstCheckDelay, fileStore, loadSettings, createScheduler, fetchFeedVersion, serverAPI, sharedDrain, runningOf }
