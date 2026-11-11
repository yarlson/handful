// The bridge window's window.handful: its view and its actions, nothing
// else. The main process answers only the main frame of this window.
const { contextBridge, ipcRenderer } = require("electron")

// The same list as bridgeActions in bridge.js. A sandboxed preload can
// require only electron, so it cannot import that list.
const actions = ["move", "later", "retry", "openSite"]

const handful = {
  get: () => ipcRenderer.invoke("bridge:get"),
  onChange(callback) {
    const listener = (_event, view) => callback(view)
    ipcRenderer.on("bridge:view", listener)
    return () => ipcRenderer.removeListener("bridge:view", listener)
  },
}
for (const name of actions) handful[name] = () => ipcRenderer.invoke(`bridge:${name}`)

contextBridge.exposeInMainWorld("handful", handful)
