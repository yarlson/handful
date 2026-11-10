const assert = require("node:assert/strict")
const fs = require("node:fs")
const os = require("node:os")
const path = require("node:path")
const { test } = require("node:test")
const { tailBytes, lastStartupError, readTail, lastStartupErrorIn, serverErrorDialog } = require("./server-log")

function tempDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "yargpt-log-"))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  return dir
}

const configError = "yargpt: config file /Users/me/.yargpt/config.json: invalid character '}' looking for beginning of object key string"
const claudeError = 'yargpt: find Claude Code CLI "/nope": exec: "/nope": stat /nope: no such file or directory'
const running = 'time=2026-11-10T10:00:00.000+01:00 level=INFO msg="YarGPT is running" url=http://127.0.0.1:7777 data=/Users/me/.yargpt claude=/Users/me/.local/bin/claude'

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
  const log = [configError, running, 'time=x level=INFO msg="shutting down"', "yargpt: restarting: launchd starts the server again", claudeError, "", "   ", claudeError.replace("/nope", "/other"), "", ""].join("\n")
  assert.equal(lastStartupError(log), claudeError.replace("/nope", "/other"))
  assert.equal(lastStartupError(`${configError}\r\n\r\n`), configError)
})

test("an error before the last start is old", () => {
  assert.equal(lastStartupError([configError, running, 'time=x level=WARN msg="web UI not available"'].join("\n")), null)
  assert.equal(lastStartupError([running, 'time=x level=INFO msg="shutting down"', "yargpt: restarting: launchd starts the server again"].join("\n")), null)
  assert.equal(lastStartupError(["flag provided but not defined: -nope", "Usage of yargpt:"].join("\n")), null)
})

test("a long error line is cut", () => {
  const line = lastStartupError(`yargpt: ${"x".repeat(5000)}\n`)
  assert.equal(line.length, 1001)
  assert.ok(line.startsWith("yargpt: xxx") && line.endsWith("…"))
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
  // The tail starts within the error line, at "yargpt: " of its middle.
  const cut = claudeError.indexOf('"/nope": stat') - 8
  assert.equal(lastStartupError(claudeError.slice(cut), true), null)
  assert.equal(lastStartupErrorIn(file, claudeError.length - 3), null)
  assert.equal(lastStartupErrorIn(file, claudeError.length + 1), claudeError)
})

test("the error screen offers config.json and the log, with the error line", () => {
  const { options, actions } = serverErrorDialog(configError, "/Users/me/Library/Logs/YarGPT/server.log")
  assert.deepEqual(options.buttons, ["Open config.json", "Open Log", "Close"])
  assert.deepEqual(actions, ["openSettingsFile", "openLog", null])
  assert.equal(options.cancelId, 2)
  assert.equal(options.defaultId, 0)
  assert.equal(options.message, "The YarGPT server did not start")
  assert.equal(options.detail, `${configError}\n\nIts log is /Users/me/Library/Logs/YarGPT/server.log.`)
  assert.equal(serverErrorDialog(null, "/l/server.log").options.detail, "Its log is /l/server.log.")
})
