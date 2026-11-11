const assert = require("node:assert/strict")
const fs = require("node:fs")
const os = require("node:os")
const path = require("node:path")
const { test } = require("node:test")
const { tailBytes, bothFoldersLine, isBothFoldersError, lastStartupError, readTail, lastStartupErrorIn, serverErrorDialog, runServerErrorAction } = require("./server-log")

function tempDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "handful-log-"))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  return dir
}

const configError = "handful: config file /Users/me/.handful/config.json: invalid character '}' looking for beginning of object key string"
const claudeError = 'handful: find Claude Code CLI "/nope": exec: "/nope": stat /nope: no such file or directory'
const running = 'time=2026-11-10T10:00:00.000+01:00 level=INFO msg="Handful is running" url=http://127.0.0.1:7777 data=/Users/me/.handful claude=/Users/me/.local/bin/claude'

test("an empty or missing log has no startup error", (t) => {
  assert.equal(lastStartupError(""), null)
  assert.equal(lastStartupError("\n\n  \n"), null)
  assert.equal(lastStartupError(undefined), null)
  const dir = tempDir(t)
  assert.equal(lastStartupErrorIn(path.join(dir, "server.log")), null)
  assert.equal(readTail(path.join(dir, "server.log")), null)
  fs.writeFileSync(path.join(dir, "empty.log"), "")
  assert.deepEqual(readTail(path.join(dir, "empty.log")), { text: "", partial: false })
  assert.equal(lastStartupErrorIn(path.join(dir, "empty.log")), null)
  assert.equal(lastStartupErrorIn(dir), null, "a folder is no log")
})

test("the last error line wins, after the blank lines and restarts that follow it", () => {
  const log = [configError, running, 'time=x level=INFO msg="shutting down"', "handful: restarting: launchd starts the server again", claudeError, "", "   ", claudeError.replace("/nope", "/other"), "", ""].join("\n")
  assert.equal(lastStartupError(log), claudeError.replace("/nope", "/other"))
  assert.equal(lastStartupError(`${configError}\r\n\r\n`), configError)
})

test("an error before the last start is old", () => {
  assert.equal(lastStartupError([configError, running, 'time=x level=WARN msg="web UI not available"'].join("\n")), null)
  assert.equal(lastStartupError([running, 'time=x level=INFO msg="shutting down"', "handful: restarting: launchd starts the server again"].join("\n")), null)
  assert.equal(lastStartupError(["flag provided but not defined: -nope", "Usage of handful:"].join("\n")), null)
})

test("a long error line is cut", () => {
  const line = lastStartupError(`handful: ${"x".repeat(5000)}\n`)
  assert.equal(line.length, 1001)
  assert.ok(line.startsWith("handful: xxx") && line.endsWith("…"))
})

test("of a huge log only the last 64 KiB are read", (t) => {
  const file = path.join(tempDir(t), "server.log")
  const filler = `${"time=x level=INFO msg=filler ".padEnd(99, "-")}\n`
  fs.writeFileSync(file, `${claudeError}\n${filler.repeat(20_000)}${configError}\n`)
  assert.ok(fs.statSync(file).size > 1_000_000)
  const tail = readTail(file)
  assert.equal(Buffer.byteLength(tail.text), tailBytes)
  assert.equal(tail.partial, true)
  assert.equal(lastStartupErrorIn(file), configError)
  // An error further back than the tail is not found.
  fs.writeFileSync(file, `${claudeError}\n${filler.repeat(20_000)}`)
  assert.equal(lastStartupErrorIn(file), null)
})

test("the partial first line of a tail is skipped", (t) => {
  const file = path.join(tempDir(t), "server.log")
  fs.writeFileSync(file, `${claudeError}\n`)
  // The tail starts within the error line, at "handful: " of its middle.
  const cut = claudeError.indexOf('"/nope": stat') - 8
  assert.equal(lastStartupError(claudeError.slice(cut), true), null)
  assert.equal(lastStartupErrorIn(file, claudeError.length - 3), null)
  assert.equal(lastStartupErrorIn(file, claudeError.length + 1), claudeError)
})

test("the error screen offers config.json and the log, with the error line", () => {
  const { options, actions } = serverErrorDialog(configError, "/Users/me/Library/Logs/Handful/server.log")
  assert.deepEqual(options.buttons, ["Open config.json", "Open Log", "Close"])
  assert.deepEqual(actions, ["openSettingsFile", "openLog", null])
  assert.equal(options.cancelId, 2)
  assert.equal(options.defaultId, 0)
  assert.equal(options.message, "The Handful server did not start")
  assert.equal(options.detail, `${configError}\n\nIts log is /Users/me/Library/Logs/Handful/server.log.`)
  assert.equal(serverErrorDialog(null, "/l/server.log").options.detail, "Its log is /l/server.log.")
})

// The server's S6 line, word for word as cmd/handful prints it.
const bothFolders = "handful: both ~/.yargpt and ~/.handful exist; keep one as ~/.handful and rename the other, then start again"

test("the S6 line, last in the log, picks S6 instead of the generic error", (t) => {
  assert.equal(bothFoldersLine, bothFolders)
  const file = path.join(tempDir(t), "server.log")
  fs.writeFileSync(file, [running, 'time=x level=INFO msg="shutting down"', bothFolders, bothFolders, ""].join("\n"))
  const line = lastStartupErrorIn(file)
  assert.equal(isBothFoldersError(line), true)
  const { options, actions } = serverErrorDialog(line, file)
  assert.equal(options.message, "Two data folders found")
  assert.equal(
    options.detail,
    "Both ~/.yargpt and ~/.handful exist, so Handful didn't move anything. Keep the folder you want as ~/.handful, and rename or remove the other one. Then open Handful again.",
  )
  assert.deepEqual(options.buttons, ["Show both in Finder", "Quit"])
  assert.equal(options.defaultId, 0)
  assert.equal(options.cancelId, 1)
  assert.deepEqual(actions, ["showBothFolders", "quit"])
})

test("other errors, and an S6 line followed by a start or another error, keep the generic screen", () => {
  for (const line of [configError, claudeError, null, `${bothFolders} again`, "both ~/.yargpt and ~/.handful exist; keep one as ~/.handful and rename the other, then start again"]) {
    assert.equal(isBothFoldersError(line), false, String(line))
    const { options, actions } = serverErrorDialog(line, "/l/server.log")
    assert.equal(options.message, "The Handful server did not start")
    assert.deepEqual(actions, ["openSettingsFile", "openLog", null])
  }
  assert.equal(lastStartupError([bothFolders, running].join("\n")), null)
  assert.equal(isBothFoldersError(lastStartupError([bothFolders, configError].join("\n"))), false)
})

test("the dialog's buttons do what they say", () => {
  const calls = []
  const deps = {
    home: "/Users/me",
    openSettingsFile: () => calls.push("openSettingsFile"),
    openLog: () => calls.push("openLog"),
    showItemInFolder: (file) => calls.push(["showItemInFolder", file]),
    quit: () => calls.push("quit"),
  }
  const s6 = serverErrorDialog(bothFolders, "/l/server.log").actions
  runServerErrorAction(s6[0], deps)
  assert.deepEqual(calls, [
    ["showItemInFolder", "/Users/me/.yargpt"],
    ["showItemInFolder", "/Users/me/.handful"],
  ])
  calls.length = 0
  runServerErrorAction(s6[1], deps)
  assert.deepEqual(calls, ["quit"])
  calls.length = 0
  for (const action of serverErrorDialog(configError, "/l/server.log").actions) runServerErrorAction(action, deps)
  assert.deepEqual(calls, ["openSettingsFile", "openLog"], "Close does nothing")
})
