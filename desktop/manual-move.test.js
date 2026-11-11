const assert = require("node:assert/strict")
const { test } = require("node:test")
const {
  oldAgentLabel,
  lastOldVersion,
  loginItemsURL,
  drainPollInterval,
  portWaitLimit,
  stopDialog,
  waitingDialog,
  loginItemsDialog,
  stopFailedTitle,
  launchctlRunner,
  isOldServer,
  findOldServer,
  handleOldServer,
  createOldServerGate,
} = require("./manual-move")

const twoBots = [
  { botId: "a", name: "Dev Lead", kind: "turn" },
  { botId: "b", name: "Core Dev", kind: "turn" },
]

const s5Message = "YarGPT is still running"
const waitingMessage = "Waiting for 2 bots to finish…"
const loginItemsMessage = "Last step: remove YarGPT from Login Items, or macOS starts it again at your next login."

// world fakes the server on the port (YarGPT 0.8.0 by default), launchd, the dialogs and the clock. Each
// dependency logs its calls in calls. The server stops answering when the
// agent is booted out, after stopsAfter ms of the fake clock; its bots
// finish after botsFinishAfter ms. choose maps a dialog's message to the
// button it clicks, or null to leave the box open.
function world({
  answering = true,
  status = 200,
  version = "0.8.0",
  running = twoBots,
  loaded = [oldAgentLabel],
  drainStatus = 200,
  bootout = { ok: true },
  stopsAfter = 600,
  botsFinishAfter = 12_000,
  choose = {},
} = {}) {
  const calls = []
  const clock = { t: 0, bootedOutAt: null }
  const aborted = []
  const serverUp = () => answering && (clock.bootedOutAt === null || clock.t < clock.bootedOutAt + stopsAfter)
  const deps = {
    uid: 501,
    now: () => clock.t,
    sleep: async (ms) => {
      calls.push(["sleep", ms])
      clock.t += ms
    },
    api: {
      version: async (timeout) => {
        calls.push(["version", timeout])
        if (!serverUp()) return null
        return { status, body: status === 200 ? { version, running: clock.t >= botsFinishAfter ? [] : running } : null }
      },
      drain: async () => {
        calls.push(["drain"])
        return serverUp() ? { status: drainStatus, body: null } : null
      },
      undrain: async () => {
        calls.push(["undrain"])
        return serverUp() ? { status: 200, body: null } : null
      },
    },
    launchctl: {
      loaded: async (label) => {
        calls.push(["loaded", label])
        return loaded.includes(label)
      },
      bootout: async (label) => {
        calls.push(["bootout", label])
        if (bootout.ok) clock.bootedOutAt = clock.t
        return bootout
      },
    },
    dialog: {
      showMessageBox: (options) => {
        calls.push(["showMessageBox", options])
        const pick = choose[options.message]
        if (pick === undefined) throw new Error(`no choice for ${options.message}`)
        if (pick === null) {
          // An open box: it closes when its signal aborts.
          return new Promise((resolve) =>
            options.signal?.addEventListener("abort", () => {
              aborted.push(options.message)
              resolve({ response: -1 })
            }),
          )
        }
        return Promise.resolve({ response: options.buttons.indexOf(pick) })
      },
      showErrorBox: (title, text) => calls.push(["showErrorBox", title, text]),
    },
    openExternal: async (url) => calls.push(["openExternal", url]),
  }
  const names = () => calls.map((c) => c[0])
  const dialogs = () => calls.filter((c) => c[0] === "showMessageBox").map((c) => c[1].message)
  return { deps, calls, clock, aborted, names, dialogs }
}

test("S5's copy names the working bots and offers three buttons", () => {
  const { options, actions } = stopDialog(twoBots)
  assert.equal(options.message, "YarGPT is still running")
  assert.equal(options.detail, "Handful needs to stop it before it can move your bots. 2 bots are working: Dev Lead, Core Dev.")
  assert.deepEqual(options.buttons, ["Stop YarGPT when they finish", "Stop YarGPT now", "Quit Handful"])
  assert.deepEqual(actions, ["finish", "now", "quit"])
  assert.equal(options.defaultId, 0)
  assert.equal(options.cancelId, 2)
})

test("S5 without working bots has only the first sentence and two buttons", () => {
  const { options, actions } = stopDialog([])
  assert.equal(options.message, "YarGPT is still running")
  assert.equal(options.detail, "Handful needs to stop it before it can move your bots.")
  assert.deepEqual(options.buttons, ["Stop YarGPT now", "Quit Handful"])
  assert.deepEqual(actions, ["now", "quit"])
  assert.equal(options.cancelId, 1)
})

test("the waiting box and the Login Items step have their exact copy", () => {
  assert.deepEqual(waitingDialog(twoBots), {
    type: "info",
    message: "Waiting for 2 bots to finish…",
    detail: "2 bots are working: Dev Lead, Core Dev.",
    buttons: ["Stop YarGPT now"],
  })
  assert.equal(waitingDialog([{ botId: "a", name: "Dev Lead", kind: "turn" }]).message, "Waiting for 1 bot to finish…")
  assert.equal(loginItemsDialog.message, "Last step: remove YarGPT from Login Items, or macOS starts it again at your next login.")
  assert.deepEqual(loginItemsDialog.buttons, ["Open Login Items", "Done"])
  assert.equal(loginItemsURL, "x-apple.systempreferences:com.apple.LoginItems-Settings.extension")
  assert.equal(oldAgentLabel, "dev.yarlson.yargpt.server")
})

test("nothing on the port: no dialog and no launchctl", async () => {
  const w = world({ answering: false })
  assert.equal(await handleOldServer(w.deps), "continue")
  assert.deepEqual(w.calls, [["version", 1000]])
})

test("YarGPT's answer without YarGPT's agent loaded: a server from a terminal, not stopped", async () => {
  // Only bootout of YarGPT's agent could stop it.
  const w = world({ loaded: ["dev.yarlson.handful.server"] })
  assert.equal(await handleOldServer(w.deps), "continue")
  assert.deepEqual(w.names(), ["version", "loaded"])
  assert.equal(await findOldServer(world({ loaded: [] }).deps), null)
})

test("YarGPT before v0.9.0 answers 404 and still counts, with no bots", async () => {
  const w = world({ status: 404 })
  assert.deepEqual(await findOldServer(w.deps), { running: [] })
  assert.deepEqual(await findOldServer(world().deps), { running: twoBots })
  // It gets S5, and Quit Handful stops nothing.
  const quit = world({ status: 404, choose: { [s5Message]: "Quit Handful" } })
  assert.equal(await handleOldServer(quit.deps), "quit")
  assert.deepEqual(quit.dialogs(), [s5Message])
})

test("Handful's own server on the port while YarGPT's agent is still loaded: no S5, no drain", async () => {
  // The user skipped the Login Items step: YarGPT's agent stays loaded, but
  // Handful's server holds the port. Any dialog would throw: choose is empty.
  for (const version of ["0.14.0", "0.14.0-rc.1", "v1.2.3", "dev-1a2b3c4", "dev"]) {
    const w = world({ version, loaded: [oldAgentLabel, "dev.yarlson.handful.server"] })
    assert.equal(await handleOldServer(w.deps), "continue", version)
    assert.deepEqual(w.names(), ["version"], version)
  }
  assert.equal(await findOldServer(world({ version: "0.14.0" }).deps), null)
})

test("YarGPT 0.13.0, the last YarGPT, and older versions get S5", async () => {
  assert.equal(lastOldVersion, "0.13.0")
  for (const version of ["0.13.0", "v0.13.0", "0.13.0-rc.2", "0.12.4", "0.9.0"]) {
    assert.deepEqual(await findOldServer(world({ version }).deps), { running: twoBots }, version)
  }
  const w = world({ version: "0.13.0", choose: { [s5Message]: "Quit Handful" } })
  assert.equal(await handleOldServer(w.deps), "quit")
  assert.deepEqual(w.dialogs(), [s5Message])
  assert.ok(!w.names().includes("drain"))
})

test("isOldServer: 404 and versions up to 0.13.0 are YarGPT, anything else is not", () => {
  assert.equal(isOldServer({ status: 404, body: null }), true)
  assert.equal(isOldServer({ status: 200, body: { version: "0.13.0" } }), true)
  assert.equal(isOldServer({ status: 200, body: { version: "0.13.1" } }), false)
  assert.equal(isOldServer({ status: 200, body: { version: "0.14.0" } }), false)
  assert.equal(isOldServer({ status: 200, body: { version: "dev-abc" } }), false)
  assert.equal(isOldServer({ status: 200, body: null }), false)
  assert.equal(isOldServer({ status: 500, body: null }), false)
})

test("Stop YarGPT now boots its agent out, waits for the port, then shows the Login Items step", async () => {
  const w = world({ choose: { [s5Message]: "Stop YarGPT now", [loginItemsMessage]: "Open Login Items" } })
  assert.equal(await handleOldServer(w.deps), "continue")
  assert.ok(!w.names().includes("drain"))
  assert.deepEqual(
    w.calls.filter((c) => c[0] === "bootout"),
    [["bootout", "dev.yarlson.yargpt.server"]],
  )
  assert.deepEqual(w.dialogs(), [s5Message, loginItemsMessage])
  // The Login Items step comes only after the port stopped answering.
  const lastVersion = w.calls.findLastIndex((c) => c[0] === "version")
  const loginItems = w.calls.findIndex((c) => c[0] === "showMessageBox" && c[1].message === loginItemsMessage)
  assert.ok(lastVersion < loginItems)
  assert.ok(w.clock.t >= 600)
  assert.deepEqual(w.calls.at(-1), ["openExternal", loginItemsURL])
})

test("Done in the Login Items step opens nothing", async () => {
  const w = world({ choose: { [s5Message]: "Stop YarGPT now", [loginItemsMessage]: "Done" } })
  assert.equal(await handleOldServer(w.deps), "continue")
  assert.ok(!w.names().includes("openExternal"))
})

test("Stop YarGPT when they finish drains, polls every 5 s until the bots finish, then boots it out", async () => {
  const w = world({ choose: { [s5Message]: "Stop YarGPT when they finish", [waitingMessage]: null, [loginItemsMessage]: "Done" } })
  assert.equal(await handleOldServer(w.deps), "continue")
  const names = w.names()
  assert.ok(names.indexOf("drain") < names.indexOf("bootout"))
  assert.deepEqual(w.dialogs(), [s5Message, waitingMessage, loginItemsMessage])
  // The bots finish at 12 s: polls at 5, 10 and 15 s.
  const beforeBootout = w.calls.slice(0, names.indexOf("bootout"))
  assert.deepEqual(
    beforeBootout.filter((c) => c[0] === "sleep").map((c) => c[1]),
    [drainPollInterval, drainPollInterval, drainPollInterval],
  )
  assert.deepEqual(w.aborted, [waitingMessage])
})

test("Stop YarGPT now in the waiting box stops it without waiting for the bots", async () => {
  const w = world({
    botsFinishAfter: Infinity,
    choose: { [s5Message]: "Stop YarGPT when they finish", [waitingMessage]: "Stop YarGPT now", [loginItemsMessage]: "Done" },
  })
  assert.equal(await handleOldServer(w.deps), "continue")
  assert.ok(w.names().includes("bootout"))
})

test("a YarGPT without /api/drain (404) is stopped at once", async () => {
  const w = world({ drainStatus: 404, choose: { [s5Message]: "Stop YarGPT when they finish", [loginItemsMessage]: "Done" } })
  assert.equal(await handleOldServer(w.deps), "continue")
  assert.deepEqual(w.dialogs(), [s5Message, loginItemsMessage])
  const names = w.names()
  assert.equal(names[names.indexOf("drain") + 1], "bootout")
})

test("Quit Handful, or closing S5, stops nothing", async () => {
  for (const choice of ["Quit Handful", "closed"]) {
    const w = world({ choose: { [s5Message]: choice } })
    assert.equal(await handleOldServer(w.deps), "quit")
    assert.ok(!w.names().includes("bootout"))
    assert.ok(!w.names().includes("drain"))
    assert.deepEqual(w.dialogs(), [s5Message])
  }
})

test("a failed bootout shows an error, no Login Items step, and quits", async () => {
  const w = world({ bootout: { ok: false, error: "Boot-out failed: 5: Input/output error" }, choose: { [s5Message]: "Stop YarGPT now" } })
  assert.equal(await handleOldServer(w.deps), "quit")
  assert.deepEqual(w.calls.at(-1), [
    "showErrorBox",
    stopFailedTitle,
    "launchctl bootout gui/501/dev.yarlson.yargpt.server failed: Boot-out failed: 5: Input/output error. Quit YarGPT, then open Handful again.",
  ])
  assert.equal(stopFailedTitle, "Handful could not stop YarGPT")
  assert.deepEqual(w.dialogs(), [s5Message])
})

test("a port that still answers 30 s after the bootout shows an error and quits", async () => {
  const w = world({ stopsAfter: Infinity, choose: { [s5Message]: "Stop YarGPT now" } })
  assert.equal(await handleOldServer(w.deps), "quit")
  assert.deepEqual(w.calls.at(-1), [
    "showErrorBox",
    "Handful could not stop YarGPT",
    "A server still answers on 127.0.0.1:7777 30 seconds after launchctl bootout. Quit it, then open Handful again.",
  ])
  assert.ok(w.clock.t >= portWaitLimit && w.clock.t < portWaitLimit + 1000)
  assert.deepEqual(w.dialogs(), [s5Message])
})

test("a drained YarGPT whose bootout fails gets DELETE /api/drain before the error box", async () => {
  const w = world({
    bootout: { ok: false, error: "Boot-out failed: 5: Input/output error" },
    choose: { [s5Message]: "Stop YarGPT when they finish", [waitingMessage]: "Stop YarGPT now" },
  })
  assert.equal(await handleOldServer(w.deps), "quit")
  const names = w.names()
  assert.deepEqual(
    names.filter((n) => ["drain", "bootout", "undrain", "showErrorBox"].includes(n)),
    ["drain", "bootout", "undrain", "showErrorBox"],
  )
})

test("a drained server that still answers after the bootout gets DELETE /api/drain", async () => {
  const w = world({ stopsAfter: Infinity, choose: { [s5Message]: "Stop YarGPT when they finish", [waitingMessage]: null } })
  assert.equal(await handleOldServer(w.deps), "quit")
  const names = w.names()
  assert.equal(names.filter((n) => n === "undrain").length, 1)
  assert.ok(names.indexOf("undrain") < names.indexOf("showErrorBox"))
})

test("an error after the drain sends DELETE /api/drain and passes the error on", async () => {
  // The waiting box fails: choose has no answer for it.
  const w = world({ choose: { [s5Message]: "Stop YarGPT when they finish" } })
  await assert.rejects(handleOldServer(w.deps), /no choice/)
  assert.deepEqual(
    w.names().filter((n) => ["drain", "undrain", "bootout"].includes(n)),
    ["drain", "undrain"],
  )
})

test("no DELETE /api/drain without a drain, or after YarGPT stopped", async () => {
  const cases = [
    world({ choose: { [s5Message]: "Quit Handful" } }),
    world({ bootout: { ok: false, error: "x" }, choose: { [s5Message]: "Stop YarGPT now" } }),
    world({ drainStatus: 404, bootout: { ok: false, error: "x" }, choose: { [s5Message]: "Stop YarGPT when they finish" } }),
    world({ choose: { [s5Message]: "Stop YarGPT when they finish", [waitingMessage]: null, [loginItemsMessage]: "Done" } }),
  ]
  for (const w of cases) {
    await handleOldServer(w.deps)
    assert.ok(!w.names().includes("undrain"))
  }
})

test("the gate never lets ensureServer register after a quit, and runs once per launch", async () => {
  async function launch(w) {
    let quits = 0
    const registered = []
    const gate = createOldServerGate(w.deps, () => quits++)
    // As in main.js's ensureServer: the gate, then the registration.
    const ensureServer = async () => (await gate()) && registered.push("register") > 0
    const first = await ensureServer()
    const second = await ensureServer()
    return { first, second, quits, registered }
  }
  for (const w of [
    world({ choose: { [s5Message]: "Quit Handful" } }),
    world({ bootout: { ok: false, error: "x" }, choose: { [s5Message]: "Stop YarGPT now" } }),
    world({ stopsAfter: Infinity, choose: { [s5Message]: "Stop YarGPT now" } }),
  ]) {
    assert.deepEqual(await launch(w), { first: false, second: false, quits: 1, registered: [] })
    assert.equal(w.dialogs().filter((m) => m === s5Message).length, 1)
  }
  const stopped = world({ choose: { [s5Message]: "Stop YarGPT now", [loginItemsMessage]: "Done" } })
  assert.deepEqual(await launch(stopped), { first: true, second: true, quits: 0, registered: ["register", "register"] })
  assert.equal(stopped.calls.filter((c) => c[0] === "bootout").length, 1)
})

test("the gate asks again after a failed dialog", async () => {
  const w = world({ choose: {} })
  const gate = createOldServerGate(w.deps, () => assert.fail("no quit"))
  await assert.rejects(gate(), /no choice/)
  await assert.rejects(gate(), /no choice/)
  assert.equal(w.dialogs().length, 2)
})

test("launchctlRunner runs /bin/launchctl with an argv, never a shell", async () => {
  const runs = []
  const execFile = (file, args, options, callback) => {
    runs.push([file, args, options.shell])
    if (args[0] === "bootout") callback(Object.assign(new Error("exit 5"), { code: 5 }), "", "Boot-out failed: 5: Input/output error\n")
    else callback(null, "", "")
  }
  const launchctl = launchctlRunner(execFile, 501)
  assert.equal(await launchctl.loaded("dev.yarlson.yargpt.server"), true)
  assert.deepEqual(await launchctl.bootout("dev.yarlson.yargpt.server"), { ok: false, error: "Boot-out failed: 5: Input/output error" })
  assert.deepEqual(runs, [
    ["/bin/launchctl", ["print", "gui/501/dev.yarlson.yargpt.server"], undefined],
    ["/bin/launchctl", ["bootout", "gui/501/dev.yarlson.yargpt.server"], undefined],
  ])
  const ok = launchctlRunner((_f, _a, _o, callback) => callback(null, "", ""), 501)
  assert.deepEqual(await ok.bootout("x"), { ok: true })
  const silent = launchctlRunner((_f, _a, _o, callback) => callback(new Error("timed out"), "", ""), 501)
  assert.deepEqual(await silent.bootout("x"), { ok: false, error: "timed out" })
})
