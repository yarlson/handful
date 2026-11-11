const assert = require("node:assert/strict")
const crypto = require("node:crypto")
const fs = require("node:fs")
const os = require("node:os")
const path = require("node:path")
const { test } = require("node:test")
const { sets, parseFeed, formatFeed, prepare, verify, verifyAll, stage, main } = require("./release-feed")

const zipName = "Handful-universal-mac.zip"
const blockmapName = "Handful-universal-mac.zip.blockmap"
const feedName = "handful-mac.yml"
const bridgeZip = "YarGPT-universal-mac.zip"

const sha = (data) => crypto.createHash("sha512").update(data).digest("base64")

// What electron-builder 26 writes for make app-universal.
const builtFeed = (version, zip, name = `Handful-${version}-universal-mac.zip`) => `version: ${version}
files:
  - url: ${name}
    sha512: ${sha(zip)}
    size: ${zip.length}
path: ${name}
sha512: ${sha(zip)}
releaseDate: '2026-11-09T15:16:32.681Z'
`

function tempDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "handful-feed-"))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  return dir
}

// dist is make app-universal's output for Handful.
function dist(t, version = "1.4.0", zip = Buffer.from("zip bytes")) {
  const dir = tempDir(t)
  fs.writeFileSync(path.join(dir, `Handful-${version}-universal-mac.zip`), zip)
  fs.writeFileSync(path.join(dir, `Handful-${version}-universal-mac.zip.blockmap`), "blockmap")
  fs.writeFileSync(path.join(dir, feedName), builtFeed(version, zip))
  return dir
}

// addBridge writes the bridge release's three files into dir, as gh release
// download leaves them: the feed already names the fixed zip.
function addBridge(dir, zip = Buffer.from("bridge zip bytes"), version = "0.13.0") {
  fs.writeFileSync(path.join(dir, bridgeZip), zip)
  fs.writeFileSync(path.join(dir, `${bridgeZip}.blockmap`), "bridge blockmap")
  fs.writeFileSync(path.join(dir, "latest-mac.yml"), builtFeed(version, zip, bridgeZip))
}

// release is the folder that the release job uploads: both sets.
function release(t) {
  const dir = dist(t, "0.14.0")
  prepare(dir, "0.14.0")
  addBridge(dir)
  return dir
}

test("the sets have fixed names: handful-mac.yml for Handful, latest-mac.yml for the bridge", () => {
  assert.deepEqual(
    Object.fromEntries(Object.entries(sets).map(([k, v]) => [k, [v.zip, v.blockmap, v.feed]])),
    {
      handful: [zipName, blockmapName, feedName],
      bridge: [bridgeZip, `${bridgeZip}.blockmap`, "latest-mac.yml"],
    },
  )
})

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
  fs.appendFileSync(path.join(dir, feedName), "stagingPercentage: 10\n")
  prepare(dir, "1.4.0-rc.1")
  assert.deepEqual(fs.readdirSync(dir).sort(), [blockmapName, zipName, feedName].sort())
  const feed = parseFeed(fs.readFileSync(path.join(dir, feedName), "utf8"))
  const sha512 = sha(zip)
  assert.deepEqual(feed, {
    version: "1.4.0-rc.1",
    files: [{ url: zipName, sha512, size: zip.length }],
    path: zipName,
    sha512,
    releaseDate: "2026-11-09T15:16:32.681Z",
  })
  assert.deepEqual(verify(dir, "handful"), [])
  assert.deepEqual(verify(dir, "handful", "1.4.0-rc.1"), [])
})

test("prepare refuses a feed of another version", (t) => {
  const dir = dist(t, "1.4.0")
  assert.throws(() => prepare(dir, "1.4.1"), /version 1.4.0, not 1.4.1/)
})

test("verify catches a Handful zip that changed after the feed was written", (t) => {
  const dir = release(t)
  assert.deepEqual(verifyAll(dir, ["handful=0.14.0", "bridge=0.13.0"]), [])
  fs.appendFileSync(path.join(dir, zipName), "tampered")
  const problems = verify(dir, "handful")
  assert.equal(problems.length, 3)
  assert.match(problems.join("\n"), /handful-mac.yml has sha512 .*, but Handful-universal-mac.zip's is/)
  assert.match(problems.join("\n"), /handful-mac.yml has files size 9, but Handful-universal-mac.zip has 17 bytes/)
  assert.deepEqual(verify(dir, "bridge"), [])
  assert.throws(() => main(["verify", dir, "handful", "bridge"]), /do not match the zips/)
})

test("verify catches a bridge zip that changed, while Handful's set still passes", (t) => {
  const dir = release(t)
  fs.appendFileSync(path.join(dir, bridgeZip), "x")
  assert.deepEqual(verify(dir, "handful"), [])
  const problems = verify(dir, "bridge")
  assert.equal(problems.length, 3)
  assert.match(problems.join("\n"), /latest-mac.yml has sha512 .*, but YarGPT-universal-mac.zip's is/)
  assert.throws(() => main(["verify", dir, "handful", "bridge"]), /latest-mac.yml has files size 16, but YarGPT-universal-mac.zip has 17 bytes/)
})

test("verify catches feeds that name the other set's zip", (t) => {
  // A Handful zip uploaded as latest-mac.yml would reach the old apps,
  // whose updater rejects it.
  const dir = release(t)
  const handfulFeed = fs.readFileSync(path.join(dir, feedName), "utf8")
  fs.writeFileSync(path.join(dir, "latest-mac.yml"), handfulFeed)
  assert.match(verify(dir, "bridge").join("\n"), /latest-mac.yml has path Handful-universal-mac.zip, not YarGPT-universal-mac.zip/)
  assert.match(verify(dir, "bridge").join("\n"), /latest-mac.yml has files url Handful-universal-mac.zip, not YarGPT-universal-mac.zip/)
  const swapped = release(t)
  fs.writeFileSync(path.join(swapped, feedName), fs.readFileSync(path.join(swapped, "latest-mac.yml")))
  assert.match(verify(swapped, "handful").join("\n"), /handful-mac.yml has path YarGPT-universal-mac.zip, not Handful-universal-mac.zip/)
})

test("verify checks the versions it is given", (t) => {
  const dir = release(t)
  assert.deepEqual(verifyAll(dir, ["handful=0.14.0", "bridge=0.12.2"]), ["latest-mac.yml has version 0.13.0, not 0.12.2"])
  assert.deepEqual(verifyAll(dir, ["handful=0.14.1"]), ["handful-mac.yml has version 0.14.0, not 0.14.1"])
})

test("verify catches missing files, and a feed with no files", (t) => {
  const dir = dist(t)
  assert.match(verify(dir, "handful").join("\n"), /Handful-universal-mac.zip is missing/)
  assert.deepEqual(verify(dir, "bridge"), ["latest-mac.yml is missing"])
  prepare(dir, "1.4.0")
  addBridge(dir)
  fs.rmSync(path.join(dir, `${bridgeZip}.blockmap`))
  assert.deepEqual(verify(dir, "bridge"), [`${bridgeZip}.blockmap is missing`])
  fs.rmSync(path.join(dir, bridgeZip))
  assert.deepEqual(verify(dir, "bridge"), [`${bridgeZip} is missing`])
  fs.rmSync(path.join(dir, blockmapName))
  const feedFile = path.join(dir, feedName)
  fs.writeFileSync(feedFile, fs.readFileSync(feedFile, "utf8").replace(/url: .*/, "url: other.zip").replace(/^path: .*/m, "path: other.zip"))
  assert.deepEqual(verify(dir, "handful"), [
    `${blockmapName} is missing`,
    `handful-mac.yml has path other.zip, not ${zipName}`,
    `handful-mac.yml has files url other.zip, not ${zipName}`,
  ])
  fs.writeFileSync(feedFile, `version: 1.4.0\nfiles:\npath: ${zipName}\n`)
  assert.match(verify(dir, "handful").join("\n"), /handful-mac.yml has no files/)
  assert.throws(() => verify(dir, "latest"), /unknown set latest/)
})

test("stage sets stagingPercentage, and 0 stops the release", (t) => {
  const dir = release(t)
  const feedFile = path.join(dir, feedName)
  stage(feedFile, "0")
  assert.match(fs.readFileSync(feedFile, "utf8"), /^stagingPercentage: 0$/m)
  assert.equal(parseFeed(fs.readFileSync(feedFile, "utf8")).stagingPercentage, 0)
  stage(feedFile, 25)
  assert.equal(parseFeed(fs.readFileSync(feedFile, "utf8")).stagingPercentage, 25)
  assert.deepEqual(verify(dir, "handful"), [], "staging keeps the zip's sha512")
  for (const bad of ["101", "-1", "5.5", "all"]) assert.throws(() => stage(feedFile, bad), /0 to 100/, bad)
  stage(feedFile, "")
  assert.equal(parseFeed(fs.readFileSync(feedFile, "utf8")).stagingPercentage, undefined, "empty releases to everyone")
  main(["stage", feedFile, "0"])
  assert.equal(parseFeed(fs.readFileSync(feedFile, "utf8")).stagingPercentage, 0)
})

test("stage refuses the bridge's latest-mac.yml", (t) => {
  const dir = release(t)
  const bridgeFeed = path.join(dir, "latest-mac.yml")
  const before = fs.readFileSync(bridgeFeed, "utf8")
  assert.throws(() => stage(bridgeFeed, "0"), /stage changes only handful-mac.yml, not latest-mac.yml/)
  assert.equal(fs.readFileSync(bridgeFeed, "utf8"), before)
})

test("main refuses unknown commands", () => {
  assert.throws(() => main(["publish"]), /usage/)
  assert.throws(() => main(["verify"]), /usage/)
  assert.throws(() => main(["verify", "dist"]), /usage/)
})
