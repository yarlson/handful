// The YarGPT app: a window on the server, which runs as a launch agent of
// this bundle, so that it keeps running when the app quits.
const { app, BrowserWindow, Menu, dialog, shell } = require("electron")
const { execFile } = require("node:child_process")
const os = require("node:os")
const path = require("node:path")
const { serverURL, isAppURL, isWebURL, ensureSettingsFile } = require("./app")

const agentLabel = "dev.yarlson.yargpt.server"
const agent = { type: "agentService", serviceName: `${agentLabel}.plist` }
const dataDir = path.join(os.homedir(), ".yargpt")
const logFile = path.join(os.homedir(), "Library", "Logs", "YarGPT", "server.log")

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
    registerAgent()
    return showWindow()
  })
}

// registerAgent makes launchd start the server now and at every login.
function registerAgent() {
  try {
    app.setLoginItemSettings({ openAtLogin: true, ...agent })
  } catch (err) {
    dialog.showErrorBox("YarGPT could not register its server", err.message)
    return
  }
  if (app.getLoginItemSettings(agent).status !== "requires-approval") return
  const choice = dialog.showMessageBoxSync({
    type: "info",
    message: "Allow the YarGPT server to run in the background",
    detail: "Turn on YarGPT in System Settings › General › Login Items & Extensions. Then the server starts now and at every login.",
    buttons: ["Open System Settings", "Later"],
  })
  if (choice === 0) void shell.openExternal("x-apple.systempreferences:com.apple.LoginItems-Settings.extension")
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
    await new Promise((resolve) => setTimeout(resolve, 250))
  }
  return false
}

// ensureServer starts the launch agent when the server does not answer, for
// example after Exit server, and waits for it.
async function ensureServer() {
  if (await serverAnswers()) return true
  await new Promise((resolve) => {
    execFile("/bin/launchctl", ["kickstart", `gui/${process.getuid()}/${agentLabel}`], { timeout: 10_000 }, () => resolve())
  })
  if (await waitForServer(8_000)) return true
  // launchd keeps the code requirement of the server from the registration,
  // and an ad-hoc signature changes with every build, so it refuses to start
  // a rebuilt or updated server until the agent is registered again.
  try {
    app.setLoginItemSettings({ openAtLogin: false, ...agent })
    app.setLoginItemSettings({ openAtLogin: true, ...agent })
  } catch (err) {
    console.error("register the server agent again:", err)
  }
  return waitForServer(20_000)
}

// showWindow shows the window, and creates it when there is none. activate
// also fires at launch, so a second call waits for the first.
function showWindow() {
  if (window) {
    window.show()
    window.focus()
    return Promise.resolve()
  }
  opening ??= openWindow().finally(() => {
    opening = null
  })
  return opening
}

async function openWindow() {
  if (!(await ensureServer())) {
    const choice = dialog.showMessageBoxSync({
      type: "error",
      message: "The YarGPT server did not start",
      detail: `Its log is ${logFile}.`,
      buttons: ["Open Log", "Close"],
    })
    if (choice === 0) void shell.openPath(logFile)
    return
  }
  // Hidden until the page paints, so a dark-mode window does not flash white.
  window = new BrowserWindow({ width: 1280, height: 860, title: "YarGPT", show: false })
  window.once("ready-to-show", () => window?.show())
  window.on("closed", () => {
    window = null
  })
  routeLinks(window.webContents)
  await window.loadURL(serverURL)
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
}

async function openSettings() {
  let file
  try {
    file = ensureSettingsFile(dataDir)
  } catch (err) {
    dialog.showErrorBox("YarGPT could not create the settings file", err.message)
    return
  }
  const err = await shell.openPath(file)
  if (err) dialog.showErrorBox("YarGPT could not open the settings file", `${file}: ${err}`)
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
