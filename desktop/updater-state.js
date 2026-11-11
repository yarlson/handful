// The update states and policies of the app. This file has no Electron code,
// so node --test covers it; updater.js connects it to electron-updater, the
// window and the menus.

const HOUR = 60 * 60 * 1000
const DAY = 24 * HOUR

const releasesURL = "https://github.com/yarlson/handful/releases"
// The feed is the latest release's assets: handful-mac.yml, the zip and its
// blockmap. The channel handful in package.json's build.publish names the
// feed; latest-mac.yml in the same release is the old app's (docs/releasing.md).
const feedURL = `${releasesURL}/latest/download/`
const feedFile = "handful-mac.yml"

const defaultDrainMinutes = 30

const errorTexts = {
  offline: "Can't reach GitHub. Check your connection.",
  checksum: "The download didn't match the release. Try again.",
  signature: "Couldn't verify the update's signature.",
  disk: "Not enough space to update.",
  other: "The update didn't work. Try again.",
}

// parseVersion parses a semantic version such as 1.4.0 or v1.4.0-rc.1. It
// returns null for anything else.
function parseVersion(text) {
  const match = /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/.exec(String(text ?? "").trim())
  if (!match) return null
  return { core: match.slice(1, 4).map(Number), pre: match[4] ? match[4].split(".") : [] }
}

function comparePrerelease(a, b) {
  // A version without a prerelease is newer than any of its prereleases.
  if (a.length === 0 || b.length === 0) return Math.sign(b.length - a.length)
  for (let i = 0; i < Math.min(a.length, b.length); i++) {
    const order = compareIdentifier(a[i], b[i])
    if (order !== 0) return order
  }
  return Math.sign(a.length - b.length)
}

function compareIdentifier(a, b) {
  const numeric = /^\d+$/
  if (numeric.test(a) && numeric.test(b)) return Math.sign(Number(a) - Number(b))
  // Numeric identifiers sort before alphanumeric ones.
  if (numeric.test(a) !== numeric.test(b)) return numeric.test(a) ? -1 : 1
  return a < b ? -1 : a > b ? 1 : 0
}

// compareVersions returns -1, 0 or 1 as a is older, equal or newer than b,
// by semantic versioning, prereleases included. It returns null when one of
// them is not a version.
function compareVersions(a, b) {
  const x = parseVersion(a)
  const y = parseVersion(b)
  if (!x || !y) return null
  for (let i = 0; i < 3; i++) {
    if (x.core[i] !== y.core[i]) return Math.sign(x.core[i] - y.core[i])
  }
  return comparePrerelease(x.pre, y.pre)
}

// isNewer reports whether candidate is a version newer than current.
function isNewer(candidate, current) {
  return compareVersions(candidate, current) === 1
}

// plainVersion drops the leading v of a tag.
function plainVersion(text) {
  return String(text ?? "").trim().replace(/^v/, "")
}

function clampDrainMinutes(value) {
  if (typeof value !== "number" || !Number.isFinite(value)) return defaultDrainMinutes
  return Math.min(240, Math.max(5, Math.round(value)))
}

// readSettings reads the updates part of ~/.yargpt/config.json:
// {"updates": {"autoCheck": false, "drainMinutes": 60}}. A missing or
// invalid file gives the defaults.
function readSettings(text) {
  let updates = {}
  try {
    const config = JSON.parse(text ?? "{}")
    if (config && typeof config.updates === "object" && config.updates !== null) updates = config.updates
  } catch {
    // The server reports an invalid file; the app keeps its defaults.
  }
  return { autoCheck: updates.autoCheck !== false, drainMinutes: clampDrainMinutes(updates.drainMinutes) }
}

// errorKind maps an error of electron-updater, Squirrel.Mac or fetch to the
// kind that the window explains.
function errorKind(err) {
  const text = `${err?.code ?? ""} ${err?.message ?? err ?? ""} ${err?.cause?.code ?? ""}`
  if (/ENOSPC|EACCES|EROFS|EPERM|not enough space|no space left/i.test(text)) return "disk"
  if (/checksum mismatch|ERR_CHECKSUM_MISMATCH/i.test(text)) return "checksum"
  if (/code signature|code requirement|did not pass validation|codesign|signature/i.test(text)) return "signature"
  if (
    /ENOTFOUND|ECONNREFUSED|ECONNRESET|ETIMEDOUT|EAI_AGAIN|ENETUNREACH|EHOSTUNREACH|net::ERR_|fetch failed|TimeoutError|aborted due to timeout|socket hang up/i.test(
      text,
    )
  )
    return "offline"
  return "other"
}

function releaseURLFor(version) {
  return version ? `${releasesURL}/tag/v${plainVersion(version)}` : `${releasesURL}/latest`
}

// feedVersion reads the version of a handful-mac.yml.
function feedVersion(yml) {
  const match = /^version:[ \t]*['"]?([^'"\s#]+)['"]?[ \t]*(?:#.*)?$/m.exec(String(yml ?? ""))
  return match && parseVersion(match[1]) ? plainVersion(match[1]) : null
}

// isDeveloperIDSigned reads the output of codesign -dv --verbose=2. An
// ad-hoc signature has no Authority lines, and Squirrel.Mac refuses its
// updates, because its designated requirement is the hash of one build.
function isDeveloperIDSigned(codesignOutput) {
  return /^Authority=Developer ID Application\b/m.test(String(codesignOutput ?? ""))
}

function plural(n, one, many) {
  return `${n} ${n === 1 ? one : many}`
}

// botsWorkingText names the bots that work, for the dialogs.
function botsWorkingText(running) {
  const names = [...new Set((running ?? []).map((r) => r.name || "a bot"))]
  return `${plural(names.length, "bot is", "bots are")} working: ${names.join(", ")}.`
}

function finishingTurnsText(running) {
  return `Finishing ${plural((running ?? []).length, "running turn", "running turns")} before starting the new version…`
}

// menuItemFor gives the label of the update item in the app menu, and the
// action of a click.
function menuItemFor(s) {
  const v = s.version
  switch (s.state) {
    case "checking":
      return { label: "Checking for updates…", enabled: false, action: null }
    case "available":
      return { label: `Download Update ${v}`, enabled: true, action: "download" }
    case "downloading":
      return { label: `Downloading ${v}… ${Math.floor(s.percent ?? 0)}%`, enabled: false, action: null }
    case "ready":
    case "limit":
      return { label: `Restart to Update ${v}…`, enabled: true, action: "restart" }
    case "waiting":
      return { label: `Updating to ${v} when bots finish…`, enabled: true, action: "restart" }
    case "restarting":
      return { label: "Restarting…", enabled: false, action: null }
    case "source":
      return { label: `Get Handful ${v}…`, enabled: true, action: "openReleasePage" }
    default:
      return { label: "Check for updates…", enabled: true, action: "check" }
  }
}

function runningOf(body) {
  return Array.isArray(body?.running) ? body.running : []
}

// bundleChangeAction decides what to do before the agent is registered again
// for a new server, from the old server's answer to GET /api/version:
// {status, body}, or null when it does not answer. "drain" asks it to finish
// its turns first; an older server without the endpoint answers 404 and is
// replaced as before.
function bundleChangeAction(response) {
  if (!response || response.status !== 200) return "register"
  return runningOf(response.body).length > 0 ? "drain" : "register"
}

// serverVersionAction compares the version that the server reports with the
// app's. A dev build of the server, or one without /api/version, is not
// checked.
function serverVersionAction(appVersion, response) {
  if (!response || response.status !== 200) return "skip"
  const version = plainVersion(response.body?.version)
  if (!version || version.startsWith("dev")) return "skip"
  return version === plainVersion(appVersion) ? "match" : "mismatch"
}

const busyPhases = new Set(["checking", "downloading", "restarting"])
// In these phases an update is downloaded; a check can still find a newer one.
const readyPhases = new Set(["ready", "waiting", "limit"])

// UpdaterModel holds the update state. Its methods take the events of the
// updater and the user's choices, and return what updater.js must do.
class UpdaterModel {
  constructor({ current, now = Date.now, store = null, settings = readSettings("{}"), mode = "auto" }) {
    this.current = plainVersion(current)
    this.now = now
    this.store = store
    this.settings = settings
    this.mode = mode // auto: electron-updater; source: only reports a new release
    this.phase = "hidden"
    this.version = undefined
    this.pendingVersion = undefined
    this.progress = {}
    this.running = []
    this.deadline = null
    this.error = undefined
    this.errorText = undefined
    this.errorStage = undefined
    this.beforeCheck = "hidden"
    this.manualCheck = false
    this.revealed = false
    this.autoTries = 0
    this.listeners = new Set()
    const saved = this.load()
    this.lastCheck = Number.isFinite(saved.lastCheck) ? saved.lastCheck : null
    this.skipped = typeof saved.skipped === "string" ? saved.skipped : null
    // Later hides the pill until the next launch or for a day: a launch
    // starts without it.
    this.laterUntil = null
  }

  load() {
    try {
      const data = JSON.parse(this.store?.read() ?? "{}")
      return data && typeof data === "object" ? data : {}
    } catch {
      return {}
    }
  }

  save() {
    try {
      this.store?.write(
        JSON.stringify({ lastCheck: this.lastCheck, skipped: this.skipped, laterUntil: this.laterUntil }, null, 2) + "\n",
      )
    } catch (err) {
      console.error("save the update state:", err)
    }
  }

  onChange(listener) {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  changed() {
    const snapshot = this.snapshot()
    for (const listener of this.listeners) listener(snapshot)
  }

  setSettings(settings) {
    this.settings = settings
  }

  get hiddenByLater() {
    return this.laterUntil !== null && this.now() < this.laterUntil
  }

  // skippedNow reports whether the user skipped the version that the pill
  // would show, and no manual check showed it again.
  get skippedNow() {
    return !this.revealed && this.skipped !== null && this.skipped === this.version && ["available", "ready", "source"].includes(this.phase)
  }

  snapshot() {
    const state = this.skippedNow ? "hidden" : this.phase
    const s = {
      state,
      current: this.current,
      drainMinutes: this.settings.drainMinutes,
      releaseUrl: releaseURLFor(state === "hidden" ? undefined : this.version),
      hiddenByLater: this.hiddenByLater,
    }
    if (state !== "hidden" && this.version) s.version = this.version
    if (state === "downloading") Object.assign(s, this.progress)
    if (readyPhases.has(state)) s.running = this.running
    if ((state === "waiting" || state === "limit") && this.deadline !== null) s.deadline = new Date(this.deadline).toISOString()
    if (state === "error") Object.assign(s, { error: this.error, errorText: this.errorText })
    return s
  }

  // autoCheckDue reports whether an automatic check should run now: at most
  // one a day, counted from the last check that succeeded.
  autoCheckDue() {
    if (!this.settings.autoCheck || busyPhases.has(this.phase)) return false
    return this.lastCheck === null || this.now() - this.lastCheck >= DAY
  }

  // beginCheck starts a check. It returns false when one already runs.
  beginCheck(manual) {
    if (this.phase === "checking" || this.phase === "restarting") return false
    this.manualCheck = manual
    if (manual) {
      this.laterUntil = null
      this.revealed = true
    }
    // With an update downloaded, a check looks for a newer one in the background.
    if (!readyPhases.has(this.phase) && this.phase !== "downloading") {
      this.beforeCheck = this.phase === "error" ? "hidden" : this.phase
      this.phase = "checking"
    }
    this.changed()
    return true
  }

  // checkDone takes the latest version of the feed and returns what to do:
  // "download", "upToDate" or "none".
  checkDone(latest) {
    this.lastCheck = this.now()
    this.save()
    const manual = this.manualCheck
    const known = readyPhases.has(this.phase) || this.phase === "downloading"
    let result = "none"
    if (!isNewer(latest, this.current)) {
      if (this.phase === "checking") this.phase = "hidden"
      result = manual && !known ? "upToDate" : "none"
    } else if (known) {
      result = isNewer(latest, this.pendingVersion ?? this.version) && this.phase !== "downloading" ? "download" : "none"
    } else if (this.mode === "source") {
      this.phase = "source"
      this.version = plainVersion(latest)
    } else if (!manual && this.skipped === plainVersion(latest)) {
      this.phase = "hidden"
      this.version = plainVersion(latest)
    } else {
      this.version = plainVersion(latest)
      this.phase = "available"
      result = "download"
    }
    if (manual) this.revealed = true
    this.changed()
    return result
  }

  checkFailed(err) {
    const kind = errorKind(err)
    // An automatic check that is offline stays silent, and so does any
    // failed automatic check of a copy that can only report a release. The
    // hourly tick tries again, since the last check that succeeded is more
    // than a day ago.
    if (!this.manualCheck && (kind === "offline" || this.mode === "source")) {
      if (this.phase === "checking") this.phase = this.beforeCheck
    } else if (this.phase === "checking") {
      this.fail(kind, "check")
    }
    this.changed()
  }

  fail(kind, stage, text = errorTexts[kind]) {
    this.phase = "error"
    this.error = kind
    this.errorText = text
    this.errorStage = stage
  }

  // downloadStarted: auto is true for the download that follows a check.
  downloadStarted(version, auto) {
    if (readyPhases.has(this.phase)) {
      this.pendingVersion = plainVersion(version)
    } else {
      this.phase = "downloading"
      this.version = plainVersion(version)
      this.progress = { percent: 0 }
      this.autoTries = auto ? this.autoTries + 1 : this.autoTries
    }
    this.changed()
  }

  downloadProgress({ percent, transferred, total }) {
    if (this.phase !== "downloading") return
    this.progress = { percent, transferred, total }
    this.changed()
  }

  downloadCancelled() {
    if (this.phase === "downloading") this.phase = "available"
    this.changed()
  }

  // downloadFailed: a failed automatic download offers Download; a failed
  // download that the user started shows the error.
  downloadFailed(err, auto) {
    if (this.pendingVersion !== undefined && readyPhases.has(this.phase)) {
      this.pendingVersion = undefined
    } else if (auto && this.autoTries <= 1) {
      this.phase = "available"
    } else {
      this.fail(errorKind(err), "download")
    }
    this.changed()
  }

  downloaded(version) {
    this.version = plainVersion(version)
    this.pendingVersion = undefined
    if (!readyPhases.has(this.phase)) this.phase = "ready"
    this.changed()
  }

  later() {
    this.laterUntil = this.now() + DAY
    this.save()
    this.changed()
  }

  skip() {
    if (this.version) this.skipped = this.version
    this.revealed = false
    this.save()
    this.changed()
  }

  // interceptsQuit reports whether ⌘Q should ask about the update first.
  interceptsQuit() {
    return readyPhases.has(this.phase) && !this.skippedNow
  }

  // startWaiting starts to wait for the running turns, up to drainMinutes.
  startWaiting(running) {
    if (!readyPhases.has(this.phase)) return null
    if (this.phase === "ready") this.deadline = this.now() + this.settings.drainMinutes * 60_000
    this.phase = "waiting"
    this.running = Array.isArray(running) ? running : []
    this.changed()
    return this.running.length === 0 ? "install" : null
  }

  // setRunning takes the server's running turns. It returns "install" when
  // the app waits and nothing runs any more.
  setRunning(running) {
    const next = Array.isArray(running) ? running : []
    const same = JSON.stringify(next) === JSON.stringify(this.running)
    this.running = next
    const install = (this.phase === "waiting" || this.phase === "limit") && next.length === 0
    if (!same) this.changed()
    return install ? "install" : null
  }

  // tick moves from waiting to limit when the time is up. The app never
  // restarts on its own while bots work; the user decides.
  tick() {
    if (this.phase === "waiting" && this.deadline !== null && this.now() >= this.deadline) {
      this.phase = "limit"
      this.changed()
    }
  }

  waitLonger() {
    if (this.phase !== "waiting" && this.phase !== "limit") return
    this.deadline = Math.max(this.deadline ?? 0, this.now()) + this.settings.drainMinutes * 60_000
    this.phase = "waiting"
    this.changed()
  }

  cancelWait() {
    if (this.phase !== "waiting" && this.phase !== "limit") return
    this.phase = "ready"
    this.deadline = null
    this.changed()
  }

  restarting() {
    this.phase = "restarting"
    this.changed()
  }

  installFailed(err) {
    this.fail(errorKind(err), "install")
    this.changed()
  }

  serverMismatch(version) {
    this.fail("other", "server", `The server is still on ${plainVersion(version)}. Quit and reopen Handful.`)
    this.changed()
  }
}

module.exports = {
  HOUR,
  DAY,
  feedURL,
  feedFile,
  releasesURL,
  errorTexts,
  parseVersion,
  compareVersions,
  isNewer,
  plainVersion,
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
}
