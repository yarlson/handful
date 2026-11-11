#!/usr/bin/env node
// release-feed prepares and checks the update feeds of a release. A Handful
// release carries two sets of three files (docs/releasing.md):
//   - handful: Handful-universal-mac.zip, its blockmap and handful-mac.yml,
//     which Handful's updater reads from
//     https://github.com/yarlson/handful/releases/latest/download/;
//   - bridge: copies of the bridge release's zip, blockmap and
//     latest-mac.yml, which the apps from before the rename read there.
//
//   node release-feed.js prepare <dist dir> <version>
//     Renames Handful-<version>-universal-mac.zip and its blockmap to
//     Handful-universal-mac.zip(.blockmap), and rewrites handful-mac.yml for
//     that name, with the sha512 and size of the renamed zip.
//   node release-feed.js verify <dir> <set>[=<version>]...
//     Fails unless, for each set (handful or bridge), its feed names its zip
//     with the zip's sha512 and size, the blockmap is there, and, when a
//     version is given, the feed has that version.
//   node release-feed.js stage <handful-mac.yml> <percentage>
//     Sets stagingPercentage, 0 to 100. 0 stops a release for everyone; an
//     empty percentage removes it, which releases to everyone.
//
// It uses no packages: the release jobs run it without npm install.
const crypto = require("node:crypto")
const fs = require("node:fs")
const path = require("node:path")

// The file names of each set. They are fixed: the site links the zips, and
// each feed names its zip.
const sets = {
  handful: { zip: "Handful-universal-mac.zip", feed: "handful-mac.yml", built: (version) => `Handful-${version}-universal-mac.zip` },
  bridge: { zip: "YarGPT-universal-mac.zip", feed: "latest-mac.yml" },
}
for (const set of Object.values(sets)) set.blockmap = `${set.zip}.blockmap`

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
      throw new Error(`line ${i + 1} is not in the expected form: ${line}`)
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

// prepare gives Handful's built zip and blockmap their fixed names, and
// points handful-mac.yml at the renamed zip.
function prepare(dist, version) {
  const { zip, blockmap, feed: feedName, built } = sets.handful
  const builtZip = path.join(dist, built(version))
  const feedFile = path.join(dist, feedName)
  const feed = parseFeed(fs.readFileSync(feedFile, "utf8"))
  if (String(feed.version) !== version) throw new Error(`${feedName} has version ${feed.version}, not ${version}`)
  fs.renameSync(builtZip, path.join(dist, zip))
  fs.renameSync(`${builtZip}.blockmap`, path.join(dist, blockmap))
  const { sha512, size } = digest(path.join(dist, zip))
  const files = (Array.isArray(feed.files) ? feed.files : []).filter((f) => String(f.url).endsWith(".zip"))
  if (files.length !== 1) throw new Error(`${feedName} lists ${files.length} zips, not 1`)
  // A tag push releases to everyone; only the stage command sets a percentage.
  const { stagingPercentage: _, ...rest } = feed
  const next = {
    ...rest,
    version: String(feed.version),
    files: [{ ...files[0], url: zip, sha512, size }],
    path: zip,
    sha512,
  }
  fs.writeFileSync(feedFile, formatFeed(next))
  return next
}

// verify returns the problems of one set of files in dir; none means that
// it is ready to upload. version, when given, is the version its feed must
// have.
function verify(dir, setName, version) {
  const set = sets[setName]
  if (!set) throw new Error(`unknown set ${setName}, not ${Object.keys(sets).join(" or ")}`)
  const { zip: zipName, blockmap: blockmapName, feed: feedName } = set
  const feedFile = path.join(dir, feedName)
  if (!fs.existsSync(feedFile)) return [`${feedName} is missing`]
  const problems = []
  const feed = parseFeed(fs.readFileSync(feedFile, "utf8"))
  const zip = path.join(dir, zipName)
  if (!fs.existsSync(zip)) return [`${zipName} is missing`]
  if (!fs.existsSync(path.join(dir, blockmapName))) problems.push(`${blockmapName} is missing`)
  if (version !== undefined && String(feed.version) !== version) problems.push(`${feedName} has version ${feed.version}, not ${version}`)
  const { sha512, size } = digest(zip)
  if (feed.path !== zipName) problems.push(`${feedName} has path ${feed.path}, not ${zipName}`)
  if (feed.sha512 !== sha512) problems.push(`${feedName} has sha512 ${feed.sha512}, but ${zipName}'s is ${sha512}`)
  const files = Array.isArray(feed.files) ? feed.files : []
  if (files.length === 0) problems.push(`${feedName} has no files`)
  for (const file of files) {
    if (file.url !== zipName) problems.push(`${feedName} has files url ${file.url}, not ${zipName}`)
    if (file.sha512 !== sha512) problems.push(`${feedName} has files sha512 ${file.sha512}, but ${zipName}'s is ${sha512}`)
    if (file.size !== size) problems.push(`${feedName} has files size ${file.size}, but ${zipName} has ${size} bytes`)
  }
  return problems
}

// verifyAll checks each set of specs, such as "handful=0.14.0" or "bridge".
function verifyAll(dir, specs) {
  return specs.flatMap((spec) => {
    const [setName, version] = spec.split("=")
    return verify(dir, setName, version)
  })
}

// stage sets stagingPercentage; an empty percentage removes it, which
// releases to everyone.
function stage(feedFile, percentage) {
  // Only Handful's feed is staged: the bridge's latest-mac.yml stays as the
  // bridge release published it.
  if (path.basename(feedFile) !== sets.handful.feed) throw new Error(`stage changes only ${sets.handful.feed}, not ${path.basename(feedFile)}`)
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
  const { zip, feed: feedName } = sets.handful
  if (command === "prepare" && rest.length === 2) {
    const feed = prepare(rest[0], rest[1])
    console.log(`${feedName}: ${feed.version}, ${zip}, ${feed.files[0].size} bytes`)
  } else if (command === "verify" && rest.length >= 2) {
    const problems = verifyAll(rest[0], rest.slice(1))
    if (problems.length) throw new Error(`the update feeds do not match the zips:\n${problems.join("\n")}`)
    for (const spec of rest.slice(1)) {
      const set = sets[spec.split("=")[0]]
      console.log(`${set.feed} matches ${set.zip}`)
    }
  } else if (command === "stage" && rest.length === 2) {
    const feed = stage(rest[0], rest[1])
    console.log(`${feedName}: ${feed.version}, stagingPercentage ${feed.stagingPercentage ?? "removed (everyone)"}`)
  } else {
    throw new Error("usage: release-feed.js prepare <dist> <version> | verify <dir> <set>[=<version>]... | stage <handful-mac.yml> <percentage>")
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

module.exports = { sets, parseFeed, formatFeed, prepare, verify, verifyAll, stage, main }
