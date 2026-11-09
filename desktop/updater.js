// The updater of the app, in the main process. Release builds, signed with a
// Developer ID, check the latest release's latest-mac.yml once a day,
// download an update in the background, and install it only when the user
// asks: Restart now, Update when idle, or ⌘Q. Copies that are signed ad-hoc,
// such as make app builds, cannot update themselves, because Squirrel.Mac
// refuses an update whose signature does not satisfy the running app's
// designated requirement; they only report a new release. updater-state.js
// holds the states and policies.
const { app, BrowserWindow, dialog, ipcMain, shell } = require("electron")
const { execFile } = require("node:child_process")
const fs = require("node:fs")
const path = require("node:path")
const { feedURL, UpdaterModel, botsWorkingText, isDeveloperIDSigned, menuItemFor } = require("./updater-state")
const { createScheduler, fetchFeedVersion, fileStore, loadSettings, runningOf, serverAPI } = require("./updater-io")

const readyPollInterval = 30_000
const waitPollInterval = 5_000
const installTimeout = 120_000

// The methods of window.yargptUpdater, in preload.js, without get.
const bridgeMethods = [
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

// signatureMode returns "auto" for a bundle signed with a Developer ID, and
// "source" for other signatures, such as ad-hoc ones. electron-builder writes
// app-update.yml, the feed of electron-updater, only for the zip of
// make app-universal; a make app build without it only reports releases too.
function signatureMode() {
  if (!fs.existsSync(path.join(process.resourcesPath, "app-update.yml"))) return Promise.resolve("source")
  const bundle = path.resolve(process.execPath, "..", "..", "..")
  return new Promise((resolve) => {
    execFile("/usr/bin/codesign", ["-dv", "--verbose=2", bundle], { timeout: 10_000 }, (_err, stdout, stderr) =>
      resolve(isDeveloperIDSigned(`${stdout}\n${stderr}`) ? "auto" : "source"),
    )
  })
}

// createUpdater makes the updater. isTrusted(event) reports whether an IPC
// call comes from the main frame of the app's page; onStateChange runs after
// each change, for example to rebuild the menu.
function createUpdater({ serverURL, dataDir, isTrusted, isAppURL, onStateChange, getWindow }) {
  const configFile = path.join(dataDir, "config.json")
  const api = serverAPI(serverURL)
  const model = new UpdaterModel({
    current: app.getVersion(),
    store: fileStore(path.join(app.getPath("userData"), "update-state.json")),
    settings: loadSettings(configFile),
  })
  let mode = "off" // off, auto or source; set by start
  let autoUpdater = null
  let token = null
  let installing = false
  let quitting = false
  let pollTimer = null
  let pollInterval = 0
  let scheduler = null
  // started settles when start knows the mode; a check waits for it.
  let started = Promise.resolve()

  model.onChange((snapshot) => {
    for (const win of BrowserWindow.getAllWindows()) {
      const contents = win.webContents
      if (!contents.isDestroyed() && isAppURL(contents.getURL())) contents.send("updater:state", snapshot)
    }
    syncPoll(snapshot.state)
    onStateChange?.(snapshot)
  })

  // syncPoll asks the server for its running turns: every 5 s while the app
  // waits for them, and every 30 s while an update is ready, so that the
  // window can name the bots that work.
  function syncPoll(state) {
    const interval = state === "waiting" || state === "limit" ? waitPollInterval : state === "ready" ? readyPollInterval : 0
    if (interval === pollInterval) return
    clearTimeout(pollTimer)
    pollTimer = null
    pollInterval = interval
    if (interval) pollTimer = setTimeout(poll, interval)
  }

  async function poll() {
    pollTimer = null
    const interval = pollInterval
    const response = await api.version()
    model.tick()
    if (model.setRunning(runningOf(response)) === "install") install()
    if (pollInterval === interval && !pollTimer && interval) pollTimer = setTimeout(poll, interval)
  }

  function loadUpdater() {
    ;({ autoUpdater } = require("electron-updater"))
    // The app starts each download itself, right after a check, so that a
    // skipped version is not downloaded.
    autoUpdater.autoDownload = false
    autoUpdater.autoInstallOnAppQuit = false
    autoUpdater.allowPrerelease = false
    autoUpdater.allowDowngrade = false
    autoUpdater.on("download-progress", (p) => model.downloadProgress({ percent: p.percent, transferred: p.transferred, total: p.total }))
    autoUpdater.on("update-downloaded", (info) => model.downloaded(info.version))
    // Squirrel.Mac reports its errors, such as a signature that does not
    // satisfy the app's requirement, only here, after quitAndInstall.
    autoUpdater.on("error", (err) => {
      if (!installing) return
      installing = false
      model.installFailed(err)
    })
  }

  async function check(manual) {
    await started.catch(() => {})
    if (mode === "off") {
      if (manual) {
        void dialog.showMessageBox({
          type: "info",
          message: "This copy of YarGPT can't check for updates",
          detail: "It runs from its source folder. Build the app with make app, or download it from GitHub.",
        })
      }
      return
    }
    if (!model.beginCheck(manual)) return
    let action
    try {
      action = model.checkDone(await latestVersion())
    } catch (err) {
      model.checkFailed(err)
      return
    }
    if (action === "upToDate") {
      void dialog.showMessageBox({ type: "info", message: `YarGPT ${model.current} is up to date` })
    } else if (action === "download") {
      void download(true)
    }
  }

  async function latestVersion() {
    if (mode === "source") return fetchFeedVersion(feedURL)
    const result = await autoUpdater.checkForUpdates()
    // A version outside the staged rollout is reported as not available.
    return result?.isUpdateAvailable ? result.updateInfo.version : model.current
  }

  async function download(auto) {
    if (mode !== "auto") return
    const version = autoUpdater.updateInfoAndProvider?.info?.version ?? model.version
    const own = newToken()
    token = own
    model.downloadStarted(version, auto)
    try {
      await autoUpdater.downloadUpdate(own)
    } catch (err) {
      if (own.cancelled) model.downloadCancelled()
      else model.downloadFailed(err, auto)
    } finally {
      if (token === own) token = null
    }
  }

  function newToken() {
    const { CancellationToken } = require("electron-updater")
    return new CancellationToken()
  }

  // install quits the app, installs the downloaded update and opens the app
  // again. Its before-quit passes, since installing is set.
  function install() {
    if (mode !== "auto" || installing) return
    installing = true
    model.restarting()
    // Squirrel.Mac quits the app within seconds; if it neither quits nor
    // reports an error, show an error instead of Restarting… forever.
    setTimeout(() => {
      if (!installing) return
      installing = false
      model.installFailed(new Error("Squirrel.Mac did not install the update"))
    }, installTimeout).unref?.()
    setImmediate(() => {
      try {
        autoUpdater.quitAndInstall(false, true)
      } catch (err) {
        installing = false
        model.installFailed(err)
      }
    })
  }

  async function updateWhenIdle() {
    model.setSettings(loadSettings(configFile))
    const response = await api.drain()
    if (model.startWaiting(runningOf(response)) === "install") install()
  }

  async function cancelUpdate() {
    await api.undrain()
    model.cancelWait()
  }

  async function retry() {
    if (model.errorStage === "download") return download(false)
    if (model.errorStage === "server") return
    return check(true)
  }

  function openReleasePage() {
    void shell.openExternal(model.snapshot().releaseUrl)
  }

  const actions = {
    download: () => download(false),
    cancelDownload: () => token?.cancel(),
    later: () => model.later(),
    skip: () => model.skip(),
    restartNow: () => install(),
    updateWhenIdle,
    cancelUpdate,
    waitLonger: () => model.waitLonger(),
    retry,
    openReleasePage,
    check: () => check(true),
  }

  function registerIPC() {
    ipcMain.handle("updater:get", (event) => {
      if (!isTrusted(event)) throw new Error("not allowed")
      return model.snapshot()
    })
    for (const name of bridgeMethods) {
      ipcMain.handle(`updater:${name}`, async (event) => {
        if (!isTrusted(event)) throw new Error("not allowed")
        await actions[name]()
      })
    }
  }

  // botsDialog asks what to do with the update while bots work.
  async function botsDialog(running, { quit }) {
    const buttons = quit
      ? ["Update when they finish", "Quit and update later", "Restart now", "Cancel"]
      : ["Update when they finish", "Restart now", "Cancel"]
    const { response } = await dialog.showMessageBox({
      type: "question",
      message: "YarGPT has an update ready.",
      detail: `${botsWorkingText(running)} Restart now stops their turns.`,
      buttons,
      defaultId: 0,
      cancelId: buttons.length - 1,
    })
    return buttons[response]
  }

  async function chooseOnBots(choice) {
    if (choice === "Update when they finish") {
      getWindow()?.hide()
      await updateWhenIdle()
    } else if (choice === "Quit and update later") {
      if (model.phase === "waiting" || model.phase === "limit") await api.undrain()
      quitting = true
      app.quit()
    } else if (choice === "Restart now") {
      install()
    }
  }

  // restartFromMenu: the app menu's Restart to Update. It asks first when
  // bots work.
  async function restartFromMenu() {
    const running = runningOf(await api.version())
    if (running.length === 0) return install()
    await chooseOnBots(await botsDialog(running, { quit: false }))
  }

  async function quitWithUpdate() {
    const running = runningOf(await api.version())
    if (running.length === 0) return install()
    await chooseOnBots(await botsDialog(running, { quit: true }))
  }

  return {
    start() {
      registerIPC()
      started = (async () => {
        if (!app.isPackaged) return
        mode = await signatureMode()
        model.mode = mode
        if (mode === "auto") loadUpdater()
        scheduler = createScheduler({ model, settings: () => loadSettings(configFile), check })
        scheduler.start()
      })()
      return started
    },
    check: () => check(true),
    snapshot: () => model.snapshot(),
    serverMismatch: (version) => model.serverMismatch(version),
    // menuItem is the update item of the app menu.
    menuItem() {
      const item = menuItemFor(model.snapshot())
      const click = {
        check: () => void check(true),
        download: () => void download(false),
        restart: () => void restartFromMenu(),
        openReleasePage,
      }[item.action]
      return { id: "updates", label: item.label, enabled: item.enabled, click }
    },
    // handleBeforeQuit asks about a ready update at ⌘Q. Installing quits the
    // app too, so it passes then, and after the user chose to quit.
    handleBeforeQuit(event) {
      if (installing || quitting || !model.interceptsQuit()) return
      event.preventDefault()
      void quitWithUpdate()
    },
  }
}

module.exports = { createUpdater, bridgeMethods }
