const assert = require("node:assert/strict")
const fs = require("node:fs")
const os = require("node:os")
const path = require("node:path")
const { test } = require("node:test")
const { checkForUpdatesURL, isAppURL, isWebURL, isCheckForUpdatesURL, isTrustedFrame, ensureSettingsFile } = require("./app")

function tempDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "handful-desktop-"))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  return dir
}

test("only pages of the server are app pages", () => {
  assert.equal(isAppURL("http://127.0.0.1:7777/chat/abc"), true)
  assert.equal(isAppURL("http://127.0.0.1:7777"), true)
  assert.equal(isAppURL("http://127.0.0.1:7778/"), false)
  assert.equal(isAppURL("http://localhost:7777/"), false)
  assert.equal(isAppURL("https://example.com/"), false)
  assert.equal(isAppURL("about:blank"), false)
  assert.equal(isAppURL("not a url"), false)
})

test("only http and https links go to the browser", () => {
  assert.equal(isWebURL("https://example.com/docs"), true)
  assert.equal(isWebURL("http://127.0.0.1:7777/api/media/x"), true)
  assert.equal(isWebURL("file:///etc/passwd"), false)
  assert.equal(isWebURL("javascript:alert(1)"), false)
  assert.equal(isWebURL("vscode://file/x"), false)
  assert.equal(isWebURL("about:blank"), false)
  assert.equal(isWebURL(""), false)
})

test("the settings file is created with an empty object in a private folder", (t) => {
  const dataDir = path.join(tempDir(t), ".yargpt")
  const file = ensureSettingsFile(dataDir)
  assert.equal(file, path.join(dataDir, "config.json"))
  assert.equal(fs.readFileSync(file, "utf8"), "{}\n")
  assert.equal(fs.statSync(dataDir).mode & 0o777, 0o700)
})

test("an existing settings file is kept as it is", (t) => {
  const dataDir = tempDir(t)
  fs.writeFileSync(path.join(dataDir, "config.json"), '{"model": "sonnet"}\n')
  const file = ensureSettingsFile(dataDir)
  assert.equal(fs.readFileSync(file, "utf8"), '{"model": "sonnet"}\n')
})

test("only handful://check-for-updates asks for a check", () => {
  assert.equal(checkForUpdatesURL, "handful://check-for-updates")
  assert.equal(isCheckForUpdatesURL("handful://check-for-updates"), true)
  assert.equal(isCheckForUpdatesURL("handful://check-for-updates/"), true)
  assert.equal(isCheckForUpdatesURL("handful://check-for-updates/x"), false)
  assert.equal(isCheckForUpdatesURL("handful://other"), false)
  assert.equal(isCheckForUpdatesURL("https://check-for-updates/"), false)
  assert.equal(isCheckForUpdatesURL("--inspect"), false)
  assert.equal(isCheckForUpdatesURL(undefined), false)
})

test("only the main frame of a server page may call the updater", () => {
  assert.equal(isTrustedFrame("http://127.0.0.1:7777/chat/abc", true), true)
  assert.equal(isTrustedFrame("http://127.0.0.1:7777/chat/abc", false), false)
  assert.equal(isTrustedFrame("https://example.com/", true), false)
  assert.equal(isTrustedFrame("about:blank", true), false)
  assert.equal(isTrustedFrame(undefined, true), false)
})
