// The Settings page: what it shows for each value of GET /api/settings. The server owns
// config.json, its checks and the restart; this file only words and arranges them.

export type Key =
  | "model"
  | "effort"
  | "maxParallel"
  | "claude"
  | "updates.autoCheck"
  | "updates.drainMinutes"
  | "updateCheck"

export type Plain = string | number | boolean

export type Value = {
  value: Plain
  default: Plain
  source: "file" | "default" | "flag"
  flag?: string // the flag as given, "-effort high"
  file?: Plain // the file's value of a key a flag sets
  running: Plain
  restart: boolean
  error?: string // why the server wouldn't start with this value
}

export type RuntimeValue = { key: string; value: Plain; source: "default" | "flag" | "app" }

export type SettingsView = {
  file: { path: string; error?: string }
  build: "app" | "adhoc" | "source"
  restart: "launchd" | "manual"
  startError?: string
  values: Partial<Record<Key, Value>>
  runtime: RuntimeValue[]
}

export type Section = "general" | "updates" | "connectors" | "runtime" | "file"

export const SECTIONS: { id: Section; label: string }[] = [
  { id: "general", label: "General" },
  { id: "updates", label: "Updates" },
  { id: "connectors", label: "Connectors" },
  { id: "runtime", label: "Runtime" },
  { id: "file", label: "Settings file" },
]

export const LABELS: Record<Key, string> = {
  model: "Default model",
  effort: "Default effort",
  maxParallel: "Turns at once",
  claude: "Claude Code program",
  "updates.autoCheck": "Check for updates automatically",
  "updates.drainMinutes": "Wait for running bots up to",
  updateCheck: "Notify me about new releases",
}

const SECTION_OF: Record<Key, Section> = {
  model: "general",
  effort: "general",
  maxParallel: "general",
  claude: "general",
  "updates.autoCheck": "updates",
  "updates.drainMinutes": "updates",
  updateCheck: "updates",
}

export const MAX_TURNS = 20

// sectionOfPath reads the section from /settings/<section>; /settings and an unknown
// section open General.
export function sectionOfPath(pathname: string): Section {
  const id = /^\/settings\/([^/]+)\/?$/.exec(pathname)?.[1]
  return SECTIONS.find((s) => s.id === id)?.id ?? "general"
}

// isSettingsPath tells whether a path is the Settings page.
export function isSettingsPath(pathname: string): boolean {
  return /^\/settings(\/|$)/.test(pathname)
}

// pending tells whether a saved value waits for a restart: it needs one, and the running
// server started with something else. A value a flag sets never waits, since the flag wins.
export function pending(v: Value): boolean {
  return v.restart && v.source !== "flag" && v.value !== v.running
}

// edited tells whether a value differs from its default, for the dot next to its label.
export function edited(v: Value): boolean {
  return v.source !== "flag" && v.value !== v.default
}

// pendingKeys lists the keys that wait for a restart, in the page's order.
export function pendingKeys(view: SettingsView): Key[] {
  return (Object.keys(LABELS) as Key[]).filter((k) => {
    const v = view.values[k]
    return v !== undefined && pending(v)
  })
}

// pendingSections are the sections with a value that waits for a restart, for their dot.
export function pendingSections(view: SettingsView): Set<Section> {
  return new Set(pendingKeys(view).map((k) => SECTION_OF[k]))
}

// bannerText is the restart banner's line, or null when nothing waits.
export function bannerText(keys: Key[]): { lead: string; labels: string } | null {
  if (keys.length === 0) return null
  const lead = keys.length === 1 ? "1 change applies after a restart" : `${keys.length} changes apply after a restart`
  return { lead, labels: keys.map((k) => LABELS[k]).join(", ") }
}

const MODEL_NAMES: Record<string, string> = { fable: "Fable", opus: "Opus", sonnet: "Sonnet", haiku: "Haiku" }
const EFFORT_NAMES: Record<string, string> = {
  low: "Low",
  medium: "Medium",
  high: "High",
  xhigh: "Extra high (xhigh)",
  max: "Max",
}

// display words a value for the page: "Default (Claude Code's)", "Opus", "On", "4".
export function display(key: Key, value: Plain | undefined): string {
  if (typeof value === "boolean") return value ? "On" : "Off"
  if (value === undefined || value === "") {
    if (key === "model") return "Default (Claude Code's)"
    if (key === "effort") return "Default (model's own)"
    return "Default"
  }
  const s = String(value)
  if (key === "model") return MODEL_NAMES[s] ?? s
  if (key === "effort") return EFFORT_NAMES[s] ?? s
  return s
}

// flagName is the flag in "-effort high": the chip says "Set by -effort".
export function flagName(flag: string | undefined): string {
  return (flag ?? "").split(" ")[0]
}

// flagHelp is the line under a value a flag sets: which flag, and what config.json says.
export function flagHelp(key: Key, v: Value): string {
  const file = v.file === undefined ? display(key, v.default) : display(key, v.file)
  return `config.json says: ${file}. Remove the flag to use this setting.`
}

const EFFORTS = Object.keys(EFFORT_NAMES)
const MODEL_ID = /^[A-Za-z0-9._:/@[\]-]{1,200}$/

// check validates what the user typed, with the server's rules, and returns the message
// to show, or null when it's fine. The server checks again before it writes.
export function check(key: Key, raw: string): string | null {
  const text = raw.trim()
  if (key === "model") {
    if (text === "") return null
    if (text.toLowerCase() === "inherit") return "Choose Default instead."
    if (/\s/.test(text) || text.startsWith("-")) return "A model ID can't contain spaces or start with '-'."
    if (!MODEL_ID.test(text))
      return "A model ID can use only letters, digits and . _ : / @ [ ] -, up to 200 characters."
    return null
  }
  if (key === "effort") {
    return text === "" || EFFORTS.includes(text.toLowerCase())
      ? null
      : "Effort must be one of low, medium, high, xhigh, max."
  }
  if (key === "maxParallel") {
    const n = Number(text)
    // Blank reads as 0.
    if (!Number.isInteger(n) || n < 1) return "Must be 1 or more."
    if (n > MAX_TURNS) return `At most ${MAX_TURNS} here; higher values only in config.json.`
    return null
  }
  if (key === "updates.drainMinutes") {
    const n = Number(text)
    return Number.isInteger(n) && n >= 5 && n <= 240 ? null : "Must be a whole number from 5 to 240."
  }
  if (key !== "claude") return null
  if (text === "") return "Name the program, or reset it to claude."
  if (raw.length > 1024) return "At most 1024 characters."
  if (raw.includes("\0")) return "The program can't contain a NUL character."
  return null
}

// parsed turns typed text into the value to save for a key.
export function parsed(key: Key, raw: string): Plain {
  if (key === "maxParallel" || key === "updates.drainMinutes") return Number(raw)
  return raw.trim()
}

// updateKeys says which update settings a build shows: the app's two (disabled on an
// ad-hoc build), or only the source build's notice switch.
export function updateKeys(build: SettingsView["build"]): { keys: Key[]; disabled: boolean } {
  if (build === "source") return { keys: ["updateCheck"], disabled: false }
  return { keys: ["updates.autoCheck", "updates.drainMinutes"], disabled: build === "adhoc" }
}

// SavedValues are the values this page saved in this session, by key.
export type SavedValues = Partial<Record<Key, Plain>>

// diskChanges are pending keys whose value isn't one the page saved: config.json changed
// by hand after the server started. A pending key always has its value.
export function diskChanges(view: SettingsView, saved: SavedValues): Key[] {
  return pendingKeys(view).filter((k) => !(k in saved) || saved[k] !== (view.values[k] as Value).value)
}

// unchanged tells whether a field still holds the value it showed, so leaving it saves
// nothing.
export function unchanged(text: string, value: Plain): boolean {
  return text.trim() === String(value)
}

// diskNotice words the changed-on-disk notice, or null when there's none.
export function diskNotice(view: SettingsView, keys: Key[]): { key: Key; text: string }[] | null {
  if (keys.length === 0) return null
  return keys.map((k) => {
    const v = view.values[k] as Value
    return { key: k, text: ` is ${display(k, v.value)} in the file, ${display(k, v.running)} in the running server` }
  })
}

const RUNTIME_LABELS: Record<string, string> = {
  addr: "Address",
  data: "Data folder",
  log: "Log file",
  "login-env": "Login shell environment",
  tray: "Menu-bar icon",
}

// runtimeLabel names a runtime value.
export function runtimeLabel(key: string): string {
  return RUNTIME_LABELS[key] ?? key
}

// runtimeSource says where a runtime value comes from.
export function runtimeSource(r: RuntimeValue): string {
  if (r.source === "app") return `set by the app (-${r.key})`
  if (r.source === "flag") return `set by flag (-${r.key})`
  return "default"
}

// shortPath shows a path under the home folder as ~/…
export function shortPath(path: string): string {
  return path.replace(/^\/Users\/[^/]+(?=\/|$)/, "~")
}

export type RestartRoute = "manual" | "blocked" | "now" | "ask"

// restartRoute decides what Restart server… does: tell the user to restart a server not
// under launchd themselves, refuse while the server wouldn't start, restart at once when
// nothing runs, or ask first while bots work. The desktop app restarts through its bridge,
// so it doesn't need launchd.
export function restartRoute(view: SettingsView, running: number, hasBridge: boolean): RestartRoute {
  if (view.startError) return "blocked"
  if (view.restart === "manual" && !hasBridge) return "manual"
  return running > 0 ? "ask" : "now"
}

// pauseLabel names a drain: an update's, or a settings restart's.
export function pauseLabel(forUpdate: boolean): string {
  return forUpdate ? "Paused for update" : "Paused for restart"
}
