const assert = require("node:assert/strict")
const { test } = require("node:test")
const {
  DAY,
  HOUR,
  errorTexts,
  compareVersions,
  isNewer,
  clampDrainMinutes,
  readSettings,
  errorKind,
  releaseURLFor,
  feedVersion,
  isDeveloperIDSigned,
  botsWorkingText,
  finishingTurnsText,
  menuItemFor,
  bundleChangeAction,
  serverVersionAction,
  UpdaterModel,
} = require("./updater-state")

function memoryStore(text = null) {
  return {
    text,
    read() {
      return this.text
    },
    write(t) {
      this.text = t
    },
  }
}

function clock(start = Date.parse("2026-11-09T10:00:00Z")) {
  const c = { t: start, now: () => c.t, advance: (ms) => (c.t += ms) }
  return c
}

function model({ current = "1.3.2", store = memoryStore(), c = clock(), settings, mode } = {}) {
  const m = new UpdaterModel({ current, store, now: c.now, settings, mode })
  const states = []
  m.onChange((s) => states.push(s))
  return { m, c, store, states }
}

// readyModel downloads version in an automatic check.
function readyModel(version = "1.4.0", options) {
  const t = model(options)
  t.m.beginCheck(false)
  assert.equal(t.m.checkDone(version), "download")
  t.m.downloadStarted(version, true)
  t.m.downloaded(version)
  return t
}

const bots = [
  { botId: "a", name: "Dev Lead", kind: "turn" },
  { botId: "b", name: "Researcher", kind: "task", since: "2026-11-09T10:00:00Z" },
]

test("versions compare by semantic versioning, prereleases included", () => {
  assert.equal(compareVersions("1.4.0", "1.3.2"), 1)
  assert.equal(compareVersions("1.3.2", "1.4.0"), -1)
  assert.equal(compareVersions("v1.3.2", "1.3.2"), 0)
  assert.equal(compareVersions("1.10.0", "1.9.9"), 1)
  assert.equal(compareVersions("2.0.0", "1.99.99"), 1)
  assert.equal(compareVersions("1.4.0", "1.4.0-rc.1"), 1)
  assert.equal(compareVersions("1.4.0-rc.1", "1.4.0"), -1)
  assert.equal(compareVersions("1.4.0-rc.2", "1.4.0-rc.1"), 1)
  assert.equal(compareVersions("1.4.0-rc.10", "1.4.0-rc.9"), 1)
  assert.equal(compareVersions("1.4.0-rc.1", "1.4.0-rc.1.1"), -1)
  assert.equal(compareVersions("1.4.0-1", "1.4.0-alpha"), -1)
  assert.equal(compareVersions("1.4.0-beta", "1.4.0-alpha"), 1)
  assert.equal(compareVersions("1.4.0+build.5", "1.4.0"), 0)
  assert.equal(compareVersions("dev-abc", "1.4.0"), null)
  assert.equal(compareVersions("1.4", "1.4.0"), null)
  assert.equal(isNewer("1.4.0", "1.3.2"), true)
  assert.equal(isNewer("1.3.2", "1.3.2"), false)
  assert.equal(isNewer("1.3.1", "1.3.2"), false)
  assert.equal(isNewer("garbage", "1.3.2"), false)
})

test("the drain limit defaults to 30 minutes and stays within 5 and 240", () => {
  assert.equal(clampDrainMinutes(undefined), 30)
  assert.equal(clampDrainMinutes("60"), 30)
  assert.equal(clampDrainMinutes(Number.NaN), 30)
  assert.equal(clampDrainMinutes(1), 5)
  assert.equal(clampDrainMinutes(1000), 240)
  assert.equal(clampDrainMinutes(45), 45)
  assert.equal(clampDrainMinutes(45.6), 46)
})

test("settings come from the updates part of config.json", () => {
  assert.deepEqual(readSettings(null), { autoCheck: true, drainMinutes: 30 })
  assert.deepEqual(readSettings("not json"), { autoCheck: true, drainMinutes: 30 })
  assert.deepEqual(readSettings('{"model": "sonnet"}'), { autoCheck: true, drainMinutes: 30 })
  assert.deepEqual(readSettings('{"updates": null}'), { autoCheck: true, drainMinutes: 30 })
  assert.deepEqual(readSettings('{"updates": {"autoCheck": false}}'), { autoCheck: false, drainMinutes: 30 })
  assert.deepEqual(readSettings('{"updates": {"autoCheck": "no", "drainMinutes": 90}}'), { autoCheck: true, drainMinutes: 90 })
})

test("errors of the updater map to the kinds that the window explains", () => {
  const cases = [
    [Object.assign(new Error("getaddrinfo ENOTFOUND github.com"), { code: "ENOTFOUND" }), "offline"],
    [new Error("connect ECONNREFUSED 127.0.0.1:443"), "offline"],
    [new Error("net::ERR_INTERNET_DISCONNECTED"), "offline"],
    [Object.assign(new TypeError("fetch failed"), { cause: { code: "ENOTFOUND" } }), "offline"],
    [new Error("sha512 checksum mismatch, expected abc, got def"), "checksum"],
    [Object.assign(new Error("mismatch"), { code: "ERR_CHECKSUM_MISMATCH" }), "checksum"],
    [
      new Error(
        "Code signature at URL file:///x/YarGPT.app/ did not pass validation: code failed to satisfy specified code requirement(s)",
      ),
      "signature",
    ],
    [Object.assign(new Error("ENOSPC: no space left on device, write"), { code: "ENOSPC" }), "disk"],
    [new Error("EACCES: permission denied, open '/x'"), "disk"],
    [new Error("HttpError: 404 Not Found"), "other"],
    ["weird", "other"],
    [undefined, "other"],
  ]
  for (const [err, kind] of cases) assert.equal(errorKind(err), kind, String(err?.message ?? err))
  assert.equal(errorTexts.offline, "Can't reach GitHub. Check your connection.")
  assert.equal(errorTexts.checksum, "The download didn't match the release. Try again.")
  assert.equal(errorTexts.signature, "Couldn't verify the update's signature.")
  assert.equal(errorTexts.disk, "Not enough space to update.")
})

test("the release page is the version's tag, or the latest release", () => {
  assert.equal(releaseURLFor("1.4.0"), "https://github.com/yarlson/yargpt/releases/tag/v1.4.0")
  assert.equal(releaseURLFor("v1.4.0"), "https://github.com/yarlson/yargpt/releases/tag/v1.4.0")
  assert.equal(releaseURLFor(undefined), "https://github.com/yarlson/yargpt/releases/latest")
})

test("the feed's version is read from latest-mac.yml", () => {
  assert.equal(feedVersion("version: 1.4.0\nfiles:\n  - url: YarGPT-universal-mac.zip\n"), "1.4.0")
  assert.equal(feedVersion("files: []\nversion: '1.4.0-rc.1'\n"), "1.4.0-rc.1")
  assert.equal(feedVersion('version: "v2.0.0" # tag\n'), "2.0.0")
  assert.equal(feedVersion("path: x.zip\n"), null)
  assert.equal(feedVersion("version: latest\n"), null)
  assert.equal(feedVersion(undefined), null)
})

test("only a Developer ID signature can update itself", () => {
  const developerID = [
    "Executable=/Applications/YarGPT.app/Contents/MacOS/YarGPT",
    "Identifier=dev.yarlson.yargpt",
    "Authority=Developer ID Application: Yar Kravtsov (ABCDE12345)",
    "Authority=Developer ID Certification Authority",
    "Authority=Apple Root CA",
    "TeamIdentifier=ABCDE12345",
  ].join("\n")
  const adHoc = "Identifier=dev.yarlson.yargpt\nSignature=adhoc\nTeamIdentifier=not set\n"
  assert.equal(isDeveloperIDSigned(developerID), true)
  assert.equal(isDeveloperIDSigned(adHoc), false)
  assert.equal(isDeveloperIDSigned("Authority=Apple Development: someone"), false)
  assert.equal(isDeveloperIDSigned("code object is not signed at all"), false)
  assert.equal(isDeveloperIDSigned(undefined), false)
})

test("the dialogs name the bots that work", () => {
  assert.equal(botsWorkingText(bots), "2 bots are working: Dev Lead, Researcher.")
  assert.equal(botsWorkingText([bots[0]]), "1 bot is working: Dev Lead.")
  // A bot with a turn and a task counts once.
  assert.equal(botsWorkingText([bots[0], { ...bots[0], kind: "task" }]), "1 bot is working: Dev Lead.")
  assert.equal(finishingTurnsText(bots), "Finishing 2 running turns before starting the new version…")
  assert.equal(finishingTurnsText([bots[0]]), "Finishing 1 running turn before starting the new version…")
})

test("the app menu's update item follows the state", () => {
  const base = { current: "1.3.2", drainMinutes: 30, releaseUrl: "", hiddenByLater: false }
  const item = (s) => menuItemFor({ ...base, ...s })
  assert.deepEqual(item({ state: "hidden" }), { label: "Check for updates…", enabled: true, action: "check" })
  assert.deepEqual(item({ state: "error" }), { label: "Check for updates…", enabled: true, action: "check" })
  assert.deepEqual(item({ state: "checking" }), { label: "Checking for updates…", enabled: false, action: null })
  assert.deepEqual(item({ state: "available", version: "1.4.0" }), { label: "Download Update 1.4.0", enabled: true, action: "download" })
  assert.deepEqual(item({ state: "downloading", version: "1.4.0", percent: 42.7 }), {
    label: "Downloading 1.4.0… 42%",
    enabled: false,
    action: null,
  })
  assert.deepEqual(item({ state: "ready", version: "1.4.0" }), { label: "Restart to Update 1.4.0…", enabled: true, action: "restart" })
  assert.deepEqual(item({ state: "limit", version: "1.4.0" }), { label: "Restart to Update 1.4.0…", enabled: true, action: "restart" })
  assert.equal(item({ state: "waiting", version: "1.4.0" }).label, "Updating to 1.4.0 when bots finish…")
  assert.deepEqual(item({ state: "restarting" }), { label: "Restarting…", enabled: false, action: null })
  assert.deepEqual(item({ state: "source", version: "1.4.0" }), { label: "Get YarGPT 1.4.0…", enabled: true, action: "openReleasePage" })
})

test("a new server waits for the old server's turns only when it reports some", () => {
  assert.equal(bundleChangeAction(null), "register")
  assert.equal(bundleChangeAction({ status: 404, body: null }), "register")
  assert.equal(bundleChangeAction({ status: 500, body: null }), "register")
  assert.equal(bundleChangeAction({ status: 200, body: { version: "1.3.2", running: [] } }), "register")
  assert.equal(bundleChangeAction({ status: 200, body: { version: "1.3.2" } }), "register")
  assert.equal(bundleChangeAction({ status: 200, body: { version: "1.3.2", running: bots } }), "drain")
})

test("the server's version is compared with the app's, except for dev servers and old servers", () => {
  const answer = (version) => ({ status: 200, body: { version, running: [] } })
  assert.equal(serverVersionAction("1.4.0", answer("1.4.0")), "match")
  assert.equal(serverVersionAction("1.4.0", answer("v1.4.0")), "match")
  assert.equal(serverVersionAction("v1.4.0", answer("1.4.0")), "match")
  assert.equal(serverVersionAction("1.4.0", answer("1.3.2")), "mismatch")
  // A server built from source reports dev-…; PR A's endpoint may be missing.
  assert.equal(serverVersionAction("1.4.0", answer("dev-5dce9db")), "skip")
  assert.equal(serverVersionAction("1.4.0", answer("")), "skip")
  assert.equal(serverVersionAction("1.4.0", { status: 404, body: null }), "skip")
  assert.equal(serverVersionAction("1.4.0", null), "skip")
})

test("an automatic check runs at most once a day, counted from the last one that succeeded", () => {
  const { m, c, store } = model()
  assert.equal(m.autoCheckDue(), true)
  m.beginCheck(false)
  assert.equal(m.autoCheckDue(), false, "not while a check runs")
  assert.equal(m.checkDone("1.3.2"), "none")
  assert.equal(m.snapshot().state, "hidden")
  assert.equal(JSON.parse(store.text).lastCheck, c.t)
  c.advance(DAY - 1)
  assert.equal(m.autoCheckDue(), false)
  c.advance(1)
  assert.equal(m.autoCheckDue(), true)
  // A failed check does not count.
  m.beginCheck(false)
  m.checkFailed(new Error("getaddrinfo ENOTFOUND github.com"))
  c.advance(HOUR)
  assert.equal(m.autoCheckDue(), true)
})

test("the last check survives a restart, Later does not", () => {
  const c = clock()
  const store = memoryStore()
  const first = model({ store, c })
  first.m.beginCheck(false)
  first.m.checkDone("1.3.2")
  first.m.later()
  const second = model({ store, c })
  assert.equal(second.m.autoCheckDue(), false)
  assert.equal(second.m.hiddenByLater, false)
  assert.equal(new UpdaterModel({ current: "1.3.2", store: memoryStore("{broken"), now: c.now }).autoCheckDue(), true)
})

test("autoCheck off makes no automatic check; a manual check still runs", () => {
  const { m } = model({ settings: readSettings('{"updates": {"autoCheck": false}}') })
  assert.equal(m.autoCheckDue(), false)
  assert.equal(m.beginCheck(true), true)
  assert.equal(m.checkDone("1.3.2"), "upToDate")
})

test("a manual check ignores the daily limit and says when the app is up to date", () => {
  const { m } = model()
  m.beginCheck(false)
  m.checkDone("1.3.2")
  assert.equal(m.beginCheck(true), true)
  assert.equal(m.snapshot().state, "checking")
  assert.equal(m.beginCheck(true), false, "one check at a time")
  assert.equal(m.checkDone("1.3.2"), "upToDate")
  assert.equal(m.snapshot().state, "hidden")
})

test("a version that is not newer is never offered", () => {
  for (const latest of ["1.3.2", "1.3.1", "1.3.2-rc.1", "0.9.0", "nonsense"]) {
    const { m } = model()
    m.beginCheck(false)
    assert.equal(m.checkDone(latest), "none", latest)
    assert.equal(m.snapshot().state, "hidden", latest)
  }
})

test("an update downloads after an automatic check and is then ready", () => {
  const { m, states } = model()
  m.beginCheck(false)
  assert.equal(m.checkDone("1.4.0"), "download")
  assert.equal(m.snapshot().state, "available")
  m.downloadStarted("1.4.0", true)
  m.downloadProgress({ percent: 50, transferred: 5, total: 10 })
  assert.deepEqual(
    { ...m.snapshot() },
    {
      state: "downloading",
      version: "1.4.0",
      current: "1.3.2",
      percent: 50,
      transferred: 5,
      total: 10,
      drainMinutes: 30,
      releaseUrl: "https://github.com/yarlson/yargpt/releases/tag/v1.4.0",
      hiddenByLater: false,
    },
  )
  m.downloaded("1.4.0")
  const ready = m.snapshot()
  assert.equal(ready.state, "ready")
  assert.deepEqual(ready.running, [])
  assert.equal(m.interceptsQuit(), true)
  assert.deepEqual(
    states.map((s) => s.state),
    ["checking", "available", "downloading", "downloading", "ready"],
  )
})

test("progress outside a download changes nothing", () => {
  const { m, states } = model()
  m.downloadProgress({ percent: 1 })
  assert.equal(states.length, 0)
})

test("a cancelled download offers Download again", () => {
  const { m } = model()
  m.beginCheck(false)
  m.checkDone("1.4.0")
  m.downloadStarted("1.4.0", true)
  m.downloadCancelled()
  assert.equal(m.snapshot().state, "available")
  assert.equal(m.snapshot().version, "1.4.0")
})

test("a failed first automatic download offers Download; a failed manual one shows the error", () => {
  const { m } = model()
  m.beginCheck(false)
  m.checkDone("1.4.0")
  m.downloadStarted("1.4.0", true)
  m.downloadFailed(new Error("sha512 checksum mismatch"), true)
  assert.equal(m.snapshot().state, "available")
  m.downloadStarted("1.4.0", false)
  m.downloadFailed(Object.assign(new Error("no space"), { code: "ENOSPC" }), false)
  const s = m.snapshot()
  assert.equal(s.state, "error")
  assert.equal(s.error, "disk")
  assert.equal(s.errorText, "Not enough space to update.")
  assert.equal(m.errorStage, "download")
})

test("a second failed automatic download shows the error", () => {
  const { m, c } = model()
  for (let i = 0; i < 2; i++) {
    m.beginCheck(false)
    m.checkDone("1.4.0")
    m.downloadStarted("1.4.0", true)
    m.downloadFailed(new Error("sha512 checksum mismatch"), true)
    c.advance(DAY)
  }
  assert.equal(m.snapshot().state, "error")
  assert.equal(m.snapshot().error, "checksum")
})

test("an offline automatic check stays silent; a manual one shows the error", () => {
  const offline = Object.assign(new Error("getaddrinfo ENOTFOUND github.com"), { code: "ENOTFOUND" })
  const { m } = model()
  m.beginCheck(false)
  m.checkFailed(offline)
  assert.equal(m.snapshot().state, "hidden")
  m.beginCheck(true)
  m.checkFailed(offline)
  assert.deepEqual(
    { state: m.snapshot().state, error: m.snapshot().error, errorText: m.snapshot().errorText },
    { state: "error", error: "offline", errorText: "Can't reach GitHub. Check your connection." },
  )
  assert.equal(m.errorStage, "check")
  // Another check after an error starts from hidden.
  m.beginCheck(false)
  m.checkFailed(offline)
  assert.equal(m.snapshot().state, "hidden")
})

test("an automatic check that fails for another reason shows the error", () => {
  const { m } = model()
  m.beginCheck(false)
  m.checkFailed(new Error("HttpError: 404"))
  assert.equal(m.snapshot().state, "error")
  assert.equal(m.snapshot().error, "other")
})

test("a copy that can only report releases stays silent when its automatic check fails", () => {
  const { m } = model({ mode: "source" })
  m.beginCheck(false)
  m.checkFailed(new Error("404 from https://github.com/x/latest-mac.yml"))
  assert.equal(m.snapshot().state, "hidden")
})

test("a copy that cannot update itself shows that a release is out", () => {
  const { m } = model({ mode: "source" })
  m.beginCheck(false)
  assert.equal(m.checkDone("v1.4.0"), "none")
  const s = m.snapshot()
  assert.equal(s.state, "source")
  assert.equal(s.version, "1.4.0")
  assert.equal(s.releaseUrl, "https://github.com/yarlson/yargpt/releases/tag/v1.4.0")
  assert.equal(m.interceptsQuit(), false)
})

test("Later hides the pill until the next launch or for a day, whichever comes first", () => {
  const { m, c } = readyModel()
  m.later()
  assert.equal(m.snapshot().hiddenByLater, true)
  assert.equal(m.snapshot().state, "ready")
  c.advance(DAY - 1)
  assert.equal(m.snapshot().hiddenByLater, true)
  c.advance(1)
  assert.equal(m.snapshot().hiddenByLater, false)
  m.later()
  m.beginCheck(true)
  assert.equal(m.snapshot().hiddenByLater, false, "a manual check shows it again")
})

test("Skip version hides that version until a newer one or a manual check", () => {
  const { m, c, store } = readyModel()
  m.skip()
  assert.equal(JSON.parse(store.text).skipped, "1.4.0")
  assert.equal(m.snapshot().state, "hidden")
  assert.equal(m.snapshot().version, undefined)
  assert.equal(m.interceptsQuit(), false)

  // After a restart, an automatic check finds the skipped version again.
  const next = model({ store, c })
  next.c.advance(DAY)
  next.m.beginCheck(false)
  assert.equal(next.m.checkDone("1.4.0"), "none")
  assert.equal(next.m.snapshot().state, "hidden")
  // A manual check shows it.
  next.m.beginCheck(true)
  assert.equal(next.m.checkDone("1.4.0"), "download")
  assert.equal(next.m.snapshot().state, "available")

  // A newer version shows again.
  const newer = model({ store, c })
  newer.m.beginCheck(false)
  assert.equal(newer.m.checkDone("1.4.1"), "download")
  assert.equal(newer.m.snapshot().state, "available")
  assert.equal(newer.m.snapshot().version, "1.4.1")
})

test("Skip version also hides a source build's notice", () => {
  const { m } = model({ mode: "source" })
  m.beginCheck(false)
  m.checkDone("1.4.0")
  m.skip()
  assert.equal(m.snapshot().state, "hidden")
})

test("Update when idle installs at once when nothing runs", () => {
  const { m } = readyModel()
  assert.equal(m.startWaiting([]), "install")
  m.restarting()
  assert.equal(m.snapshot().state, "restarting")
})

test("Update when idle waits for the running turns, then installs", () => {
  const { m, c } = readyModel()
  assert.equal(m.startWaiting(bots), null)
  const s = m.snapshot()
  assert.equal(s.state, "waiting")
  assert.deepEqual(s.running, bots)
  assert.equal(s.deadline, new Date(c.t + 30 * 60_000).toISOString())
  assert.equal(m.interceptsQuit(), true)
  assert.equal(m.setRunning([bots[1]]), null)
  assert.equal(m.setRunning([]), "install")
})

test("the drain limit moves to limit and never restarts by itself", () => {
  const { m, c } = readyModel("1.4.0", { settings: readSettings('{"updates": {"drainMinutes": 10}}') })
  m.startWaiting(bots)
  c.advance(10 * 60_000 - 1)
  m.tick()
  assert.equal(m.snapshot().state, "waiting")
  c.advance(1)
  m.tick()
  assert.equal(m.snapshot().state, "limit")
  assert.equal(m.setRunning(bots), null, "still working: no restart")
  assert.equal(m.snapshot().drainMinutes, 10)
  // Wait longer adds drainMinutes.
  c.advance(60_000)
  m.waitLonger()
  assert.equal(m.snapshot().state, "waiting")
  assert.equal(m.snapshot().deadline, new Date(c.t + 10 * 60_000).toISOString())
  m.waitLonger()
  assert.equal(m.snapshot().deadline, new Date(c.t + 20 * 60_000).toISOString())
  // When the bots finish after the limit, the update installs.
  c.advance(30 * 60_000)
  m.tick()
  assert.equal(m.setRunning([]), "install")
})

test("Cancel update goes back to ready", () => {
  const { m } = readyModel()
  m.startWaiting(bots)
  m.cancelWait()
  assert.equal(m.snapshot().state, "ready")
  assert.equal(m.snapshot().deadline, undefined)
  // These do nothing outside waiting.
  m.cancelWait()
  m.waitLonger()
  m.tick()
  assert.equal(m.snapshot().state, "ready")
  assert.equal(m.setRunning(bots), null)
  assert.deepEqual(m.snapshot().running, bots, "the ready state names the working bots")
})

test("a newer version found while waiting downloads in the background and replaces the ready one", () => {
  const { m, c } = readyModel()
  m.startWaiting(bots)
  c.advance(DAY)
  assert.equal(m.autoCheckDue(), true)
  m.beginCheck(false)
  assert.equal(m.snapshot().state, "waiting", "the check runs in the background")
  assert.equal(m.checkDone("1.4.0"), "none", "the same version is not downloaded again")
  m.beginCheck(false)
  assert.equal(m.checkDone("1.4.1"), "download")
  m.downloadStarted("1.4.1", true)
  assert.equal(m.snapshot().state, "waiting")
  assert.equal(m.snapshot().version, "1.4.0")
  m.downloaded("1.4.1")
  assert.equal(m.snapshot().state, "waiting")
  assert.equal(m.snapshot().version, "1.4.1")
})

test("a failed background download keeps the ready update", () => {
  const { m } = readyModel()
  m.beginCheck(false)
  m.checkDone("1.4.1")
  m.downloadStarted("1.4.1", true)
  m.downloadFailed(new Error("net::ERR_CONNECTION_RESET"), true)
  assert.equal(m.snapshot().state, "ready")
  assert.equal(m.snapshot().version, "1.4.0")
})

test("a refused signature at install shows the signature error", () => {
  const { m } = readyModel()
  m.restarting()
  m.installFailed(new Error("Code signature at URL file:///x did not pass validation"))
  assert.equal(m.snapshot().state, "error")
  assert.equal(m.snapshot().errorText, "Couldn't verify the update's signature.")
})

test("a server that stays on its old version shows an error", () => {
  const { m } = model({ current: "1.4.0" })
  m.serverMismatch("v1.3.2")
  const s = m.snapshot()
  assert.equal(s.state, "error")
  assert.equal(s.error, "other")
  assert.equal(s.errorText, "The server is still on 1.3.2. Quit and reopen YarGPT.")
})

test("a store that cannot write does not break the updater", (t) => {
  t.mock.method(console, "error", () => {})
  const { m } = model({
    store: {
      read: () => null,
      write: () => {
        throw new Error("EROFS")
      },
    },
  })
  m.beginCheck(false)
  assert.equal(m.checkDone("1.3.2"), "none")
  assert.equal(console.error.mock.callCount(), 1)
})

test("onChange returns an unsubscribe function", () => {
  const { m } = model()
  let calls = 0
  const off = m.onChange(() => calls++)
  m.later()
  off()
  m.later()
  assert.equal(calls, 1)
})
