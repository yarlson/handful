#!/usr/bin/env node
// release-feed prepares the update feed of a release: the zip, its blockmap
// and latest-mac.yml, which the app's updater reads from
// https://github.com/yarlson/yargpt/releases/latest/download/.
//
//   node release-feed.js prepare <dist dir> <version>
//     Renames YarGPT-<version>-universal-mac.zip and its blockmap to
//     YarGPT-universal-mac.zip(.blockmap), and rewrites latest-mac.yml for
//     that name, with the sha512 and size of the renamed zip.
//   node release-feed.js verify <dist dir>
//     Fails unless latest-mac.yml names YarGPT-universal-mac.zip with its
//     sha512 and size, and the blockmap is there.
//   node release-feed.js stage <latest-mac.yml> <percentage>
//     Sets stagingPercentage, 0 to 100. 0 stops a release for everyone; an
//     empty percentage removes it, which releases to everyone.
//
// It uses no packages: the release job runs it without npm install.
const crypto = require("node:crypto")
const fs = require("node:fs")
const path = require("node:path")

const zipName = "YarGPT-universal-mac.zip"
const blockmapName = `${zipName}.blockmap`
const feedName = "latest-mac.yml"

// parseFeed parses latest-mac.yml as electron-builder writes it: scalars at
// the top, and a files list of maps of scalars.
function parseFeed(text) {
  const feed = {}
  let files = null
  for (const [i, line] of text.split("\n").entries()) {
    if (line.trim() === "" || line.trimStart().startsWith("#")) continue
    const item = /^ {2}- ([A-Za-z0-9_]+):(?: (.*))?$/.exec(line)
    const field = /^ {4}([A-Za-z0-9_]+):(?: (.*))?$/.exec(line)
    const top = /^([A-Za-z0-9_]+):(?: (.*))?$/.exec(line)
    if (item && files) {
      files.push({ [item[1]]: scalar(item[2]) })
    } else if (field && files?.length) {
      files.at(-1)[field[1]] = scalar(field[2])
    } else if (top) {
      files = top[1] === "files" ? [] : null
      feed[top[1]] = files ?? scalar(top[2])
    } else {
      throw new Error(`${feedName} line ${i + 1} is not in the expected form: ${line}`)
    }
  }
  return feed
}

function scalar(text = "") {
  const value = text.trim()
  if (value.startsWith("'")) return value.slice(1, -1).replaceAll("''", "'")
  if (value.startsWith('"')) return JSON.parse(value)
  if (/^\d+$/.test(value)) return Number(value)
  return value
}

// formatScalar quotes a string unless YAML reads it back as the same string.
function formatScalar(value) {
  if (typeof value === "number") return String(value)
  const text = String(value)
  const plain = /^[A-Za-z_/][A-Za-z0-9._\-/+=]*$/.test(text) && !/^(true|false|null|yes|no|on|off|~)$/i.test(text)
  return plain ? text : `'${text.replaceAll("'", "''")}'`
}

function formatFeed(feed) {
  const lines = []
  for (const [key, value] of Object.entries(feed)) {
    if (!Array.isArray(value)) {
      lines.push(`${key}: ${formatScalar(value)}`)
      continue
    }
    lines.push(`${key}:`)
    for (const file of value) {
      Object.entries(file).forEach(([k, v], j) => lines.push(`${j === 0 ? "  - " : "    "}${k}: ${formatScalar(v)}`))
    }
  }
  return `${lines.join("\n")}\n`
}

// digest returns the sha512, base64 as electron-builder writes it, and the
// size of a file.
function digest(file) {
  const data = fs.readFileSync(file)
  return { sha512: crypto.createHash("sha512").update(data).digest("base64"), size: data.length }
}

function prepare(dist, version) {
  const builtZip = path.join(dist, `YarGPT-${version}-universal-mac.zip`)
  const feedFile = path.join(dist, feedName)
  const feed = parseFeed(fs.readFileSync(feedFile, "utf8"))
  if (String(feed.version) !== version) throw new Error(`${feedName} has version ${feed.version}, not ${version}`)
  fs.renameSync(builtZip, path.join(dist, zipName))
  fs.renameSync(`${builtZip}.blockmap`, path.join(dist, blockmapName))
  const { sha512, size } = digest(path.join(dist, zipName))
  const files = (Array.isArray(feed.files) ? feed.files : []).filter((f) => String(f.url).endsWith(".zip"))
  if (files.length !== 1) throw new Error(`${feedName} lists ${files.length} zips, not 1`)
  // A tag push releases to everyone; only the stage command sets a percentage.
  const { stagingPercentage: _, ...rest } = feed
  const next = {
    ...rest,
    version: String(feed.version),
    files: [{ ...files[0], url: zipName, sha512, size }],
    path: zipName,
    sha512,
  }
  fs.writeFileSync(feedFile, formatFeed(next))
  return next
}

// verify returns the problems of the feed in dist; none means that it is
// ready to upload.
function verify(dist) {
  const problems = []
  const feed = parseFeed(fs.readFileSync(path.join(dist, feedName), "utf8"))
  const zip = path.join(dist, zipName)
  if (!fs.existsSync(zip)) return [`${zipName} is missing`]
  if (!fs.existsSync(path.join(dist, blockmapName))) problems.push(`${blockmapName} is missing`)
  const { sha512, size } = digest(zip)
  if (feed.path !== zipName) problems.push(`path is ${feed.path}, not ${zipName}`)
  if (feed.sha512 !== sha512) problems.push(`sha512 is ${feed.sha512}, but the zip's is ${sha512}`)
  const files = Array.isArray(feed.files) ? feed.files : []
  if (files.length === 0) problems.push("files is empty")
  for (const file of files) {
    if (file.url !== zipName) problems.push(`files has url ${file.url}, not ${zipName}`)
    if (file.sha512 !== sha512) problems.push(`files has sha512 ${file.sha512}, but the zip's is ${sha512}`)
    if (file.size !== size) problems.push(`files has size ${file.size}, but the zip has ${size} bytes`)
  }
  return problems
}

// stage sets stagingPercentage; an empty percentage removes it, which
// releases to everyone.
function stage(feedFile, percentage) {
  const text = String(percentage ?? "").trim()
  if (text !== "" && (!/^\d+$/.test(text) || Number(text) > 100)) {
    throw new Error(`staging percentage ${percentage} is not a whole number from 0 to 100`)
  }
  const { stagingPercentage: _, ...feed } = parseFeed(fs.readFileSync(feedFile, "utf8"))
  if (text !== "") feed.stagingPercentage = Number(text)
  fs.writeFileSync(feedFile, formatFeed(feed))
  return feed
}

function main(args) {
  const [command, ...rest] = args
  if (command === "prepare" && rest.length === 2) {
    const feed = prepare(rest[0], rest[1])
    console.log(`${feedName}: ${feed.version}, ${zipName}, ${feed.files[0].size} bytes`)
  } else if (command === "verify" && rest.length === 1) {
    const problems = verify(rest[0])
    if (problems.length) throw new Error(`the update feed does not match the zip:\n${problems.join("\n")}`)
    console.log(`${feedName} matches ${zipName}`)
  } else if (command === "stage" && rest.length === 2) {
    const feed = stage(rest[0], rest[1])
    console.log(`${feedName}: ${feed.version}, stagingPercentage ${feed.stagingPercentage ?? "removed (everyone)"}`)
  } else {
    throw new Error("usage: release-feed.js prepare <dist> <version> | verify <dist> | stage <latest-mac.yml> <percentage>")
  }
}

if (require.main === module) {
  try {
    main(process.argv.slice(2))
  } catch (err) {
    console.error(`release-feed: ${err.message}`)
    process.exit(1)
  }
}

module.exports = { zipName, blockmapName, parseFeed, formatFeed, prepare, verify, stage, main }
