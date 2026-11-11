// The server's last startup error, read from its log for the error screen.
// cmd/handful/main.go ends a server that fails with the line "handful: <error>",
// for example `handful: config file /Users/me/.handful/config.json: ...`.
const fs = require("node:fs")
const path = require("node:path")

const tailBytes = 64 * 1024
const maxLineLength = 1000
const errorPrefix = "handful: "
// A server that ends to restart writes this line; it is no error.
const restartLine = "handful: restarting: launchd starts the server again"
// slog's line when a server has started.
const runningMessage = 'msg="Handful is running"'
// The server's line when both data folders exist, so that it moved nothing
// and stopped (spec §5, S6).
const bothFoldersLine = "handful: both ~/.yargpt and ~/.handful exist; keep one as ~/.handful and rename the other, then start again"

// isBothFoldersError reports whether a startup error is the S6 line.
function isBothFoldersError(errorLine) {
  return errorLine === bothFoldersLine
}

// lastStartupError returns the last line of text that reports a fatal
// error, or null. A start after that line means the error is old, so it
// returns null then too. partial says that text starts within a line, whose
// rest it skips.
function lastStartupError(text, partial = false) {
  if (typeof text !== "string" || text === "") return null
  const lines = text.split(/\r?\n/)
  if (partial) lines.shift()
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i].trimEnd()
    if (line === "" || line === restartLine) continue
    if (line.includes(runningMessage)) return null
    if (line.startsWith(errorPrefix)) return line.length > maxLineLength ? `${line.slice(0, maxLineLength)}…` : line
  }
  return null
}

// readTail reads at most the last max bytes of file. It returns {text,
// partial}, or null when the file cannot be read.
function readTail(file, max = tailBytes, fsys = fs) {
  let fd
  try {
    fd = fsys.openSync(file, "r")
    const { size } = fsys.fstatSync(fd)
    const start = Math.max(0, size - max)
    const buffer = Buffer.alloc(size - start)
    let read = 0
    while (read < buffer.length) {
      const n = fsys.readSync(fd, buffer, read, buffer.length - read, start + read)
      if (n === 0) break
      read += n
    }
    return { text: buffer.subarray(0, read).toString("utf8"), partial: start > 0 }
  } catch {
    return null
  } finally {
    if (fd !== undefined) fsys.closeSync(fd)
  }
}

// lastStartupErrorIn returns the last startup error of the log file, or null.
function lastStartupErrorIn(file, max = tailBytes, fsys = fs) {
  const tail = readTail(file, max, fsys)
  return tail ? lastStartupError(tail.text, tail.partial) : null
}

// serverErrorDialog returns the options of the message box that says the
// server did not start, and the action of each button. The S6 line gets S6,
// which says what to do with the two data folders, instead.
function serverErrorDialog(errorLine, logFile) {
  if (isBothFoldersError(errorLine)) {
    return {
      options: {
        type: "warning",
        message: "Two data folders found",
        detail:
          "Both ~/.yargpt and ~/.handful exist, so Handful didn't move anything. Keep the folder you want as ~/.handful, and rename or remove the other one. Then open Handful again.",
        buttons: ["Show both in Finder", "Quit"],
        defaultId: 0,
        cancelId: 1,
      },
      actions: ["showBothFolders", "quit"],
    }
  }
  const where = `Its log is ${logFile}.`
  return {
    options: {
      type: "error",
      message: "The Handful server did not start",
      detail: errorLine ? `${errorLine}\n\n${where}` : where,
      buttons: ["Open config.json", "Open Log", "Close"],
      defaultId: 0,
      cancelId: 2,
    },
    actions: ["openSettingsFile", "openLog", null],
  }
}

// runServerErrorAction does what the chosen button of serverErrorDialog says.
//   home: the home folder, which holds ~/.yargpt and ~/.handful
//   openSettingsFile(), openLog(): open config.json and the server log
//   showItemInFolder(file): shows file in the Finder
//   quit(): quits the app
// Show both in Finder only shows the folders; it removes nothing.
function runServerErrorAction(action, { home, openSettingsFile, openLog, showItemInFolder, quit }) {
  if (action === "openSettingsFile") openSettingsFile()
  else if (action === "openLog") openLog()
  else if (action === "showBothFolders") {
    showItemInFolder(path.join(home, ".yargpt"))
    showItemInFolder(path.join(home, ".handful"))
  } else if (action === "quit") quit()
}

module.exports = {
  tailBytes,
  bothFoldersLine,
  isBothFoldersError,
  lastStartupError,
  readTail,
  lastStartupErrorIn,
  serverErrorDialog,
  runServerErrorAction,
}
