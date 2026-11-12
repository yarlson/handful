const assert = require("node:assert/strict")
const fs = require("node:fs")
const os = require("node:os")
const path = require("node:path")
const { test } = require("node:test")
const {
  readKey,
  keyHeaders,
  syncKeyCookie,
  checkForUpdatesURL,
  isAppURL,
  isWebURL,
  isCheckForUpdatesURL,
  isTrustedFrame,
  ensureSettingsFile,
  settingsDir,
  settingsFile,
  ensureSettingsFileIn,
} = require("./app")

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
  const dataDir = path.join(tempDir(t), ".handful")
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

test("the data folder is ~/.handful when it exists, of any kind", (t) => {
  const home = tempDir(t)
  fs.mkdirSync(path.join(home, ".handful"))
  assert.equal(settingsDir(home), path.join(home, ".handful"))
  // Both real folders, the case of S6: still ~/.handful, and nothing is created.
  fs.mkdirSync(path.join(home, ".yargpt"))
  assert.equal(settingsDir(home), path.join(home, ".handful"))
  // A link, even one to a missing folder, counts too.
  const linked = tempDir(t)
  fs.symlinkSync(path.join(linked, "gone"), path.join(linked, ".handful"))
  fs.mkdirSync(path.join(linked, ".yargpt"))
  assert.equal(settingsDir(linked), path.join(linked, ".handful"))
})

test("the data folder is ~/.yargpt while it is a real folder and ~/.handful is missing", (t) => {
  const home = tempDir(t)
  fs.mkdirSync(path.join(home, ".yargpt"))
  assert.equal(settingsDir(home), path.join(home, ".yargpt"))
  assert.equal(settingsFile(home), path.join(home, ".yargpt", "config.json"))
})

test("the data folder is ~/.handful when ~/.yargpt is the link the move leaves, a file, or missing", (t) => {
  const home = tempDir(t)
  assert.equal(settingsDir(home), path.join(home, ".handful"), "neither exists")
  fs.writeFileSync(path.join(home, ".yargpt"), "")
  assert.equal(settingsDir(home), path.join(home, ".handful"), "~/.yargpt is a file")
  // After the move: ~/.yargpt links to ~/.handful.
  const moved = tempDir(t)
  fs.mkdirSync(path.join(moved, ".handful"))
  fs.symlinkSync(path.join(moved, ".handful"), path.join(moved, ".yargpt"))
  assert.equal(settingsDir(moved), path.join(moved, ".handful"))
  // A link to a folder elsewhere is no real ~/.yargpt either.
  const elsewhere = tempDir(t)
  fs.mkdirSync(path.join(elsewhere, "data"))
  fs.symlinkSync(path.join(elsewhere, "data"), path.join(elsewhere, ".yargpt"))
  assert.equal(settingsDir(elsewhere), path.join(elsewhere, ".handful"))
})

test("the data folder is found again each time, as the server moves it", (t) => {
  const home = tempDir(t)
  fs.mkdirSync(path.join(home, ".yargpt"))
  assert.equal(settingsFile(home), path.join(home, ".yargpt", "config.json"))
  fs.renameSync(path.join(home, ".yargpt"), path.join(home, ".handful"))
  fs.symlinkSync(path.join(home, ".handful"), path.join(home, ".yargpt"))
  assert.equal(settingsFile(home), path.join(home, ".handful", "config.json"))
})

test("Open Settings File with a real ~/.yargpt and no ~/.handful writes ~/.yargpt/config.json and creates no ~/.handful", (t) => {
  const home = tempDir(t)
  fs.mkdirSync(path.join(home, ".yargpt"), { mode: 0o700 })
  const file = ensureSettingsFileIn(home)
  assert.equal(file, path.join(home, ".yargpt", "config.json"))
  assert.equal(fs.readFileSync(file, "utf8"), "{}\n")
  assert.equal(fs.existsSync(path.join(home, ".handful")), false)
  assert.deepEqual(fs.readdirSync(home), [".yargpt"])
})

test("Open Settings File with no data folder creates ~/.handful/config.json", (t) => {
  const home = tempDir(t)
  const file = ensureSettingsFileIn(home)
  assert.equal(file, path.join(home, ".handful", "config.json"))
  assert.deepEqual(fs.readdirSync(home), [".handful"])
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

test("readKey reads ui-key from the data folder, and reads it again after a rotation", (t) => {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "handful-key-"))
  t.after(() => fs.rmSync(home, { recursive: true, force: true }))
  fs.mkdirSync(path.join(home, ".handful"))
  const file = path.join(home, ".handful", "ui-key")
  fs.writeFileSync(file, "AbC-123_xyz\n")
  assert.equal(readKey(home), "AbC-123_xyz")
  fs.writeFileSync(file, "rotated_key")
  assert.equal(readKey(home), "rotated_key")
})

test("readKey uses a real ~/.yargpt before the move", (t) => {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "handful-key-"))
  t.after(() => fs.rmSync(home, { recursive: true, force: true }))
  fs.mkdirSync(path.join(home, ".yargpt"))
  fs.writeFileSync(path.join(home, ".yargpt", "ui-key"), "old_folder_key")
  assert.equal(readKey(home), "old_folder_key")
})

test("readKey without a key file, or with a broken one, gives no key", (t) => {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "handful-key-"))
  t.after(() => fs.rmSync(home, { recursive: true, force: true }))
  const logged = []
  const log = (line) => logged.push(line)
  assert.equal(readKey(home, fs, log), null)
  assert.deepEqual(logged, [], "a missing file is an older server, not an error")
  fs.mkdirSync(path.join(home, ".handful"))
  const file = path.join(home, ".handful", "ui-key")
  for (const bad of ["", "  \n", "has space", "a\nb", "x".repeat(1025)]) {
    fs.writeFileSync(file, bad)
    assert.equal(readKey(home, fs, log), null, JSON.stringify(bad))
  }
  fs.rmSync(file)
  fs.mkdirSync(file)
  assert.equal(readKey(home, fs, log), null)
  assert.equal(logged.length, 6)
  assert.ok(logged.every((line) => !line.includes("has space")), "the file's text is never logged")
})

test("keyHeaders carries the key as X-Handful-Key, and nothing without one", () => {
  assert.deepEqual(keyHeaders("k1"), { "X-Handful-Key": "k1" })
  assert.deepEqual(keyHeaders(null), {})
})

test("syncKeyCookie sets an HttpOnly, SameSite=Strict cookie for the server, and removes it without a key", async () => {
  const calls = []
  const cookies = {
    set: async (details) => calls.push(["set", details]),
    remove: async (url, name) => calls.push(["remove", url, name]),
  }
  await syncKeyCookie({ cookies, url: "http://127.0.0.1:7777", key: "k1" })
  await syncKeyCookie({ cookies, url: "http://127.0.0.1:7777", key: null })
  assert.deepEqual(calls, [
    ["set", { url: "http://127.0.0.1:7777", name: "handful_key", value: "k1", httpOnly: true, sameSite: "strict" }],
    ["remove", "http://127.0.0.1:7777", "handful_key"],
  ])
})
