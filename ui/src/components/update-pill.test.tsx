import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { UpdaterState } from "../lib/updates.ts"
import { Body, PausedForUpdate, UpdatePill } from "./update-pill.tsx"

const noop = () => {}
const actions = {
  get: async () => s({}),
  onChange: () => noop,
  download: noop,
  cancelDownload: noop,
  later: noop,
  skip: noop,
  restartNow: noop,
  updateWhenIdle: noop,
  cancelUpdate: noop,
  waitLonger: noop,
  retry: noop,
  openReleasePage: noop,
  check: noop,
}
const now = Date.parse("2026-11-09T15:00:00Z")
function s(x: Partial<UpdaterState>): UpdaterState {
  return {
    state: "available",
    version: "1.4.0",
    current: "1.3.2",
    drainMinutes: 30,
    releaseUrl: "https://github.com/yarlson/yargpt/releases/tag/v1.4.0",
    hiddenByLater: false,
    ...x,
  }
}
const body = (x: Partial<UpdaterState>, confirm = false) =>
  renderToStaticMarkup(<Body s={s(x)} a={actions} bots={[]} now={now} confirm={confirm} setConfirm={noop} />)
const pill = (x: Partial<UpdaterState> | null) =>
  renderToStaticMarkup(<UpdatePill update={{ state: x && s(x), actions }} bots={[]} />)
const run = [
  { botId: "a", name: "Dev Lead", kind: "turn" as const, since: now - 3 * 60_000 },
  { botId: "b", name: "Researcher", kind: "task" as const },
]

test("the pill is a labelled button that opens a dialog, styled by its state", () => {
  const html = pill({ state: "available" })
  assert.include(html, 'aria-haspopup="dialog"')
  assert.include(html, 'aria-expanded="false"')
  assert.include(html, 'aria-label="Update available: 1.4.0. Open update options."')
  assert.include(html, "bg-blue-50")
  assert.include(pill({ state: "ready" }), "bg-blue-600")
  assert.include(pill({ state: "error" }), "bg-amber-50")
  assert.notInclude(pill({ state: "error" }), "destructive")
  assert.include(pill({ state: "restarting" }), 'aria-disabled="true"')
})

test("a narrow column gets the short label, and downloading fills by its percent", () => {
  const html = pill({ state: "downloading", percent: 42 })
  assert.include(html, "Downloading 1.4.0… 42%")
  assert.include(html, "@max-[520px]:hidden")
  assert.include(html, "width:42%")
})

test("hidden, checking and Later show no pill", () => {
  for (const x of [
    null,
    { state: "hidden" as const },
    { state: "checking" as const },
    { state: "ready" as const, hiddenByLater: true },
  ]) {
    assert.notInclude(pill(x), "<button", JSON.stringify(x))
  }
})

test("available offers Download, Later and Skip, with the release link", () => {
  const html = body({ state: "available" })
  assert.include(html, "YarGPT 1.4.0 is out")
  assert.include(html, "You have 1.3.2")
  assert.include(html, 'href="https://github.com/yarlson/yargpt/releases/tag/v1.4.0"')
  for (const t of ["Download", "Later", "Skip this version"]) assert.include(html, `>${t}<`)
})

test("downloading shows a progress bar and its size", () => {
  const html = body({ state: "downloading", percent: 41.7, transferred: 48e6, total: 115e6 })
  assert.match(html, /<progress[^>]*aria-label="Downloading 1.4.0"[^>]*value="42"[^>]*max="100"/)
  assert.include(html, "48 of 115 MB · 42%")
  assert.include(html, "Cancel download")
  assert.notInclude(html, "Skip this version")
  assert.notInclude(body({ state: "downloading" }), " MB")
})

test("ready with no bots working restarts; with bots working it offers to wait", () => {
  const idle = body({ state: "ready" })
  assert.include(idle, "YarGPT 1.4.0 is ready")
  assert.include(idle, "Restart YarGPT to finish updating.")
  assert.include(idle, ">Restart now<")
  assert.notInclude(idle, "Update when they finish")
  const busy = body({ state: "ready", running: run })
  assert.include(busy, "2 bots are working:")
  assert.include(busy, "Dev Lead")
  assert.include(busy, "turn · 3 min")
  assert.include(busy, "background task")
  assert.include(busy, "bg-emerald-500")
  assert.include(busy, "bg-sky-500")
  assert.include(busy, "Update when they finish")
  assert.include(busy, "Restart now stops their running turns.")
  assert.include(body({ state: "ready", running: [run[0]] }), "1 bot is working:")
})

test("Restart now while bots work asks first, with waiting as the main action", () => {
  const html = body({ state: "ready", running: run }, true)
  assert.include(html, "Stop 2 running bots?")
  assert.match(html, /data-main="true"[^>]*>Update when they finish</)
  assert.include(html, ">Back<")
  assert.include(body({ state: "ready", running: [run[0]] }, true), "Stop 1 running bot?")
})

test("waiting lists the bots, the time left and how to back out", () => {
  const html = body({ state: "waiting", running: run, deadline: new Date(now + 21 * 60_000).toISOString() })
  assert.include(html, "Updating after 2 bots finish")
  assert.match(html, /<progress[^>]*aria-label="Time left before YarGPT asks you"[^>]*value="21"[^>]*max="30"/)
  assert.include(html, "21 of 30 min left.")
  assert.include(html, "New turns wait. Team goals are paused for the update.")
  assert.include(html, "A background task can keep going until it ends")
  assert.include(html, "Cancel update")
  assert.notInclude(body({ state: "waiting", running: [run[0]] }), "background task can keep")
  assert.include(body({ state: "waiting", running: [run[0]] }), "Updating after 1 bot finishes")
})

test("past the limit it never restarts on its own: wait longer or restart", () => {
  const html = body({ state: "limit", running: [run[1]] })
  assert.include(html, "Still working after 30 min")
  assert.include(html, "Wait 30 more min")
  assert.include(html, "Restart now")
  assert.include(body({ state: "limit", running: [run[1]] }, true), "Stop 1 running bot?")
})

test("an error says why and offers to retry or download by hand", () => {
  const html = body({ state: "error", error: "signature" })
  assert.include(html, "Couldn&#x27;t verify the update&#x27;s signature.")
  assert.include(html, "Try again")
  assert.include(html, "Download manually ↗")
})

test("a source build points to the release and the rebuild command", () => {
  const html = body({ state: "source" })
  assert.include(html, "built locally, so it can&#x27;t update itself")
  assert.include(html, "git pull &amp;&amp; make build")
  assert.include(html, "Open release page ↗")
  assert.include(html, "Skip this version")
})

test("states with no popover render nothing", () => {
  assert.equal(body({ state: "restarting" }), "")
  assert.equal(body({ state: "hidden" }), "")
})

test("the goal bar's badge says the team is paused for the update, in words", () => {
  const html = renderToStaticMarkup(<PausedForUpdate />)
  assert.include(html, "Paused for update")
  assert.include(html, 'title="New turns wait until YarGPT restarts. Running turns finish."')
  assert.notInclude(html, "amber")
})
