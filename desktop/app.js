const fs = require("node:fs")
const path = require("node:path")

// The server's address. The OAuth redirect URI holds the port, so it is fixed.
const serverURL = "http://127.0.0.1:7777"

// isAppURL reports whether url is a page of the Handful server.
function isAppURL(url) {
  try {
    return new URL(url).origin === serverURL
  } catch {
    return false
  }
}

// isWebURL reports whether url may go to the default browser. Other schemes,
// such as file:, could open local files or start apps.
function isWebURL(url) {
  try {
    const { protocol } = new URL(url)
    return protocol === "http:" || protocol === "https:"
  } catch {
    return false
  }
}

// The menu-bar icon of the server opens this address for Check for updates….
const checkForUpdatesURL = "handful://check-for-updates"

// isCheckForUpdatesURL reports whether url asks the app to check for updates.
function isCheckForUpdatesURL(url) {
  try {
    const u = new URL(url)
    return u.protocol === "handful:" && u.host === "check-for-updates" && (u.pathname === "" || u.pathname === "/")
  } catch {
    return false
  }
}

// isTrustedFrame reports whether an IPC call may reach the updater: only the
// main frame of a server page may call it, not an embedded frame or another
// site.
function isTrustedFrame(frameURL, isMainFrame) {
  return isMainFrame === true && isAppURL(frameURL)
}

// ensureSettingsFile creates dataDir/config.json with an empty object when it
// is missing, and returns its path.
function ensureSettingsFile(dataDir) {
  fs.mkdirSync(dataDir, { recursive: true, mode: 0o700 })
  const file = path.join(dataDir, "config.json")
  try {
    fs.writeFileSync(file, "{}\n", { flag: "wx" })
  } catch (err) {
    if (err.code !== "EEXIST") throw err
  }
  return file
}

// settingsDir returns the data folder that holds config.json: ~/.handful when
// it exists, of any kind; else ~/.yargpt when it is a real folder, not a
// link; else ~/.handful. The server moves a real ~/.yargpt to ~/.handful when
// it starts, and refuses to start when both exist (S6), so the app must not
// create ~/.handful while ~/.yargpt is a real folder. The server can move the
// folder while the app runs, so call it each time the folder is used.
function settingsDir(home, fsys = fs) {
  const handful = path.join(home, ".handful")
  if (lstatOrNull(handful, fsys)) return handful
  const old = path.join(home, ".yargpt")
  if (lstatOrNull(old, fsys)?.isDirectory()) return old
  return handful
}

function lstatOrNull(file, fsys) {
  try {
    return fsys.lstatSync(file)
  } catch {
    return null
  }
}

// settingsFile returns the path of config.json in settingsDir(home).
function settingsFile(home, fsys = fs) {
  return path.join(settingsDir(home, fsys), "config.json")
}

// ensureSettingsFileIn creates config.json in settingsDir(home) when it is
// missing, and returns its path: Open Settings File and the Settings page's
// openFile.
function ensureSettingsFileIn(home, fsys = fs) {
  return ensureSettingsFile(settingsDir(home, fsys))
}

module.exports = {
  serverURL,
  checkForUpdatesURL,
  isAppURL,
  isWebURL,
  isCheckForUpdatesURL,
  isTrustedFrame,
  ensureSettingsFile,
  settingsDir,
  settingsFile,
  ensureSettingsFileIn,
}
