import { assert, test } from "vitest"
import {
  bannerText,
  LABELS,
  SECTIONS,
  check,
  diskChanges,
  diskNotice,
  display,
  edited,
  flagHelp,
  flagName,
  isSettingsPath,
  MAX_TURNS,
  parsed,
  pauseLabel,
  pending,
  pendingKeys,
  pendingSections,
  restartRoute,
  runtimeLabel,
  runtimeSource,
  sectionOfPath,
  shortPath,
  unchanged,
  updateKeys,
  type SettingsView,
  type Value,
} from "./settings.ts"

const v = (x: Partial<Value>): Value => ({
  value: "",
  default: "",
  source: "default",
  running: "",
  restart: true,
  ...x,
})
const view = (values: SettingsView["values"], x: Partial<SettingsView> = {}): SettingsView => ({
  file: { path: "/Users/ana/.yargpt/config.json" },
  build: "app",
  restart: "launchd",
  values,
  runtime: [],
  ...x,
})

test("a path picks its section, and only /settings paths are the page", () => {
  assert.equal(sectionOfPath("/settings"), "general")
  assert.equal(sectionOfPath("/settings/"), "general")
  assert.equal(sectionOfPath("/settings/updates"), "updates")
  assert.equal(sectionOfPath("/settings/file/"), "file")
  assert.equal(sectionOfPath("/settings/nope"), "general")
  assert.isTrue(isSettingsPath("/settings"))
  assert.isTrue(isSettingsPath("/settings/runtime"))
  assert.isFalse(isSettingsPath("/settingsx"))
  assert.isFalse(isSettingsPath("/chat/a"))
})

test("a saved restart value waits until the running server uses it, unless a flag sets it", () => {
  assert.isTrue(pending(v({ value: 4, running: 3, source: "file" })))
  assert.isFalse(pending(v({ value: 3, running: 3, source: "file" })))
  assert.isFalse(pending(v({ value: 4, running: 3, source: "flag" })))
  assert.isFalse(pending(v({ value: false, running: true, restart: false, source: "file" })))
})

test("the edited dot marks a value that differs from its default, never a flag's", () => {
  assert.isTrue(edited(v({ value: "opus", default: "", source: "file" })))
  assert.isFalse(edited(v({ value: "", default: "" })))
  assert.isFalse(edited(v({ value: "high", default: "", source: "flag" })))
})

test("pending keys come in the page's order, with their sections and the banner line", () => {
  const vw = view({
    updateCheck: v({ value: false, running: true, source: "file" }),
    maxParallel: v({ value: 4, running: 3, source: "file" }),
    model: v({ value: "opus", running: "", source: "file" }),
    effort: v({ value: "high", running: "", source: "flag" }),
  })
  assert.deepEqual(pendingKeys(vw), ["model", "maxParallel", "updateCheck"])
  assert.deepEqual([...pendingSections(vw)], ["general", "updates"])
  assert.deepEqual(bannerText(["model", "maxParallel"]), {
    lead: "2 changes apply after a restart",
    labels: "Default model, Turns at once",
  })
  assert.deepEqual(bannerText(["claude"]), { lead: "1 change applies after a restart", labels: "Claude Code program" })
  assert.isNull(bannerText([]))
  assert.deepEqual(pendingKeys(view({})), [])
})

test("values read as words", () => {
  assert.equal(display("model", ""), "Default (Claude Code's)")
  assert.equal(display("model", "opus"), "Opus")
  assert.equal(display("model", "claude-opus-5-5"), "claude-opus-5-5")
  assert.equal(display("effort", ""), "Default (model's own)")
  assert.equal(display("effort", "xhigh"), "Extra high (xhigh)")
  assert.equal(display("effort", "odd"), "odd")
  assert.equal(display("updateCheck", true), "On")
  assert.equal(display("updates.autoCheck", false), "Off")
  assert.equal(display("maxParallel", 4), "4")
  assert.equal(display("claude", undefined), "Default")
})

test("a flag-set value names its flag and what config.json says", () => {
  assert.equal(flagName("-effort high"), "-effort")
  assert.equal(flagName(undefined), "")
  assert.equal(
    flagHelp("effort", v({ source: "flag", flag: "-effort high", file: "low" })),
    "config.json says: Low. Remove the flag to use this setting.",
  )
  assert.equal(
    flagHelp("effort", v({ source: "flag", flag: "-effort high" })),
    "config.json says: Default (model's own). Remove the flag to use this setting.",
  )
})

test("typed values are checked with the server's rules", () => {
  const cases: [Parameters<typeof check>[0], string, string | null][] = [
    ["model", "", null],
    ["model", "Opus", null],
    ["model", "claude-opus-5-5", null],
    ["model", "us.anthropic.claude[1m]", null],
    ["model", "inherit", "Choose Default instead."],
    ["model", "claude opus", "A model ID can't contain spaces or start with '-'."],
    ["model", "-x", "A model ID can't contain spaces or start with '-'."],
    ["model", "a*b", "A model ID can use only letters, digits and . _ : / @ [ ] -, up to 200 characters."],
    ["model", "a".repeat(201), "A model ID can use only letters, digits and . _ : / @ [ ] -, up to 200 characters."],
    ["model", "a".repeat(200), null],
    ["effort", "", null],
    ["effort", "Max", null],
    ["effort", "ultra", "Effort must be one of low, medium, high, xhigh, max."],
    ["maxParallel", "1", null],
    ["maxParallel", "20", null],
    ["maxParallel", "0", "Must be 1 or more."],
    ["maxParallel", "", "Must be 1 or more."],
    ["maxParallel", "2.5", "Must be 1 or more."],
    ["maxParallel", "21", "At most 20 here; higher values only in config.json."],
    ["updates.drainMinutes", "5", null],
    ["updates.drainMinutes", "240", null],
    ["updates.drainMinutes", "4", "Must be a whole number from 5 to 240."],
    ["updates.drainMinutes", "241", "Must be a whole number from 5 to 240."],
    ["updates.drainMinutes", "", "Must be a whole number from 5 to 240."],
    ["updates.drainMinutes", "7.5", "Must be a whole number from 5 to 240."],
    ["claude", "/opt/claude", null],
    ["claude", "  ", "Name the program, or reset it to claude."],
    ["claude", "x".repeat(1025), "At most 1024 characters."],
    ["claude", "x".repeat(1024), null],
    ["claude", "a\0b", "The program can't contain a NUL character."],
    ["updateCheck", "anything", null],
  ]
  for (const [key, raw, want] of cases)
    assert.equal(check(key, raw), want, `${key} ${JSON.stringify(raw).slice(0, 30)}`)
  assert.equal(MAX_TURNS, 20)
})

test("typed text becomes the value to save", () => {
  assert.strictEqual(parsed("maxParallel", " 4 "), 4)
  assert.strictEqual(parsed("updates.drainMinutes", "30"), 30)
  assert.strictEqual(parsed("claude", " /opt/claude "), "/opt/claude")
})

test("each build shows its own update settings", () => {
  assert.deepEqual(updateKeys("app"), { keys: ["updates.autoCheck", "updates.drainMinutes"], disabled: false })
  assert.deepEqual(updateKeys("adhoc"), { keys: ["updates.autoCheck", "updates.drainMinutes"], disabled: true })
  assert.deepEqual(updateKeys("source"), { keys: ["updateCheck"], disabled: false })
})

test("a pending change the page didn't make is a change on disk", () => {
  const vw = view({
    maxParallel: v({ value: 4, running: 3, source: "file" }),
    model: v({ value: "opus", running: "", source: "file" }),
  })
  assert.deepEqual(diskChanges(vw, { model: "opus" }), ["maxParallel"])
  assert.deepEqual(diskChanges(vw, {}), ["model", "maxParallel"])
  // A hand edit after the page's own save is a change on disk again.
  assert.deepEqual(diskChanges(vw, { model: "opus", maxParallel: 5 }), ["maxParallel"])
  assert.deepEqual(diskChanges(vw, { model: "opus", maxParallel: 4 }), [])
  assert.deepEqual(diskNotice(vw, ["maxParallel"]), [
    { key: "maxParallel", text: " is 4 in the file, 3 in the running server" },
  ])
  assert.isNull(diskNotice(vw, []))
})

test("a field that still holds what it showed saves nothing when left", () => {
  assert.isTrue(unchanged("3", 3))
  assert.isTrue(unchanged(" 3 ", 3))
  assert.isTrue(unchanged("claude", "claude"))
  assert.isTrue(unchanged("50", 50))
  assert.isFalse(unchanged("4", 3))
  assert.isFalse(unchanged("", 3))
})

test("runtime values have names and sources", () => {
  assert.equal(runtimeLabel("addr"), "Address")
  assert.equal(runtimeLabel("data"), "Data folder")
  assert.equal(runtimeLabel("log"), "Log file")
  assert.equal(runtimeLabel("login-env"), "Login shell environment")
  assert.equal(runtimeLabel("tray"), "Menu-bar icon")
  assert.equal(runtimeLabel("other"), "other")
  assert.equal(runtimeSource({ key: "log", value: "x", source: "app" }), "set by the app (-log)")
  assert.equal(runtimeSource({ key: "addr", value: "x", source: "flag" }), "set by flag (-addr)")
  assert.equal(runtimeSource({ key: "data", value: "x", source: "default" }), "default")
})

test("a home path is shortened to ~", () => {
  assert.equal(shortPath("/Users/ana/.yargpt/config.json"), "~/.yargpt/config.json")
  assert.equal(shortPath("/Users/ana"), "~")
  assert.equal(shortPath("/tmp/x"), "/tmp/x")
  assert.equal(shortPath("/Users/ana2x/y").startsWith("~"), true)
})

test("Restart server… refuses a bad file, sends a source build to the terminal, else restarts or asks", () => {
  assert.equal(restartRoute(view({}, { startError: "bad" }), 0, true), "blocked")
  assert.equal(restartRoute(view({}, { restart: "manual" }), 0, false), "manual")
  assert.equal(restartRoute(view({}, { restart: "manual" }), 0, true), "now")
  assert.equal(restartRoute(view({}), 0, false), "now")
  assert.equal(restartRoute(view({}), 2, false), "ask")
})

test("a drain is named for an update or a restart", () => {
  assert.equal(pauseLabel(true), "Paused for update")
  assert.equal(pauseLabel(false), "Paused for restart")
})

test("each section and setting has its name", () => {
  assert.deepEqual(
    SECTIONS.map((x) => [x.id, x.label]),
    [
      ["general", "General"],
      ["updates", "Updates"],
      ["connectors", "Connectors"],
      ["runtime", "Runtime"],
      ["file", "Settings file"],
    ],
  )
  assert.deepEqual(LABELS, {
    model: "Default model",
    effort: "Default effort",
    maxParallel: "Turns at once",
    claude: "Claude Code program",
    "updates.autoCheck": "Check for updates automatically",
    "updates.drainMinutes": "Wait for running bots up to",
    updateCheck: "Notify me about new releases",
  })
})

test("every alias and effort level has its word, and other keys keep their text", () => {
  assert.deepEqual(
    ["fable", "opus", "sonnet", "haiku"].map((m) => display("model", m)),
    ["Fable", "Opus", "Sonnet", "Haiku"],
  )
  assert.deepEqual(
    ["low", "medium", "high", "xhigh", "max"].map((e) => display("effort", e)),
    ["Low", "Medium", "High", "Extra high (xhigh)", "Max"],
  )
  assert.equal(display("claude", "high"), "high")
  assert.equal(display("claude", "opus"), "opus")
})

test("only paths that start with /settings are the page", () => {
  assert.equal(sectionOfPath("/x/settings/updates"), "general")
  assert.equal(sectionOfPath("/settings/updates/more"), "general")
  assert.isFalse(isSettingsPath("/x/settings"))
  assert.equal(shortPath("/x/Users/ana/y"), "/x/Users/ana/y")
})

test("a flag-set key the file doesn't set shows the default the file would give", () => {
  assert.equal(
    flagHelp("claude", v({ source: "flag", flag: "-claude /x", default: "claude" })),
    "config.json says: claude. Remove the flag to use this setting.",
  )
})

test("only the claude program needs a name; other text keys take anything", () => {
  assert.isNull(check("updateCheck", ""))
  assert.isNull(check("updates.autoCheck", "\0"))
})

test("each setting's pending dot lands on its own section", () => {
  const one = (key: keyof SettingsView["values"]) => [
    ...pendingSections(view({ [key]: v({ value: 2, running: 1, source: "file" }) })),
  ]
  assert.deepEqual(one("claude"), ["general"])
  assert.deepEqual(one("effort"), ["general"])
  assert.deepEqual(one("updates.autoCheck"), ["updates"])
  assert.deepEqual(one("updates.drainMinutes"), ["updates"])
})
