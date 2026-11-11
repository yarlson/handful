// The server's last startup error, read from its log for the error screen.
// cmd/handful/main.go ends a server that fails with the line "handful: <error>",
// for example `handful: config file /Users/me/.yargpt/config.json: ...`.
const fs = require("node:fs")

const tailBytes = 64 * 1024
const maxLineLength = 1000
const errorPrefix = "handful: "
// A server that ends to restart writes this line; it is no error.
const restartLine = "handful: restarting: launchd starts the server again"
// slog's line when a server has started.
const runningMessage = 'msg="Handful is running"'

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
// server did not start, and the action of each button.
function serverErrorDialog(errorLine, logFile) {
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

module.exports = { tailBytes, lastStartupError, readTail, lastStartupErrorIn, serverErrorDialog }
