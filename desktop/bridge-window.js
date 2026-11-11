// The window of the bridge to Handful: a small window with a local page,
// bridge.html, that shows S1, S2 or S3 from bridge.js's view. Its preload
// exposes only this window's view and actions, and the IPC answers only
// this window. bridge.js holds the logic and its tests.
const { BrowserWindow, dialog, ipcMain } = require("electron")
const path = require("node:path")
const { registerBridgeIPC } = require("./bridge")

const width = 440
const minHeight = 150
const maxHeight = 600

// createBridgeWindow shows the bridge's window while the bridge wants it.
// Closing the window is Later. It returns askLimit, the native question at
// the drain limit.
function createBridgeWindow(bridge) {
  let win = null

  const contents = () => (win && !win.isDestroyed() ? win.webContents : null)
  registerBridgeIPC(ipcMain, bridge, contents)

  function create() {
    win = new BrowserWindow({
      width,
      height: 230,
      useContentSize: true,
      resizable: false,
      minimizable: false,
      maximizable: false,
      fullscreenable: false,
      title: "YarGPT is now Handful",
      show: false,
      webPreferences: {
        preload: path.join(__dirname, "bridge-preload.js"),
        contextIsolation: true,
        sandbox: true,
        nodeIntegration: false,
        spellcheck: false,
        enablePreferredSizeMode: true,
      },
    })
    const own = win
    const wc = own.webContents
    wc.setWindowOpenHandler(() => ({ action: "deny" }))
    wc.on("will-navigate", (event) => event.preventDefault())
    wc.on("will-redirect", (event) => event.preventDefault())
    // The window fits the page's height: S1 with bots has a line more.
    wc.on("preferred-size-changed", (_event, size) => {
      if (!own.isDestroyed()) own.setContentSize(width, Math.min(maxHeight, Math.max(minHeight, Math.ceil(size.height))))
    })
    own.once("ready-to-show", () => {
      if (bridge.snapshot().visible) own.show()
    })
    own.on("closed", () => {
      if (win === own) win = null
      // Closing the window is Later: hidden until the next launch, and a
      // move that runs goes on.
      if (bridge.snapshot().visible) bridge.later()
    })
    void own.loadFile(path.join(__dirname, "bridge.html")).catch((err) => console.error("load the bridge window:", err))
  }

  bridge.onChange((s) => {
    if (!s.visible) {
      win?.hide()
      return
    }
    if (!win) {
      create()
      return
    }
    win.webContents.send("bridge:view", s.view)
    if (!win.isVisible()) {
      win.show()
      win.focus()
    }
  })

  // askLimit asks what to do when bots still work at the drain limit, and
  // resolves the index of the chosen button.
  async function askLimit({ message, buttons }) {
    const options = { type: "question", message, buttons, defaultId: 1, cancelId: buttons.length - 1 }
    const parent = win && !win.isDestroyed() && win.isVisible() ? win : null
    const { response } = parent ? await dialog.showMessageBox(parent, options) : await dialog.showMessageBox(options)
    return response
  }

  return { askLimit }
}

module.exports = { createBridgeWindow }
