import { useEffect, useRef, useState, type ReactNode } from "react"
import {
  Bot as BotIcon,
  Download,
  Hourglass,
  LoaderCircle,
  Package,
  Pause,
  RotateCw,
  TriangleAlert,
} from "lucide-react"
import { Popover } from "radix-ui"
import { api, type Bot } from "@/lib/api"
import { botColor } from "@/lib/bot-color"
import {
  announcement,
  ariaLabel,
  doing,
  drainFraction,
  errorReason,
  label,
  laterActive,
  minutesLeft,
  shown,
  sourceState,
  tone,
  type RunningBot,
  type UpdaterState,
} from "@/lib/updates"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

// The desktop app's preload exposes the updater; a browser or a source build has none.
type Bridge = {
  get: () => Promise<UpdaterState>
  onChange: (cb: (s: UpdaterState) => void) => () => void
  download: () => void
  cancelDownload: () => void
  later: () => void
  skip: () => void
  restartNow: () => void
  updateWhenIdle: () => void
  cancelUpdate: () => void
  waitLonger: () => void
  retry: () => void
  openReleasePage: () => void
  check: () => void
}

function bridge(): Bridge | undefined {
  return (window as unknown as { handfulUpdater?: Bridge }).handfulUpdater
}

const SKIP_KEY = "handful.update.skip"
const LATER_KEY = "handful.update.later"

// useUpdate follows the update state: from the desktop app when it runs in one, else from
// the server's /api/version, which knows of new releases for a source build.
export function useUpdate(): { state: UpdaterState | null; actions: Bridge } {
  const [state, setState] = useState<UpdaterState | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const b = bridge()
  useEffect(() => {
    if (b) {
      void b.get().then(setState)
      return b.onChange(setState)
    }
    let live = true
    const poll = () =>
      api.version().then(
        (v) => live && setState(sourceState(v, localStorage.getItem(SKIP_KEY))),
        () => {},
      )
    void poll()
    const t = setInterval(() => {
      setNow(Date.now())
      void poll()
    }, 30_000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [b])
  // A source build keeps Skip and Later in the browser.
  const later = Number(localStorage.getItem(LATER_KEY)) || 0
  const web: Bridge = {
    get: async () => state as UpdaterState,
    onChange: () => () => {},
    download: () => {},
    cancelDownload: () => {},
    later: () => {
      const at = Date.now()
      localStorage.setItem(LATER_KEY, String(at))
      setNow(at)
    },
    skip: () => {
      if (state?.version) localStorage.setItem(SKIP_KEY, state.version)
      setState((s) => (s ? { ...s, state: "hidden" } : s))
    },
    restartNow: () => {},
    updateWhenIdle: () => {},
    cancelUpdate: () => {},
    waitLonger: () => {},
    retry: () => {},
    openReleasePage: () => window.open(state?.releaseUrl, "_blank", "noopener,noreferrer"),
    check: () => {},
  }
  const shownState = !b && state && laterActive(later, now) ? { ...state, hiddenByLater: true } : state
  return { state: shownState, actions: b ?? web }
}

const toneClass = {
  accent: "bg-blue-50 text-blue-700 ring-blue-600/20 dark:bg-blue-950 dark:text-blue-300 dark:ring-blue-400/30",
  solid: "bg-blue-600 text-white ring-transparent hover:bg-blue-700",
  neutral: "bg-popover text-foreground ring-foreground/10 dark:ring-foreground/15",
  warning: "bg-amber-50 text-amber-800 ring-amber-600/30 dark:bg-amber-950 dark:text-amber-300 dark:ring-amber-400/30",
}

function Icon({ s }: { s: UpdaterState }) {
  const c = "size-3.5 shrink-0"
  if (s.state === "downloading") return <Download className={cn(c, "text-muted-foreground")} aria-hidden />
  if (s.state === "ready" || s.state === "limit") return <RotateCw className={c} aria-hidden />
  if (s.state === "waiting") return <Hourglass className={c} aria-hidden />
  if (s.state === "restarting") return <LoaderCircle className={cn(c, "motion-safe:animate-spin")} aria-hidden />
  if (s.state === "error") return <TriangleAlert className={c} aria-hidden />
  return <Package className={cn(c, s.state === "source" && "text-muted-foreground")} aria-hidden />
}

// BotRows lists the bots the update waits for, with what each is doing and the sidebar's
// dot: green for a running turn, blue for background tasks.
function BotRows({ running, bots, now }: { running: RunningBot[]; bots: Bot[]; now: number }) {
  return (
    <ul className="flex flex-col gap-1.5 rounded-lg border p-2.5 text-[13px]">
      {running.map((r) => {
        const known = bots.find((b) => b.id === r.botId)
        return (
          <li key={r.botId} className="flex items-center gap-2">
            <BotIcon
              className={cn("size-3.5 shrink-0", known ? botColor(known.id).text : "text-muted-foreground")}
              aria-hidden
            />
            <span className="min-w-0 flex-1 truncate">{r.name}</span>
            <span className="text-muted-foreground shrink-0 tabular-nums">{doing(r, now)}</span>
            <span
              className={cn("size-2 shrink-0 rounded-full", r.kind === "turn" ? "bg-emerald-500" : "bg-sky-500")}
              aria-hidden
            />
          </li>
        )
      })}
    </ul>
  )
}

function Footer({ children }: { children: ReactNode }) {
  return (
    <div className="text-muted-foreground -mx-4 flex justify-between border-t px-2 pt-2 [&>button]:text-muted-foreground">
      {children}
    </div>
  )
}

function SubLine({ s }: { s: UpdaterState }) {
  return (
    <p className="text-muted-foreground text-xs">
      You have {s.current} ·{" "}
      <a href={s.releaseUrl} target="_blank" rel="noopener noreferrer" className="underline">
        What's new in {s.version}
      </a>
    </p>
  )
}

// Body is the popover's content for a state.
export function Body({
  s,
  a,
  bots,
  now,
  confirm,
  setConfirm,
}: {
  s: UpdaterState
  a: Bridge
  bots: Bot[]
  now: number
  confirm: boolean
  setConfirm: (v: boolean) => void
}) {
  const running = s.running ?? []
  const later = (
    <Button size="sm" variant="ghost" onClick={a.later}>
      Later
    </Button>
  )
  const skip = (
    <Button size="sm" variant="ghost" onClick={a.skip}>
      Skip this version
    </Button>
  )
  const tasksNote = running.some((r) => r.kind === "task") && (
    <p className="text-muted-foreground text-xs">
      A background task can keep going until it ends; the time limit bounds it.
    </p>
  )
  if (s.state === "available") {
    return (
      <>
        <p className="text-sm font-medium">Handful {s.version} is out</p>
        <SubLine s={s} />
        <Button size="sm" className="self-start" onClick={a.download} data-main>
          Download
        </Button>
        <Footer>
          {later}
          {skip}
        </Footer>
      </>
    )
  }
  if (s.state === "downloading") {
    const pct = Math.round(s.percent ?? 0)
    const mb = (n?: number) => (n === undefined ? "" : String(Math.round(n / 1e6)))
    return (
      <>
        <p className="text-sm font-medium">Downloading {s.version}</p>
        <progress
          aria-label={`Downloading ${s.version}`}
          value={pct}
          max={100}
          className="h-1.5 w-full appearance-none overflow-hidden rounded-full [&::-moz-progress-bar]:bg-blue-600 [&::-webkit-progress-bar]:bg-muted [&::-webkit-progress-value]:bg-blue-600 dark:[&::-moz-progress-bar]:bg-blue-400 dark:[&::-webkit-progress-value]:bg-blue-400 bg-muted"
        />
        <p className="text-muted-foreground text-xs tabular-nums">
          {s.total ? `${mb(s.transferred)} of ${mb(s.total)} MB · ` : ""}
          {pct}%
        </p>
        <Button size="sm" variant="outline" className="self-start" onClick={a.cancelDownload} data-main>
          Cancel download
        </Button>
        <Footer>{later}</Footer>
      </>
    )
  }
  if ((s.state === "ready" || s.state === "limit") && confirm) {
    return (
      <>
        <p className="text-sm font-medium">
          Stop {running.length} running {running.length === 1 ? "bot" : "bots"}?
        </p>
        <BotRows running={running} bots={bots} now={now} />
        <p className="text-muted-foreground text-[13px]">Restarting now stops their running turns.</p>
        <div className="flex gap-2">
          <Button size="sm" onClick={a.updateWhenIdle} data-main>
            Update when they finish
          </Button>
          <Button size="sm" variant="outline" onClick={a.restartNow}>
            Restart now
          </Button>
        </div>
        <Footer>
          <Button size="sm" variant="ghost" onClick={() => setConfirm(false)}>
            Back
          </Button>
        </Footer>
      </>
    )
  }
  if (s.state === "limit") {
    return (
      <>
        <p className="text-sm font-medium">Still working after {s.drainMinutes} min</p>
        <BotRows running={running} bots={bots} now={now} />
        <p className="text-muted-foreground text-[13px]">Restart now stops them, or wait longer.</p>
        <div className="flex gap-2">
          <Button size="sm" onClick={a.waitLonger} data-main>
            Wait {s.drainMinutes} more min
          </Button>
          <Button size="sm" variant="outline" onClick={() => setConfirm(true)}>
            Restart now
          </Button>
        </div>
      </>
    )
  }
  if (s.state === "ready") {
    if (running.length === 0) {
      return (
        <>
          <p className="text-sm font-medium">Handful {s.version} is ready</p>
          <SubLine s={s} />
          <p className="text-muted-foreground text-[13px]">Restart Handful to finish updating.</p>
          <Button size="sm" className="self-start" onClick={a.restartNow} data-main>
            Restart now
          </Button>
          <Footer>
            {later}
            {skip}
          </Footer>
        </>
      )
    }
    return (
      <>
        <p className="text-sm font-medium">Handful {s.version} is ready</p>
        <SubLine s={s} />
        <p className="text-[13px]">{running.length === 1 ? "1 bot is" : `${running.length} bots are`} working:</p>
        <BotRows running={running} bots={bots} now={now} />
        <div className="flex gap-2">
          <Button size="sm" onClick={a.updateWhenIdle} data-main>
            Update when they finish
          </Button>
          <Button size="sm" variant="outline" onClick={() => setConfirm(true)}>
            Restart now
          </Button>
        </div>
        <p className="text-muted-foreground flex items-center gap-1.5 text-xs">
          <TriangleAlert className="size-3.5 shrink-0" aria-hidden />
          Restart now stops their running turns.
        </p>
        <Footer>
          {later}
          {skip}
        </Footer>
      </>
    )
  }
  if (s.state === "waiting") {
    const left = minutesLeft(s.deadline, now)
    return (
      <>
        <p className="text-sm font-medium">
          Updating after {running.length} {running.length === 1 ? "bot finishes" : "bots finish"}
        </p>
        <p className="text-muted-foreground -mt-2 text-[13px]">
          New turns wait. Teams pause their tasks for the update.
        </p>
        <BotRows running={running} bots={bots} now={now} />
        <progress
          aria-label="Time left before Handful asks you"
          value={Math.round(drainFraction(s.deadline, s.drainMinutes, now) * s.drainMinutes)}
          max={s.drainMinutes}
          className="h-1.5 w-full appearance-none overflow-hidden rounded-full [&::-moz-progress-bar]:bg-blue-600 [&::-webkit-progress-bar]:bg-muted [&::-webkit-progress-value]:bg-blue-600 dark:[&::-moz-progress-bar]:bg-blue-400 dark:[&::-webkit-progress-value]:bg-blue-400 bg-muted"
        />
        <p className="text-muted-foreground text-[13px] tabular-nums">
          {left} of {s.drainMinutes} min left. Handful restarts on its own when they finish; if the limit passes, it
          asks you first.
        </p>
        {tasksNote}
        <div className="flex justify-between">
          <Button size="sm" variant="outline" onClick={a.restartNow}>
            Restart now
          </Button>
          <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={a.cancelUpdate} data-main>
            Cancel update
          </Button>
        </div>
      </>
    )
  }
  if (s.state === "error") {
    return (
      <>
        <p className="text-sm font-medium">Update failed</p>
        <p className="text-muted-foreground text-[13px]">{errorReason(s)}</p>
        <div className="flex gap-2">
          <Button size="sm" onClick={a.retry} data-main>
            Try again
          </Button>
          <Button size="sm" variant="outline" onClick={a.openReleasePage}>
            Download manually ↗
          </Button>
        </div>
        <Footer>{later}</Footer>
      </>
    )
  }
  if (s.state === "source") {
    return (
      <>
        <p className="text-sm font-medium">Handful {s.version} is out</p>
        <p className="text-muted-foreground text-[13px]">
          This copy was built locally, so it can't update itself. Download the release, or pull and rebuild:
        </p>
        <code className="bg-muted rounded-md px-2 py-1 font-mono text-xs">git pull &amp;&amp; make build</code>
        <Button size="sm" className="self-start" onClick={a.openReleasePage} data-main>
          Open release page ↗
        </Button>
        <Footer>
          {later}
          {skip}
        </Footer>
      </>
    )
  }
  return null
}

// UpdatePill shows the update state at the bottom right of the main column and opens its
// actions. floating places it in the window corner, for views without a composer.
export function UpdatePill({
  update,
  bots,
  floating = false,
}: {
  update: { state: UpdaterState | null; actions: Bridge }
  bots: Bot[]
  floating?: boolean
}) {
  const { state: s, actions } = update
  const [open, setOpen] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const [said, setSaid] = useState("")
  const lastState = useRef<string | null>(null)
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 15_000)
    return () => clearInterval(t)
  }, [])
  // Announce each state once, never each progress tick.
  useEffect(() => {
    if (!s || s.state === lastState.current) return
    lastState.current = s.state
    setSaid(shown(s) ? announcement(s) : "")
  }, [s])
  if (!s || !shown(s))
    return (
      <span className="sr-only" aria-live="polite">
        {said}
      </span>
    )
  const t = tone(s.state)
  const text = label(s, now)
  const pct = Math.round(s.percent ?? 0)
  const restarting = s.state === "restarting"
  return (
    <div
      className={cn(
        "z-10",
        floating ? "fixed right-4 bottom-4" : "absolute right-3 bottom-full mb-2",
        "motion-safe:animate-in motion-safe:fade-in-0 motion-safe:slide-in-from-bottom-1",
      )}
    >
      <span className="sr-only" aria-live="polite">
        {said}
      </span>
      <Popover.Root
        open={open && !restarting}
        onOpenChange={(v) => {
          setOpen(v)
          if (!v) setConfirm(false)
        }}
      >
        <Popover.Trigger asChild>
          <button
            type="button"
            aria-haspopup="dialog"
            aria-expanded={open}
            aria-label={ariaLabel(s, now)}
            aria-disabled={restarting || undefined}
            className={cn(
              "focus-visible:ring-ring/50 relative inline-flex h-7 items-center gap-1.5 overflow-hidden rounded-full pr-3 pl-2.5 text-xs font-medium shadow-md ring-1 outline-none focus-visible:ring-3 aria-expanded:ring-3",
              toneClass[t],
            )}
          >
            {s.state === "downloading" && (
              <span
                className="absolute inset-y-0 left-0 bg-blue-600/15 dark:bg-blue-400/20"
                style={{ width: `${pct}%` }}
                aria-hidden
              />
            )}
            <span className="relative inline-flex items-center gap-1.5">
              <Icon s={s} />
              <span className="tabular-nums @max-[520px]:hidden">
                {s.state === "waiting" ? (
                  <>
                    {text.full.split(" · ")[0]}
                    <span className="text-muted-foreground"> · </span>
                    {text.full.split(" · ")[1]}
                  </>
                ) : (
                  text.full
                )}
              </span>
              <span className="hidden tabular-nums @max-[520px]:inline">{text.short}</span>
            </span>
          </button>
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            side="top"
            align="end"
            sideOffset={8}
            aria-label="Update"
            onOpenAutoFocus={(e) => {
              // Focus starts on the main action, not the first link.
              const main = (e.currentTarget as HTMLElement).querySelector<HTMLElement>("[data-main]")
              if (!main) return
              e.preventDefault()
              main.focus()
            }}
            className="bg-popover text-popover-foreground ring-foreground/10 z-50 flex w-80 flex-col gap-3 rounded-xl p-4 shadow-lg ring-1"
          >
            <Body s={s} a={actions} bots={bots} now={now} confirm={confirm} setConfirm={setConfirm} />
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </div>
  )
}

// PausedForUpdate is the goal bar's badge while the server waits to restart, for an update
// or for new settings.
export function PausedForUpdate({ label = "Paused for update" }: { label?: string }) {
  return (
    <Badge variant="secondary" title="New turns wait until Handful restarts. Running turns finish.">
      <Pause className="size-3" aria-hidden />
      {label}
    </Badge>
  )
}
