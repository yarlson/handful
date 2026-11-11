// The bridge between the Settings page and the main process:
// window.handfulSettings. It restarts the server, chooses the Claude Code
// program, opens or reveals the settings file, and shows the old app in the
// Finder. Electron and the launch agent come in as dependencies, so the tests
// run without them.
const path = require("node:path")

// The methods of window.handfulSettings, in preload.js. Each one is the IPC
// channel settings:<name>.
const settingsMethods = ["restartServer", "chooseClaude", "openFile", "revealFile", "revealLog", "showYarGPTApp"]

// The old app, which the move notice's button shows (S4): in /Applications,
// or next to Handful.app. The first of oldAppPlaces that exists is shown.
const oldAppName = "YarGPT.app"

// oldAppPlaces returns where showYarGPTApp looks for YarGPT.app, in order.
function oldAppPlaces(appBundle) {
  const places = [path.join("/Applications", oldAppName)]
  if (appBundle) {
    const nextTo = path.join(path.dirname(appBundle), oldAppName)
    if (!places.includes(nextTo)) places.push(nextTo)
  }
  return places
}

const restartWhens = new Set(["now", "idle"])

// settingsAPI calls the server's /api/settings and /api/restart. Each call
// returns {status, statusText, body}, or null when the server does not
// answer. Unlike serverAPI, it reads the body of an error, which says what
// went wrong.
function settingsAPI(base, fetchImpl = fetch) {
  async function call(method, endpoint, body, timeout) {
    try {
      // The API's CSRF guard requires a JSON content type on every change.
      const init = method === "GET" ? {} : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }
      const res = await fetchImpl(`${base}${endpoint}`, { method, ...init, signal: AbortSignal.timeout(timeout) })
      let parsed = null
      try {
        parsed = await res.json()
      } catch {
        parsed = null
      }
      return { status: res.status, statusText: res.statusText, body: parsed }
    } catch {
      return null
    }
  }
  return {
    settings: (timeout = 2000) => call("GET", "/api/settings", null, timeout),
    restart: (when) => call("POST", "/api/restart", { when }, 5000),
  }
}

// createSettingsBridge makes the bridge's methods.
//   api: settingsAPI of the server
//   registerAgent(): boots the agent out and registers it again
//   startServer(): starts the registered server and resolves whether it answers
//   dialog: {showOpenDialog(options), showErrorBox(title, text)}
//   shell: {openPath(file), showItemInFolder(file)}
//   ensureSettingsFile(): creates config.json when it is missing, returns its path
//   configFile(): the path of config.json, which revealFile shows; the data
//     folder can move while the app runs, so it is asked each time
//   logFile: the server log, which revealLog shows
//   appBundle: the path of the running Handful.app
//   exists(file): whether file exists
function createSettingsBridge({ api, registerAgent, startServer, dialog, shell, ensureSettingsFile, configFile, logFile, appBundle, exists }) {
  let restarting = null

  async function restartNow() {
    await registerAgent()
    if (await startServer()) return { ok: true }
    return { ok: false, error: `The server did not start. Its log is ${logFile}.` }
  }

  async function restartIdle() {
    const res = await api.restart("idle")
    if (!res) return { ok: false, error: "The server did not answer." }
    if (res.status === 202) return { ok: true }
    return { ok: false, error: res.body?.message || res.statusText || `The server answered ${res.status}.` }
  }

  // restartServer restarts the server now, or once no work runs. It refuses
  // when the server reports that a restarted server would not start. A
  // server that does not answer is down already, so it has nothing to
  // refuse.
  async function restartServer(when) {
    if (!restartWhens.has(when)) return { ok: false, error: 'when must be "now" or "idle".' }
    if (restarting) return { ok: false, error: "The server is restarting already." }
    const current = await api.settings()
    const startError = current?.body?.startError
    if (startError) return { ok: false, error: startError }
    restarting = when === "now" ? restartNow() : restartIdle()
    try {
      return await restarting
    } finally {
      restarting = null
    }
  }

  async function chooseClaude() {
    const result = await dialog.showOpenDialog({
      title: "Choose the Claude Code program",
      buttonLabel: "Choose",
      // Claude Code installs itself in ~/.local/bin, a hidden folder.
      properties: ["openFile", "showHiddenFiles"],
    })
    if (result.canceled || result.filePaths.length === 0) return null
    return result.filePaths[0]
  }

  async function openFile() {
    let file
    try {
      file = ensureSettingsFile()
    } catch (err) {
      dialog.showErrorBox("Handful could not create the settings file", err.message)
      return
    }
    const err = await shell.openPath(file)
    if (err) dialog.showErrorBox("Handful could not open the settings file", `${file}: ${err}`)
  }

  // The page names no file: these show only config.json and the server log.
  const revealFile = () => shell.showItemInFolder(configFile())
  const revealLog = () => shell.showItemInFolder(logFile)

  // showYarGPTApp shows the old app in the Finder, so that the user can
  // remove it, and resolves whether it found one. The page names no path.
  async function showYarGPTApp() {
    const found = oldAppPlaces(appBundle).find((place) => exists(place))
    if (!found) return false
    shell.showItemInFolder(found)
    return true
  }

  return { restartServer, chooseClaude, openFile, revealFile, revealLog, showYarGPTApp }
}

// registerSettingsIPC answers the settings:* channels for the trusted page
// only: isTrusted(event) as for the updater. Only restartServer takes an
// argument from the page.
function registerSettingsIPC(ipcMain, bridge, isTrusted) {
  for (const name of settingsMethods) {
    ipcMain.handle(`settings:${name}`, async (event, arg) => {
      if (!isTrusted(event)) throw new Error("not allowed")
      return name === "restartServer" ? bridge.restartServer(arg) : bridge[name]()
    })
  }
}

module.exports = { settingsMethods, oldAppPlaces, settingsAPI, createSettingsBridge, registerSettingsIPC }
