const assert = require("node:assert/strict")
const http = require("node:http")
const { test } = require("node:test")
const { settingsMethods, settingsAPI, createSettingsBridge, registerSettingsIPC } = require("./settings-bridge")

const configFile = "/Users/me/.yargpt/config.json"
const logFile = "/Users/me/Library/Logs/YarGPT/server.log"

// fakes builds a bridge whose dependencies log their calls.
function fakes({ settings = { status: 200, body: { values: {} } }, restart = { status: 202, body: {} }, started = true, open = "", chosen } = {}) {
  const calls = []
  const deps = {
    api: {
      settings: async () => {
        calls.push("GET /api/settings")
        return settings
      },
      restart: async (when) => {
        calls.push(`POST /api/restart ${when}`)
        return restart
      },
    },
    registerAgent: async () => calls.push("registerAgent"),
    startServer: async () => {
      calls.push("startServer")
      return started
    },
    dialog: {
      showOpenDialog: async (options) => {
        calls.push(["showOpenDialog", options])
        return chosen ? { canceled: false, filePaths: [chosen] } : { canceled: true, filePaths: [] }
      },
      showErrorBox: (title, text) => calls.push(["showErrorBox", title, text]),
    },
    shell: {
      openPath: async (file) => {
        calls.push(["openPath", file])
        return open
      },
      showItemInFolder: (file) => calls.push(["showItemInFolder", file]),
    },
    ensureSettingsFile: () => {
      calls.push("ensureSettingsFile")
      return configFile
    },
    configFile,
    logFile,
  }
  return { calls, bridge: createSettingsBridge(deps), deps }
}

test("restart now registers the agent again and waits for the server", async () => {
  const { calls, bridge } = fakes()
  assert.deepEqual(await bridge.restartServer("now"), { ok: true })
  assert.deepEqual(calls, ["GET /api/settings", "registerAgent", "startServer"])
})

test("restart now reports the log when the server does not start", async () => {
  const { bridge } = fakes({ started: false })
  assert.deepEqual(await bridge.restartServer("now"), { ok: false, error: `The server did not start. Its log is ${logFile}.` })
})

test("a restart is refused when a restarted server would not start", async () => {
  const startError = 'find Claude Code CLI "/nope": exec: "/nope": stat /nope: no such file or directory'
  for (const when of ["now", "idle"]) {
    const { calls, bridge } = fakes({ settings: { status: 200, body: { startError } } })
    assert.deepEqual(await bridge.restartServer(when), { ok: false, error: startError })
    assert.deepEqual(calls, ["GET /api/settings"], "nothing restarts")
  }
})

test("a server that does not answer is restarted now", async () => {
  const { calls, bridge } = fakes({ settings: null })
  assert.deepEqual(await bridge.restartServer("now"), { ok: true })
  assert.deepEqual(calls, ["GET /api/settings", "registerAgent", "startServer"])
})

test("restart when idle asks the server to restart once no work runs", async () => {
  const { calls, bridge } = fakes()
  assert.deepEqual(await bridge.restartServer("idle"), { ok: true })
  assert.deepEqual(calls, ["GET /api/settings", "POST /api/restart idle"])
})

test("restart when idle reports the server's refusal", async () => {
  const refused = fakes({ restart: { status: 409, statusText: "Conflict", body: { key: "claude", message: "YarGPT wouldn't start after a restart.", error: "x" } } })
  assert.deepEqual(await refused.bridge.restartServer("idle"), { ok: false, error: "YarGPT wouldn't start after a restart." })
  const manual = fakes({ restart: { status: 501, statusText: "Not Implemented", body: null } })
  assert.deepEqual(await manual.bridge.restartServer("idle"), { ok: false, error: "Not Implemented" })
  const bare = fakes({ restart: { status: 500, statusText: "", body: null } })
  assert.deepEqual(await bare.bridge.restartServer("idle"), { ok: false, error: "The server answered 500." })
  const gone = fakes({ restart: null })
  assert.deepEqual(await gone.bridge.restartServer("idle"), { ok: false, error: "The server did not answer." })
  assert.deepEqual(gone.calls, ["GET /api/settings", "POST /api/restart idle"], "no restart now instead")
})

test("restart refuses an unknown when, and a second restart while one runs", async () => {
  const { calls, bridge, deps } = fakes()
  for (const when of [undefined, "later", "", 1, { when: "now" }]) {
    assert.equal((await bridge.restartServer(when)).ok, false)
  }
  assert.deepEqual(calls, [])
  let finish
  deps.startServer = () => new Promise((resolve) => (finish = resolve))
  const slow = createSettingsBridge(deps)
  const first = slow.restartServer("now")
  while (!finish) await new Promise((resolve) => setImmediate(resolve))
  assert.deepEqual(await slow.restartServer("now"), { ok: false, error: "The server is restarting already." })
  finish(true)
  assert.deepEqual(await first, { ok: true })
})

test("chooseClaude returns the chosen program, or null when cancelled", async () => {
  const chose = fakes({ chosen: "/Users/me/.local/bin/claude" })
  assert.equal(await chose.bridge.chooseClaude(), "/Users/me/.local/bin/claude")
  const [, options] = chose.calls[0]
  assert.equal(options.title, "Choose the Claude Code program")
  assert.ok(options.properties.includes("openFile"))
  assert.ok(!options.properties.includes("multiSelections"))
  assert.equal(await fakes().bridge.chooseClaude(), null)
})

test("openFile creates config.json if needed and opens it", async () => {
  const { calls, bridge } = fakes()
  await bridge.openFile()
  assert.deepEqual(calls, ["ensureSettingsFile", ["openPath", configFile]])
  const failed = fakes({ open: "No application knows how to open it." })
  await failed.bridge.openFile()
  assert.deepEqual(failed.calls.at(-1), ["showErrorBox", "YarGPT could not open the settings file", `${configFile}: No application knows how to open it.`])
  const broken = fakes()
  broken.deps.ensureSettingsFile = () => {
    throw new Error("EACCES: permission denied")
  }
  await createSettingsBridge(broken.deps).openFile()
  assert.deepEqual(broken.calls, [["showErrorBox", "YarGPT could not create the settings file", "EACCES: permission denied"]])
})

test("revealFile shows only config.json and the server log", () => {
  const { calls, bridge } = fakes()
  bridge.revealFile(configFile)
  bridge.revealFile("/Users/me/Library/Logs/YarGPT/../YarGPT/server.log")
  assert.deepEqual(calls, [
    ["showItemInFolder", configFile],
    ["showItemInFolder", logFile],
  ])
  for (const file of ["/etc/passwd", "/Users/me/.yargpt", "/Users/me/.yargpt/config.json.bak", "config.json", "", undefined, null, 1, [configFile]]) {
    assert.throws(() => bridge.revealFile(file), /not allowed/, String(file))
  }
  assert.equal(calls.length, 2)
})

test("the settings channels answer only the trusted page", async () => {
  const handlers = new Map()
  const ipcMain = { handle: (channel, fn) => handlers.set(channel, fn) }
  const { calls, bridge } = fakes()
  registerSettingsIPC(ipcMain, bridge, (event) => event.trusted)
  assert.deepEqual([...handlers.keys()], settingsMethods.map((name) => `settings:${name}`))
  for (const [, handler] of handlers) {
    await assert.rejects(handler({ trusted: false }, "now"), /not allowed/)
  }
  assert.deepEqual(calls, [])
  assert.deepEqual(await handlers.get("settings:restartServer")({ trusted: true }, "idle"), { ok: true })
  await handlers.get("settings:revealFile")({ trusted: true }, logFile)
  assert.deepEqual(calls.at(-1), ["showItemInFolder", logFile])
})

test("settingsAPI reads the settings and asks for a restart with a JSON body", async (t) => {
  const log = []
  const server = http.createServer((req, res) => {
    let body = ""
    req.on("data", (chunk) => (body += chunk))
    req.on("end", () => {
      log.push(`${req.method} ${req.url} ${body}`)
      if (req.method !== "GET" && req.headers["content-type"] !== "application/json") {
        res.writeHead(403).end()
      } else if (req.url === "/api/settings") {
        res.writeHead(200, { "Content-Type": "application/json" }).end('{"startError":"bad"}')
      } else if (body === '{"when":"idle"}') {
        res.writeHead(409, { "Content-Type": "application/json" }).end('{"key":"","message":"no","error":"no"}')
      } else {
        res.writeHead(501).end("not json")
      }
    })
  })
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve))
  t.after(() => new Promise((resolve) => server.close(resolve)))
  const api = settingsAPI(`http://127.0.0.1:${server.address().port}`)
  assert.deepEqual(await api.settings(), { status: 200, statusText: "OK", body: { startError: "bad" } })
  assert.deepEqual(await api.restart("idle"), { status: 409, statusText: "Conflict", body: { key: "", message: "no", error: "no" } })
  assert.deepEqual(await api.restart("now"), { status: 501, statusText: "Not Implemented", body: null })
  assert.deepEqual(log, ["GET /api/settings ", 'POST /api/restart {"when":"idle"}', 'POST /api/restart {"when":"now"}'])

  const closed = http.createServer()
  await new Promise((resolve) => closed.listen(0, "127.0.0.1", resolve))
  const port = closed.address().port
  await new Promise((resolve) => closed.close(resolve))
  assert.equal(await settingsAPI(`http://127.0.0.1:${port}`).settings(), null)
})
