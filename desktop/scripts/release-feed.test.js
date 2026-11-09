const assert = require("node:assert/strict")
const crypto = require("node:crypto")
const fs = require("node:fs")
const os = require("node:os")
const path = require("node:path")
const { test } = require("node:test")
const { zipName, blockmapName, parseFeed, formatFeed, prepare, verify, stage, main } = require("./release-feed")

// What electron-builder 26 writes for make app-universal.
const builtFeed = (version, zip) => `version: ${version}
files:
  - url: YarGPT-${version}-universal-mac.zip
    sha512: ${crypto.createHash("sha512").update(zip).digest("base64")}
    size: ${zip.length}
path: YarGPT-${version}-universal-mac.zip
sha512: ${crypto.createHash("sha512").update(zip).digest("base64")}
releaseDate: '2026-11-09T15:16:32.681Z'
`

function dist(t, version = "1.4.0", zip = Buffer.from("zip bytes")) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "yargpt-feed-"))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  fs.writeFileSync(path.join(dir, `YarGPT-${version}-universal-mac.zip`), zip)
  fs.writeFileSync(path.join(dir, `YarGPT-${version}-universal-mac.zip.blockmap`), "blockmap")
  fs.writeFileSync(path.join(dir, "latest-mac.yml"), builtFeed(version, zip))
  return dir
}

test("the feed parses and formats the way electron-builder writes it", () => {
  const text = builtFeed("1.4.0", Buffer.from("x"))
  const feed = parseFeed(text)
  assert.equal(feed.version, "1.4.0")
  assert.equal(feed.files.length, 1)
  assert.equal(feed.files[0].size, 1)
  assert.equal(feed.releaseDate, "2026-11-09T15:16:32.681Z")
  assert.deepEqual(parseFeed(formatFeed(feed)), feed)
  assert.deepEqual(parseFeed('version: "1.10.0"\nnote: \'it\'\'s\'\n'), { version: "1.10.0", note: "it's" })
  // A version that YAML could read as a number stays a string.
  assert.match(formatFeed({ version: "1.10" }), /^version: '1.10'$/m)
  assert.match(formatFeed({ flag: "true" }), /^flag: 'true'$/m)
  assert.throws(() => parseFeed("version: 1.0.0\n  weird line\n"), /line 2/)
})

test("prepare renames the zip and the blockmap and points the feed at them", (t) => {
  const zip = Buffer.from("the stapled zip")
  const dir = dist(t, "1.4.0-rc.1", zip)
  fs.appendFileSync(path.join(dir, "latest-mac.yml"), "stagingPercentage: 10\n")
  prepare(dir, "1.4.0-rc.1")
  assert.deepEqual(fs.readdirSync(dir).sort(), [blockmapName, zipName, "latest-mac.yml"].sort())
  const feed = parseFeed(fs.readFileSync(path.join(dir, "latest-mac.yml"), "utf8"))
  const sha512 = crypto.createHash("sha512").update(zip).digest("base64")
  assert.deepEqual(feed, {
    version: "1.4.0-rc.1",
    files: [{ url: zipName, sha512, size: zip.length }],
    path: zipName,
    sha512,
    releaseDate: "2026-11-09T15:16:32.681Z",
  })
  assert.deepEqual(verify(dir), [])
})

test("prepare refuses a feed of another version", (t) => {
  const dir = dist(t, "1.4.0")
  assert.throws(() => prepare(dir, "1.4.1"), /version 1.4.0, not 1.4.1/)
})

test("verify catches a zip that changed after the feed was written", (t) => {
  const dir = dist(t)
  prepare(dir, "1.4.0")
  fs.appendFileSync(path.join(dir, zipName), "tampered")
  const problems = verify(dir)
  assert.equal(problems.length, 3)
  assert.match(problems.join("\n"), /sha512 is .*, but the zip's is/)
  assert.match(problems.join("\n"), /files has size 9, but the zip has 17 bytes/)
  assert.throws(() => main(["verify", dir]), /does not match the zip/)
})

test("verify catches a feed that names another file, and missing files", (t) => {
  const dir = dist(t)
  assert.match(verify(dir).join("\n"), /YarGPT-universal-mac.zip is missing/)
  prepare(dir, "1.4.0")
  fs.rmSync(path.join(dir, blockmapName))
  const feedFile = path.join(dir, "latest-mac.yml")
  fs.writeFileSync(feedFile, fs.readFileSync(feedFile, "utf8").replace(/url: .*/, "url: other.zip").replace(/^path: .*/m, "path: other.zip"))
  assert.deepEqual(verify(dir), [
    `${blockmapName} is missing`,
    `path is other.zip, not ${zipName}`,
    `files has url other.zip, not ${zipName}`,
  ])
  fs.writeFileSync(feedFile, "version: 1.4.0\nfiles:\npath: YarGPT-universal-mac.zip\n")
  assert.match(verify(dir).join("\n"), /files is empty/)
})

test("stage sets stagingPercentage, and 0 stops the release", (t) => {
  const dir = dist(t)
  prepare(dir, "1.4.0")
  const feedFile = path.join(dir, "latest-mac.yml")
  stage(feedFile, "0")
  assert.match(fs.readFileSync(feedFile, "utf8"), /^stagingPercentage: 0$/m)
  assert.equal(parseFeed(fs.readFileSync(feedFile, "utf8")).stagingPercentage, 0)
  stage(feedFile, 25)
  assert.equal(parseFeed(fs.readFileSync(feedFile, "utf8")).stagingPercentage, 25)
  assert.deepEqual(verify(dir), [], "staging keeps the zip's sha512")
  for (const bad of ["101", "-1", "5.5", "all"]) assert.throws(() => stage(feedFile, bad), /0 to 100/, bad)
  stage(feedFile, "")
  assert.equal(parseFeed(fs.readFileSync(feedFile, "utf8")).stagingPercentage, undefined, "empty releases to everyone")
  main(["stage", feedFile, "0"])
  assert.equal(parseFeed(fs.readFileSync(feedFile, "utf8")).stagingPercentage, 0)
})

test("main refuses unknown commands", () => {
  assert.throws(() => main(["publish"]), /usage/)
  assert.throws(() => main(["verify"]), /usage/)
})
