import { useCallback, useEffect, useRef, useState, type DragEvent, type KeyboardEvent } from "react"
import {
  Inbox,
  LayoutDashboard,
  AlertTriangle,
  MessageCircleQuestion,
  Menu,
  PanelRight,
  PauseCircle,
  Plug,
  Plus,
  Settings as SettingsIcon,
  ShieldQuestion,
} from "lucide-react"
import { ChatIcon } from "@/components/bot-colors"
import { api, type Bot, type Entry } from "@/lib/api"
import { type Link, linkLabel } from "@/lib/connection"
import { useHandful } from "@/lib/store"
import { Chat } from "@/components/chat"
import { BotPanel, useLoad } from "@/components/bot-panel"
import { useNarrow } from "@/lib/narrow"
import { MoveNotice } from "@/components/move-notice"
import { NowChip } from "@/components/board"
import { AcrossScreen, InboxScreen } from "@/components/tasks"
import { tasksKey } from "@/lib/tasks"
import { openSettingsPage, SettingsPage } from "@/components/settings"
import { isSettingsPath, pauseLabel, sectionOfPath } from "@/lib/settings"
import { UpdatePill, useUpdate } from "@/components/update-pill"
import { shown } from "@/lib/updates"
import { WaitingView } from "@/components/questions"
import { countBadge, waitingCount, waitingKey, type Asked } from "@/lib/questions"
import { FALLBACK_ALIASES, ModelChip, ModelEffortFields, OPEN_SETTINGS, useDefaults } from "@/components/model"
import { inheritedFromGlobal, overrides, teamEffective } from "@/lib/model"
import { ConnectorsDialog } from "@/components/connectors"
import { Badge } from "@/components/ui/badge"
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
import { PairAgain, PairBanner, PairPage, usePairRequest } from "@/components/phones"
import { rightsOf } from "@/lib/phones"
import { needsKey, phoneSession, useSession } from "@/lib/session"
import { KeyPage } from "@/components/key-page"

// NEEDS_YOU_PATH opens Waiting for you, so a notification, a bookmark and Back reach it.
const NEEDS_YOU_PATH = "/needs-you"

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
                  : {
                      model: { value: "", source: "claude-code" },
                      effort: { value: "", source: "claude-code" },
                    }
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
      const team = await api.createTeam({
        name,
        instructions: purpose,
        memberIds: members,
        leadId,
        agreement,
      })
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

export function SidebarItem({
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
  return (
    <button
      type="button"
      onClick={onSelect}
      onKeyDown={onKeyDown}
      aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown"
      aria-current={selected ? "page" : undefined}
      className={cn(
        "relative flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm max-md:py-3",
        selected
          ? "bg-sidebar-foreground/10 font-medium before:absolute before:inset-y-1.5 before:left-0 before:w-0.75 before:rounded-full before:bg-pink-600"
          : "hover:bg-muted transition-colors duration-(--duration-fast)",
      )}
    >
      <ChatIcon chat={bot} />
      <span className="flex-1 truncate">{bot.name}</span>
      {bot.attention === "approval" && (
        <ShieldQuestion className="size-4 shrink-0 text-attention" aria-label="Needs your approval">
          <title>Needs your approval</title>
        </ShieldQuestion>
      )}
      {bot.attention === "goal" && (
        <PauseCircle className="size-4 shrink-0 text-attention" aria-label="The task is paused">
          <title>The task is paused</title>
        </PauseCircle>
      )}
      {bot.attention === "model" && (
        <AlertTriangle className="size-4 shrink-0 text-attention" aria-label="Check the bot's model">
          <title>Check the bot's model</title>
        </AlertTriangle>
      )}
      {bot.attention === "question" && (
        <MessageCircleQuestion className="size-4 shrink-0 text-attention" aria-label="Waiting for your reply">
          <title>Waiting for your reply</title>
        </MessageCircleQuestion>
      )}
      {(bot.incomingHandoffs ?? 0) > 0 && (
        <Badge variant="info" aria-label={`${bot.incomingHandoffs} in the inbox`}>
          {bot.incomingHandoffs}
        </Badge>
      )}
      {countBadge(bot.openQuestions) && (
        <Badge variant="attention" aria-label={`${bot.openQuestions} open questions`}>
          {countBadge(bot.openQuestions)}
        </Badge>
      )}
      {bot.running && <span className="size-2 animate-pulse rounded-full bg-success" title="Working" />}
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
          {insertAt === i && <div className="bg-primary absolute -top-px right-2 left-2 h-0.5 rounded-full" />}
          {insertAt === items.length && i === items.length - 1 && (
            <div className="bg-primary absolute right-2 -bottom-px left-2 h-0.5 rounded-full" />
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

const sidebarWidthKey = "handful.sidebarWidth"
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

// SectionTitle heads a sidebar section; without onAction, as on a phone, it has no add button.
function SectionTitle({ title, action, onAction }: { title: string; action: string; onAction?: () => void }) {
  return (
    <div className="text-muted-foreground mt-3 flex min-h-6 items-center justify-between px-2 text-xs font-medium">
      {title}
      {onAction && (
        <Button size="icon" variant="ghost" className="size-6 max-md:size-11" aria-label={action} onClick={onAction}>
          <Plus />
        </Button>
      )}
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

// SidebarFooter is the connection state and the ways to Connectors and Settings, which a phone
// doesn't have. The buttons are icons, so the row fits the narrowest sidebar; their names stay for
// screen readers.
export function SidebarFooter({
  link,
  onConnectors,
  onSettings,
}: {
  link: Link
  onConnectors?: () => void
  onSettings?: () => void
}) {
  return (
    <div className="text-muted-foreground flex items-center gap-1.5 px-3 py-2 text-xs" data-connection={link}>
      <span className={cn("size-1.5 shrink-0 rounded-full", link === "live" ? "bg-success" : "bg-attention")} />
      <span className="min-w-0 flex-1 truncate">{linkLabel(link)}</span>
      {onConnectors && (
        <Button size="icon-xs" variant="ghost" aria-label="Connectors" title="Connectors" onClick={onConnectors}>
          <Plug />
        </Button>
      )}
      {onSettings && (
        <Button size="icon-xs" variant="ghost" aria-label="Settings" title="Settings" onClick={onSettings}>
          <SettingsIcon />
        </Button>
      )}
    </div>
  )
}

// NO_ENTRIES is the entries of a chat that isn't loaded: one array, so that its renders
// don't see a change.
const NO_ENTRIES: Entry[] = []

export function App() {
  const {
    bots,
    transcripts,
    transcriptStatus,
    unread,
    link,
    error,
    setError,
    pausedUntil,
    draining,
    restartRev,
    pairRev,
    select,
    loadOlder,
    reveal,
    refreshBots,
    reorder,
  } = useHandful()
  const update = useUpdate()
  // A paired phone sees only what it may use; the server refuses the rest.
  const session = useSession()
  const rights = rightsOf(session)
  const pair = usePairRequest(pairRev, rights.computer)
  const [width, setWidth] = useState(loadSidebarWidth)
  const resizing = useRef(false)
  useEffect(() => saveSidebarWidth(width), [width])
  const [selected, setSelected] = useState<string | null>(chatInLocation)
  // path is the address bar's path; /settings… shows the Settings page instead of the chats.
  const [path, setPath] = useState(() => location.pathname)
  const [creating, setCreating] = useState(false)
  const [creatingRoom, setCreatingRoom] = useState(false)
  const [creatingTeam, setCreatingTeam] = useState(false)
  const [connectors, setConnectors] = useState(false)
  // Below 64rem the panel covers the chat, so a narrow window starts with it closed.
  const [panel, setPanel] = useState(() => window.matchMedia("(min-width: 64rem)").matches)
  // drawer: on a narrow window the sidebar is a drawer over the page; above md it is always shown.
  const [drawer, setDrawer] = useState(false)
  const narrow = useNarrow()
  const menuButton = useRef<HTMLButtonElement>(null)
  const sidebar = useRef<HTMLElement | null>(null)
  const menu = narrow ? (
    <Button
      ref={menuButton}
      size="icon"
      variant="ghost"
      className="-ml-2 size-11 shrink-0"
      aria-label="Open menu"
      aria-expanded={drawer}
      aria-controls="app-sidebar"
      onClick={() => setDrawer(true)}
    >
      <Menu />
    </Button>
  ) : null
  // The keyboard goes into the drawer when it opens, and back to the menu button when it closes.
  const drawerWasOpen = useRef(false)
  useEffect(() => {
    if (drawer) sidebar.current?.querySelector<HTMLElement>("nav button")?.focus()
    else if (drawerWasOpen.current) menuButton.current?.focus()
    drawerWasOpen.current = drawer
  }, [drawer])
  // closesDrawerOnEscape makes Escape inside the drawer close it.
  const closesDrawerOnEscape = useCallback((el: HTMLElement | null) => {
    sidebar.current = el
    if (!el) return
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") setDrawer(false)
    }
    el.addEventListener("keydown", onKey)
    return () => el.removeEventListener("keydown", onKey)
  }, [])
  // The keyboard goes into the panel when it opens over the chat, or when the control that opened
  // it left the page (the Now chip); it goes back to the panel button when the panel closes and
  // took the keyboard with it.
  const panelButton = useRef<HTMLButtonElement>(null)
  const panelBox = useRef<HTMLElement | null>(null)
  const panelWasOpen = useRef(panel)
  useEffect(() => {
    const lost = !document.activeElement || document.activeElement === document.body
    const over = !window.matchMedia("(min-width: 64rem)").matches
    if (panel && !panelWasOpen.current && (over || lost))
      panelBox.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus()
    if (!panel && panelWasOpen.current && lost) panelButton.current?.focus()
    panelWasOpen.current = panel
  }, [panel])
  // panelKeys makes Escape inside the panel close it where it covers the chat.
  const panelKeys = useCallback((el: HTMLElement | null) => {
    panelBox.current = el
    if (!el) return
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape" && !window.matchMedia("(min-width: 64rem)").matches) setPanel(false)
    }
    el.addEventListener("keydown", onKey)
    return () => el.removeEventListener("keydown", onKey)
  }, [])
  // screen is what the main area shows: the selected chat, its team's inbox, Waiting for you or Across teams.
  const [screen, setScreen] = useState<"chat" | "inbox" | "waiting" | "across">(() =>
    location.pathname === NEEDS_YOU_PATH ? "waiting" : "chat",
  )
  const [show, setShow] = useState<{ tab: string; n: number } | undefined>()
  const showTab = (tab: string) => {
    setPanel(true)
    setShow((s) => ({ tab, n: (s?.n ?? 0) + 1 }))
    // The settings tab renders after this click; then its model select takes focus.
    if (tab === "settings") requestAnimationFrame(() => document.getElementById("bot-model")?.focus())
  }
  const defaults = useDefaults()
  const aliases = defaults?.modelAliases ?? FALLBACK_ALIASES

  // showScreen shows a screen of the main area; in a narrow window the drawer closes on the choice.
  // Waiting for you has its own address; leaving it goes back to the chat's.
  const showScreen = (s: typeof screen) => {
    setScreen(s)
    setDrawer(false)
    const needsYou = location.pathname === NEEDS_YOU_PATH
    if (s === "waiting" && !needsYou) history.pushState(null, "", NEEDS_YOU_PATH)
    if (s !== "waiting" && needsYou) history.pushState(null, "", selected ? chatPath(selected) : "/")
    setPath(location.pathname)
  }
  const openChat = (id: string) => {
    if (location.pathname !== chatPath(id)) history.pushState(null, "", chatPath(id))
    setPath(location.pathname)
    showScreen("chat")
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
    const onPopState = () => {
      setPath(location.pathname)
      const id = chatInLocation()
      if (id) setSelected(id)
      if (location.pathname === NEEDS_YOU_PATH) setScreen("waiting")
      else setScreen((s) => (s === "waiting" ? "chat" : s))
    }
    window.addEventListener("popstate", onPopState)
    return () => window.removeEventListener("popstate", onPopState)
  }, [])
  useEffect(() => {
    if (bots.length === 0 || bots.some((b) => b.id === selected)) return
    // Replace, not push: otherwise Back returns to the missing chat, which opens the first chat again.
    if (!isSettingsPath(location.pathname) && location.pathname !== NEEDS_YOU_PATH)
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
  const entries = transcript?.entries ?? NO_ENTRIES
  const waiting = useLoad(() => api.waiting(), waitingKey(bots))
  const waitingN = waiting.data ? waitingCount(waiting.data) : 0
  const taskKey = tasksKey(bots)
  const needs = useLoad(() => api.needsYou(), taskKey)
  const moved = useLoad(() => api.moveNotice(), "")
  const needsN = (needs.data?.length ?? 0) + (pair.request ? 1 : 0)
  const chatName = (id: string) => bots.find((b) => b.id === id)?.name ?? "a deleted chat"

  // view is what the main area shows; only a chat has the side panel.
  const view =
    screen === "across"
      ? "across"
      : screen === "inbox" && bot?.team
        ? "inbox"
        : screen === "waiting" && waiting.data
          ? "waiting"
          : bot
            ? "chat"
            : "empty"
  // A drain that the updater didn't start is a settings restart.
  const pause = pauseLabel(update.state?.state === "waiting" || update.state?.state === "limit")

  if (path === "/pair") return <PairPage paired={session.phone && !session.lost} />
  if (session.lost) return <PairAgain />
  if (needsKey(session)) return <KeyPage keyFile={session.keyFile ?? ""} onSaved={phoneSession.recheck} />
  if (isSettingsPath(path) && rights.computer) {
    return (
      <>
        <SettingsPage
          top={<PairBanner request={pair.request} onDecided={pair.reload} />}
          pairRev={pairRev}
          section={sectionOfPath(path)}
          onSection={(s) => {
            history.replaceState(null, "", `/settings/${s}`)
            setPath(location.pathname)
          }}
          onBack={() => {
            history.pushState(null, "", selected ? chatPath(selected) : "/")
            setPath(location.pathname)
          }}
          onManageConnectors={() => setConnectors(true)}
          restartRev={restartRev}
        />
        <ConnectorsDialog open={connectors} onOpenChange={setConnectors} bots={agents} />
      </>
    )
  }

  return (
    <div className="bg-background text-foreground relative flex h-dvh overflow-clip">
      {narrow && drawer && (
        <div aria-hidden className="fixed inset-0 z-40 bg-black/40" onClick={() => setDrawer(false)} />
      )}
      <aside
        id="app-sidebar"
        aria-label="Chats"
        className={cn(
          "bg-sidebar text-sidebar-foreground flex shrink-0 flex-col",
          "max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-50 max-md:w-[min(20rem,85vw)]! max-md:pt-[env(safe-area-inset-top)] max-md:shadow-xl",
          narrow && !drawer && "hidden",
        )}
        style={{ width }}
        ref={closesDrawerOnEscape}
      >
        <div className="flex items-center px-3 py-3">
          <img src="/logo.svg" alt="Handful" className="size-7 rounded-[6px] dark:ring-1 dark:ring-white/10" />
        </div>
        {moved.data?.show && rights.computer && (
          <MoveNotice
            teams={teams}
            error={moved.error}
            onDone={() => void api.closeMoveNotice().then(moved.reload, (e: Error) => moved.setError(e.message))}
            onRename={(t, name) => void api.updateBot(t.id, { name }).catch((e: Error) => moved.setError(e.message))}
          />
        )}
        <nav className="flex flex-1 flex-col gap-0.5 overflow-y-auto px-2">
          <button
            type="button"
            aria-current={screen === "across" ? "page" : undefined}
            className={cn(
              "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm max-md:py-3",
              screen === "across"
                ? "bg-sidebar-foreground/10 font-medium"
                : "hover:bg-muted transition-colors duration-(--duration-fast)",
            )}
            onClick={() => showScreen("across")}
          >
            <LayoutDashboard className="size-4 shrink-0" />
            <span className="flex-1">Across teams</span>
            {needsN > 0 && (
              <Badge variant="attention" aria-label={`${needsN} need you`}>
                {needsN}
              </Badge>
            )}
          </button>
          <button
            type="button"
            aria-current={screen === "waiting" ? "page" : undefined}
            className={cn(
              "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm max-md:py-3",
              screen === "waiting"
                ? "bg-sidebar-foreground/10 font-medium"
                : "hover:bg-muted transition-colors duration-(--duration-fast)",
            )}
            onClick={() => showScreen("waiting")}
          >
            <Inbox className="size-4 shrink-0" />
            <span className="flex-1">Waiting for you</span>
            {waitingN > 0 && (
              <Badge variant="attention" aria-label={`${waitingN} waiting`}>
                {waitingN}
              </Badge>
            )}
          </button>
          <SectionTitle
            title="Bots"
            action="New bot"
            onAction={rights.computer ? () => setCreating(true) : undefined}
          />
          <BotList
            items={agents}
            selected={selected}
            unread={unread}
            onSelect={openChat}
            onReorder={(ids) => void reorder(ids)}
          />
          <SectionTitle
            title="Teams"
            action="New team"
            onAction={rights.computer ? () => setCreatingTeam(true) : undefined}
          />
          <BotList
            items={teams}
            selected={selected}
            unread={unread}
            onSelect={openChat}
            onReorder={(ids) => void reorder(ids)}
          />
          <SectionTitle
            title="Rooms"
            action="New room"
            onAction={rights.computer ? () => setCreatingRoom(true) : undefined}
          />
          <BotList
            items={rooms}
            selected={selected}
            unread={unread}
            onSelect={openChat}
            onReorder={(ids) => void reorder(ids)}
          />
          {bots.length === 0 && rights.computer && (
            <Button variant="outline" size="sm" className="mt-2" onClick={() => setCreating(true)}>
              <Plus /> Create your first bot
            </Button>
          )}
        </nav>
        <SidebarFooter
          link={link}
          onConnectors={rights.computer ? () => setConnectors(true) : undefined}
          onSettings={rights.computer ? () => openSettingsPage("general") : undefined}
        />
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
        className="hover:bg-primary/30 focus-visible:bg-primary/40 w-1 shrink-0 cursor-col-resize border-l max-md:hidden"
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

      <main className="flex min-w-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col">
          <PairBanner request={pair.request} onDecided={pair.reload} />
          {error && (
            <div className="bg-destructive/10 text-destructive flex items-center justify-between px-4 py-2 text-sm">
              {error}
              <Button size="xs" variant="ghost" onClick={() => setError(null)}>
                Dismiss
              </Button>
            </div>
          )}
          {pausedUntil && (
            <div className="bg-attention-surface px-4 py-2 text-sm">
              Claude usage limit reached. New turns wait until{" "}
              {new Date(pausedUntil).toLocaleString([], {
                weekday: "short",
                hour: "2-digit",
                minute: "2-digit",
              })}
              .
            </div>
          )}
          {view === "across" ? (
            <AcrossScreen
              menu={menu}
              needs={needs.data ?? []}
              pairRequest={pair.request}
              onPairDecided={pair.reload}
              teams={teams}
              version={taskKey}
              chatName={chatName}
              onOpen={openChat}
              onChange={needs.reload}
            />
          ) : view === "inbox" && bot?.team ? (
            <InboxScreen
              menu={menu}
              key={bot.id}
              team={bot}
              version={`${transcript?.rev ?? 0}:${taskKey}`}
              teams={teams}
              botName={(id) => bots.find((b) => b.id === id)?.name ?? "a deleted bot"}
              usageLimit={Boolean(pausedUntil)}
              draining={draining}
              onBack={() => setScreen("chat")}
            />
          ) : view === "waiting" && waiting.data ? (
            <WaitingView
              menu={menu}
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
              <header className="flex h-12 items-center gap-2 border-b px-4 max-md:h-14 max-md:px-3">
                {menu}
                <span
                  className="max-w-[60%] shrink-0 truncate font-medium max-md:max-w-[40%] max-md:min-w-0 max-md:shrink"
                  title={bot.name}
                >
                  {bot.name}
                </span>
                <span
                  className={cn(
                    "text-muted-foreground min-w-0 flex-1 truncate text-sm max-md:hidden",
                    bot.team && !panel && "max-xl:hidden",
                  )}
                  title={subtitle}
                >
                  {subtitle}
                </span>
                {bot.team && !panel && (
                  <NowChip
                    key={bot.id}
                    team={bot}
                    version={`${transcript?.rev ?? 0}:${taskKey}:${bot.team.dailyBudget ?? ""}`}
                    onOpen={() => showTab("board")}
                  />
                )}
                {bot.team && !panel && <span className="flex-1 xl:hidden max-md:hidden" />}
                {bot.effective && rights.computer && (
                  <ModelChip effective={bot.effective} aliases={aliases} onClick={() => showTab("settings")} />
                )}
                {bot.team && defaults && rights.computer && (
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
                  ref={panelButton}
                  className="shrink-0 max-md:size-11"
                  aria-label="Toggle panel"
                  onClick={() => setPanel((v) => !v)}
                >
                  <PanelRight />
                </Button>
              </header>
              <div className="flex min-h-0 flex-1">
                <Chat
                  bot={bot}
                  entries={entries}
                  rev={transcript?.rev ?? 0}
                  older={!(transcript?.complete ?? true)}
                  onLoadOlder={() => void loadOlder(bot.id)}
                  onReveal={(entryId) => reveal(bot.id, entryId)}
                  status={transcriptStatus.get(bot.id) ?? "unloaded"}
                  answering={answering.get(bot.id) ?? null}
                  draining={draining}
                  pause={pause}
                  pill={update.state && shown(update.state) ? <UpdatePill update={update} bots={bots} /> : null}
                  onAnswer={(q) => setAnswer(bot.id, q)}
                  mentionables={
                    bot.kind === "room"
                      ? (bot.members ?? [])
                      : agents.filter((a) => a.id !== bot.id).map((a) => ({ id: a.id, name: a.name }))
                  }
                />
              </div>
            </>
          ) : (
            <div className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
              Create a bot to start.
            </div>
          )}
          {view !== "chat" && update.state && shown(update.state) && (
            <div className="@container">
              <UpdatePill update={update} bots={bots} floating />
            </div>
          )}
        </div>
        {view === "chat" && bot && panel && (
          <aside
            aria-label="Chat panel"
            className="bg-sidebar text-sidebar-foreground flex w-80 shrink-0 flex-col border-l max-lg:absolute max-lg:inset-y-0 max-lg:right-0 max-lg:z-30 max-lg:shadow-xl max-md:fixed max-md:inset-0 max-md:z-50 max-md:w-full max-md:border-l-0 max-md:pt-[env(safe-area-inset-top)]"
            ref={panelKeys}
          >
            <BotPanel
              bot={bot}
              agents={agents}
              version={transcript?.rev ?? 0}
              onDeleted={() => setSelected(null)}
              show={show}
              teams={teams}
              onOpenInbox={() => setScreen("inbox")}
              paused={draining ? pause : null}
              onClose={() => setPanel(false)}
            />
          </aside>
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
