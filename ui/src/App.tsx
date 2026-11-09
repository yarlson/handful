import { useEffect, useRef, useState, type DragEvent, type KeyboardEvent } from "react"
import {
  Inbox,
  AlertTriangle,
  Bot as BotIcon,
  MessageCircleQuestion,
  MessagesSquare,
  PanelRight,
  PauseCircle,
  Plug,
  Plus,
  ShieldQuestion,
  Users,
} from "lucide-react"
import { botColor } from "@/lib/bot-color"
import { api, type Bot } from "@/lib/api"
import { orderedEntries, useYarGPT } from "@/lib/store"
import { Chat } from "@/components/chat"
import { BotPanel, useLoad } from "@/components/bot-panel"
import { GoalBar } from "@/components/board"
import { WaitingView } from "@/components/questions"
import { countBadge, waitingCount, waitingKey, type Asked } from "@/lib/questions"
import { FALLBACK_ALIASES, ModelChip, ModelEffortFields, OPEN_SETTINGS, useDefaults } from "@/components/model"
import { inheritedFromGlobal, overrides, teamEffective } from "@/lib/model"
import { ConnectorsDialog } from "@/components/connectors"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { cn } from "@/lib/utils"

function NewBotDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onCreated: (id: string) => Promise<void>
}) {
  const [name, setName] = useState("")
  const [instructions, setInstructions] = useState("")
  const [model, setModel] = useState("")
  const [effort, setEffort] = useState("")
  const [error, setError] = useState<string | null>(null)
  const defaults = useDefaults()
  const create = async () => {
    try {
      const bot = await api.createBot(name, instructions, { model, effort })
      setName("")
      setInstructions("")
      setModel("")
      setEffort("")
      setError(null)
      await onCreated(bot.id)
      onOpenChange(false)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New bot</DialogTitle>
          <DialogDescription>
            A bot has its own chat, memory and routines. It runs Claude Code on this computer.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="new-name">Name</Label>
          <Input id="new-name" placeholder="Scout" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="new-instructions">What is it for? (optional)</Label>
          <Textarea
            id="new-instructions"
            rows={4}
            placeholder="Research assistant. Keep answers short."
            value={instructions}
            onChange={(e) => setInstructions(e.target.value)}
          />
        </div>
        <details className="text-sm">
          <summary className="cursor-pointer">Model and effort (optional)</summary>
          <div className="mt-3 flex flex-col gap-3">
            <ModelEffortFields
              id="new"
              model={model}
              effort={effort}
              onModel={setModel}
              onEffort={setEffort}
              inherited={
                defaults
                  ? inheritedFromGlobal(defaults)
                  : { model: { value: "", source: "claude-code" }, effort: { value: "", source: "claude-code" } }
              }
              defaults={defaults}
            />
          </div>
        </details>
        {error && <output className="text-destructive text-sm">{error}</output>}
        <DialogFooter>
          <Button disabled={!name.trim()} onClick={() => void create()}>
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function NewRoomDialog({
  open,
  onOpenChange,
  agents,
  onCreated,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  agents: Bot[]
  onCreated: (id: string) => Promise<void>
}) {
  const [name, setName] = useState("")
  const [purpose, setPurpose] = useState("")
  const [members, setMembers] = useState<Set<string>>(new Set())
  const [error, setError] = useState<string | null>(null)
  const toggle = (id: string, on: boolean) =>
    setMembers((prev) => {
      const next = new Set(prev)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })
  const create = async () => {
    try {
      const room = await api.createRoom(name, purpose, [...members])
      setName("")
      setPurpose("")
      setMembers(new Set())
      setError(null)
      await onCreated(room.id)
      onOpenChange(false)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New room</DialogTitle>
          <DialogDescription>
            A group chat with several bots. They take turns; write @Name to ask one of them.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="room-name">Name</Label>
          <Input id="room-name" placeholder="Planning" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="room-purpose">Purpose (optional)</Label>
          <Textarea id="room-purpose" rows={2} value={purpose} onChange={(e) => setPurpose(e.target.value)} />
        </div>
        <div className="flex flex-col gap-2">
          <Label>Members</Label>
          {agents.length === 0 && <p className="text-muted-foreground text-sm">Create a bot first.</p>}
          {agents.map((a) => (
            <label key={a.id} className="flex items-center gap-2 text-sm">
              <Checkbox checked={members.has(a.id)} onCheckedChange={(v) => toggle(a.id, v === true)} />
              {a.name}
            </label>
          ))}
        </div>
        {error && <p className="text-destructive text-sm">{error}</p>}
        <DialogFooter>
          <Button disabled={!name.trim() || members.size === 0} onClick={() => void create()}>
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function NewTeamDialog({
  open,
  onOpenChange,
  agents,
  onCreated,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  agents: Bot[]
  onCreated: (id: string) => Promise<void>
}) {
  const [name, setName] = useState("")
  const [purpose, setPurpose] = useState("")
  const [agreement, setAgreement] = useState("")
  const [members, setMembers] = useState<string[]>([])
  const [lead, setLead] = useState("")
  const [error, setError] = useState<string | null>(null)
  // The lead is a member; without a choice, the first member leads.
  const leadId = members.includes(lead) ? lead : (members[0] ?? "")
  const toggle = (id: string, on: boolean) => setMembers((prev) => (on ? [...prev, id] : prev.filter((m) => m !== id)))
  const create = async () => {
    try {
      const team = await api.createTeam({ name, instructions: purpose, memberIds: members, leadId, agreement })
      setName("")
      setPurpose("")
      setAgreement("")
      setMembers([])
      setLead("")
      setError(null)
      await onCreated(team.id)
      onOpenChange(false)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New team</DialogTitle>
          <DialogDescription>
            Bots that work together for you in one channel and one shared folder. Your messages go to the lead; write
            @Name to ask a member.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="team-name">Name</Label>
          <Input id="team-name" placeholder="Release" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="team-purpose">Purpose (optional)</Label>
          <Textarea id="team-purpose" rows={2} value={purpose} onChange={(e) => setPurpose(e.target.value)} />
        </div>
        <div className="flex flex-col gap-2">
          <Label>Members</Label>
          {agents.length === 0 && <p className="text-muted-foreground text-sm">Create a bot first.</p>}
          {agents.map((a) => (
            <label key={a.id} className="flex items-center gap-2 text-sm">
              <Checkbox checked={members.includes(a.id)} onCheckedChange={(v) => toggle(a.id, v === true)} />
              {a.name}
            </label>
          ))}
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="team-lead">Lead</Label>
          <select
            id="team-lead"
            className="border-input h-9 rounded-md border bg-transparent px-2 text-sm"
            disabled={members.length === 0}
            value={leadId}
            onChange={(e) => setLead(e.target.value)}
          >
            {members.map((id) => (
              <option key={id} value={id}>
                {agents.find((a) => a.id === id)?.name}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="team-agreement">Working agreement (optional)</Label>
          <Textarea
            id="team-agreement"
            rows={3}
            placeholder="How the team works and when work is done, for example: tests pass and another member checks each change."
            value={agreement}
            onChange={(e) => setAgreement(e.target.value)}
          />
        </div>
        {error && <p className="text-destructive text-sm">{error}</p>}
        <DialogFooter>
          <Button disabled={!name.trim() || members.length === 0} onClick={() => void create()}>
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function SidebarItem({
  bot,
  selected,
  unread,
  onSelect,
  onKeyDown,
}: {
  bot: Bot
  selected: boolean
  unread: boolean
  onSelect: () => void
  onKeyDown: (e: KeyboardEvent<HTMLButtonElement>) => void
}) {
  const Icon = bot.team ? Users : bot.kind === "room" ? MessagesSquare : BotIcon
  return (
    <button
      type="button"
      onClick={onSelect}
      onKeyDown={onKeyDown}
      aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown"
      aria-current={selected ? "page" : undefined}
      className={cn(
        "relative flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm",
        selected
          ? "bg-sidebar-foreground/10 font-medium before:absolute before:inset-y-1.5 before:left-0 before:w-0.75 before:rounded-full before:bg-pink-600"
          : "hover:bg-sidebar-foreground/5",
      )}
    >
      <Icon className={cn("size-4 shrink-0", bot.kind !== "room" && botColor(bot.id).text)} />
      <span className="flex-1 truncate">{bot.name}</span>
      {bot.attention === "approval" && (
        <ShieldQuestion className="size-4 shrink-0 text-amber-500" aria-label="Needs your approval">
          <title>Needs your approval</title>
        </ShieldQuestion>
      )}
      {bot.attention === "goal" && (
        <PauseCircle className="size-4 shrink-0 text-amber-500" aria-label="The goal is paused">
          <title>The goal is paused</title>
        </PauseCircle>
      )}
      {bot.attention === "model" && (
        <AlertTriangle className="size-4 shrink-0 text-amber-500" aria-label="Check the bot's model">
          <title>Check the bot's model</title>
        </AlertTriangle>
      )}
      {bot.attention === "question" && (
        <MessageCircleQuestion className="size-4 shrink-0 text-amber-500" aria-label="Waiting for your reply">
          <title>Waiting for your reply</title>
        </MessageCircleQuestion>
      )}
      {(bot.incomingHandoffs ?? 0) > 0 && (
        <span
          className="rounded-full bg-sky-500/15 px-1.5 text-xs text-sky-700 dark:text-sky-400"
          aria-label={`${bot.incomingHandoffs} incoming ${bot.incomingHandoffs === 1 ? "handoff" : "handoffs"}`}
        >
          {bot.incomingHandoffs}
        </span>
      )}
      {countBadge(bot.openQuestions) && (
        <span
          className="rounded-full bg-amber-500/15 px-1.5 text-xs text-amber-700 dark:text-amber-400"
          aria-label={`${bot.openQuestions} open questions`}
        >
          {countBadge(bot.openQuestions)}
        </span>
      )}
      {bot.running && <span className="size-2 animate-pulse rounded-full bg-emerald-500" title="Working" />}
      {!bot.running && bot.tasks.length > 0 && (
        <span
          className="size-2 animate-[pulse_3s_cubic-bezier(0.4,0,0.6,1)_infinite] rounded-full bg-sky-500"
          title="Background tasks"
        />
      )}
      {!bot.running && !bot.attention && unread && <span className="bg-primary size-2 rounded-full" title="Unread" />}
    </button>
  )
}

// BotList is one sortable sidebar section. Items move by drag and drop
// within the section, or with Alt+ArrowUp / Alt+ArrowDown on a focused item.
function BotList({
  items,
  selected,
  unread,
  onSelect,
  onReorder,
}: {
  items: Bot[]
  selected: string | null
  unread: Set<string>
  onSelect: (id: string) => void
  onReorder: (ids: string[]) => void
}) {
  const [dragId, setDragId] = useState<string | null>(null)
  const [insertAt, setInsertAt] = useState<number | null>(null)
  // move takes the item at from and puts it before the item at index to (to may equal items.length).
  const move = (from: number, to: number) => {
    const ids = items.map((b) => b.id)
    const [id] = ids.splice(from, 1)
    ids.splice(to > from ? to - 1 : to, 0, id)
    if (ids.join() !== items.map((b) => b.id).join()) onReorder(ids)
  }
  // dropIndex is where a dragged item lands: before item i in its upper half, after it otherwise.
  const dropIndex = (e: DragEvent<HTMLDivElement>, i: number) => {
    const r = e.currentTarget.getBoundingClientRect()
    return e.clientY < r.top + r.height / 2 ? i : i + 1
  }
  const endDrag = () => {
    setDragId(null)
    setInsertAt(null)
  }
  return (
    <div className="flex flex-col gap-0.5">
      {items.map((b, i) => (
        <div
          key={b.id}
          draggable
          className={cn("relative", dragId === b.id && "opacity-50")}
          onDragStart={(e) => {
            setDragId(b.id)
            e.dataTransfer.effectAllowed = "move"
            e.dataTransfer.setData("text/plain", b.name)
          }}
          onDragOver={(e) => {
            if (!dragId) return
            e.preventDefault()
            setInsertAt(dropIndex(e, i))
          }}
          onDrop={(e) => {
            e.preventDefault()
            // The drop event carries its own position; insertAt only draws the line.
            if (dragId)
              move(
                items.findIndex((x) => x.id === dragId),
                dropIndex(e, i),
              )
            endDrag()
          }}
          onDragEnd={endDrag}
        >
          {insertAt === i && <div className="bg-primary absolute -top-px right-2 left-2 h-0.5 rounded" />}
          {insertAt === items.length && i === items.length - 1 && (
            <div className="bg-primary absolute right-2 -bottom-px left-2 h-0.5 rounded" />
          )}
          <SidebarItem
            bot={b}
            selected={b.id === selected}
            unread={unread.has(b.id)}
            onSelect={() => onSelect(b.id)}
            onKeyDown={(e) => {
              if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return
              e.preventDefault()
              const to = e.key === "ArrowUp" ? i - 1 : i + 2
              if (to >= 0 && to <= items.length) move(i, to)
            }}
          />
        </div>
      ))}
    </div>
  )
}

const sidebarWidthKey = "yargpt.sidebarWidth"
const sidebarWidth = { min: 180, max: 480, initial: 240 }

function clampWidth(w: number) {
  return Math.round(Math.min(sidebarWidth.max, Math.max(sidebarWidth.min, w)))
}

// The sidebar width is a per-browser preference. Storage can be unavailable
// (for example in a private window); the width then resets on reload.
function loadSidebarWidth() {
  try {
    const saved = Number(localStorage.getItem(sidebarWidthKey))
    return saved ? clampWidth(saved) : sidebarWidth.initial
  } catch {
    return sidebarWidth.initial
  }
}

function saveSidebarWidth(w: number) {
  try {
    localStorage.setItem(sidebarWidthKey, String(w))
  } catch {
    // Not saved: the width resets on reload, which is acceptable.
  }
}

// A chat's address is /chat/<bot id>. Bot ids are UUIDs, so they need no encoding.
function chatPath(id: string) {
  return `/chat/${id}`
}

function chatInLocation() {
  return /^\/chat\/([^/]+)$/.exec(location.pathname)?.[1] ?? null
}

function SectionTitle({ title, action, onAction }: { title: string; action: string; onAction: () => void }) {
  return (
    <div className="text-muted-foreground mt-3 flex items-center justify-between px-2 text-xs font-medium uppercase">
      {title}
      <Button size="icon" variant="ghost" className="size-6" aria-label={action} onClick={onAction}>
        <Plus />
      </Button>
    </div>
  )
}

// chatSubtitle names who is in a room or a team, and what a bot does.
function chatSubtitle(bot: Bot) {
  if (bot.kind !== "room") return bot.instructions
  const members = bot.members ?? []
  const lead = members.find((m) => m.id === bot.team?.leadId)
  if (!lead) return `with ${members.map((m) => m.name).join(", ")}`
  const others = members.filter((m) => m.id !== lead.id).map((m) => m.name)
  return `led by ${lead.name}, with ${others.join(", ") || "no other members"}`
}

export function App() {
  const { bots, transcripts, unread, connected, error, setError, pausedUntil, select, refreshBots, reorder } =
    useYarGPT()
  const [width, setWidth] = useState(loadSidebarWidth)
  const resizing = useRef(false)
  useEffect(() => saveSidebarWidth(width), [width])
  const [selected, setSelected] = useState<string | null>(chatInLocation)
  const [creating, setCreating] = useState(false)
  const [creatingRoom, setCreatingRoom] = useState(false)
  const [creatingTeam, setCreatingTeam] = useState(false)
  const [connectors, setConnectors] = useState(false)
  const [panel, setPanel] = useState(true)
  const [waitingOpen, setWaitingOpen] = useState(false)
  const [show, setShow] = useState<{ tab: string; n: number } | undefined>()
  const showTab = (tab: string) => {
    setPanel(true)
    setShow((s) => ({ tab, n: (s?.n ?? 0) + 1 }))
    // The settings tab renders after this click; then its model select takes focus.
    if (tab === "settings") requestAnimationFrame(() => document.getElementById("bot-model")?.focus())
  }
  const defaults = useDefaults()
  const aliases = defaults?.modelAliases ?? FALLBACK_ALIASES

  const openChat = (id: string) => {
    if (location.pathname !== chatPath(id)) history.pushState(null, "", chatPath(id))
    setWaitingOpen(false)
    setSelected(id)
  }
  // The question that each chat's next message answers, set by an Answer button.
  const [answering, setAnswering] = useState(() => new Map<string, Asked>())
  const setAnswer = (chatId: string, q: Asked | null) =>
    setAnswering((m) => {
      const next = new Map(m)
      if (q) next.set(chatId, q)
      else next.delete(chatId)
      return next
    }) // An error or mismatch entry's "Open settings" asks for the bot's settings.
  useEffect(() => {
    const open = (e: Event) => {
      const id = (e as CustomEvent<string>).detail
      if (id !== selected) openChat(id)
      showTab("settings")
    }
    window.addEventListener(OPEN_SETTINGS, open)
    return () => window.removeEventListener(OPEN_SETTINGS, open)
  })

  useEffect(() => {
    const onPopState = () => setSelected(chatInLocation())
    window.addEventListener("popstate", onPopState)
    return () => window.removeEventListener("popstate", onPopState)
  }, [])
  useEffect(() => {
    if (bots.length === 0 || bots.some((b) => b.id === selected)) return
    // Replace, not push: otherwise Back returns to the missing chat, which opens the first chat again.
    history.replaceState(null, "", chatPath(bots[0].id))
    setSelected(bots[0].id)
  }, [bots, selected])
  useEffect(() => select(selected), [selected, select])

  const bot = bots.find((b) => b.id === selected)
  const subtitle = bot ? chatSubtitle(bot) : ""
  const agents = bots.filter((b) => b.kind !== "room")
  const rooms = bots.filter((b) => b.kind === "room" && !b.team)
  const teams = bots.filter((b) => b.team)
  const transcript = selected ? transcripts.get(selected) : undefined
  const entries = orderedEntries(transcript)
  const waiting = useLoad(() => api.waiting(), waitingKey(bots))
  const waitingN = waiting.data ? waitingCount(waiting.data) : 0

  return (
    <div className="bg-background text-foreground flex h-screen">
      <aside className="bg-sidebar text-sidebar-foreground flex shrink-0 flex-col" style={{ width }}>
        <div className="flex items-center px-3 py-3">
          <img src="/logo.svg" alt="YarGPT" className="size-7" />
        </div>
        <nav className="flex flex-1 flex-col gap-0.5 overflow-y-auto px-2">
          <button
            type="button"
            aria-current={waitingOpen ? "page" : undefined}
            className={cn(
              "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm",
              waitingOpen ? "bg-sidebar-foreground/10 font-medium" : "hover:bg-sidebar-foreground/5",
            )}
            onClick={() => setWaitingOpen(true)}
          >
            <Inbox className="size-4 shrink-0" />
            <span className="flex-1">Waiting for you</span>
            {waitingN > 0 && (
              <span
                className="rounded-full bg-amber-500/15 px-1.5 text-xs text-amber-700 dark:text-amber-400"
                aria-label={`${waitingN} waiting`}
              >
                {waitingN}
              </span>
            )}
          </button>
          <SectionTitle title="Bots" action="New bot" onAction={() => setCreating(true)} />
          <BotList
            items={agents}
            selected={selected}
            unread={unread}
            onSelect={openChat}
            onReorder={(ids) => void reorder(ids)}
          />
          <SectionTitle title="Teams" action="New team" onAction={() => setCreatingTeam(true)} />
          <BotList
            items={teams}
            selected={selected}
            unread={unread}
            onSelect={openChat}
            onReorder={(ids) => void reorder(ids)}
          />
          <SectionTitle title="Rooms" action="New room" onAction={() => setCreatingRoom(true)} />
          <BotList
            items={rooms}
            selected={selected}
            unread={unread}
            onSelect={openChat}
            onReorder={(ids) => void reorder(ids)}
          />
          {bots.length === 0 && (
            <Button variant="outline" size="sm" className="mt-2" onClick={() => setCreating(true)}>
              <Plus /> Create your first bot
            </Button>
          )}
        </nav>
        <div className="text-muted-foreground flex items-center gap-1.5 px-3 py-2 text-xs">
          <span className={cn("size-1.5 rounded-full", connected ? "bg-emerald-500" : "bg-amber-500")} />
          <span className="flex-1">{connected ? "Connected" : "Reconnecting…"}</span>
          <Button size="xs" variant="ghost" onClick={() => setConnectors(true)}>
            <Plug /> Connectors
          </Button>
        </div>
      </aside>
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize sidebar"
        aria-valuemin={sidebarWidth.min}
        aria-valuemax={sidebarWidth.max}
        aria-valuenow={width}
        tabIndex={0}
        title="Drag to resize; double-click to reset"
        className="hover:bg-primary/30 focus-visible:bg-primary/40 w-1 shrink-0 cursor-col-resize border-l outline-none"
        onPointerDown={(e) => {
          e.preventDefault()
          e.currentTarget.setPointerCapture(e.pointerId)
          resizing.current = true
        }}
        onPointerMove={(e) => {
          if (resizing.current) setWidth(clampWidth(e.clientX))
        }}
        onPointerUp={(e) => {
          resizing.current = false
          e.currentTarget.releasePointerCapture(e.pointerId)
        }}
        onKeyDown={(e) => {
          if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
            e.preventDefault()
            setWidth((w) => clampWidth(w + (e.key === "ArrowRight" ? 16 : -16)))
          }
        }}
        onDoubleClick={() => setWidth(sidebarWidth.initial)}
      />

      <main className="flex min-w-0 flex-1 flex-col">
        {error && (
          <div className="bg-destructive/10 text-destructive flex items-center justify-between px-4 py-2 text-sm">
            {error}
            <Button size="xs" variant="ghost" onClick={() => setError(null)}>
              Dismiss
            </Button>
          </div>
        )}
        {pausedUntil && (
          <div className="bg-amber-500/10 px-4 py-2 text-sm">
            Claude usage limit reached. New turns wait until{" "}
            {new Date(pausedUntil).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })}.
          </div>
        )}
        {waitingOpen && waiting.data ? (
          <WaitingView
            waiting={waiting.data}
            bots={bots}
            onOpen={openChat}
            onAnswer={(chatId, q) => {
              setAnswer(chatId, q)
              openChat(chatId)
              requestAnimationFrame(() => document.getElementById("composer-input")?.focus())
            }}
            onChange={waiting.reload}
          />
        ) : bot ? (
          <>
            <header className="flex h-12 items-center gap-2 border-b px-4">
              <span className="max-w-[60%] shrink-0 truncate font-medium" title={bot.name}>
                {bot.name}
              </span>
              <span className="text-muted-foreground min-w-0 flex-1 truncate text-sm" title={subtitle}>
                {subtitle}
              </span>
              {bot.effective && (
                <ModelChip effective={bot.effective} aliases={aliases} onClick={() => showTab("settings")} />
              )}
              {bot.team && defaults && (
                <ModelChip
                  effective={teamEffective(bot.team, defaults)}
                  aliases={aliases}
                  prefix="Team: "
                  suffix={overrides(bot.members ?? []) > 0 ? "· mixed" : ""}
                  action="Open members."
                  onClick={() => showTab("members")}
                />
              )}
              <Button
                size="icon"
                variant="ghost"
                className="shrink-0"
                aria-label="Toggle panel"
                onClick={() => setPanel((v) => !v)}
              >
                <PanelRight />
              </Button>
            </header>
            {bot.team && (
              <GoalBar
                key={bot.id}
                team={bot}
                version={transcript?.rev ?? 0}
                onShowBoard={() => showTab("board")}
                teams={teams}
              />
            )}
            <div className="flex min-h-0 flex-1">
              <Chat
                bot={bot}
                entries={entries}
                answering={answering.get(bot.id) ?? null}
                onAnswer={(q) => setAnswer(bot.id, q)}
                mentionables={
                  bot.kind === "room"
                    ? (bot.members ?? [])
                    : agents.filter((a) => a.id !== bot.id).map((a) => ({ id: a.id, name: a.name }))
                }
              />
              {panel && (
                <aside className="w-80 shrink-0 border-l">
                  <BotPanel
                    bot={bot}
                    agents={agents}
                    version={transcript?.rev ?? 0}
                    onDeleted={() => setSelected(null)}
                    show={show}
                    teams={teams}
                  />
                </aside>
              )}
            </div>
          </>
        ) : (
          <div className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
            Create a bot to start.
          </div>
        )}
      </main>
      <ConnectorsDialog open={connectors} onOpenChange={setConnectors} bots={agents} />
      <NewTeamDialog
        open={creatingTeam}
        onOpenChange={setCreatingTeam}
        agents={agents}
        onCreated={async (id) => {
          await refreshBots()
          openChat(id)
        }}
      />
      <NewRoomDialog
        open={creatingRoom}
        onOpenChange={setCreatingRoom}
        agents={agents}
        onCreated={async (id) => {
          await refreshBots()
          openChat(id)
        }}
      />
      <NewBotDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={async (id) => {
          // Load the new bot into the list first; otherwise the selection guard resets it.
          await refreshBots()
          openChat(id)
        }}
      />
    </div>
  )
}
