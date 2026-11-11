import { assert, test } from "vitest"
import {
  announcement,
  ariaLabel,
  doing,
  drainFraction,
  errorReason,
  label,
  LATER_MS,
  laterActive,
  minutesLeft,
  shown,
  sourceState,
  tone,
  type UpdaterState,
} from "./updates.ts"

const now = Date.parse("2026-11-09T15:00:00Z")
const s = (x: Partial<UpdaterState>): UpdaterState => ({
  state: "available",
  version: "1.4.0",
  current: "1.3.2",
  drainMinutes: 30,
  releaseUrl: "https://example.com/r",
  hiddenByLater: false,
  ...x,
})

test("the pill shows for every state but hidden and checking, unless Later hid it", () => {
  assert.isFalse(shown(s({ state: "hidden" })))
  assert.isFalse(shown(s({ state: "checking" })))
  assert.isTrue(shown(s({ state: "ready" })))
  assert.isFalse(shown(s({ state: "ready", hiddenByLater: true })))
})

test("only ready is solid, an error is amber, available is the accent", () => {
  assert.equal(tone("available"), "accent")
  assert.equal(tone("ready"), "solid")
  assert.equal(tone("limit"), "solid")
  assert.equal(tone("error"), "warning")
  for (const st of ["downloading", "waiting", "restarting", "source"] as const) assert.equal(tone(st), "neutral")
})

test("each state has its own words, and a short form for a narrow column", () => {
  const deadline = new Date(now + 20.5 * 60_000).toISOString()
  const run = [
    { botId: "a", name: "A", kind: "turn" as const },
    { botId: "b", name: "B", kind: "task" as const },
  ]
  const cases: [Partial<UpdaterState>, string, string][] = [
    [{ state: "available" }, "Update available: 1.4.0", "1.4.0"],
    [{ state: "downloading", percent: 41.6 }, "Downloading 1.4.0… 42%", "42%"],
    [{ state: "downloading" }, "Downloading 1.4.0… 0%", "0%"],
    [{ state: "ready" }, "Restart to update: 1.4.0", "Restart: 1.4.0"],
    [{ state: "limit" }, "Restart to update: 1.4.0", "Restart: 1.4.0"],
    [{ state: "waiting", running: run, deadline }, "Updating after 2 bots finish · 21 min", "21 min"],
    [{ state: "waiting", running: [run[0]], deadline }, "Updating after 1 bot finish · 21 min", "21 min"],
    [{ state: "waiting" }, "Updating after 0 bots finish · 0 min", "0 min"],
    [{ state: "restarting" }, "Restarting…", "Restarting…"],
    [{ state: "error" }, "Update failed", "Update failed"],
    [{ state: "source" }, "1.4.0 is out", "1.4.0 is out"],
    [{ state: "hidden" }, "", ""],
    [{ state: "available", version: undefined }, "Update available: ", ""],
  ]
  for (const [x, full, short] of cases) assert.deepEqual(label(s(x), now), { full, short }, JSON.stringify(x))
})

test("minutes left round up and never go below 0", () => {
  assert.equal(minutesLeft(undefined, now), 0)
  assert.equal(minutesLeft("not a date", now), 0)
  assert.equal(minutesLeft(new Date(now - 1).toISOString(), now), 0)
  assert.equal(minutesLeft(new Date(now - 5 * 60_000).toISOString(), now), 0)
  assert.equal(minutesLeft(new Date(now).toISOString(), now), 0)
  assert.equal(minutesLeft(new Date(now + 1).toISOString(), now), 1)
  assert.equal(minutesLeft(new Date(now + 60_000).toISOString(), now), 1)
  assert.equal(minutesLeft(new Date(now + 60_001).toISOString(), now), 2)
})

test("the pill's label holds the whole state", () => {
  assert.equal(ariaLabel(s({ state: "available" }), now), "Update available: 1.4.0. Open update options.")
  assert.equal(ariaLabel(s({ state: "restarting" }), now), "Restarting Handful to finish the update.")
})

test("each state is announced once in words", () => {
  const cases: [UpdaterState["state"], string][] = [
    ["available", "Handful 1.4.0 is available."],
    ["downloading", "Downloading Handful 1.4.0."],
    ["ready", "Update 1.4.0 is ready to install."],
    ["waiting", "Handful updates to 1.4.0 when the running bots finish."],
    ["limit", "Bots are still working after 30 minutes. Choose whether to restart."],
    ["restarting", "Restarting Handful."],
    ["error", "The update failed."],
    ["source", "Handful 1.4.0 is out."],
    ["hidden", ""],
  ]
  for (const [state, text] of cases) assert.equal(announcement(s({ state })), text, state)
  assert.equal(announcement(s({ state: "source", version: undefined })), "Handful  is out.")
})

test("an error says why in plain words, preferring the app's own text", () => {
  assert.equal(
    errorReason({ errorText: "The server is still on 1.3.2. Quit and reopen Handful." }),
    "The server is still on 1.3.2. Quit and reopen Handful.",
  )
  assert.equal(errorReason({ error: "offline" }), "No network. Check your connection.")
  assert.equal(errorReason({ error: "checksum" }), "Couldn't verify the update: the download didn't match the release.")
  assert.equal(errorReason({ error: "signature" }), "Couldn't verify the update's signature.")
  assert.equal(errorReason({ error: "disk" }), "Not enough space to update.")
  assert.equal(errorReason({ error: "other" }), "Download failed.")
  assert.equal(errorReason({}), "Download failed.")
})

test("a running bot says what it does and for how long", () => {
  assert.equal(doing({ botId: "a", name: "A", kind: "task" }, now), "background task")
  assert.equal(doing({ botId: "a", name: "A", kind: "turn" }, now), "turn")
  assert.equal(doing({ botId: "a", name: "A", kind: "turn", since: now - 3.5 * 60_000 }, now), "turn · 3 min")
  assert.equal(
    doing({ botId: "a", name: "A", kind: "turn", since: new Date(now - 2 * 60_000).toISOString() }, now),
    "turn · 2 min",
  )
  assert.equal(doing({ botId: "a", name: "A", kind: "turn", since: now + 60_000 }, now), "turn · 0 min")
  assert.equal(doing({ botId: "a", name: "A", kind: "turn", since: "bad" }, now), "turn")
})

test("the waiting bar drains from 1 to 0 over the limit", () => {
  assert.equal(drainFraction(new Date(now + 15 * 60_000).toISOString(), 30, now), 0.5)
  assert.equal(drainFraction(new Date(now + 60 * 60_000).toISOString(), 30, now), 1)
  assert.equal(drainFraction(undefined, 30, now), 0)
  assert.equal(drainFraction(new Date(now + 60_000).toISOString(), 0, now), 0)
})

test("a source build shows a newer release unless the user skipped it", () => {
  const v = {
    version: "1.3.2",
    sourceBuild: true,
    latest: "1.4.0",
    releaseUrl: "https://r",
    draining: false,
    running: [],
  }
  assert.deepEqual(sourceState(v, null), {
    state: "source",
    version: "1.4.0",
    current: "1.3.2",
    drainMinutes: 30,
    releaseUrl: "https://r",
    hiddenByLater: false,
  })
  assert.equal(sourceState(v, "1.4.0").state, "hidden")
  assert.equal(sourceState(v, "1.3.9").state, "source")
  assert.equal(sourceState({ ...v, latest: undefined }, null).state, "hidden")
  assert.equal(sourceState({ ...v, sourceBuild: false }, null).state, "hidden")
  assert.equal(
    sourceState({ ...v, releaseUrl: undefined }, null).releaseUrl,
    "https://github.com/yarlson/handful/releases/latest",
  )
})

test("Later hides a source build's pill for 24 hours", () => {
  assert.equal(LATER_MS, 86_400_000)
  assert.isFalse(laterActive(0, now))
  assert.isTrue(laterActive(now - LATER_MS + 1, now))
  assert.isFalse(laterActive(now - LATER_MS, now))
})
