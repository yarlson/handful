import { useEffect, useState, type ReactNode } from "react"
import { ArrowLeft } from "lucide-react"
import { api } from "@/lib/api"
import {
  bannerText,
  check,
  diskChanges,
  diskNotice,
  display,
  drainLimit,
  minutesUntil,
  edited,
  flagHelp,
  flagName,
  LABELS,
  parsed,
  pending,
  pendingKeys,
  pendingSections,
  restartRoute,
  runtimeLabel,
  runtimeSource,
  SECTIONS,
  shortPath,
  unchanged,
  updateKeys,
  type Key,
  type Plain,
  type SavedValues,
  type Section,
  type SettingsView,
  type Value,
} from "@/lib/settings"
import { doing, type RunningBot } from "@/lib/updates"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"

// The desktop app's preload exposes these; a browser has none.
type SettingsBridge = {
  restartServer: (when: "now" | "idle") => Promise<{ ok: true } | { ok: false; error: string }>
  openFile: () => void
  revealFile: () => void
  revealLog: () => void
  chooseClaude: () => Promise<string | null>
}

export function settingsBridge(): SettingsBridge | undefined {
  return (window as unknown as { handfulSettings?: SettingsBridge }).handfulSettings
}

type UpdaterCheck = { check: () => void }

function updaterBridge(): UpdaterCheck | undefined {
  return (window as unknown as { handfulUpdater?: UpdaterCheck }).handfulUpdater
}

// openSettingsPage goes to a section of the Settings page.
export function openSettingsPage(section: Section = "general") {
  history.pushState(null, "", `/settings/${section}`)
  window.dispatchEvent(new PopStateEvent("popstate"))
}

const selectClass =
  "border-input h-9 w-full rounded-md border bg-transparent px-2 text-sm disabled:opacity-60 aria-disabled:cursor-not-allowed aria-disabled:opacity-60"

// lockProps disables a control. A value a flag sets stays in the tab order instead, marked
// aria-disabled, so a screen reader still reaches the line that says why; its onChange ignores
// input.
function lockProps(locked: boolean, v: Value, describedBy: string) {
  if (locked) return { disabled: true, "aria-describedby": describedBy }
  if (v.source !== "flag") return { "aria-describedby": describedBy }
  return {
    "aria-disabled": true,
    "aria-describedby": `${describedBy} ${describedBy.split(" ")[0].replace(/-help$/, "")}-flag`,
  }
}

// Row lays out one setting: its label, chips and help on the left, its control on the right.
function Row({
  id,
  label,
  chips,
  help,
  children,
  labels = true,
}: {
  id: string
  label: string
  chips?: ReactNode
  help: string
  children: ReactNode
  // labels is false when the control is a button, which names itself.
  labels?: boolean
}) {
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_16rem] gap-4 border-b py-4 last:border-b-0 max-sm:grid-cols-1">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-1.5">
          {labels ? (
            <label htmlFor={id} className="text-sm font-medium">
              {label}
            </label>
          ) : (
            <span className="text-sm font-medium">{label}</span>
          )}
          {chips}
        </div>
        <p id={`${id}-help`} className="text-muted-foreground mt-1 text-xs">
          {help}
        </p>
      </div>
      <div className="flex min-w-0 flex-col gap-1">{children}</div>
    </div>
  )
}

// Chips are the markers next to a label: the edited dot, Restart required, Set by -flag.
export function Chips({ v }: { v: Value }) {
  return (
    <>
      {edited(v) && (
        <>
          <span className="size-1.5 rounded-full bg-blue-600 dark:bg-blue-400" aria-hidden />
          <span className="sr-only">changed from default</span>
        </>
      )}
      {pending(v) && (
        <span className="rounded-full border border-amber-500/40 bg-amber-50 px-2 text-xs text-amber-800 dark:bg-amber-950 dark:text-amber-300">
          Restart required
        </span>
      )}
      {v.source === "flag" && <Badge variant="secondary">Set by {flagName(v.flag)}</Badge>}
    </>
  )
}

type Save = (key: Key, value: Plain | null) => Promise<string | null>

// Under is what goes below a control: the error, the running value, Saved, Reset.
function Under({
  id,
  k,
  v,
  error,
  saved,
  onReset,
  locked,
}: {
  id: string
  k: Key
  v: Value
  error: string | null
  saved: boolean
  onReset: () => void
  locked: boolean
}) {
  const err = error ?? v.error ?? null
  return (
    <>
      {err && (
        <p id={`${id}-error`} className="text-destructive text-xs">
          {err}
        </p>
      )}
      {v.source === "flag" && (
        <p id={`${id}-flag`} className="text-muted-foreground text-xs">
          Set by the <code className="bg-muted rounded px-1 font-mono">{v.flag}</code> flag for this run.{" "}
          {flagHelp(k, v)}
        </p>
      )}
      {pending(v) && <p className="text-muted-foreground text-xs">Running server uses: {display(k, v.running)}</p>}
      <div className="flex items-center gap-3 text-xs">
        {edited(v) && !locked && (
          <button type="button" className="text-muted-foreground underline" onClick={onReset}>
            Reset to default{typeof v.default === "number" ? ` (${v.default})` : ""}
          </button>
        )}
        <output className="text-muted-foreground" aria-live="polite">
          {saved ? "Saved" : ""}
        </output>
      </div>
    </>
  )
}

// useField keeps one field's input, error and Saved flash.
function useField(k: Key, save: Save) {
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  useEffect(() => {
    if (!saved) return
    const t = setTimeout(() => setSaved(false), 2000)
    return () => clearTimeout(t)
  }, [saved])
  const commit = async (value: Plain | null) => {
    const err = await save(k, value)
    setError(err)
    setSaved(err === null)
  }
  return { error, setError, saved, commit }
}

// TextField saves on blur or Enter, after checking what was typed.
export function TextField({
  k,
  v,
  save,
  locked,
  mono,
  suffix,
  after,
}: {
  k: Key
  v: Value
  save: Save
  locked: boolean
  mono?: boolean
  suffix?: string
  after?: ReactNode
}) {
  const id = `setting-${k.replace(".", "-")}`
  const [text, setText] = useState(String(v.value))
  // A saved or reloaded value replaces what was typed.
  const [shown, setShown] = useState(v.value)
  if (shown !== v.value) {
    setShown(v.value)
    setText(String(v.value))
  }
  const f = useField(k, save)
  const disabled = locked || v.source === "flag"
  const done = () => {
    if (unchanged(text, v.value)) {
      f.setError(null)
      return
    }
    const err = check(k, text)
    f.setError(err)
    if (err) return
    void f.commit(parsed(k, text))
  }
  const err = f.error ?? v.error
  return (
    <Row id={id} label={LABELS[k]} chips={<Chips v={v} />} help={helpOf(k)}>
      <div className="flex items-center gap-2">
        <Input
          id={id}
          className={cn("min-w-0 aria-disabled:cursor-not-allowed aria-disabled:opacity-60", mono && "font-mono")}
          readOnly={v.source === "flag"}
          inputMode={typeof v.default === "number" ? "numeric" : undefined}
          value={text}
          {...lockProps(locked, v, `${id}-help${err ? ` ${id}-error` : ""}`)}
          aria-invalid={err ? true : undefined}
          onChange={(e) => {
            if (v.source === "flag") return
            setText(e.target.value)
            if (f.error) f.setError(check(k, e.target.value))
          }}
          onBlur={done}
          onKeyDown={(e) => {
            if (e.key === "Enter") done()
          }}
        />
        {after}
      </div>
      {suffix && <p className="text-muted-foreground text-xs">{suffix}</p>}
      <Under
        id={id}
        k={k}
        v={v}
        error={f.error}
        saved={f.saved}
        locked={disabled}
        onReset={() => void f.commit(null)}
      />
    </Row>
  )
}

const MODELS: [string, string][] = [
  ["", "Default (Claude Code's)"],
  ["fable", "Fable"],
  ["opus", "Opus"],
  ["sonnet", "Sonnet"],
  ["haiku", "Haiku"],
]
const EFFORTS: [string, string][] = [
  ["", "Default (model's own)"],
  ["low", "Low"],
  ["medium", "Medium"],
  ["high", "High"],
  ["xhigh", "Extra high (xhigh)"],
  ["max", "Max"],
]
const CUSTOM = "\u0000custom"

// SelectField saves on change. The model select can switch to a custom model ID.
export function SelectField({ k, v, save, locked }: { k: "model" | "effort"; v: Value; save: Save; locked: boolean }) {
  const id = `setting-${k}`
  const options = k === "model" ? MODELS : EFFORTS
  const known = options.some(([o]) => o === v.value)
  const [custom, setCustom] = useState(!known)
  const [text, setText] = useState(known ? "" : String(v.value))
  // A saved or reloaded value decides again whether the custom field shows.
  const [shown, setShown] = useState(v.value)
  if (shown !== v.value) {
    setShown(v.value)
    setCustom(!known)
    if (!known) setText(String(v.value))
  }
  const f = useField(k, save)
  const disabled = locked || v.source === "flag"
  const err = f.error ?? v.error
  const saveCustom = () => {
    if (unchanged(text, v.value)) {
      f.setError(null)
      return
    }
    const e = check(k, text)
    f.setError(e)
    if (!e && text.trim()) void f.commit(text.trim())
  }
  return (
    <Row id={id} label={LABELS[k]} chips={<Chips v={v} />} help={helpOf(k)}>
      <select
        id={id}
        className={selectClass}
        value={custom ? CUSTOM : String(v.value)}
        {...lockProps(locked, v, `${id}-help${err ? ` ${id}-error` : ""}`)}
        aria-invalid={err && !custom ? true : undefined}
        onChange={(e) => {
          if (v.source === "flag") return
          const next = e.target.value
          if (next === CUSTOM) {
            setCustom(true)
            return
          }
          setCustom(false)
          f.setError(null)
          void f.commit(next === "" ? null : next)
        }}
      >
        {options.map(([value, label]) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
        {k === "model" && <option value={CUSTOM}>Custom model ID…</option>}
      </select>
      {custom && (
        <Input
          aria-label="Custom model ID"
          className="font-mono"
          placeholder="claude-opus-5-5"
          value={text}
          disabled={disabled}
          aria-invalid={err ? true : undefined}
          aria-describedby={err ? `${id}-error` : undefined}
          onChange={(e) => setText(e.target.value)}
          onBlur={saveCustom}
          onKeyDown={(e) => {
            if (e.key === "Enter") saveCustom()
          }}
        />
      )}
      <Under
        id={id}
        k={k}
        v={v}
        error={f.error}
        saved={f.saved}
        locked={disabled}
        onReset={() => void f.commit(null)}
      />
    </Row>
  )
}

// SwitchField saves on change.
export function SwitchField({ k, v, save, locked }: { k: Key; v: Value; save: Save; locked: boolean }) {
  const id = `setting-${k.replace(".", "-")}`
  const f = useField(k, save)
  return (
    <Row id={id} label={LABELS[k]} chips={<Chips v={v} />} help={helpOf(k)}>
      <Switch
        id={id}
        className="self-end"
        checked={v.value === true}
        disabled={locked}
        aria-describedby={`${id}-help`}
        onCheckedChange={(on) => void f.commit(on)}
      />
      <Under id={id} k={k} v={v} error={f.error} saved={f.saved} locked={locked} onReset={() => void f.commit(null)} />
    </Row>
  )
}

const HELP: Record<Key, string> = {
  model: "Used by bots and teams that don't pick their own. Aliases always mean the latest model of that family.",
  effort: "How hard bots think. The default is each model's own level.",
  maxParallel:
    "How many bot turns can run at the same time, across all bots. More turns use more of your Claude plan at once.",
  claude: "The claude command Handful runs for every turn. Change it only if Claude Code isn't on your PATH.",
  "updates.autoCheck":
    "Once a day. Each check sends one request to GitHub and nothing else. Applies at the next check, within an hour.",
  "updates.drainMinutes":
    "Before an update restarts Handful, it lets running turns finish for this long. Then it asks you. Applies at the next update.",
  updateCheck: "Once a day, the server checks GitHub and shows a notice. It never updates itself.",
}

function helpOf(k: Key): string {
  return HELP[k]
}

// RestartBanner says which saved changes wait for a restart, and offers it.
export function RestartBanner({ view, onRestart }: { view: SettingsView; onRestart: () => void }) {
  const text = bannerText(pendingKeys(view))
  if (!text) return null
  const manual = view.restart === "manual" && !settingsBridge()
  return (
    <output className="mb-6 flex flex-wrap items-center gap-3 rounded-lg border border-amber-500/40 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-300">
      <p className="min-w-0 flex-1">
        <strong className="font-semibold">{text.lead}</strong>: {text.labels}.
        {view.startError && (
          <span className="mt-1 block">
            Handful wouldn't start with the file as it is: {view.startError}. Fix it before restarting.
          </span>
        )}
        {manual && !view.startError && (
          <span className="mt-1 block">
            Restart the server to apply: stop it and start <code className="font-mono">./bin/handful</code> again.
          </span>
        )}
      </p>
      {!manual && (
        <Button
          id="settings-restart"
          size="sm"
          variant="outline"
          className="border-amber-800 text-amber-800 dark:border-amber-300 dark:text-amber-300"
          disabled={Boolean(view.startError)}
          onClick={onRestart}
        >
          Restart server…
        </Button>
      )}
    </output>
  )
}

// RestartDialog asks before a restart stops the turns of working bots.
function RestartDialog({
  running,
  now,
  limit,
  open,
  onOpenChange,
  onRestart,
}: {
  running: RunningBot[]
  now: number
  limit: number
  open: boolean
  onOpenChange: (v: boolean) => void
  onRestart: (when: "now" | "idle") => void
}) {
  const n = running.length
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        onCloseAutoFocus={(e) => {
          // The dialog has no trigger of its own; the keyboard goes back to Restart server….
          const button = document.getElementById("settings-restart")
          if (!button) return
          e.preventDefault()
          button.focus()
        }}
      >
        <DialogHeader>
          <DialogTitle>Restart the server?</DialogTitle>
          <DialogDescription className="text-foreground">
            {n === 1 ? "1 bot is" : `${n} bots are`} working. A restart now stops their turns.
          </DialogDescription>
        </DialogHeader>
        <ul className="list-disc pl-6 text-sm">
          {running.map((r) => (
            <li key={r.botId}>
              {r.name} — {doing(r, now).replace(" · ", ", ")}
            </li>
          ))}
        </ul>
        <p className="text-muted-foreground text-xs">
          New turns wait while Handful waits for them, up to {limit} minutes. Then it asks you again.
        </p>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="outline" onClick={() => onRestart("now")}>
            Restart now
          </Button>
          <Button onClick={() => onRestart("idle")}>Restart when they finish</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// useRestart runs a restart through the desktop app or the server, and reloads the page when
// the server is back. Whether an idle restart waits comes from the server (restartWait), so
// it shows again after the page is left or reloaded. onChange reloads the settings.
function useRestart(waiting: boolean, onChange: () => void) {
  const [local, setState] = useState<"idle" | "now" | "error">("idle")
  const [error, setError] = useState<string | null>(null)
  const state = local === "now" ? "now" : waiting ? "draining" : local
  useEffect(() => {
    if (state !== "now" && state !== "draining") return
    let down = false
    let live = true
    const t = setInterval(() => {
      api.version().then(
        () => {
          if (live && down) location.reload()
        },
        () => {
          down = true
        },
      )
    }, 1000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [state])
  const restart = async (when: "now" | "idle") => {
    setError(null)
    try {
      const bridge = settingsBridge()
      if (bridge) {
        // The app's "now" resolves only once the new server answers, so the page never sees
        // it go down; show Restarting… while it waits, and reload as soon as it's back.
        if (when === "now") setState("now")
        const r = await bridge.restartServer(when)
        if (!r.ok) throw new Error(r.error)
        if (when === "now") {
          location.reload()
          return
        }
      } else {
        await api.restart(when)
      }
      if (when === "now") setState("now")
      else onChange()
    } catch (e) {
      setError((e as Error).message)
      setState("error")
    }
  }
  const cancel = async () => {
    try {
      await api.cancelRestart()
      onChange()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return { state, error, restart, cancel }
}

const SAVED_KEY = "handful.settings.saved"

// loadSaved reads the values this page saved in this browser session, which survive leaving
// the page and reloading it.
function loadSaved(): SavedValues {
  try {
    return JSON.parse(sessionStorage.getItem(SAVED_KEY) ?? "{}") as SavedValues
  } catch {
    // Unreadable storage counts as nothing saved.
    return {}
  }
}

function storeSaved(saved: SavedValues): SavedValues {
  try {
    sessionStorage.setItem(SAVED_KEY, JSON.stringify(saved))
  } catch {
    // Storage that refuses the write keeps the values for this page view only.
  }
  return saved
}

// SettingsPage is the whole Settings page: its sections, and saving each value as it changes.
export function SettingsPage({
  section,
  onSection,
  onBack,
  onManageConnectors,
  restartRev = 0,
}: {
  // restartRev changes with each restart event from the server; the page reloads on it.
  restartRev?: number
  section: Section
  onSection: (s: Section) => void
  onBack: () => void
  onManageConnectors: () => void
}) {
  const [view, setView] = useState<SettingsView | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [version, setVersion] = useState<string | null>(null)
  const [connectors, setConnectors] = useState<number | null>(null)
  const [running, setRunning] = useState<{ bots: RunningBot[]; at: number }>({ bots: [], at: 0 })
  const [asking, setAsking] = useState(false)
  const [resetting, setResetting] = useState(false)
  const [resetError, setResetError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  // The keys this page saved: a pending key it didn't save changed on disk.
  const [saved, setSaved] = useState(loadSaved)
  const load = () =>
    api.settings().then(
      (v) => {
        setView(v)
        setLoadError(null)
      },
      (e: Error) => setLoadError(e.message),
    )
  const restart = useRestart(Boolean(view?.restartWait), () => void load())
  // now ticks while a restart waits, for its minutes left.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!view?.restartWait) return
    const tick = () => setNow(Date.now())
    const first = setTimeout(tick, 0)
    const t = setInterval(tick, 15_000)
    return () => {
      clearTimeout(first)
      clearInterval(t)
    }
  }, [view?.restartWait])
  // A restart event (waiting, cancelled, expired) reloads the settings that report it.
  useEffect(() => {
    if (restartRev > 0) void load()
    // load reads only the API; restartRev is the trigger.
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [restartRev])
  useEffect(() => {
    void load()
    api.version().then(
      (v) => setVersion(v.version),
      () => setVersion(null),
    )
    api.connectors().then(
      (c) => setConnectors(c.length),
      () => setConnectors(null),
    )
  }, [])
  const save: Save = async (key, value) => {
    try {
      const next = await api.patchSettings({ [key]: value })
      setView(next)
      setSaved((s) => storeSaved({ ...s, [key]: next.values[key]?.value }))
      return null
    } catch (e) {
      return (e as Error).message
    }
  }
  const askRestart = async () => {
    if (!view) return
    const r = await api.version().catch(() => null)
    const now = r?.running ?? []
    const route = restartRoute(view, now.length, Boolean(settingsBridge()))
    if (route === "ask") {
      setRunning({ bots: now, at: Date.now() })
      setAsking(true)
    } else if (route === "now") {
      void restart.restart("now")
    }
  }
  if (!view) {
    return (
      <div className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
        {loadError ? `Couldn't load the settings: ${loadError}` : "Loading settings…"}
      </div>
    )
  }
  const locked = Boolean(view.file.error)
  const dots = pendingSections(view)
  const val = (k: Key) => view.values[k]
  const bridge = settingsBridge()
  const updates = updateKeys(view.build)
  const disk = diskNotice(view, diskChanges(view, saved))
  const title = SECTIONS.find((s) => s.id === section)?.label ?? "General"
  return (
    <div className="bg-background text-foreground relative flex h-dvh min-w-0 flex-1 overflow-clip max-md:flex-col">
      <aside className="flex w-52 shrink-0 flex-col gap-3 border-r px-2 py-4 max-md:w-full max-md:gap-1 max-md:border-r-0 max-md:border-b max-md:pt-[max(0.5rem,env(safe-area-inset-top))] max-md:pb-0">
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground flex items-center gap-1 px-2 text-xs max-md:min-h-11 max-md:text-sm"
          onClick={onBack}
        >
          <ArrowLeft className="size-3" /> Back to chats
        </button>
        <h1 className="px-2 text-sm font-semibold">Settings</h1>
        <nav
          aria-label="Settings sections"
          className="flex flex-col gap-0.5 max-md:-mx-2 max-md:flex-row max-md:overflow-x-auto max-md:px-2"
        >
          {SECTIONS.map((s) => (
            <button
              key={s.id}
              type="button"
              aria-current={s.id === section ? "page" : undefined}
              className={cn(
                "flex items-center justify-between rounded-md px-2 py-1.5 text-left text-sm max-md:shrink-0 max-md:gap-1.5 max-md:rounded-b-none max-md:px-3 max-md:py-3",
                s.id === section ? "bg-muted font-medium" : "hover:bg-muted/60",
              )}
              onClick={() => onSection(s.id)}
            >
              {s.label}
              {dots.has(s.id) && (
                <>
                  <span className="size-1.5 rounded-full bg-amber-800 dark:bg-amber-300" aria-hidden />
                  <span className="sr-only">, restart required</span>
                </>
              )}
            </button>
          ))}
        </nav>
      </aside>
      <main className="relative min-w-0 flex-1 overflow-y-auto px-8 py-5 max-md:px-4">
        <div className="max-w-[680px]">
          {restart.state === "now" && (
            <output className="bg-muted mb-6 block rounded-lg px-4 py-3 text-sm">
              Restarting… Handful is back in a few seconds.
            </output>
          )}
          {restart.state === "draining" && view.restartWait && (
            <output className="bg-muted mb-6 flex items-center gap-3 rounded-lg px-4 py-3 text-sm">
              <span className="flex-1">
                Handful restarts when the running bots finish. New turns wait. If they're still working in{" "}
                {minutesUntil(view.restartWait.deadline, now)} min, it asks you again.
              </span>
              <Button size="sm" variant="outline" onClick={() => void restart.cancel()}>
                Cancel restart
              </Button>
            </output>
          )}
          {restart.error && (
            <p role="alert" className="text-destructive mb-4 text-sm">
              {restart.error}
            </p>
          )}
          {restart.state !== "now" && restart.state !== "draining" && view.restartExpired && (
            <div
              role="alert"
              className="mb-6 flex flex-wrap items-center gap-3 rounded-lg border border-amber-500/40 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-300"
            >
              <span className="min-w-0 flex-1">
                Bots were still working after {drainLimit(view)} min, so Handful didn't restart. Restart now stops their
                turns.
              </span>
              <Button size="sm" variant="outline" onClick={() => void restart.restart("now")}>
                Restart now
              </Button>
              <Button size="sm" onClick={() => void restart.restart("idle")}>
                Wait {drainLimit(view)} more min
              </Button>
            </div>
          )}
          {restart.state !== "now" && restart.state !== "draining" && !view.restartExpired && (
            <RestartBanner view={view} onRestart={() => void askRestart()} />
          )}
          {view.file.error && (
            <div
              role="alert"
              className="border-destructive/40 text-destructive mb-6 rounded-lg border px-4 py-3 text-sm"
            >
              <p>config.json has an error: {view.file.error}. Fix it in an editor, or reset it.</p>
              {resetError && <p className="mt-1">Couldn't reset the file: {resetError}</p>}
              <div className="mt-2 flex gap-2">
                {bridge && (
                  <Button size="sm" variant="outline" onClick={bridge.openFile}>
                    Open in editor
                  </Button>
                )}
                {resetting ? (
                  <>
                    <span className="text-foreground self-center text-xs">Replace config.json with an empty file?</span>
                    <Button
                      size="sm"
                      variant="destructive"
                      onClick={() =>
                        void api.resetSettingsFile().then(
                          (v) => {
                            setView(v)
                            setResetting(false)
                          },
                          (e: Error) => setResetError(e.message),
                        )
                      }
                    >
                      Reset file
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setResetting(false)}>
                      Keep it
                    </Button>
                  </>
                ) : (
                  <Button size="sm" variant="outline" onClick={() => setResetting(true)}>
                    Reset file…
                  </Button>
                )}
              </div>
            </div>
          )}
          <section aria-labelledby="settings-title">
            <h2 id="settings-title" className="text-lg font-semibold">
              {title}
            </h2>
            {section === "general" && (
              <>
                <p className="text-muted-foreground mt-1 mb-2 text-sm">
                  Defaults for every bot. A bot or team can set its own model and effort in its Settings tab.
                </p>
                {val("model") && <SelectField k="model" v={val("model") as Value} save={save} locked={locked} />}
                {val("effort") && <SelectField k="effort" v={val("effort") as Value} save={save} locked={locked} />}
                {val("maxParallel") && (
                  <TextField k="maxParallel" v={val("maxParallel") as Value} save={save} locked={locked} />
                )}
                <details
                  className="border-t pt-3"
                  open={Boolean(val("claude") && (pending(val("claude") as Value) || val("claude")?.error))}
                >
                  <summary className="text-muted-foreground cursor-pointer text-sm">Advanced</summary>
                  {val("claude") && (
                    <TextField
                      k="claude"
                      v={val("claude") as Value}
                      save={save}
                      locked={locked}
                      mono
                      after={
                        bridge && (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={locked || val("claude")?.source === "flag"}
                            onClick={() =>
                              void bridge.chooseClaude().then((p) => {
                                if (p) void save("claude", p)
                              })
                            }
                          >
                            Choose…
                          </Button>
                        )
                      }
                    />
                  )}
                </details>
              </>
            )}
            {section === "updates" && (
              <>
                <p className="text-muted-foreground mt-1 mb-2 text-sm">
                  {view.build === "app" &&
                    `Handful ${version ?? ""} · signed release. Changes here apply without a restart.`}
                  {view.build === "adhoc" &&
                    "This copy was built locally, so it can't update itself. New releases show as a notice."}
                  {view.build === "source" && "This server was built from source. Update it by pulling and rebuilding."}
                </p>
                {updates.keys.map((k) => {
                  const v = val(k)
                  if (!v) return null
                  const off = locked || updates.disabled
                  return k === "updates.drainMinutes" ? (
                    <TextField key={k} k={k} v={v} save={save} locked={off} suffix="minutes, 5 to 240" />
                  ) : (
                    <SwitchField key={k} k={k} v={v} save={save} locked={off} />
                  )
                })}
                {view.build === "app" && updaterBridge() && (
                  <Row id="settings-check-now" label="Check now" help="Looks for a new release now." labels={false}>
                    <Button
                      size="sm"
                      aria-describedby="settings-check-now-help"
                      variant="outline"
                      className="self-start"
                      onClick={() => updaterBridge()?.check()}
                    >
                      Check for updates
                    </Button>
                  </Row>
                )}
              </>
            )}
            {section === "connectors" && (
              <>
                <p className="text-muted-foreground mt-1 mb-2 text-sm">
                  External MCP servers that bots can use. Each one is set up and signed in to in its own dialog.
                </p>
                <Row
                  id="settings-connectors"
                  label="Connectors"
                  labels={false}
                  help={
                    connectors === null
                      ? "Couldn't count the connectors."
                      : connectors === 1
                        ? "1 connector is set up."
                        : `${connectors} connectors are set up.`
                  }
                >
                  <Button
                    size="sm"
                    aria-describedby="settings-connectors-help"
                    variant="outline"
                    className="self-start"
                    onClick={onManageConnectors}
                  >
                    Manage connectors…
                  </Button>
                </Row>
              </>
            )}
            {section === "runtime" && (
              <>
                <p className="text-muted-foreground mt-1 mb-2 text-sm">
                  Set when the server starts, with command-line flags. The app sets these for you; shown here so you
                  know where things are.
                </p>
                <dl>
                  {view.runtime.map((r) => (
                    <div
                      key={r.key}
                      className="grid grid-cols-[minmax(0,1fr)_16rem] gap-4 border-b py-4 last:border-b-0 max-sm:grid-cols-1"
                    >
                      <dt className="min-w-0">
                        <span className="text-sm font-medium">{runtimeLabel(r.key)}</span>
                        {r.key === "addr" && (
                          <span className="text-muted-foreground mt-1 block text-xs">
                            Only this computer can reach Handful.
                          </span>
                        )}
                      </dt>
                      <dd className="flex min-w-0 flex-col gap-1">
                        <span className="font-mono text-sm [overflow-wrap:anywhere]">
                          {typeof r.value === "boolean" ? (r.value ? "On" : "Off") : shortPath(String(r.value))}
                        </span>
                        <span className="text-muted-foreground text-xs">{runtimeSource(r)}</span>
                        {r.key === "log" && bridge && (
                          <button
                            type="button"
                            className="text-muted-foreground self-start text-xs underline"
                            onClick={bridge.revealLog}
                          >
                            Reveal in Finder
                          </button>
                        )}
                      </dd>
                    </div>
                  ))}
                </dl>
              </>
            )}
            {section === "file" && (
              <>
                <p className="text-muted-foreground mt-1 mb-4 text-sm">
                  This page reads and writes the same file. Editing it by hand still works.
                </p>
                {disk && (
                  <output className="mb-4 block rounded-lg border border-blue-600/20 bg-blue-50 px-4 py-3 text-sm text-blue-700 dark:border-blue-400/30 dark:bg-blue-950 dark:text-blue-300">
                    config.json changed on disk after the server started:{" "}
                    {disk.map((d, i) => (
                      <span key={d.key}>
                        {i > 0 && "; "}
                        <strong>{LABELS[d.key]}</strong>
                        {d.text}
                      </span>
                    ))}
                    . {disk.length === 1 ? "It applies" : "They apply"} after a restart.
                  </output>
                )}
                <div className="grid grid-cols-[minmax(0,1fr)_16rem] gap-4 border-t py-4 max-sm:grid-cols-1">
                  <span className="text-sm font-medium">Location</span>
                  <div className="flex min-w-0 flex-col gap-2">
                    <code className="font-mono text-sm [overflow-wrap:anywhere]">{shortPath(view.file.path)}</code>
                    <div className="flex flex-wrap gap-2">
                      {bridge && (
                        <>
                          <Button size="sm" variant="outline" onClick={bridge.openFile}>
                            Open in editor
                          </Button>
                          <Button size="sm" variant="outline" onClick={bridge.revealFile}>
                            Reveal in Finder
                          </Button>
                        </>
                      )}
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          void navigator.clipboard.writeText(view.file.path).then(() => {
                            setCopied(true)
                            setTimeout(() => setCopied(false), 2000)
                          })
                        }
                      >
                        {copied ? "Copied" : "Copy path"}
                      </Button>
                    </div>
                  </div>
                </div>
              </>
            )}
          </section>
        </div>
      </main>
      <RestartDialog
        running={running.bots}
        now={running.at}
        limit={view ? drainLimit(view) : 30}
        open={asking}
        onOpenChange={setAsking}
        onRestart={(when) => {
          setAsking(false)
          void restart.restart(when)
        }}
      />
    </div>
  )
}
