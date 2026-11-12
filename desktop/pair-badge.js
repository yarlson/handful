// The Dock badge for a phone that asks to pair. While the window is open, the
// page tells the app when a pair request starts and ends; while it is closed,
// createPairWatch asks the server. While a request waits and the window is not
// in front, the Dock icon shows a badge and bounces once. Electron and the
// network come in as dependencies, so the tests run without them.

const badgeText = "1"

// createPairBadge makes the badge's state.
//   dock: {setBadge(text), bounce(type)}, or null where there is no Dock
//   isFocused(): whether the window is in front, where the banner shows
function createPairBadge({ dock, isFocused }) {
  let waiting = false

  function clear() {
    dock?.setBadge("")
  }

  return {
    // change takes the page's news: true when a request starts, false when it
    // is allowed, denied or timed out. Anything but true counts as ended.
    change(value) {
      const now = value === true
      if (now === waiting) return
      waiting = now
      if (!now) return clear()
      if (isFocused()) return
      dock?.setBadge(badgeText)
      dock?.bounce("informational")
    },
    // focus: the window came to the front, where the banner shows the request.
    focus: clear,
    // reset: a new page starts, which tells the request's state afresh.
    reset() {
      waiting = false
      clear()
    },
  }
}

const retryDelay = 5_000

// createPairWatch tells onWaiting whether a pair request waits, while the
// window is closed and no page can tell it. It reads GET /api/phone at start
// and on each "pair" event of GET /api/events, and again when the request
// runs out, since running out publishes no event. A server without phone
// access answers 404, which means no request.
//   fetchImpl, base: the server
//   timers: {setTimeout, clearTimeout}; now(): the time in ms
function createPairWatch({ fetchImpl = fetch, base, onWaiting, timers = globalThis, now = Date.now, log = console.debug }) {
  let running = false
  let stream = null
  let expiry = null
  let retry = null
  let reads = 0

  function clearTimer(timer) {
    if (timer) timers.clearTimeout(timer)
  }

  // read asks the server for the waiting request. Only the latest read counts.
  async function read() {
    const mine = ++reads
    let request = null
    try {
      const res = await fetchImpl(`${base}/api/phone`, { signal: AbortSignal.timeout(5_000) })
      if (res.ok) request = (await res.json())?.pairing?.request ?? null
    } catch (err) {
      log(`pair watch: read /api/phone: ${err.message}`)
    }
    if (!running || mine !== reads) return
    clearTimer(expiry)
    expiry = null
    const waiting = typeof request?.expiresAt === "number" && request.expiresAt > now()
    if (waiting) expiry = timers.setTimeout(read, request.expiresAt - now() + 50)
    onWaiting(waiting)
  }

  // listen reads the event stream until it ends, then tries again.
  async function listen() {
    const abort = new AbortController()
    stream = abort
    try {
      const res = await fetchImpl(`${base}/api/events`, { signal: abort.signal })
      if (!res.ok || !res.body) throw new Error(`status ${res.status}`)
      const decoder = new TextDecoder()
      let buffered = ""
      for await (const chunk of res.body) {
        buffered += decoder.decode(chunk, { stream: true })
        let end
        while ((end = buffered.indexOf("\n\n")) >= 0) {
          const message = buffered.slice(0, end)
          buffered = buffered.slice(end + 2)
          if (isPairEvent(message)) void read()
        }
      }
    } catch (err) {
      if (!abort.signal.aborted) log(`pair watch: /api/events: ${err.message}`)
    }
    if (running && stream === abort) {
      retry = timers.setTimeout(() => {
        retry = null
        if (running) void reconnect()
      }, retryDelay)
    }
  }

  // reconnect reads the state, which may have changed while the stream was
  // down, and listens again.
  async function reconnect() {
    void read()
    await listen()
  }

  return {
    start() {
      if (running) return
      running = true
      void reconnect()
    },
    stop() {
      running = false
      reads++
      stream?.abort()
      stream = null
      clearTimer(expiry)
      clearTimer(retry)
      expiry = null
      retry = null
    },
  }
}

// isPairEvent reports whether an event-stream message is the server's "pair"
// event.
function isPairEvent(message) {
  for (const line of message.split("\n")) {
    if (!line.startsWith("data: ")) continue
    try {
      if (JSON.parse(line.slice(6))?.type === "pair") return true
    } catch {
      return false
    }
  }
  return false
}

module.exports = { createPairBadge, createPairWatch, isPairEvent, badgeText, retryDelay }
