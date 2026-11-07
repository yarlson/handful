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

module.exports = { serverURL, isAppURL, isWebURL, ensureSettingsFile }
