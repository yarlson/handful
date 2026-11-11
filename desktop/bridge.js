// The bridge to Handful, the new name of YarGPT. YarGPT 0.13.0 is the last
// YarGPT release: it looks for Handful's release feed, offers to move, and
// on Move to Handful lets running bots finish, downloads Handful, checks its
// signature, installs it, unregisters YarGPT's server and opens Handful.
// This file has no Electron code: the network, the disk, the programs, the
// launch agent and the clock come in as dependencies, so node --test covers
// it. main.js and bridge-window.js connect it to Electron.
const crypto = require("node:crypto")
const fsp = require("node:fs/promises")
const os = require("node:os")
const path = require("node:path")
const { HOUR, botsWorkingText, compareVersions, parseVersion, plainVersion } = require("./updater-state")
const { runningOf } = require("./updater-io")

const releaseBase = "https://github.com/yarlson/handful/releases/latest/download/"
const feedURL = `${releaseBase}handful-mac.yml`

// downloadURL is the zip of the release that the feed names. A release
// published after the feed was read cannot change it.
function downloadURL(feed) {
  return `https://github.com/yarlson/handful/releases/download/v${feed.version}/${feed.path}`
}

// handfulRequirement is the code requirement that Handful.app must satisfy:
// Apple's anchor, a Developer ID Application certificate, and YarGPT's own
// team. A team identifier alone is not enough, because any signature can
// claim one.
function handfulRequirement(teamID) {
  return (
    "anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] and " +
    `certificate leaf[field.1.2.840.113635.100.6.1.13] and certificate leaf[subject.OU] = "${teamID}"`
  )
}
const siteURL = "https://handful.dev"
const handfulBundleID = "dev.yarlson.handful"
const handfulApp = "Handful.app"

const maxFeedBytes = 64 * 1024
const maxZipBytes = 2 * 1024 * 1024 * 1024
const feedTimeout = 15_000
const downloadTimeout = 30 * 60_000
const recheckInterval = 6 * HOUR
const drainPollInterval = 5_000
const offerPollInterval = 5_000

const texts = {
  heading: "YarGPT is now Handful",
  intro: 'Same app, new name: the old one said "GPT", but it runs Claude Code. Your bots, chats, teams and settings come with you.',
  how: "YarGPT lets running bots finish, downloads Handful and opens it in its place.",
  move: "Move to Handful",
  moveWhenDone: "Move when they finish",
  later: "Later",
  menu: "Move to Handful…",
  failedHeading: "Couldn't move to Handful",
  keeps: "YarGPT keeps running as before. Nothing was moved.",
  tryAgain: "Try again",
  site: "Download from handful.dev",
  verifying: "Checking Handful's signature…",
  installing: "Installing Handful…",
  opening: "Opening Handful…",
  reasons: {
    network: "Couldn't reach GitHub.",
    mismatch: "The download didn't match the release.",
    signature: "Handful's signature didn't check out.",
    write: "Couldn't write to /Applications.",
  },
}

// BridgeError is a failure of a step with the kind of reason that S3 shows:
// network, mismatch, signature or write.
class BridgeError extends Error {
  constructor(kind, message, options) {
    super(message, options)
    this.kind = kind
  }
}

function plural(n, one, many) {
  return `${n} ${n === 1 ? one : many}`
}

function uniqueNames(running) {
  return [...new Set((running ?? []).map((r) => r?.name || "a bot"))]
}

// busyText: "2 bots are working: Dev Lead, Core Dev. The move starts when
// they finish."
function busyText(running) {
  return `${botsWorkingText(running)} The move starts when they finish.`
}

function waitingText(running) {
  return `Waiting for ${plural(uniqueNames(running).length, "bot", "bots")} to finish…`
}

function downloadingText(percent) {
  return `Downloading Handful… ${Math.max(0, Math.min(100, Math.floor(percent ?? 0)))}%`
}

// limitDialog is the question at the drain limit: the app never moves on its
// own while bots work.
function limitDialog(minutes, running, waitMinutes) {
  return {
    message: `Still working after ${minutes} min: ${uniqueNames(running).join(", ")}. Moving now stops them.`,
    buttons: ["Move now", `Wait ${waitMinutes} more min`, "Cancel"],
    choices: ["move", "wait", "cancel"],
  }
}

// reasonText is the reason line of S3. A failure of no known kind shows the
// error's own message.
function reasonText(err) {
  return texts.reasons[err?.kind] ?? (String(err?.message ?? err ?? "").trim() || "Something went wrong.")
}

// view is what the window shows for a state: S1 (offer), S2 (moving) or S3
// (failed). The page renders it as text, so it holds no markup.
function view(state) {
  if (state.screen === "failed") {
    return {
      screen: "failed",
      heading: texts.failedHeading,
      lines: [state.reason, texts.keeps],
      status: "",
      buttons: [
        { id: "retry", label: texts.tryAgain, primary: true },
        { id: "site", label: texts.site, primary: false },
      ],
    }
  }
  if (state.screen === "moving") {
    return { screen: "moving", heading: texts.heading, lines: [], status: statusText(state), buttons: [] }
  }
  const busy = state.running.length > 0
  return {
    screen: "offer",
    heading: texts.heading,
    lines: busy ? [texts.intro, texts.how, busyText(state.running)] : [texts.intro, texts.how],
    status: "",
    buttons: [
      { id: "move", label: busy ? texts.moveWhenDone : texts.move, primary: true },
      { id: "later", label: texts.later, primary: false },
    ],
  }
}

function statusText(state) {
  switch (state.step) {
    case "drain":
      return waitingText(state.running)
    case "download":
      return downloadingText(state.percent)
    case "verify":
      return texts.verifying
    case "install":
      return texts.installing
    case "open":
      return texts.opening
    default:
      return ""
  }
}

// teamIdentifierOf reads the team ID of a signature from the output of
// codesign -dv --verbose=2. An ad-hoc signature reports "not set", and gives
// null, which keeps the bridge off.
function teamIdentifierOf(codesignOutput) {
  const match = /^TeamIdentifier=([A-Z0-9]{10})$/m.exec(String(codesignOutput ?? ""))
  return match ? match[1] : null
}

const zipPathPattern = /^[A-Za-z0-9._-]+\.zip$/
const sha512Pattern = /^[A-Za-z0-9+/]{86}==$/

function feedScalar(text = "") {
  const value = text.trim()
  const quoted = /^'([^']*)'$/.exec(value) ?? /^"([^"\\]*)"$/.exec(value)
  return quoted ? quoted[1] : value
}

// parseFeedLines reads handful-mac.yml as electron-builder writes it:
// scalars at the top, and a files list of maps of scalars. It returns null
// for any other form, or a key given twice.
function parseFeedLines(text) {
  const top = new Map()
  const files = []
  let inFiles = false
  for (const line of text.split(/\r?\n/)) {
    if (line.trim() === "" || line.trimStart().startsWith("#")) continue
    const key = /^([A-Za-z0-9_]+):(?:[ \t]+(.*))?$/.exec(line)
    const item = /^ {2}- ([A-Za-z0-9_]+):(?:[ \t]+(.*))?$/.exec(line)
    const field = /^ {4}([A-Za-z0-9_]+):(?:[ \t]+(.*))?$/.exec(line)
    if (key) {
      if (top.has(key[1])) return null
      inFiles = key[1] === "files"
      top.set(key[1], inFiles ? null : feedScalar(key[2]))
    } else if (inFiles && item) {
      files.push(new Map([[item[1], feedScalar(item[2])]]))
    } else if (inFiles && field && files.length > 0 && !files.at(-1).has(field[1])) {
      files.at(-1).set(field[1], feedScalar(field[2]))
    } else {
      return null
    }
  }
  return { top, files }
}

// parseHandfulFeed reads version, path, sha512 and size from
// handful-mac.yml. The feed is untrusted: it returns null unless each field
// is in its expected form, so that path cannot leave the release's folder.
// The size and sha512 come from the top, or from the files entry of path.
function parseHandfulFeed(text) {
  if (typeof text !== "string" || Buffer.byteLength(text) > maxFeedBytes) return null
  const parsed = parseFeedLines(text)
  if (!parsed) return null
  const { top, files } = parsed
  const version = top.get("version")
  const zip = top.get("path")
  if (!version || !parseVersion(version) || !zip || !zipPathPattern.test(zip)) return null
  const entry = files.find((f) => f.get("url") === zip)
  const sha512 = top.get("sha512") ?? entry?.get("sha512")
  const size = top.get("size") ?? entry?.get("size")
  if (!sha512 || !sha512Pattern.test(sha512) || !/^[1-9]\d{0,9}$/.test(size ?? "")) return null
  if (Number(size) > maxZipBytes) return null
  return { version: plainVersion(version), path: zip, sha512, size: Number(size) }
}

// readCapped reads a response body up to max bytes. It returns null when the
// body is longer.
async function readCapped(res, max) {
  const declared = Number(res.headers?.get?.("content-length"))
  if (Number.isFinite(declared) && declared > max) return null
  const chunks = []
  let total = 0
  if (res.body) {
    for await (const chunk of res.body) {
      total += chunk.length
      if (total > max) return null
      chunks.push(Buffer.from(chunk))
    }
  }
  return Buffer.concat(chunks).toString("utf8")
}

// fetchHandfulFeed returns the parsed feed, or null when it is missing,
// unreachable or not in the expected form.
async function fetchHandfulFeed({ fetchImpl = fetch, timeoutSignal = AbortSignal.timeout, log = console.debug } = {}) {
  try {
    const res = await fetchImpl(feedURL, { signal: timeoutSignal(feedTimeout), redirect: "follow" })
    if (!res.ok) {
      log(`handful feed: ${res.status}`)
      return null
    }
    const feed = parseHandfulFeed(await readCapped(res, maxFeedBytes))
    if (!feed) log("handful feed: not in the expected form")
    return feed
  } catch (err) {
    log(`handful feed: ${err?.message ?? err}`)
    return null
  }
}

// installDir is the folder for Handful.app: /Applications when YarGPT.app is
// in it or in one of its folders, else the folder that holds YarGPT.app.
function installDir(yargptApp) {
  const resolved = path.resolve(yargptApp)
  if (resolved.startsWith("/Applications/")) return "/Applications"
  return path.dirname(resolved)
}

const writeCodes = new Set(["EACCES", "EPERM", "EROFS", "ENOSPC"])
const writeTexts = /Permission denied|Operation not permitted|Read-only file system|No space left on device/i

// asWriteError gives an install error the write kind when the folder cannot
// be written; others keep their message.
function asWriteError(err) {
  if (writeCodes.has(err?.code) || writeTexts.test(`${err?.stderr ?? ""} ${err?.message ?? ""}`)) {
    return new BridgeError("write", err.message, { cause: err })
  }
  return err
}

// createBridge makes the bridge. Its dependencies:
//   fetchImpl, timeoutSignal(ms): fetch and AbortSignal.timeout
//   api: serverAPI of the server (version, drain, undrain)
//   exec(file, args): runs a program, resolves {stdout, stderr}, rejects on failure
//   agent: {unregister(), register(), startServer()} of YarGPT's launch agent
//   shell: {openPath(path) -> error string, openExternal(url)}
//   exit(): quits YarGPT without asking
//   askLimit({message, buttons}): the drain limit's question, resolves the button's index
//   settings(): {drainMinutes}
//   teamID: the team ID of YarGPT's own signature, or null to stay off
//   yargptApp: the path of YarGPT.app
//   tmpRoot: the folder for temporary folders
//   sleep(ms), now(), timers {setTimeout, clearTimeout}, log, fs (node:fs/promises)
function createBridge(deps) {
  const {
    fetchImpl = fetch,
    timeoutSignal = AbortSignal.timeout,
    api,
    exec,
    agent,
    shell,
    exit,
    askLimit,
    settings,
    teamID,
    yargptApp,
    tmpRoot = os.tmpdir(),
    sleep,
    now = Date.now,
    timers = globalThis,
    log = console.debug,
    fs = fsp,
  } = deps
  const state = { found: false, feed: null, screen: "offer", visible: false, running: [], step: null, percent: 0, reason: "" }
  const listeners = new Set()
  let moving = null
  let drained = false
  let recheckTimer = null
  let offerTimer = null

  function changed() {
    syncOfferPoll()
    const snapshot = { found: state.found, visible: state.visible, screen: state.screen, view: view(state) }
    for (const listener of listeners) listener(snapshot)
  }

  // While S1 shows, ask the server for its running bots, so that the window
  // names them.
  function syncOfferPoll() {
    const want = state.visible && state.screen === "offer"
    if (want && !offerTimer) offerTimer = timers.setTimeout(pollOffer, offerPollInterval)
    if (!want && offerTimer) {
      timers.clearTimeout(offerTimer)
      offerTimer = null
    }
  }

  async function pollOffer() {
    offerTimer = null
    await refreshRunning()
    syncOfferPoll()
  }

  async function refreshRunning() {
    if (state.screen !== "offer") return
    const running = runningOf(await api.version())
    if (state.screen !== "offer") return
    if (JSON.stringify(running) === JSON.stringify(state.running)) return
    state.running = running
    changed()
  }

  async function checkFeed() {
    const feed = await fetchHandfulFeed({ fetchImpl, timeoutSignal, log })
    if (!feed || state.found) return !!feed
    state.found = true
    state.feed = feed
    state.running = runningOf(await api.version())
    state.screen = "offer"
    state.visible = true
    changed()
    return true
  }

  // start checks the feed now and every 6 hours until it is found. A copy
  // without a Developer ID team ID stays off.
  async function start() {
    if (!teamID) return
    const found = await checkFeed()
    if (!found) recheckTimer = timers.setTimeout(() => start().catch((err) => log(`handful feed: ${err.message}`)), recheckInterval)
  }

  function stop() {
    if (recheckTimer) timers.clearTimeout(recheckTimer)
    if (offerTimer) timers.clearTimeout(offerTimer)
    recheckTimer = null
    offerTimer = null
  }

  function setStep(step, extra = {}) {
    Object.assign(state, { screen: "moving", step, percent: 0 }, extra)
    changed()
  }

  async function makeTemp(temps) {
    const dir = await fs.mkdtemp(path.join(tmpRoot, "yargpt-handful-"))
    temps.push(dir)
    return dir
  }

  // drain asks the server to finish its turns and waits for them. It returns
  // false when the user cancels at the limit.
  async function drain() {
    await api.drain()
    drained = true
    const started = now()
    let deadline = started + settings().drainMinutes * 60_000
    for (;;) {
      const running = runningOf(await api.version())
      if (running.length === 0) return true
      setStep("drain", { running })
      if (now() >= deadline) {
        const waitMinutes = settings().drainMinutes
        const question = limitDialog(Math.round((now() - started) / 60_000), running, waitMinutes)
        const choice = question.choices[await askLimit(question)] ?? "cancel"
        if (choice === "move") return true
        if (choice === "cancel") {
          await releaseDrain()
          Object.assign(state, { screen: "offer", step: null, running })
          changed()
          return false
        }
        deadline = now() + waitMinutes * 60_000
      }
      await sleep(drainPollInterval)
    }
  }

  async function releaseDrain() {
    if (!drained) return
    drained = false
    await api.undrain()
  }

  // download streams the zip to a temporary file, hashes it as it goes, and
  // stops above the feed's size. It returns the file.
  async function download(feed, temps) {
    setStep("download")
    const file = path.join(await makeTemp(temps), feed.path)
    const out = await fs.open(file, "wx")
    const hash = crypto.createHash("sha512")
    let received = 0
    try {
      let res
      try {
        res = await fetchImpl(downloadURL(feed), { signal: timeoutSignal(downloadTimeout), redirect: "follow" })
      } catch (err) {
        throw new BridgeError("network", `download ${feed.path}: ${err.message}`, { cause: err })
      }
      if (!res.ok) throw new BridgeError("network", `download ${feed.path}: ${res.status}`)
      const iterator = res.body?.[Symbol.asyncIterator]?.()
      if (!iterator) throw new BridgeError("network", `download ${feed.path}: no body`)
      for (;;) {
        let next
        try {
          next = await iterator.next()
        } catch (err) {
          throw new BridgeError("network", `download ${feed.path}: ${err.message}`, { cause: err })
        }
        if (next.done) break
        const chunk = Buffer.from(next.value)
        received += chunk.length
        if (received > feed.size) {
          await iterator.return?.().catch(() => {})
          throw new BridgeError("mismatch", `${feed.path} is larger than ${feed.size} bytes`)
        }
        hash.update(chunk)
        await out.write(chunk)
        const percent = Math.floor((received / feed.size) * 100)
        if (percent !== state.percent) setStep("download", { percent })
      }
    } finally {
      await out.close()
    }
    if (received !== feed.size) throw new BridgeError("mismatch", `${feed.path} has ${received} bytes, not ${feed.size}`)
    if (hash.digest("base64") !== feed.sha512) throw new BridgeError("mismatch", `the sha512 of ${feed.path} does not match`)
    return file
  }

  async function plistValue(bundle, key) {
    const { stdout } = await exec("/usr/bin/plutil", ["-extract", key, "raw", "-o", "-", path.join(bundle, "Contents", "Info.plist")])
    return String(stdout).trim()
  }

  // verify unpacks the zip and checks that it holds only Handful.app, signed
  // with a Developer ID of YarGPT's team, with Handful's bundle ID. It
  // returns the app.
  async function verify(zip, temps) {
    setStep("verify")
    const dir = await makeTemp(temps)
    try {
      await exec("/usr/bin/ditto", ["-x", "-k", zip, dir])
    } catch (err) {
      throw new BridgeError("mismatch", `unpack the download: ${err.message}`, { cause: err })
    }
    const entries = await fs.readdir(dir)
    const app = path.join(dir, handfulApp)
    if (entries.length !== 1 || entries[0] !== handfulApp || !(await fs.lstat(app)).isDirectory()) {
      throw new BridgeError("mismatch", `the download holds ${entries.join(", ") || "nothing"}, not only ${handfulApp}`)
    }
    try {
      await exec("/usr/bin/codesign", ["--verify", "--deep", "--strict", `-R=${handfulRequirement(teamID)}`, app])
    } catch (err) {
      throw new BridgeError("signature", `codesign --verify: ${err.stderr || err.message}`, { cause: err })
    }
    let bundleID = null
    try {
      bundleID = await plistValue(app, "CFBundleIdentifier")
    } catch (err) {
      throw new BridgeError("signature", `read the bundle ID: ${err.message}`, { cause: err })
    }
    if (bundleID !== handfulBundleID) throw new BridgeError("signature", `Handful's bundle ID is ${bundleID}`)
    return app
  }

  async function exists(file) {
    try {
      await fs.lstat(file)
      return true
    } catch (err) {
      if (err.code === "ENOENT") return false
      throw err
    }
  }

  // installedVersion is the version of an installed Handful.app, "" when it
  // cannot be read, or null when there is none.
  async function installedVersion(target) {
    if (!(await exists(target))) return null
    try {
      return await plistValue(target, "CFBundleShortVersionString")
    } catch {
      return ""
    }
  }

  // install copies Handful.app next to the folder's old one and swaps them,
  // unless the same or a newer Handful is there. It returns the installed app.
  async function install(feed, source) {
    setStep("install")
    const dir = installDir(yargptApp)
    const target = path.join(dir, handfulApp)
    const current = await installedVersion(target)
    // compareVersions gives null for a version it cannot read: replace that app.
    if (current && (compareVersions(current, feed.version) ?? -1) >= 0) return target
    const partial = path.join(dir, `.${handfulApp}.partial`)
    const old = path.join(dir, `.${handfulApp}.old`)
    try {
      await fs.rm(partial, { recursive: true, force: true })
      await exec("/usr/bin/ditto", [source, partial])
      const aside = current !== null
      if (aside) {
        await fs.rm(old, { recursive: true, force: true })
        await fs.rename(target, old)
      }
      try {
        await fs.rename(partial, target)
      } catch (err) {
        if (aside) await fs.rename(old, target)
        throw err
      }
      if (aside) await fs.rm(old, { recursive: true, force: true }).catch((err) => log(`remove the old Handful: ${err.message}`))
    } catch (err) {
      await fs.rm(partial, { recursive: true, force: true }).catch(() => {})
      throw asWriteError(err)
    }
    return target
  }

  async function open(app) {
    setStep("open")
    const err = await shell.openPath(app)
    if (err) throw new Error(`Couldn't open Handful: ${err}`)
  }

  async function cleanup(temps) {
    for (const dir of temps.splice(0)) {
      await fs.rm(dir, { recursive: true, force: true }).catch((err) => log(`remove ${dir}: ${err.message}`))
    }
  }

  async function run() {
    const temps = []
    let unregistered = false
    try {
      // The feed read at start can be stale: a newer Handful can be out.
      const feed = await fetchHandfulFeed({ fetchImpl, timeoutSignal, log })
      if (!feed) throw new BridgeError("network", "read handful-mac.yml")
      state.feed = feed
      if (!(await drain())) return
      const zip = await download(feed, temps)
      const source = await verify(zip, temps)
      const app = await install(feed, source)
      // A failed unregister can have booted the server out already.
      unregistered = true
      await agent.unregister()
      await cleanup(temps)
      await open(app)
      exit()
    } catch (err) {
      log(`move to Handful: ${err?.message ?? err}`)
      if (unregistered) {
        try {
          await agent.register()
          await agent.startServer()
        } catch (again) {
          log(`register YarGPT's server again: ${again?.message ?? again}`)
        }
      }
      await releaseDrain()
      await cleanup(temps)
      Object.assign(state, { screen: "failed", step: null, reason: reasonText(err), visible: true })
      changed()
    }
  }

  // move runs the steps. Each one can run again, so Try again starts over.
  function move() {
    if (!state.found) return Promise.resolve()
    moving ??= run().finally(() => {
      moving = null
    })
    return moving
  }

  return {
    start,
    stop,
    checkFeed,
    move,
    retry: move,
    // later hides the window until the next launch; the menu item stays.
    later() {
      if (state.screen === "moving") {
        state.visible = false
      } else {
        Object.assign(state, { screen: "offer", visible: false })
      }
      changed()
    },
    // show opens the window from the menu: S1, the move that runs, or S3
    // while it shows (later turns a hidden S3 into S1).
    async show() {
      if (!state.found) return
      state.visible = true
      changed()
      await refreshRunning()
    },
    openSite: () => shell.openExternal(siteURL),
    isMoving: () => moving !== null,
    holdsDrain: () => drained,
    releaseDrain,
    snapshot: () => ({ found: state.found, visible: state.visible, screen: state.screen, view: view(state) }),
    onChange(listener) {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    // menuItem is the app menu's Move to Handful…, or null before the feed
    // is found.
    menuItem(click) {
      return state.found ? { id: "handful", label: texts.menu, click } : null
    },
  }
}

// The methods of window.handful in bridge-preload.js, without get. Each one
// is the IPC channel bridge:<name>.
const bridgeActions = ["move", "later", "retry", "openSite"]

// registerBridgeIPC answers the bridge:* channels only for the main frame of
// the bridge window: contents() returns its webContents, or null.
function registerBridgeIPC(ipcMain, bridge, contents) {
  const trusted = (event) => {
    const own = contents()
    return !!own && event.sender === own && !!event.senderFrame && event.senderFrame.parent === null
  }
  ipcMain.handle("bridge:get", (event) => {
    if (!trusted(event)) throw new Error("not allowed")
    return bridge.snapshot().view
  })
  for (const name of bridgeActions) {
    ipcMain.handle(`bridge:${name}`, async (event) => {
      if (!trusted(event)) throw new Error("not allowed")
      await bridge[name]()
    })
  }
}

module.exports = {
  feedURL,
  downloadURL,
  handfulRequirement,
  releaseBase,
  siteURL,
  handfulBundleID,
  maxFeedBytes,
  maxZipBytes,
  recheckInterval,
  drainPollInterval,
  texts,
  BridgeError,
  busyText,
  waitingText,
  downloadingText,
  limitDialog,
  reasonText,
  view,
  teamIdentifierOf,
  parseHandfulFeed,
  fetchHandfulFeed,
  installDir,
  createBridge,
  bridgeActions,
  registerBridgeIPC,
}
