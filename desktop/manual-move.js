// The manual path of the move from YarGPT (spec ad09e72c §4.2): Handful was
// installed by hand while YarGPT's server still runs as its launch agent and
// holds port 7777. Before Handful registers its own agent, it offers to stop
// YarGPT's (S5), then asks the user to remove YarGPT from Login Items, or
// macOS would start it again at the next login. This file has no Electron
// code: main.js passes in the dialogs, launchctl, the server API and the
// clock, so node --test covers every path.
const { botsWorkingText } = require("./updater-state")
const { runningOf } = require("./updater-io")

// YarGPT's launch agent. Every YarGPT app since the first one has used it.
const oldAgentLabel = "dev.yarlson.yargpt.server"
const loginItemsURL = "x-apple.systempreferences:com.apple.LoginItems-Settings.extension"
const serverAddress = "127.0.0.1:7777"

const versionTimeout = 1000
const drainPollInterval = 5000
const portPollInterval = 250
const portWaitLimit = 30_000

// stopDialog returns the options of S5 and the action of each button. With
// no bots working, it has no "when they finish" button.
function stopDialog(running) {
  const busy = running.length > 0
  const first = "Handful needs to stop it before it can move your bots."
  const buttons = busy ? ["Stop YarGPT when they finish", "Stop YarGPT now", "Quit Handful"] : ["Stop YarGPT now", "Quit Handful"]
  return {
    options: {
      type: "warning",
      message: "YarGPT is still running",
      detail: busy ? `${first} ${botsWorkingText(running)}` : first,
      buttons,
      defaultId: 0,
      cancelId: buttons.length - 1,
    },
    actions: busy ? ["finish", "now", "quit"] : ["now", "quit"],
  }
}

// waitingDialog returns the options of the box shown while YarGPT's bots
// finish. Its button stops YarGPT without waiting longer.
function waitingDialog(running) {
  const n = new Set(running.map((r) => r.name || "a bot")).size
  return {
    type: "info",
    message: `Waiting for ${n} ${n === 1 ? "bot" : "bots"} to finish…`,
    detail: botsWorkingText(running),
    buttons: ["Stop YarGPT now"],
  }
}

// The step after YarGPT stopped, with its two buttons.
const loginItemsDialog = {
  type: "info",
  message: "Last step: remove YarGPT from Login Items, or macOS starts it again at your next login.",
  buttons: ["Open Login Items", "Done"],
  defaultId: 0,
  cancelId: 1,
}

const stopFailedTitle = "Handful could not stop YarGPT"

// launchctlRunner runs launchctl with an argv, never through a shell, on the
// agents of the user uid. Each call resolves {ok, error}.
function launchctlRunner(execFile, uid) {
  const run = (subcommand, label) =>
    new Promise((resolve) => {
      execFile("/bin/launchctl", [subcommand, `gui/${uid}/${label}`], { timeout: 10_000 }, (err, _stdout, stderr) =>
        resolve(err ? { ok: false, error: String(stderr || err.message).trim() } : { ok: true }),
      )
    })
  return {
    loaded: async (label) => (await run("print", label)).ok,
    bootout: (label) => run("bootout", label),
  }
}

// findOldServer decides whether a YarGPT server holds the port. Its answer
// does not name its app, so the rule is:
//   - nothing answers /api/version: no. Any HTTP status counts as an answer,
//     as YarGPT before v0.9.0 has no such endpoint;
//   - something answers and YarGPT's agent is loaded: yes. Every YarGPT app
//     ran its server as that agent, and only then can bootout stop it;
//   - something answers but YarGPT's agent is not loaded: no. It is
//     Handful's own server, or a server run from a terminal, which the normal
//     start handles as before.
// It returns {running} for a YarGPT server, or null.
async function findOldServer({ api, launchctl }) {
  const answer = await api.version(versionTimeout)
  if (!answer) return null
  if (!(await launchctl.loaded(oldAgentLabel))) return null
  return { running: runningOf(answer) }
}

// waitForBots asks YarGPT to finish its turns, then polls it every 5 s until
// none run, or until the user chooses Stop YarGPT now. A YarGPT without
// /api/drain answers 404 and is stopped at once.
async function waitForBots({ api, dialog, sleep }, running) {
  const drained = await api.drain()
  if (!drained || drained.status !== 200) return
  let current = running
  if (current.length === 0) return
  const abort = new AbortController()
  let stopNow = false
  const box = dialog.showMessageBox({ ...waitingDialog(current), signal: abort.signal }).then(() => {
    stopNow = true
  })
  try {
    while (!stopNow) {
      await Promise.race([sleep(drainPollInterval), box])
      if (stopNow) break
      current = runningOf(await api.version(versionTimeout))
      if (current.length === 0) break
    }
  } finally {
    abort.abort()
  }
}

// stopOldServer boots YarGPT's agent out and waits until the port stops
// answering. It returns null, or the problem for the error box.
async function stopOldServer({ api, launchctl, sleep, now }, uid) {
  const result = await launchctl.bootout(oldAgentLabel)
  if (!result.ok) {
    return `launchctl bootout gui/${uid}/${oldAgentLabel} failed: ${result.error || "unknown error"}. Quit YarGPT, then open Handful again.`
  }
  const deadline = now() + portWaitLimit
  for (;;) {
    if (!(await api.version(versionTimeout))) return null
    if (now() >= deadline) break
    await sleep(portPollInterval)
  }
  return `A server still answers on ${serverAddress} ${portWaitLimit / 1000} seconds after launchctl bootout. Quit it, then open Handful again.`
}

// handleOldServer runs the manual path once, before Handful registers its
// agent. It resolves "continue" when Handful may start its own server, and
// "quit" when the app must quit without registering anything: the user
// chose Quit Handful, or YarGPT did not stop.
//
// deps: api {version(timeout), drain()}, as serverAPI in updater-io.js;
// launchctl {loaded(label), bootout(label)}, as launchctlRunner; dialog
// {showMessageBox(options) -> Promise<{response}>, showErrorBox(title,
// text)}; openExternal(url); sleep(ms); now(); uid.
async function handleOldServer(deps) {
  const found = await findOldServer(deps)
  if (!found) return "continue"
  const { options, actions } = stopDialog(found.running)
  const { response } = await deps.dialog.showMessageBox(options)
  const action = actions[response] ?? "quit"
  if (action === "quit") return "quit"
  if (action === "finish") await waitForBots(deps, found.running)
  const problem = await stopOldServer(deps, deps.uid)
  if (problem) {
    deps.dialog.showErrorBox(stopFailedTitle, problem)
    return "quit"
  }
  const choice = await deps.dialog.showMessageBox(loginItemsDialog)
  if (choice.response === 0) await deps.openExternal(loginItemsURL)
  return "continue"
}

// createOldServerGate returns the check that ensureServer runs before it
// registers Handful's agent. The manual path runs once per launch, and a
// launch whose answer was "quit" never passes: the gate calls quit and keeps
// resolving false, so nothing gets registered.
function createOldServerGate(deps, quit) {
  let result = null
  return async function gate() {
    result ??= handleOldServer(deps).then(
      (next) => {
        if (next !== "continue") quit()
        return next === "continue"
      },
      (err) => {
        // A failed dialog or launchctl call: the next activation asks again.
        result = null
        throw err
      },
    )
    return result
  }
}

module.exports = {
  oldAgentLabel,
  loginItemsURL,
  drainPollInterval,
  portWaitLimit,
  stopDialog,
  waitingDialog,
  loginItemsDialog,
  stopFailedTitle,
  launchctlRunner,
  findOldServer,
  handleOldServer,
  createOldServerGate,
}
