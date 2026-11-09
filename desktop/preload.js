// The bridge between the page and the updater in the main process:
// window.yargptUpdater. The main process answers only the main frame of the
// server's page.
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

contextBridge.exposeInMainWorld("yargptUpdater", updater)
