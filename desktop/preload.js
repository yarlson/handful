// The bridges between the page and the main process: window.handfulUpdater
// for the updater, and window.handfulSettings for the Settings page. The main
// process answers only the main frame of the server's page.
const { contextBridge, ipcRenderer } = require("electron")

// The same list as bridgeMethods in updater.js. A sandboxed preload can
// require only electron, so it cannot import that list.
const methods = [
  "download",
  "cancelDownload",
  "later",
  "skip",
  "restartNow",
  "updateWhenIdle",
  "cancelUpdate",
  "waitLonger",
  "retry",
  "openReleasePage",
  "check",
]

const updater = {
  get: () => ipcRenderer.invoke("updater:get"),
  onChange(callback) {
    const listener = (_event, state) => callback(state)
    ipcRenderer.on("updater:state", listener)
    return () => ipcRenderer.removeListener("updater:state", listener)
  },
}
for (const name of methods) updater[name] = () => ipcRenderer.invoke(`updater:${name}`)

contextBridge.exposeInMainWorld("handfulUpdater", updater)

// The methods of settingsMethods in settings-bridge.js.
//   restartServer("now" | "idle") -> {ok: true} | {ok: false, error}
//   chooseClaude() -> the chosen program's path, or null
//   openFile() opens config.json in the default editor
//   revealFile() shows config.json in the Finder, revealLog() the server log
contextBridge.exposeInMainWorld("handfulSettings", {
  restartServer: (when) => ipcRenderer.invoke("settings:restartServer", when),
  chooseClaude: () => ipcRenderer.invoke("settings:chooseClaude"),
  openFile: () => ipcRenderer.invoke("settings:openFile"),
  revealFile: () => ipcRenderer.invoke("settings:revealFile"),
  revealLog: () => ipcRenderer.invoke("settings:revealLog"),
})
