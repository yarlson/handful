const assert = require("node:assert/strict")
const crypto = require("node:crypto")
const fs = require("node:fs")
const os = require("node:os")
const path = require("node:path")
const { test } = require("node:test")
const {
  feedURL,
  releaseBase,
  siteURL,
  maxFeedBytes,
  recheckInterval,
  texts,
  BridgeError,
  busyText,
  waitingText,
  downloadingText,
  limitDialog,
  reasonText,
  view,
  teamIdentifierOf,
  parseHandfulFeed,
  installDir,
  createBridge,
  registerBridgeIPC,
} = require("./bridge")

const team = "ABCDE12345"
const sha512 = (bytes) => crypto.createHash("sha512").update(bytes).digest("base64")

function tempDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "yargpt-bridge-test-"))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  return dir
}

function feedYml({ version = "1.0.0", zip = "Handful-universal-mac.zip", sha, size }) {
  return [
    `version: ${version}`,
    "files:",
    `  - url: ${zip}`,
    `    sha512: ${sha}`,
    `    size: ${size}`,
    `path: ${zip}`,
    `sha512: ${sha}`,
    "releaseDate: '2026-11-11T10:00:00.000Z'",
    "",
  ].join("\n")
}

// writeBundle makes a fake app bundle whose Info.json the fake plutil reads.
function writeBundle(dir, info, marker = "new") {
  fs.mkdirSync(path.join(dir, "Contents"), { recursive: true })
  fs.writeFileSync(path.join(dir, "Contents", "Info.json"), JSON.stringify(info))
  fs.writeFileSync(path.join(dir, "Contents", "marker"), marker)
}

function fakeTimers() {
  const pending = []
  return {
    pending,
    setTimeout(fn, ms) {
      const timer = { fn, ms }
      pending.push(timer)
      return timer
    },
    clearTimeout(timer) {
      const i = pending.indexOf(timer)
      if (i >= 0) pending.splice(i, 1)
    },
  }
}

// harness makes a bridge whose network, server, programs, agent and shell
// are fakes that record their calls in order. YarGPT.app is in a temporary
// folder, so Handful.app goes next to it there.
function harness(t, opts = {}) {
  const root = tempDir(t)
  const appsDir = path.join(root, "Apps")
  const tmpRoot = path.join(root, "tmp")
  fs.mkdirSync(appsDir)
  fs.mkdirSync(tmpRoot)
  const zip = opts.zip ?? Buffer.from("PK fake Handful zip ".repeat(500))
  const feed = { version: "1.0.0", zip: "Handful-universal-mac.zip", sha: sha512(zip), size: zip.length, ...opts.feed }
  const yml = opts.yml ?? feedYml(feed)
  const calls = []
  const asked = []
  const running = opts.running ?? []
  let clock = 1_000_000
  const answers = opts.answers ?? []
  const timers = fakeTimers()
  const changes = []

  async function fetchImpl(url) {
    if (url === feedURL) {
      calls.push("fetch feed")
      if (opts.feedResponse) return opts.feedResponse()
      return new Response(yml)
    }
    calls.push(`fetch ${url}`)
    if (opts.downloadResponse) return opts.downloadResponse()
    if (url === `${releaseBase}${feed.zip}`) return new Response(zip)
    return new Response("", { status: 404 })
  }

  async function exec(file, args) {
    const name = path.basename(file)
    if (name === "ditto" && args[0] === "-x") {
      calls.push("unzip")
      if (opts.unzipFails) throw new Error("ditto: not a zip")
      const layout = opts.layout ?? ["Handful.app"]
      for (const entry of layout) {
        writeBundle(path.join(args[3], entry), { CFBundleIdentifier: opts.bundleID ?? "dev.yarlson.handful", CFBundleShortVersionString: feed.version })
      }
      return { stdout: "", stderr: "" }
    }
    if (name === "ditto") {
      calls.push(`copy ${path.basename(args[1])}`)
      if (opts.copyError) throw opts.copyError
      fs.cpSync(args[0], args[1], { recursive: true })
      return { stdout: "", stderr: "" }
    }
    if (name === "codesign" && args[0] === "--verify") {
      calls.push("codesign verify")
      if (opts.verifyFails) throw Object.assign(new Error("Command failed"), { stderr: "a sealed resource is missing or invalid" })
      return { stdout: "", stderr: "" }
    }
    if (name === "codesign") {
      calls.push("codesign team")
      return { stdout: "", stderr: `Executable=/x\nIdentifier=dev.yarlson.handful\nTeamIdentifier=${opts.team ?? team}\n` }
    }
    if (name === "plutil") {
      const key = args[1]
      calls.push(`plutil ${key}`)
      const info = JSON.parse(fs.readFileSync(path.join(path.dirname(args[5]), "Info.json"), "utf8"))
      if (!(key in info)) throw new Error(`plutil: no ${key}`)
      return { stdout: `${info[key]}\n`, stderr: "" }
    }
    throw new Error(`unexpected exec ${file} ${args.join(" ")}`)
  }

  const bridge = createBridge({
    fetchImpl,
    timeoutSignal: () => undefined,
    api: {
      version: async () => {
        calls.push("version")
        if (opts.silent) return null
        const now = typeof running === "function" ? running(clock) : running
        return { status: 200, body: { version: "0.13.0", running: now } }
      },
      drain: async () => {
        calls.push("drain")
        return { status: 200, body: {} }
      },
      undrain: async () => {
        calls.push("undrain")
        return { status: 200, body: {} }
      },
    },
    exec,
    agent: {
      unregister: async () => {
        calls.push("unregister")
        if (opts.unregisterFails) throw new Error("bootout failed")
      },
      register: async () => calls.push("register"),
      startServer: async () => {
        calls.push("startServer")
        return true
      },
    },
    shell: {
      openPath: async (p) => {
        calls.push(`open ${p}`)
        return opts.openError ?? ""
      },
      openExternal: async (url) => calls.push(`browser ${url}`),
    },
    exit: () => calls.push("exit"),
    askLimit: async (question) => {
      asked.push(question)
      calls.push("ask")
      const answer = answers.shift()
      return typeof answer === "function" ? answer() : answer
    },
    settings: () => ({ drainMinutes: opts.drainMinutes ?? 30 }),
    teamID: opts.teamID === undefined ? team : opts.teamID,
    yargptApp: path.join(appsDir, "YarGPT.app"),
    tmpRoot,
    sleep: async (ms) => {
      clock += ms
    },
    now: () => clock,
    timers,
    log: () => {},
  })
  bridge.onChange((s) => changes.push(s))
  return { bridge, calls, asked, changes, timers, appsDir, tmpRoot, feed, zip, now: () => clock }
}

const visibleSteps = (calls) => calls.filter((c) => !c.startsWith("version") && !c.startsWith("plutil"))

function assertNothingInstalled(h) {
  assert.deepEqual(fs.readdirSync(h.appsDir), [])
}

function assertTempClean(h) {
  assert.deepEqual(fs.readdirSync(h.tmpRoot), [])
}

// found starts the bridge, which finds the feed.
async function found(h) {
  await h.bridge.start()
  assert.equal(h.bridge.snapshot().found, true)
}

test("teamIdentifierOf reads a Developer ID team and refuses an ad-hoc signature", () => {
  assert.equal(teamIdentifierOf("Authority=Developer ID Application: Someone (ABCDE12345)\nTeamIdentifier=ABCDE12345\n"), team)
  assert.equal(teamIdentifierOf("Signature=adhoc\nTeamIdentifier=not set\n"), null)
  assert.equal(teamIdentifierOf(""), null)
  assert.equal(teamIdentifierOf("TeamIdentifier=abc\n"), null)
  assert.equal(teamIdentifierOf(undefined), null)
})

test("parseHandfulFeed reads electron-builder's feed", () => {
  const sha = sha512(Buffer.from("x"))
  assert.deepEqual(parseHandfulFeed(feedYml({ version: "v1.2.0", sha, size: 1234 })), {
    version: "1.2.0",
    path: "Handful-universal-mac.zip",
    sha512: sha,
    size: 1234,
  })
  // Top-level size, quoted scalars, comments and CRLF.
  const text = `# feed\r\nversion: '2.0.0'\r\npath: "Handful.zip"\r\nsha512: ${sha}\r\nsize: 99\r\n`
  assert.deepEqual(parseHandfulFeed(text), { version: "2.0.0", path: "Handful.zip", sha512: sha, size: 99 })
})

test("parseHandfulFeed refuses a hostile or malformed feed", () => {
  const sha = sha512(Buffer.from("x"))
  const good = { sha, size: 10 }
  for (const zip of ["../evil.zip", "a/b.zip", "evil.zip?x=1", "evil.dmg", ".zip", "%2e%2e.zip", "evil zip.zip", "https://evil/x.zip"]) {
    assert.equal(parseHandfulFeed(feedYml({ ...good, zip })), null, zip)
  }
  for (const size of ["0", "-1", "1e3", "2147483649", "99999999999", "", "ten"]) {
    assert.equal(parseHandfulFeed(feedYml({ ...good, size })), null, `size ${size}`)
  }
  assert.ok(parseHandfulFeed(feedYml({ ...good, size: 2147483648 })), "2 GiB is allowed")
  for (const bad of ["abc", sha.slice(0, -2), `${sha}==`, "a".repeat(88)]) {
    assert.equal(parseHandfulFeed(feedYml({ ...good, sha: bad })), null, `sha ${bad}`)
  }
  assert.equal(parseHandfulFeed(feedYml({ ...good, version: "latest" })), null)
  assert.equal(parseHandfulFeed(`${feedYml(good)}version: 9.9.9\n`), null, "a key twice")
  assert.equal(parseHandfulFeed(`${feedYml(good)}  nested: 1\n`), null, "a line out of form")
  assert.equal(parseHandfulFeed(`path: Handful.zip\nsha512: ${sha}\nsize: 10\n`), null, "no version")
  assert.equal(parseHandfulFeed(`version: 1.0.0\nsha512: ${sha}\nsize: 10\n`), null, "no path")
  assert.equal(parseHandfulFeed(`version: 1.0.0\npath: H.zip\nsize: 10\n`), null, "no sha512")
  assert.equal(parseHandfulFeed(`version: 1.0.0\npath: H.zip\nsha512: ${sha}\n`), null, "no size")
  assert.equal(parseHandfulFeed(`${feedYml(good)}#${"x".repeat(maxFeedBytes)}\n`), null, "too big")
  assert.equal(parseHandfulFeed("<html>Not Found</html>"), null)
  assert.equal(parseHandfulFeed(null), null)
})

test("the copy of S1, S2 and S3 is word for word", () => {
  const offer = view({ screen: "offer", running: [] })
  assert.equal(offer.heading, "YarGPT is now Handful")
  assert.deepEqual(offer.lines, [
    'Same app, new name: the old one said "GPT", but it runs Claude Code. Your bots, chats, teams and settings come with you.',
    "YarGPT lets running bots finish, downloads Handful and opens it in its place.",
  ])
  assert.deepEqual(offer.buttons, [
    { id: "move", label: "Move to Handful", primary: true },
    { id: "later", label: "Later", primary: false },
  ])
  const busy = view({ screen: "offer", running: [{ name: "Dev Lead" }, { name: "Core Dev" }] })
  assert.equal(busy.lines[2], "2 bots are working: Dev Lead, Core Dev. The move starts when they finish.")
  assert.equal(busy.buttons[0].label, "Move when they finish")
  assert.equal(busyText([{ name: "Dev Lead" }]), "1 bot is working: Dev Lead. The move starts when they finish.")

  assert.equal(waitingText([{ name: "A" }, { name: "B" }]), "Waiting for 2 bots to finish…")
  assert.equal(waitingText([{ name: "A" }]), "Waiting for 1 bot to finish…")
  assert.equal(downloadingText(42.7), "Downloading Handful… 42%")
  assert.equal(view({ screen: "moving", step: "drain", running: [{ name: "A" }, { name: "B" }] }).status, "Waiting for 2 bots to finish…")
  assert.equal(view({ screen: "moving", step: "download", percent: 42, running: [] }).status, "Downloading Handful… 42%")
  assert.equal(view({ screen: "moving", step: "verify", running: [] }).status, "Checking Handful's signature…")
  assert.equal(view({ screen: "moving", step: "install", running: [] }).status, "Installing Handful…")
  assert.equal(view({ screen: "moving", step: "open", running: [] }).status, "Opening Handful…")
  assert.deepEqual(view({ screen: "moving", step: "verify", running: [] }).buttons, [])

  const failed = view({ screen: "failed", reason: "Couldn't reach GitHub.", running: [] })
  assert.equal(failed.heading, "Couldn't move to Handful")
  assert.deepEqual(failed.lines, ["Couldn't reach GitHub.", "YarGPT keeps running as before. Nothing was moved."])
  assert.deepEqual(failed.buttons, [
    { id: "retry", label: "Try again", primary: true },
    { id: "site", label: "Download from handful.dev", primary: false },
  ])
  assert.equal(reasonText(new BridgeError("network", "x")), "Couldn't reach GitHub.")
  assert.equal(reasonText(new BridgeError("mismatch", "x")), "The download didn't match the release.")
  assert.equal(reasonText(new BridgeError("signature", "x")), "Handful's signature didn't check out.")
  assert.equal(reasonText(new BridgeError("write", "x")), "Couldn't write to /Applications.")
  assert.equal(reasonText(new Error("Couldn't open Handful: gone")), "Couldn't open Handful: gone")
  assert.equal(texts.menu, "Move to Handful…")

  assert.deepEqual(limitDialog(30, [{ name: "Dev Lead" }, { name: "Core Dev" }], 30), {
    message: "Still working after 30 min: Dev Lead, Core Dev. Moving now stops them.",
    buttons: ["Move now", "Wait 30 more min", "Cancel"],
    choices: ["move", "wait", "cancel"],
  })
})

test("installDir is /Applications for an app in it, else YarGPT.app's folder", () => {
  assert.equal(installDir("/Applications/YarGPT.app"), "/Applications")
  assert.equal(installDir("/Applications/Tools/YarGPT.app"), "/Applications")
  assert.equal(installDir("/Users/me/Downloads/YarGPT.app"), "/Users/me/Downloads")
  assert.equal(installDir("/Applications-old/YarGPT.app"), "/Applications-old")
})

test("a missing feed shows nothing and checks again in 6 hours", async (t) => {
  const h = harness(t, { feedResponse: () => new Response("Not Found", { status: 404 }) })
  await h.bridge.start()
  assert.equal(h.bridge.snapshot().found, false)
  assert.equal(h.bridge.menuItem(() => {}), null)
  assert.deepEqual(h.changes, [])
  assert.deepEqual(
    h.timers.pending.map((p) => p.ms),
    [recheckInterval],
  )
  assert.equal(recheckInterval, 6 * 60 * 60 * 1000)
})

test("a feed that cannot be fetched, or is not a feed, shows nothing", async (t) => {
  const responses = [
    () => Promise.reject(new TypeError("fetch failed")),
    () => new Response("oops", { status: 500 }),
    () => new Response("<html>hello</html>"),
    () => new Response(`version: 1.0.0\npath: ../../evil.zip\nsha512: ${sha512(Buffer.from("x"))}\nsize: 1\n`),
    // A body over 64 KiB, without a Content-Length, is cut off.
    () => {
      let pulls = 0
      const body = new ReadableStream({
        pull(controller) {
          pulls++
          if (pulls > 1000) throw new Error("read too far")
          controller.enqueue(new Uint8Array(4096).fill(35))
        },
      })
      return new Response(body)
    },
  ]
  for (const feedResponse of responses) {
    const h = harness(t, { feedResponse })
    await h.bridge.start()
    assert.equal(h.bridge.snapshot().found, false)
    assert.equal(h.bridge.menuItem(() => {}), null)
    assert.deepEqual(h.changes, [])
  }
})

test("an ad-hoc signed copy never looks for the feed", async (t) => {
  const h = harness(t, { teamID: null })
  await h.bridge.start()
  assert.deepEqual(h.calls, [])
  assert.deepEqual(h.timers.pending, [])
})

test("a feed found at a later check shows S1 then", async (t) => {
  let present = false
  const h = harness(t, { feedResponse: () => (present ? new Response(h.yml) : new Response("", { status: 404 })) })
  h.yml = feedYml({ sha: sha512(h.zip), size: h.zip.length, zip: "Handful-universal-mac.zip" })
  await h.bridge.start()
  assert.equal(h.bridge.snapshot().found, false)
  present = true
  await h.timers.pending.shift().fn()
  assert.equal(h.bridge.snapshot().found, true)
  assert.equal(h.bridge.snapshot().visible, true)
  assert.ok(!h.timers.pending.some((p) => p.ms === recheckInterval), "no check after it is found")
})

test("a found feed shows S1 and the menu item, idle and with bots", async (t) => {
  const idle = harness(t)
  await found(idle)
  const s = idle.bridge.snapshot()
  assert.equal(s.visible, true)
  assert.equal(s.view.heading, "YarGPT is now Handful")
  assert.equal(s.view.lines.length, 2)
  assert.equal(s.view.buttons[0].label, "Move to Handful")
  const click = () => {}
  assert.deepEqual(idle.bridge.menuItem(click), { id: "handful", label: "Move to Handful…", click })

  const busy = harness(t, { running: [{ name: "Dev Lead" }, { name: "Core Dev" }] })
  await found(busy)
  const b = busy.bridge.snapshot().view
  assert.equal(b.lines[2], "2 bots are working: Dev Lead, Core Dev. The move starts when they finish.")
  assert.equal(b.buttons[0].label, "Move when they finish")
})

test("Later hides S1 until the next launch; the menu item shows it again", async (t) => {
  const h = harness(t)
  await found(h)
  h.bridge.later()
  assert.equal(h.bridge.snapshot().visible, false)
  assert.ok(h.bridge.menuItem(() => {}))
  assert.deepEqual(h.timers.pending, [], "no polling while hidden")
  await h.bridge.show()
  assert.equal(h.bridge.snapshot().visible, true)
  assert.equal(h.bridge.snapshot().screen, "offer")
})

test("S1 names the bots that start or finish while it shows", async (t) => {
  let running = []
  const h = harness(t, { running: () => running })
  await found(h)
  running = [{ name: "Dev Lead" }]
  await h.timers.pending.shift().fn()
  assert.equal(h.bridge.snapshot().view.lines[2], "1 bot is working: Dev Lead. The move starts when they finish.")
  assert.equal(h.timers.pending.length, 1, "polls again")
})

test("Move to Handful drains, downloads, verifies, installs, unregisters and opens, in that order", async (t) => {
  const h = harness(t)
  await found(h)
  await h.bridge.move()
  const target = path.join(h.appsDir, "Handful.app")
  assert.deepEqual(visibleSteps(h.calls), [
    "fetch feed",
    "drain",
    `fetch ${releaseBase}Handful-universal-mac.zip`,
    "unzip",
    "codesign verify",
    "codesign team",
    "copy .Handful.app.partial",
    "unregister",
    `open ${target}`,
    "exit",
  ])
  assert.deepEqual(fs.readdirSync(h.appsDir), ["Handful.app"])
  assert.equal(fs.readFileSync(path.join(target, "Contents", "marker"), "utf8"), "new")
  assertTempClean(h)
  assert.ok(!h.calls.includes("undrain"))
  const statuses = h.changes.map((c) => c.view.status).filter(Boolean)
  assert.deepEqual([...new Set(statuses.filter((s) => !s.startsWith("Downloading")))], [
    "Checking Handful's signature…",
    "Installing Handful…",
    "Opening Handful…",
  ])
  assert.ok(statuses.includes("Downloading Handful… 0%"))
  assert.ok(statuses.includes("Downloading Handful… 100%"))
})

test("the download reports its progress", async (t) => {
  const zip = Buffer.alloc(1000, 7)
  const h = harness(t, {
    zip,
    downloadResponse: () => {
      const parts = [zip.subarray(0, 420), zip.subarray(420)]
      return new Response(
        new ReadableStream({
          pull(controller) {
            const next = parts.shift()
            if (next) controller.enqueue(new Uint8Array(next))
            else controller.close()
          },
        }),
      )
    },
  })
  await found(h)
  await h.bridge.move()
  const statuses = h.changes.map((c) => c.view.status)
  assert.ok(statuses.includes("Downloading Handful… 42%"), statuses.join(" | "))
  assert.ok(h.calls.includes("exit"))
})

test("with bots working, S2 waits for them and the move starts when they finish", async (t) => {
  const start = 1_000_000
  const h = harness(t, { running: (now) => (now < start + 12_000 ? [{ name: "Dev Lead" }, { name: "Core Dev" }] : []) })
  await found(h)
  await h.bridge.move()
  assert.ok(h.changes.some((c) => c.screen === "moving" && c.view.status === "Waiting for 2 bots to finish…"))
  assert.equal(h.asked.length, 0)
  assert.ok(h.calls.indexOf("drain") < h.calls.findIndex((c) => c.startsWith(`fetch ${releaseBase}`)))
  assert.ok(h.calls.includes("exit"))
  assert.equal(h.now(), start + 15_000, "polls every 5 s")
})

test("at the drain limit the bridge asks and never moves on its own", async (t) => {
  let answer
  const decided = new Promise((resolve) => {
    answer = resolve
  })
  const h = harness(t, { running: [{ name: "Dev Lead" }], drainMinutes: 30, answers: [() => decided] })
  await found(h)
  const moving = h.bridge.move()
  // Let the drain loop reach the question.
  while (h.asked.length === 0) await new Promise((resolve) => setImmediate(resolve))
  for (let i = 0; i < 20; i++) await new Promise((resolve) => setImmediate(resolve))
  assert.deepEqual(h.asked[0].message, "Still working after 30 min: Dev Lead. Moving now stops them.")
  assert.deepEqual(h.asked[0].buttons, ["Move now", "Wait 30 more min", "Cancel"])
  assert.ok(!h.calls.some((c) => c.startsWith(`fetch ${releaseBase}`)), "no download while it asks")
  answer(0) // Move now
  await moving
  assert.ok(h.calls.includes("unregister"))
  assert.ok(h.calls.includes("exit"))
})

test("Wait more asks again later; Cancel releases the drain and goes back to S1", async (t) => {
  const h = harness(t, { running: [{ name: "Dev Lead" }, { name: "Core Dev" }], drainMinutes: 10, answers: [1, 2] })
  await found(h)
  await h.bridge.move()
  assert.equal(h.asked.length, 2)
  assert.equal(h.asked[0].message, "Still working after 10 min: Dev Lead, Core Dev. Moving now stops them.")
  assert.equal(h.asked[1].message, "Still working after 20 min: Dev Lead, Core Dev. Moving now stops them.")
  assert.deepEqual(h.asked[1].buttons, ["Move now", "Wait 10 more min", "Cancel"])
  assert.deepEqual(visibleSteps(h.calls).slice(-4), ["drain", "ask", "ask", "undrain"])
  assert.ok(!h.calls.some((c) => c.startsWith(`fetch ${releaseBase}`)))
  const s = h.bridge.snapshot()
  assert.equal(s.screen, "offer")
  assert.equal(s.view.buttons[0].label, "Move when they finish")
  assertNothingInstalled(h)
})

test("a server that does not answer counts as idle", async (t) => {
  const h = harness(t, { silent: true })
  await found(h)
  await h.bridge.move()
  assert.equal(h.asked.length, 0)
  assert.ok(h.calls.includes("exit"))
})

// failCase runs Move to Handful with opts and checks S3's reason, the
// release of the drain and the clean-up.
async function failCase(t, opts, reason) {
  const h = harness(t, opts)
  await found(h)
  await h.bridge.move()
  const s = h.bridge.snapshot()
  assert.equal(s.screen, "failed")
  assert.equal(s.visible, true)
  assert.equal(s.view.heading, "Couldn't move to Handful")
  assert.deepEqual(s.view.lines, [reason, "YarGPT keeps running as before. Nothing was moved."])
  assert.ok(h.calls.includes("undrain"), "the drain is released")
  assert.ok(!h.calls.includes("exit"))
  assertTempClean(h)
  return h
}

test("a download that fails shows Couldn't reach GitHub. and installs nothing", async (t) => {
  for (const downloadResponse of [
    () => Promise.reject(new TypeError("fetch failed")),
    () => new Response("", { status: 503 }),
    () =>
      new Response(
        new ReadableStream({
          pull(controller) {
            controller.error(new Error("socket hang up"))
          },
        }),
      ),
  ]) {
    const h = await failCase(t, { downloadResponse }, "Couldn't reach GitHub.")
    assertNothingInstalled(h)
    assert.ok(!h.calls.includes("unregister"))
    assert.ok(!h.calls.includes("unzip"))
  }
})

test("a download that does not match the feed shows the mismatch", async (t) => {
  const zip = Buffer.from("the real zip")
  // Wrong sha512.
  let h = await failCase(t, { zip, feed: { sha: sha512(Buffer.from("another zip")) } }, "The download didn't match the release.")
  assertNothingInstalled(h)
  assert.ok(!h.calls.includes("unzip"))
  // Shorter than its size.
  h = await failCase(t, { zip, feed: { size: zip.length + 1 } }, "The download didn't match the release.")
  assertNothingInstalled(h)
})

test("the download stops above the feed's size", async (t) => {
  let pulls = 0
  const h = await failCase(
    t,
    {
      zip: Buffer.alloc(10_000),
      downloadResponse: () =>
        new Response(
          new ReadableStream({
            // A server that sends more than the feed's size; it gives up
            // after 1 MB, so a missing cap fails instead of reading forever.
            pull(controller) {
              pulls++
              if (pulls > 256) controller.error(new Error("the server gave up"))
              else controller.enqueue(new Uint8Array(4096))
            },
          }),
        ),
    },
    "The download didn't match the release.",
  )
  assert.ok(pulls <= 5, `read ${pulls} chunks`)
  assertNothingInstalled(h)
})

test("a zip that is not only Handful.app shows the mismatch", async (t) => {
  for (const opts of [{ layout: ["Handful.app", "evil.app"] }, { layout: ["Other.app"] }, { layout: [] }, { unzipFails: true }]) {
    const h = await failCase(t, opts, "The download didn't match the release.")
    assertNothingInstalled(h)
    assert.ok(!h.calls.includes("codesign verify"))
  }
})

test("a signature that does not check out installs nothing", async (t) => {
  for (const opts of [{ verifyFails: true }, { team: "ZZZZZ99999" }, { team: "not set" }, { bundleID: "dev.evil.handful" }]) {
    const h = await failCase(t, opts, "Handful's signature didn't check out.")
    assertNothingInstalled(h)
    assert.ok(!h.calls.includes("unregister"))
  }
})

test("a folder that cannot be written shows Couldn't write to /Applications.", async (t) => {
  for (const copyError of [
    Object.assign(new Error("Command failed: ditto"), { code: 1, stderr: "ditto: /Apps/.Handful.app.partial: Permission denied" }),
    Object.assign(new Error("EACCES: permission denied"), { code: "EACCES" }),
    Object.assign(new Error("EROFS"), { code: "EROFS" }),
    Object.assign(new Error("ENOSPC"), { code: "ENOSPC" }),
    Object.assign(new Error("EPERM"), { code: "EPERM" }),
  ]) {
    const h = await failCase(t, { copyError }, "Couldn't write to /Applications.")
    assertNothingInstalled(h)
    assert.ok(!h.calls.includes("unregister"))
  }
  // Another install error shows its message.
  await failCase(t, { copyError: new Error("ditto: something odd") }, "ditto: something odd")
})

test("an open that fails registers the server again and starts it", async (t) => {
  const h = await failCase(t, { openError: "The application can't be opened." }, "Couldn't open Handful: The application can't be opened.")
  const steps = visibleSteps(h.calls)
  assert.deepEqual(steps.slice(steps.indexOf("unregister")), [
    "unregister",
    `open ${path.join(h.appsDir, "Handful.app")}`,
    "register",
    "startServer",
    "undrain",
  ])
})

test("an unregister that fails registers the server again too", async (t) => {
  const h = await failCase(t, { unregisterFails: true }, "bootout failed")
  const steps = visibleSteps(h.calls)
  assert.deepEqual(steps.slice(steps.indexOf("unregister")), ["unregister", "register", "startServer", "undrain"])
})

test("a failure before unregister leaves the agent alone", async (t) => {
  const h = await failCase(t, { verifyFails: true }, "Handful's signature didn't check out.")
  assert.ok(!h.calls.includes("register"))
  assert.ok(!h.calls.includes("startServer"))
})

test("Try again runs the steps again and Download from handful.dev opens the site", async (t) => {
  let fail = true
  const h = harness(t, { downloadResponse: () => (fail ? Promise.reject(new TypeError("fetch failed")) : new Response(h.zip)) })
  await found(h)
  await h.bridge.move()
  assert.equal(h.bridge.snapshot().screen, "failed")
  await h.bridge.openSite()
  assert.ok(h.calls.includes(`browser ${siteURL}`))
  assert.equal(siteURL, "https://handful.dev")
  fail = false
  await h.bridge.retry()
  assert.equal(h.calls.filter((c) => c === "drain").length, 2)
  assert.ok(h.calls.includes("exit"))
  assert.deepEqual(fs.readdirSync(h.appsDir), ["Handful.app"])
})

test("a move that runs is not started twice", async (t) => {
  const h = harness(t)
  await found(h)
  await Promise.all([h.bridge.move(), h.bridge.move()])
  assert.equal(h.calls.filter((c) => c === "drain").length, 1)
})

test("install skips a Handful.app of the same or a newer version", async (t) => {
  for (const version of ["1.0.0", "1.2.0"]) {
    const h = harness(t)
    const existing = path.join(h.appsDir, "Handful.app")
    writeBundle(existing, { CFBundleIdentifier: "dev.yarlson.handful", CFBundleShortVersionString: version }, "existing")
    await found(h)
    await h.bridge.move()
    assert.ok(!h.calls.some((c) => c.startsWith("copy")), version)
    assert.equal(fs.readFileSync(path.join(existing, "Contents", "marker"), "utf8"), "existing")
    assert.ok(h.calls.includes(`open ${existing}`))
    assert.ok(h.calls.includes("exit"))
  }
})

test("install replaces an older Handful.app and leaves no partial copy", async (t) => {
  for (const version of ["0.9.0", "garbage"]) {
    const h = harness(t)
    const existing = path.join(h.appsDir, "Handful.app")
    writeBundle(existing, { CFBundleIdentifier: "dev.yarlson.handful", CFBundleShortVersionString: version }, "old")
    await found(h)
    await h.bridge.move()
    assert.equal(fs.readFileSync(path.join(existing, "Contents", "marker"), "utf8"), "new")
    assert.deepEqual(fs.readdirSync(h.appsDir), ["Handful.app"])
  }
})

test("Handful.app goes into the folder of YarGPT.app when it is outside /Applications", async (t) => {
  const h = harness(t)
  await found(h)
  await h.bridge.move()
  assert.ok(fs.existsSync(path.join(h.appsDir, "Handful.app", "Contents", "marker")))
})

test("a failed install keeps the older Handful.app", async (t) => {
  const h = harness(t, { copyError: Object.assign(new Error("EACCES"), { code: "EACCES" }) })
  const existing = path.join(h.appsDir, "Handful.app")
  writeBundle(existing, { CFBundleIdentifier: "dev.yarlson.handful", CFBundleShortVersionString: "0.9.0" }, "old")
  await found(h)
  await h.bridge.move()
  assert.equal(fs.readFileSync(path.join(existing, "Contents", "marker"), "utf8"), "old")
  assert.deepEqual(fs.readdirSync(h.appsDir), ["Handful.app"])
})

test("closing S2 hides the window and the move goes on", async (t) => {
  const h = harness(t, { running: (now) => (now < 1_010_000 ? [{ name: "A" }] : []) })
  await found(h)
  const moving = h.bridge.move()
  assert.equal(h.bridge.isMoving(), true)
  h.bridge.later()
  await moving
  assert.ok(h.calls.includes("exit"))
})

test("the IPC answers only the main frame of the bridge window", async () => {
  const handlers = new Map()
  const ipcMain = { handle: (channel, fn) => handlers.set(channel, fn) }
  const done = []
  const bridge = {
    snapshot: () => ({ view: { heading: "h" } }),
    move: async () => done.push("move"),
    later: async () => done.push("later"),
    retry: async () => done.push("retry"),
    openSite: async () => done.push("openSite"),
  }
  const own = { id: 1 }
  let current = own
  registerBridgeIPC(ipcMain, bridge, () => current)
  assert.deepEqual([...handlers.keys()].sort(), ["bridge:get", "bridge:later", "bridge:move", "bridge:openSite", "bridge:retry"])
  const main = { parent: null }
  assert.deepEqual(handlers.get("bridge:get")({ sender: own, senderFrame: main }), { heading: "h" })
  await handlers.get("bridge:move")({ sender: own, senderFrame: main })
  assert.deepEqual(done, ["move"])
  await assert.rejects(handlers.get("bridge:move")({ sender: { id: 2 }, senderFrame: main }), /not allowed/)
  await assert.rejects(handlers.get("bridge:later")({ sender: own, senderFrame: { parent: main } }), /not allowed/)
  await assert.rejects(handlers.get("bridge:retry")({ sender: own, senderFrame: null }), /not allowed/)
  assert.throws(() => handlers.get("bridge:get")({ sender: { id: 2 }, senderFrame: main }), /not allowed/)
  current = null
  await assert.rejects(handlers.get("bridge:openSite")({ sender: own, senderFrame: main }), /not allowed/)
  assert.deepEqual(done, ["move"])
})
