const fs = require("node:fs")
const path = require("node:path")

// The server's address. The OAuth redirect URI holds the port, so it is fixed.
const serverURL = "http://127.0.0.1:7777"

// isAppURL reports whether url is a page of the YarGPT server.
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
const checkForUpdatesURL = "yargpt://check-for-updates"

// isCheckForUpdatesURL reports whether url asks the app to check for updates.
function isCheckForUpdatesURL(url) {
  try {
    const u = new URL(url)
    return u.protocol === "yargpt:" && u.host === "check-for-updates" && (u.pathname === "" || u.pathname === "/")
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

module.exports = { serverURL, checkForUpdatesURL, isAppURL, isWebURL, isCheckForUpdatesURL, isTrustedFrame, ensureSettingsFile }
