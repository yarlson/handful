// The YarGPT app: a window on the server, which runs as a launch agent of
// this bundle, so that it keeps running when the app quits.
const { app, BrowserWindow, Menu, dialog, session, shell } = require("electron")
const { execFile } = require("node:child_process")
const crypto = require("node:crypto")
const fs = require("node:fs")
const os = require("node:os")
const path = require("node:path")
const { setTimeout: sleep } = require("node:timers/promises")
const { serverURL, isAppURL, isWebURL, ensureSettingsFile } = require("./app")

const agentLabel = "dev.yarlson.yargpt.server"
const agent = { type: "agentService", serviceName: `${agentLabel}.plist` }
const agentTarget = `gui/${process.getuid()}/${agentLabel}`
const serverBinary = path.join(process.resourcesPath, "yargpt")
const dataDir = path.join(os.homedir(), ".yargpt")
const logFile = path.join(os.homedir(), "Library", "Logs", "YarGPT", "server.log")
// The page copies messages to the clipboard and plays videos full screen.
// Electron grants every other permission too, unless a handler refuses it.
const pagePermissions = new Set(["clipboard-sanitized-write", "fullscreen"])

let window = null
let opening = null

if (!app.requestSingleInstanceLock()) {
  app.quit()
} else {
  app.on("second-instance", () => void showWindow())
  app.on("activate", () => void showWindow())
  // Keep running without windows, as macOS apps do; activate opens a new one.
  app.on("window-all-closed", () => {})
  app.whenReady().then(() => {
    Menu.setApplicationMenu(buildMenu())
    session.defaultSession.setPermissionRequestHandler((_contents, permission, callback, details) =>
      callback(pagePermissions.has(permission) && isAppURL(details.requestingUrl)),
    )
    session.defaultSession.setPermissionCheckHandler(
      (_contents, permission, origin) => pagePermissions.has(permission) && isAppURL(origin),
    )
    return showWindow()
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

// registerAgent registers the agent again, so that launchd loads it with the
// code requirement of this server and starts it. A registration while the old
// job is loaded keeps the old requirement. bootout returns at once, and
// launchd removes the job only when its server has stopped, so wait for that.
async function registerAgent() {
  await launchctl("bootout")
  const deadline = Date.now() + 30_000
  while ((await launchctl("print")) && Date.now() < deadline) await sleep(250)
  app.setLoginItemSettings({ openAtLogin: false, ...agent })
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
    await registerAgent()
    if (agentStatus() !== "enabled") return false
    registerAgain = true
  }
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
// activate also fires at launch, so a second call waits for the first.
function showWindow() {
  opening ??= openWindow()
    .catch((err) => dialog.showErrorBox("YarGPT could not open its window", err.message))
    .finally(() => {
      opening = null
    })
  return opening
}

async function openWindow() {
  if (!(await ensureServer())) {
    showServerError()
    return
  }
  if (window) {
    window.show()
    window.focus()
    return
  }
  // Hidden until the page paints, so a dark-mode window does not flash white.
  window = new BrowserWindow({ width: 1280, height: 860, title: "YarGPT", show: false })
  window.once("ready-to-show", () => window?.show())
  window.on("closed", () => {
    window = null
  })
  routeLinks(window.webContents)
  try {
    await window.loadURL(serverURL)
  } catch (err) {
    // The next activation creates the window again.
    window.destroy()
    throw err
  }
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
  const choice = dialog.showMessageBoxSync({
    type: "error",
    message: "The YarGPT server did not start",
    detail: `Its log is ${logFile}.`,
    buttons: ["Open Log", "Close"],
  })
  if (choice === 0) void openFile(logFile, "the server log")
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

function buildMenu() {
  return Menu.buildFromTemplate([
    {
      label: app.name,
      submenu: [
        { role: "about" },
        { type: "separator" },
        { id: "settings", label: "Settings…", accelerator: "Command+,", click: () => void openSettings() },
        { type: "separator" },
        { role: "services" },
        { type: "separator" },
        { role: "hide" },
        { role: "hideOthers" },
        { role: "unhide" },
        { type: "separator" },
        { role: "quit" },
      ],
    },
    { role: "editMenu" },
    { role: "viewMenu" },
    { role: "windowMenu" },
  ])
}
