// The YarGPT app: a window on the server, which runs as a launch agent of
// this bundle, so that it keeps running when the app quits.
const { app, BrowserWindow, Menu, clipboard, dialog, ipcMain, session, shell } = require("electron")
const { execFile } = require("node:child_process")
const crypto = require("node:crypto")
const fs = require("node:fs")
const os = require("node:os")
const path = require("node:path")
const { setTimeout: sleep } = require("node:timers/promises")
const { promisify } = require("node:util")
const { createBridge, teamIdentifierOf } = require("./bridge")
const { createBridgeWindow } = require("./bridge-window")
const { serverURL, isAppURL, isWebURL, isCheckForUpdatesURL, isTrustedFrame, ensureSettingsFile } = require("./app")
const { contextMenuTemplate } = require("./context-menu")
const { appMenuTemplate } = require("./menu")
const { lastStartupErrorIn, serverErrorDialog } = require("./server-log")
const { createSettingsBridge, registerSettingsIPC, settingsAPI } = require("./settings-bridge")
const { createUpdater } = require("./updater")
const { bundleChangeAction, finishingTurnsText, serverVersionAction } = require("./updater-state")
const { loadSettings, runningOf, serverAPI } = require("./updater-io")

const agentLabel = "dev.yarlson.yargpt.server"
const agent = { type: "agentService", serviceName: `${agentLabel}.plist` }
const agentTarget = `gui/${process.getuid()}/${agentLabel}`
const serverBinary = path.join(process.resourcesPath, "yargpt")
const dataDir = path.join(os.homedir(), ".yargpt")
const configFile = path.join(dataDir, "config.json")
const logFile = path.join(os.homedir(), "Library", "Logs", "YarGPT", "server.log")
// The page copies messages to the clipboard and plays videos full screen.
// Electron grants every other permission too, unless a handler refuses it.
const pagePermissions = new Set(["clipboard-sanitized-write", "fullscreen"])

const api = serverAPI(serverURL)

let window = null
let opening = null
let serverVersionChecked = false
// The bridge to Handful; null until it starts, and in a copy without a
// Developer ID team ID.
let bridge = null
let bridgeFound = false
let bridgeQuitting = false

// isTrusted reports whether an IPC call comes from the main frame of the
// app's page; the updater and the settings bridge answer only it.
const isTrusted = (event) => isTrustedFrame(event.senderFrame?.url, !!event.senderFrame && event.senderFrame.parent === null)

const updater = createUpdater({
  serverURL,
  dataDir,
  isAppURL,
  isTrusted,
  onStateChange: () => {
    if (app.isReady()) Menu.setApplicationMenu(buildMenu())
  },
  getWindow: () => window,
})

// The Settings page's window.yargptSettings. Restart now registers the agent
// again, as a new server does, and waits for the server to answer.
const settingsBridge = createSettingsBridge({
  api: settingsAPI(serverURL),
  registerAgent,
  startServer: async () => agentStatus() === "enabled" && startServer(serverDigest(), true),
  dialog: {
    showOpenDialog: (options) => (window ? dialog.showOpenDialog(window, options) : dialog.showOpenDialog(options)),
    showErrorBox: (title, text) => dialog.showErrorBox(title, text),
  },
  shell,
  ensureSettingsFile: () => ensureSettingsFile(dataDir),
  configFile,
  logFile,
})

if (!app.requestSingleInstanceLock()) {
  app.quit()
} else {
  app.on("second-instance", (_event, argv) => {
    if (argv.some(isCheckForUpdatesURL)) openAppURL(argv.find(isCheckForUpdatesURL))
    else void showWindow()
  })
  app.on("activate", () => void showWindow())
  // The menu-bar icon's Check for updates… opens yargpt://check-for-updates.
  // macOS sends it before ready when the URL launches the app.
  app.on("open-url", (event, url) => {
    event.preventDefault()
    openAppURL(url)
  })
  // Keep running without windows, as macOS apps do; activate opens a new one.
  app.on("window-all-closed", () => {})
  // ⌘Q releases a move's hold on the server's drain mode, and asks about a
  // downloaded update first. A move that succeeds quits with app.exit, which
  // skips this.
  app.on("before-quit", (event) => {
    if (!bridgeQuitting && bridge?.holdsDrain()) {
      event.preventDefault()
      bridgeQuitting = true
      void bridge.releaseDrain().finally(() => app.quit())
      return
    }
    updater.handleBeforeQuit(event)
  })
  app.whenReady().then(() => {
    Menu.setApplicationMenu(buildMenu())
    registerSettingsIPC(ipcMain, settingsBridge, isTrusted)
    void updater.start().catch((err) => console.error("start the updater:", err))
    session.defaultSession.setPermissionRequestHandler((_contents, permission, callback, details) =>
      callback(pagePermissions.has(permission) && isAppURL(details.requestingUrl)),
    )
    session.defaultSession.setPermissionCheckHandler(
      (_contents, permission, origin) => pagePermissions.has(permission) && isAppURL(origin),
    )
    // The bridge looks for Handful once the window has opened.
    return showWindow().finally(() => void startBridge().catch((err) => console.error("start the Handful bridge:", err)))
  })
}

// launchd keeps the code requirement of the server from the registration of
// the agent, and an ad-hoc signature changes with every build, so launchd
// refuses to start a rebuilt or updated server until the agent is registered
// again. This file holds the SHA-256 of the server that the current
// registration started.
function registeredServerFile() {
  return path.join(app.getPath("userData"), "registered-server.sha256")
}

function serverDigest() {
  return crypto.createHash("sha256").update(fs.readFileSync(serverBinary)).digest("hex")
}

function registeredDigest() {
  try {
    return fs.readFileSync(registeredServerFile(), "utf8")
  } catch (err) {
    if (err.code === "ENOENT") return null
    throw err
  }
}

function saveRegisteredDigest(digest) {
  try {
    fs.writeFileSync(registeredServerFile(), digest)
  } catch (err) {
    // The next launch then registers the agent again and restarts the server.
    console.error("save the digest of the registered server:", err)
  }
}

// agentStatus is the source of truth for the registration:
// setLoginItemSettings only logs the errors of macOS.
function agentStatus() {
  return app.getLoginItemSettings(agent).status
}

// unregisterAgent stops the server and unregisters the agent, so that
// launchd no longer starts it. bootout returns at once, and launchd removes
// the job only when its server has stopped, so wait for that. The data folder
// stays.
async function unregisterAgent() {
  await launchctl("bootout")
  const deadline = Date.now() + 30_000
  while ((await launchctl("print")) && Date.now() < deadline) await sleep(250)
  app.setLoginItemSettings({ openAtLogin: false, ...agent })
}

// registerAgent registers the agent again, so that launchd loads it with the
// code requirement of this server and starts it. A registration while the old
// job is loaded keeps the old requirement, so unregister it first.
async function registerAgent() {
  await unregisterAgent()
  app.setLoginItemSettings({ openAtLogin: true, ...agent })
}

// launchctl runs a launchctl subcommand on the server job and returns whether
// it succeeded. It fails when launchd has not loaded the job.
function launchctl(subcommand) {
  return new Promise((resolve) => {
    execFile("/bin/launchctl", [subcommand, agentTarget], { timeout: 10_000 }, (err) => resolve(!err))
  })
}

async function serverAnswers() {
  try {
    const res = await fetch(`${serverURL}/api/bots`, { signal: AbortSignal.timeout(1000) })
    return res.ok
  } catch {
    return false
  }
}

async function waitForServer(ms) {
  const deadline = Date.now() + ms
  while (Date.now() < deadline) {
    if (await serverAnswers()) return true
    await sleep(250)
  }
  return false
}

// openAppURL handles a yargpt: address: it shows the window and checks for
// updates.
function openAppURL(url) {
  if (!isCheckForUpdatesURL(url)) return
  void app.whenReady().then(async () => {
    await showWindow()
    await updater.check()
  })
}

// drainOldServer lets the running server finish its turns before the agent
// is registered again for a new server, which stops the old one. A server
// without /api/version answers 404 and is replaced at once, as before.
async function drainOldServer() {
  const first = await api.version(1000)
  if (bundleChangeAction(first) !== "drain") return
  await api.drain()
  const abort = new AbortController()
  let startNow = false
  const box = dialog
    .showMessageBox({
      type: "info",
      message: finishingTurnsText(runningOf(first)),
      detail: "Start now stops them.",
      buttons: ["Start now"],
      signal: abort.signal,
    })
    .then(() => {
      startNow = true
    })
  try {
    while (!startNow) {
      await Promise.race([sleep(5_000), box])
      if (startNow || runningOf(await api.version()).length === 0) break
    }
  } finally {
    abort.abort()
  }
}

// checkServerVersion compares the server's version with the app's, once per
// launch. The server of an old app can still run after an update; then the
// agent is registered again once. A dev server, or one without
// /api/version, is not checked.
async function checkServerVersion() {
  if (serverVersionChecked || !app.isPackaged) return
  serverVersionChecked = true
  if (serverVersionAction(app.getVersion(), await api.version()) !== "mismatch") return
  await drainOldServer()
  await registerAgent()
  if (agentStatus() === "enabled") await startServer(serverDigest(), true)
  const again = await api.version()
  if (serverVersionAction(app.getVersion(), again) === "mismatch") updater.serverMismatch(again.body.version)
}

// ensureServer makes the server of this app run. It registers the agent when
// the server binary changed since the last registration, and starts the
// server when it does not answer, for example after Exit server. It returns
// false when the server does not start.
async function ensureServer() {
  const digest = serverDigest()
  const registered = digest === registeredDigest()
  if (registered && (await serverAnswers())) return true
  if (agentStatus() === "requires-approval") return false
  let registerAgain = false
  if (!registered || agentStatus() !== "enabled") {
    // A new server replaces the old one: let the old one finish its turns.
    if (!registered) await drainOldServer()
    await registerAgent()
    if (agentStatus() !== "enabled") return false
    registerAgain = true
  }
  return startServer(digest, registerAgain)
}

// startServer starts the registered agent's server and waits until it
// answers. It saves the digest of the server that started.
async function startServer(digest, registerAgain) {
  // kickstart starts a stopped job and leaves a running one alone, which can
  // still be starting, or stopping after Exit server. So ask again until the
  // server answers.
  const started = Date.now()
  while (Date.now() - started < 45_000) {
    await launchctl("kickstart")
    if (await waitForServer(2_000)) {
      saveRegisteredDigest(digest)
      return true
    }
    // A registration can keep the requirement of the old server. Then launchd
    // refuses the first spawn, macOS records the new requirement at the next
    // spawn one ThrottleInterval later, and a registration after that loads it.
    if (registerAgain && Date.now() - started > 20_000) {
      registerAgain = false
      await registerAgent()
    }
  }
  return false
}

// showWindow makes the server run, then shows the window or creates it.
// activate also fires at launch, so a second call waits for the first. It
// resolves whether the window shows the server's page.
function showWindow() {
  opening ??= openWindow()
    .catch((err) => {
      dialog.showErrorBox("YarGPT could not open its window", err.message)
      return false
    })
    .finally(() => {
      opening = null
    })
  return opening
}

async function openWindow() {
  if (!(await ensureServer())) {
    showServerError()
    return false
  }
  await checkServerVersion()
  if (window) {
    window.show()
    window.focus()
    return true
  }
  // Hidden until the page paints, so a dark-mode window does not flash white.
  // The preload script gives the page window.yargptUpdater and window.yargptSettings.
  window = new BrowserWindow({
    width: 1280,
    height: 860,
    title: "YarGPT",
    show: false,
    webPreferences: { preload: path.join(__dirname, "preload.js"), contextIsolation: true, sandbox: true },
  })
  window.once("ready-to-show", () => window?.show())
  window.on("closed", () => {
    window = null
  })
  routeLinks(window.webContents)
  showContextMenus(window)
  try {
    await window.loadURL(serverURL)
  } catch (err) {
    // The next activation creates the window again.
    window.destroy()
    throw err
  }
  return true
}

function showServerError() {
  const status = agentStatus()
  if (status === "requires-approval") {
    const choice = dialog.showMessageBoxSync({
      type: "info",
      message: "Allow the YarGPT server to run in the background",
      detail: "Turn on YarGPT in System Settings › General › Login Items & Extensions. Then the server starts now and at every login.",
      buttons: ["Open System Settings", "Later"],
    })
    if (choice === 0) void shell.openExternal("x-apple.systempreferences:com.apple.LoginItems-Settings.extension")
    return
  }
  if (status !== "enabled") {
    dialog.showErrorBox("YarGPT could not register its server", `macOS reports its launch agent as ${status}. Open YarGPT again to try again.`)
    return
  }
  // The detail shows the server's last startup error, such as an invalid
  // config.json.
  const { options, actions } = serverErrorDialog(lastStartupErrorIn(logFile), logFile)
  const action = actions[dialog.showMessageBoxSync(options)]
  if (action === "openSettingsFile") void openSettings()
  else if (action === "openLog") void openFile(logFile, "the server log")
}

function openInBrowser(url) {
  if (isWebURL(url)) void shell.openExternal(url)
}

// routeLinks keeps the window on the server's pages and sends other links to
// the default browser.
function routeLinks(contents) {
  contents.setWindowOpenHandler(({ url }) => {
    // Connect opens an empty tab first and sets its address later.
    if (url === "about:blank") return { action: "allow", overrideBrowserWindowOptions: { show: false } }
    openInBrowser(url)
    return { action: "deny" }
  })
  contents.on("did-create-window", (child) => {
    child.webContents.setWindowOpenHandler(() => ({ action: "deny" }))
    child.webContents.on("will-navigate", (event, url) => {
      event.preventDefault()
      openInBrowser(url)
      child.close()
    })
  })
  contents.on("will-navigate", (event, url) => {
    if (isAppURL(url)) return
    event.preventDefault()
    openInBrowser(url)
  })
  contents.on("will-frame-navigate", (event) => {
    if (!event.isMainFrame && !isAppURL(event.url)) event.preventDefault()
  })
  contents.on("will-redirect", (event) => {
    if (!isAppURL(event.url)) event.preventDefault()
  })
}

// showContextMenus shows the edit, link and spelling commands on a right-click.
// Electron shows no menu of its own.
function showContextMenus(win) {
  const contents = win.webContents
  contents.on("context-menu", (_event, params) => {
    const template = contextMenuTemplate(params, {
      replaceMisspelling: (word) => contents.replaceMisspelling(word),
      learnWord: (word) => contents.session.addWordToSpellCheckerDictionary(word),
      copyText: (text) => clipboard.writeText(text),
    })
    if (template.length > 0) Menu.buildFromTemplate(template).popup({ window: win })
  })
}

async function openFile(file, name) {
  const err = await shell.openPath(file)
  if (err) dialog.showErrorBox(`YarGPT could not open ${name}`, `${file}: ${err}`)
}

async function openSettings() {
  let file
  try {
    file = ensureSettingsFile(dataDir)
  } catch (err) {
    dialog.showErrorBox("YarGPT could not create the settings file", err.message)
    return
  }
  await openFile(file, "the settings file")
}

// openSettingsPage shows the window on the Settings page.
async function openSettingsPage() {
  if (!(await showWindow()) || !window) return
  try {
    await window.loadURL(`${serverURL}/settings`)
  } catch (err) {
    console.error("open the Settings page:", err)
  }
}

// appBundle is the path of YarGPT.app.
function appBundle() {
  return path.resolve(process.execPath, "..", "..", "..")
}

// ownTeamID reads the team ID of the app's own signature. A dev copy, or one
// signed ad-hoc, has none, and the bridge stays off.
function ownTeamID() {
  if (!app.isPackaged) return Promise.resolve(null)
  return new Promise((resolve) => {
    execFile("/usr/bin/codesign", ["-dv", "--verbose=2", appBundle()], { timeout: 10_000 }, (_err, stdout, stderr) =>
      resolve(teamIdentifierOf(`${stdout}\n${stderr}`)),
    )
  })
}

const execFileAsync = promisify(execFile)

// startBridge starts the bridge to Handful: it looks for Handful's release
// now and every 6 hours, and shows its window when it finds one.
async function startBridge() {
  if (bridge) return
  const teamID = await ownTeamID()
  if (!teamID) {
    console.debug("handful bridge: off, the app has no Developer ID team ID")
    return
  }
  let bridgeWindow = null
  bridge = createBridge({
    api,
    exec: (file, args) => execFileAsync(file, args, { timeout: 10 * 60_000, maxBuffer: 4 * 1024 * 1024 }),
    agent: {
      unregister: unregisterAgent,
      register: registerAgent,
      startServer: async () => agentStatus() === "enabled" && startServer(serverDigest(), true),
    },
    shell,
    // app.exit skips before-quit, so the updater does not ask about an update.
    exit: () => app.exit(0),
    askLimit: (question) => bridgeWindow.askLimit(question),
    settings: () => loadSettings(configFile),
    teamID,
    yargptApp: appBundle(),
    sleep: (ms) => sleep(ms),
  })
  bridgeWindow = createBridgeWindow(bridge)
  bridge.onChange((s) => {
    if (s.found === bridgeFound) return
    bridgeFound = s.found
    Menu.setApplicationMenu(buildMenu())
  })
  await bridge.start()
}

function buildMenu() {
  const moveItem = bridge?.menuItem(() => void bridge.show()) ?? null
  return Menu.buildFromTemplate(
    appMenuTemplate({ appName: app.name, updateItem: updater.menuItem(), moveItem, openSettingsPage, openSettingsFile: openSettings }),
  )
}
