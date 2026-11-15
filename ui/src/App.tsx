import { useCallback, useEffect, useRef, useState, type DragEvent, type KeyboardEvent } from "react"
import {
  ArrowRight,
  Inbox,
  LayoutDashboard,
  AlertTriangle,
  MessageCircleQuestion,
  Menu,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRight,
  PauseCircle,
  Plug,
  Plus,
  Search,
  Settings as SettingsIcon,
  ShieldQuestion,
} from "lucide-react"
import { BotHuesProvider, ChatIcon } from "@/components/bot-colors"
import { api, isNotFound, type Bot, type Entry } from "@/lib/api"
import { type Link, linkLabel } from "@/lib/connection"
import { useHandful } from "@/lib/store"
import { Chat } from "@/components/chat"
import { BotPanel, useLoad } from "@/components/bot-panel"
import { useNarrow, useWindowWidth } from "@/lib/narrow"
import { BUILDER } from "@/lib/starts"
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
import { matchesName, modKey } from "@/lib/search"
import {
  clampPanel,
  clampSidebar,
  loadStored,
  loadWidth,
  PANEL,
  PANEL_WIDTH_KEY,
  panelLayout,
  saveStored,
  SEPARATOR,
  SIDEBAR,
  SIDEBAR_COLLAPSED_KEY,
  SIDEBAR_WIDTH_KEY,
  STEP,
} from "@/lib/layout"
import { chatPlace, chatUrl, lastChat, panelTabs, saveLastChat, type PanelTab, type Reading } from "@/lib/place"
import { Reader } from "@/components/reader"

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
              <ChatIcon chat={a} />
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
              <ChatIcon chat={a} />
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
  // onKeyDown moves the item; without it the item does not move.
  onKeyDown?: (e: KeyboardEvent<HTMLButtonElement>) => void
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      onKeyDown={onKeyDown}
      aria-keyshortcuts={onKeyDown && "Alt+ArrowUp Alt+ArrowDown"}
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
// Without onReorder, as while the sidebar is filtered, they do not move.
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
  onReorder?: (ids: string[]) => void
}) {
  const [dragId, setDragId] = useState<string | null>(null)
  const [insertAt, setInsertAt] = useState<number | null>(null)
  // move takes the item at from and puts it before the item at index to (to may equal items.length).
  const move = (from: number, to: number) => {
    const ids = items.map((b) => b.id)
    const [id] = ids.splice(from, 1)
    ids.splice(to > from ? to - 1 : to, 0, id)
    if (ids.join() !== items.map((b) => b.id).join()) onReorder?.(ids)
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
          draggable={onReorder !== undefined}
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
            onKeyDown={
              onReorder &&
              ((e) => {
                if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return
                e.preventDefault()
                const to = e.key === "ArrowUp" ? i - 1 : i + 2
                if (to >= 0 && to <= items.length) move(i, to)
              })
            }
          />
        </div>
      ))}
    </div>
  )
}

// MAC says the shortcuts use ⌘, not Ctrl.
const MAC = /Mac|iPhone|iPad/.test(navigator.userAgent)
const SIDEBAR_SHORTCUT = MAC ? "⌘B" : "Ctrl+B"
const SIDEBAR_KEYS = MAC ? "Meta+B" : "Control+B"

// READER is the history state of an address that opened the reader, so that closing it goes
// back instead of adding an address.
const READER = { reader: true }

// here is the address bar's path and query.
function here() {
  return { path: location.pathname, search: location.search }
}

// ResizeHandle is the separator at one edge of the element it controls, and sets that element's
// width: by drag, by the arrow keys STEP pixels a press, and back to initial on a double-click.
// edge is the side of the element the handle is on.
function ResizeHandle({
  label,
  controls,
  edge,
  width,
  min,
  max,
  initial,
  onWidth,
}: {
  label: string
  controls: string
  edge: "left" | "right"
  width: number
  min: number
  max: number
  initial: number
  onWidth: (w: number) => void
}) {
  const dragging = useRef(false)
  const set = (w: number) => onWidth(Math.round(Math.min(max, Math.max(min, w))))
  const grows = edge === "right" ? "ArrowRight" : "ArrowLeft"
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-controls={controls}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={width}
      tabIndex={0}
      title="Drag to resize; double-click to reset"
      className="hover:bg-primary/30 focus-visible:bg-primary/40 w-1 shrink-0 cursor-col-resize border-l max-md:hidden"
      onPointerDown={(e) => {
        e.preventDefault()
        e.currentTarget.setPointerCapture(e.pointerId)
        dragging.current = true
      }}
      onPointerMove={(e) => {
        if (dragging.current) set(edge === "right" ? e.clientX : window.innerWidth - e.clientX)
      }}
      onPointerUp={(e) => {
        dragging.current = false
        e.currentTarget.releasePointerCapture(e.pointerId)
      }}
      onKeyDown={(e) => {
        if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return
        e.preventDefault()
        set(width + (e.key === grows ? STEP : -STEP))
      }}
      onDoubleClick={() => set(initial)}
    />
  )
}

// SidebarFilter narrows the sidebar's chats by name. Escape clears a filter that is set and goes
// no further, so the drawer closes only on the next Escape.
function SidebarFilter({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const clearsOnEscape = useCallback(
    (el: HTMLInputElement | null) => {
      if (!el) return
      const onKey = (e: globalThis.KeyboardEvent) => {
        if (e.key !== "Escape" || el.value === "") return
        e.stopPropagation()
        onChange("")
      }
      el.addEventListener("keydown", onKey)
      return () => el.removeEventListener("keydown", onKey)
    },
    [onChange],
  )
  return (
    <div className="px-2 pb-1">
      <Input
        ref={clearsOnEscape}
        aria-label="Filter chats"
        placeholder="Filter chats"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="max-md:h-11"
      />
    </div>
  )
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
  const [width, setWidth] = useState(() => loadWidth(SIDEBAR_WIDTH_KEY, clampSidebar, SIDEBAR.initial))
  useEffect(() => saveStored(SIDEBAR_WIDTH_KEY, String(width)), [width])
  // collapsed hides the sidebar above md, until its button or Mod+B shows it again.
  const [collapsed, setCollapsed] = useState(() => loadStored(SIDEBAR_COLLAPSED_KEY) === "1")
  useEffect(() => saveStored(SIDEBAR_COLLAPSED_KEY, collapsed ? "1" : "0"), [collapsed])
  // panelWidth is the width the user set; the panel shows narrower where the chat needs room.
  const [panelWidth, setPanelWidth] = useState(() => loadWidth(PANEL_WIDTH_KEY, clampPanel, PANEL.initial))
  useEffect(() => saveStored(PANEL_WIDTH_KEY, String(panelWidth)), [panelWidth])
  // where is the address bar. /settings… shows Settings in the main area; a chat's address says
  // what is open in it (place.ts).
  const [where, setWhere] = useState(here)
  // go moves the address bar to an address of the app. A replaced address keeps its history
  // state.
  const go = useCallback((to: string, how: "push" | "replace" = "push", state: unknown = null) => {
    if (to !== location.pathname + location.search) {
      if (how === "push") history.pushState(state, "", to)
      else history.replaceState(history.state, "", to)
    }
    setWhere(here())
  }, [])
  const [selected, setSelected] = useState<string | null>(
    () => chatPlace(location.pathname, location.search)?.chat ?? null,
  )
  const [creating, setCreating] = useState(false)
  const [creatingRoom, setCreatingRoom] = useState(false)
  const [creatingTeam, setCreatingTeam] = useState(false)
  const [connectors, setConnectors] = useState(false)
  const narrow = useNarrow()
  const windowWidth = useWindowWidth()
  const addressed = chatPlace(where.path, where.search)
  // place is what the address says is open in the selected chat.
  const place = addressed?.chat === selected ? addressed : null
  const lay = panelLayout(windowWidth, narrow || collapsed ? 0 : width + SEPARATOR, panelWidth)
  // over says the panel covers the chat, so it closes on Escape and on a choice.
  const over = lay.mode !== "inline"
  // The panel starts open where it fits beside the chat, and where the address names its tab.
  const [panel, setPanel] = useState(() => !over || Boolean(place?.tab))
  // drawer: on a narrow window the sidebar is a drawer over the page; above md it is always shown.
  const [drawer, setDrawer] = useState(false)
  const [filter, setFilter] = useState("")
  // search is the open chat search: its chat, and a count that each open request raises.
  const [search, setSearch] = useState<{ chat: string; n: number } | null>(null)
  // listsRev changes when the reader deletes an item, so the panel's lists load again.
  const [listsRev, setListsRev] = useState(0)
  const menuButton = useRef<HTMLButtonElement>(null)
  const collapseButton = useRef<HTMLButtonElement>(null)
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
  ) : collapsed ? (
    <Button
      ref={menuButton}
      size="icon"
      variant="ghost"
      className="-ml-2 shrink-0"
      aria-label="Open sidebar"
      title={`Open sidebar (${SIDEBAR_SHORTCUT})`}
      aria-keyshortcuts={SIDEBAR_KEYS}
      aria-expanded={false}
      aria-controls="app-sidebar"
      onClick={() => setCollapsed(false)}
    >
      <PanelLeftOpen />
    </Button>
  ) : null
  // The keyboard goes into the drawer when it opens, and back to the menu button when it closes.
  const drawerWasOpen = useRef(false)
  useEffect(() => {
    if (drawer) sidebar.current?.querySelector<HTMLElement>("nav button")?.focus()
    else if (drawerWasOpen.current) menuButton.current?.focus()
    drawerWasOpen.current = drawer
  }, [drawer])
  // A sidebar button that hides itself takes the keyboard with it; it goes to the button that
  // undoes the change.
  const wasCollapsed = useRef(collapsed)
  useEffect(() => {
    if (collapsed === wasCollapsed.current) return
    wasCollapsed.current = collapsed
    // A hidden sidebar can still hold the focus until the browser's next frame.
    const active = document.activeElement
    const lost = !active || active === document.body || (collapsed && sidebar.current?.contains(active))
    if (lost) (collapsed ? menuButton : collapseButton).current?.focus()
  }, [collapsed])
  // Mod+B, matched by the key's place so that it works on any layout, shows or hides the sidebar.
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (!modKey(e, "KeyB", MAC)) return
      // A dialog holds the keyboard; the sidebar behind it stays as it is.
      if (e.target instanceof Element && e.target.closest('[role="dialog"], [role="alertdialog"]')) return
      e.preventDefault()
      if (narrow) setDrawer((d) => !d)
      else setCollapsed((c) => !c)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [narrow])
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
  // closePanel closes the panel and takes its tab out of the address.
  const closePanel = useCallback(() => {
    setPanel(false)
    const p = chatPlace(location.pathname, location.search)
    if (p?.tab) go(chatUrl({ ...p, tab: null }), "replace")
  }, [go])
  // The keyboard goes into the panel when it opens over the chat, or when the control that opened
  // it left the page (the Now chip); it goes back to the panel button when the panel closes and
  // took the keyboard with it.
  const panelButton = useRef<HTMLButtonElement>(null)
  const panelBox = useRef<HTMLElement | null>(null)
  const panelWasOpen = useRef(panel)
  useEffect(() => {
    const lost = !document.activeElement || document.activeElement === document.body
    if (panel && !panelWasOpen.current && (over || lost))
      panelBox.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus()
    if (!panel && panelWasOpen.current && lost) panelButton.current?.focus()
    panelWasOpen.current = panel
  }, [panel, over])
  // panelKeys makes Escape inside the panel close it where it covers the chat.
  const panelKeys = useCallback(
    (el: HTMLElement | null) => {
      panelBox.current = el
      if (!el || !over) return
      const onKey = (e: globalThis.KeyboardEvent) => {
        if (e.key === "Escape" && !e.defaultPrevented) closePanel()
      }
      el.addEventListener("keydown", onKey)
      return () => el.removeEventListener("keydown", onKey)
    },
    [over, closePanel],
  )
  // screen is what the main area shows: the selected chat, its team's inbox, Waiting for you or Across teams.
  const [screen, setScreen] = useState<"chat" | "inbox" | "waiting" | "across">(() =>
    location.pathname === NEEDS_YOU_PATH ? "waiting" : "chat",
  )
  const showTab = (tab: PanelTab) => {
    if (!selected) return
    setPanel(true)
    go(chatUrl({ chat: selected, tab }))
    // The settings tab renders after this click; then its model select takes focus.
    if (tab === "settings") requestAnimationFrame(() => document.getElementById("bot-model")?.focus())
  }
  const defaults = useDefaults()
  const aliases = defaults?.modelAliases ?? FALLBACK_ALIASES

  // showScreen shows a screen of the main area; in a narrow window the drawer closes on the choice.
  // Waiting for you has its own address; another screen leaves it, and Settings, for the chat's.
  const showScreen = (s: typeof screen) => {
    setScreen(s)
    setDrawer(false)
    if (s === "waiting") {
      if (location.pathname !== NEEDS_YOU_PATH) go(NEEDS_YOU_PATH)
    } else if (location.pathname === NEEDS_YOU_PATH || isSettingsPath(location.pathname))
      go(selected ? chatUrl({ chat: selected }) : "/")
  }
  // openChat opens a chat, with the panel on tab when it is given. Without it the panel keeps
  // its tab.
  const openChat = (id: string, asked: PanelTab | null = panel ? (place?.tab ?? null) : null) => {
    const target = bots.find((b) => b.id === id)
    const tab = target && panelTabs(target, rights.computer).some((t) => t.tab === asked) ? asked : null
    go(chatUrl({ chat: id, tab }))
    if (tab) setPanel(true)
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
      openChat((e as CustomEvent<string>).detail, "settings")
      requestAnimationFrame(() => document.getElementById("bot-model")?.focus())
    }
    window.addEventListener(OPEN_SETTINGS, open)
    return () => window.removeEventListener(OPEN_SETTINGS, open)
  })

  useEffect(() => {
    const onPopState = () => {
      setWhere(here())
      const p = chatPlace(location.pathname, location.search)
      if (p) setSelected(p.chat)
      if (p?.tab) setPanel(true)
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
      go(chatUrl({ chat: bots[0].id }), "replace")
    setSelected(bots[0].id)
  }, [bots, selected, go])
  useEffect(() => select(selected), [selected, select])
  // Settings close back to the last chat shown.
  useEffect(() => {
    const p = chatPlace(where.path, where.search)
    if (p) saveLastChat(p)
  }, [where])

  // closeSettings goes back to the last chat shown, which a full load of Settings did not select.
  const closeSettings = () => {
    const p = lastChat()
    if (!p) return go(selected ? chatUrl({ chat: selected }) : "/")
    go(chatUrl(p))
    setSelected(p.chat)
  }
  const bot = bots.find((b) => b.id === selected)
  const subtitle = bot ? chatSubtitle(bot) : ""
  const agents = bots.filter((b) => b.kind !== "room")
  const rooms = bots.filter((b) => b.kind === "room" && !b.team)
  const teams = bots.filter((b) => b.team)
  const named = (list: Bot[]) => list.filter((b) => matchesName(b.name, filter))
  const sections = [
    { title: "Bots", action: "New bot", items: named(agents), onAction: () => setCreating(true) },
    { title: "Teams", action: "New team", items: named(teams), onAction: () => setCreatingTeam(true) },
    { title: "Rooms", action: "New room", items: named(rooms), onAction: () => setCreatingRoom(true) },
  ]
  const filtered = filter.trim() !== ""
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
    isSettingsPath(where.path) && rights.computer
      ? "settings"
      : screen === "across"
        ? "across"
        : screen === "inbox" && bot?.team
          ? "inbox"
          : screen === "waiting" && waiting.data
            ? "waiting"
            : bot
              ? "chat"
              : "empty"
  const reading = view === "chat" ? (place?.reading ?? null) : null
  // openReader shows an artifact or a skill over the chat. A panel over the chat closes, also in
  // the address that Back returns to, and an inline panel stays on its tab.
  const openReader = (r: Reading) => {
    if (!selected) return
    if (over) closePanel()
    const to = chatUrl({ chat: selected, tab: over ? null : (place?.tab ?? null), reading: r })
    if (reading) go(to, "replace")
    else go(to, "push", READER)
  }
  const closeReader = () => {
    if ((history.state as typeof READER | null)?.reader) return history.back()
    const p = chatPlace(location.pathname, location.search)
    if (p) go(chatUrl({ ...p, reading: null }), "replace")
  }
  // post is a post that a link opened. The chat shows it once the server has it; a post the chat
  // doesn't have leaves the address and says so.
  const post = view === "chat" ? (place?.post ?? null) : null
  const [postFound, setPostFound] = useState<string | null>(null)
  useEffect(() => {
    if (!selected || !post) return
    let live = true
    api.entry(selected, post).then(
      () => live && setPostFound(`${selected}:${post}`),
      (e: Error) => {
        if (!live) return
        if (!isNotFound(e)) return setError(e.message)
        const p = chatPlace(location.pathname, location.search)
        if (p) go(chatUrl({ ...p, post: null }), "replace")
        setError("Not found. The linked message was deleted or is in another chat.")
      },
    )
    return () => {
      live = false
    }
  }, [selected, post, go, setError])
  const searchN = view === "chat" && search?.chat === selected ? search.n : 0
  const openSearch = useCallback(
    () => setSearch((s) => (selected ? { chat: selected, n: (s?.chat === selected ? s.n : 0) + 1 } : null)),
    [selected],
  )
  useEffect(() => {
    if (view !== "chat") return
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (!modKey(e, "KeyF", MAC)) return
      e.preventDefault()
      openSearch()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [view, openSearch])
  // A drain that the updater didn't start is a settings restart.
  const pause = pauseLabel(update.state?.state === "waiting" || update.state?.state === "limit")

  if (where.path === "/pair") return <PairPage paired={session.phone && !session.lost} />
  if (session.lost) return <PairAgain />
  if (needsKey(session)) return <KeyPage keyFile={session.keyFile ?? ""} onSaved={phoneSession.recheck} />

  const layout = (
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
          (narrow ? !drawer : collapsed) && "hidden",
        )}
        style={{ width }}
        ref={closesDrawerOnEscape}
      >
        <div className="flex items-center justify-between px-3 py-3">
          <img src="/logo.svg" alt="Handful" className="size-7 rounded-[6px] dark:ring-1 dark:ring-white/10" />
          <Button
            ref={collapseButton}
            size="icon-sm"
            variant="ghost"
            className="max-md:hidden"
            aria-label="Collapse sidebar"
            title={`Collapse sidebar (${SIDEBAR_SHORTCUT})`}
            aria-keyshortcuts={SIDEBAR_KEYS}
            aria-expanded
            aria-controls="app-sidebar"
            onClick={() => setCollapsed(true)}
          >
            <PanelLeftClose />
          </Button>
        </div>
        {moved.data?.show && rights.computer && (
          <MoveNotice
            teams={teams}
            error={moved.error}
            onDone={() => void api.closeMoveNotice().then(moved.reload, (e: Error) => moved.setError(e.message))}
            onRename={(t, name) => void api.updateBot(t.id, { name }).catch((e: Error) => moved.setError(e.message))}
          />
        )}
        {bots.length > 0 && <SidebarFilter value={filter} onChange={setFilter} />}
        <nav className="flex flex-1 flex-col gap-0.5 overflow-y-auto px-2">
          <button
            type="button"
            aria-current={view === "across" ? "page" : undefined}
            className={cn(
              "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm max-md:py-3",
              view === "across"
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
            aria-current={view === "waiting" ? "page" : undefined}
            className={cn(
              "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm max-md:py-3",
              view === "waiting"
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
          {sections.map(
            (sec) =>
              (!filtered || sec.items.length > 0) && (
                <div key={sec.title} className="flex flex-col gap-0.5">
                  <SectionTitle
                    title={sec.title}
                    action={sec.action}
                    onAction={rights.computer ? sec.onAction : undefined}
                  />
                  <BotList
                    items={sec.items}
                    selected={view === "settings" ? null : selected}
                    unread={unread}
                    onSelect={(id) => openChat(id)}
                    onReorder={filtered ? undefined : (ids) => void reorder(ids)}
                  />
                </div>
              ),
          )}
          {filtered && sections.every((sec) => sec.items.length === 0) && (
            <p className="text-muted-foreground mt-3 px-2 text-sm">No matches</p>
          )}
          {bots.length === 0 && rights.computer && (
            <Button variant="outline" size="sm" className="mt-2" onClick={() => setCreating(true)}>
              <Plus /> Create your first bot
            </Button>
          )}
          {bots.length === 1 && bots[0].role === BUILDER && (
            <Button variant="outline" size="sm" className="mt-2" onClick={() => openChat(bots[0].id)}>
              <ArrowRight /> Set up your bots with {bots[0].name}
            </Button>
          )}
        </nav>
        <SidebarFooter
          link={link}
          onConnectors={rights.computer ? () => setConnectors(true) : undefined}
          onSettings={
            rights.computer
              ? () => {
                  setDrawer(false)
                  openSettingsPage("general")
                }
              : undefined
          }
        />
      </aside>
      {!collapsed && (
        <ResizeHandle
          label="Resize sidebar"
          controls="app-sidebar"
          edge="right"
          width={width}
          min={SIDEBAR.min}
          max={SIDEBAR.max}
          initial={SIDEBAR.initial}
          onWidth={setWidth}
        />
      )}

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
          {view === "settings" ? (
            <SettingsPage
              menu={menu}
              pairRev={pairRev}
              section={sectionOfPath(where.path)}
              onSection={(s) => go(`/settings/${s}`, "replace")}
              onClose={closeSettings}
              onManageConnectors={() => setConnectors(true)}
              restartRev={restartRev}
            />
          ) : view === "across" ? (
            <AcrossScreen
              menu={menu}
              needs={needs.data ?? []}
              pairRequest={pair.request}
              onPairDecided={pair.reload}
              teams={teams}
              version={taskKey}
              chatName={chatName}
              onOpen={(id) => openChat(id)}
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
              onOpen={(id) => openChat(id)}
              onAnswer={(chatId, q) => {
                setAnswer(chatId, q)
                openChat(chatId)
                requestAnimationFrame(() => document.getElementById("composer-input")?.focus())
              }}
              onChange={waiting.reload}
            />
          ) : bot ? (
            <div className="relative flex min-h-0 flex-1 flex-col">
              <header
                inert={reading !== null}
                className="flex h-12 items-center gap-2 border-b px-4 max-md:h-14 max-md:px-3"
              >
                {/* The reader covers this header and shows the menu button, which has one ref. */}
                {!reading && menu}
                <ChatIcon chat={bot} />
                <span className="max-w-[60%] shrink-0 truncate font-medium max-md:max-w-[30%]" title={bot.name}>
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
                  className="shrink-0 max-md:size-11"
                  aria-label="Search this chat"
                  title={`Search this chat (${MAC ? "⌘F" : "Ctrl+F"})`}
                  aria-keyshortcuts={MAC ? "Meta+F" : "Control+F"}
                  aria-expanded={searchN > 0}
                  onClick={openSearch}
                >
                  <Search />
                </Button>
                <Button
                  size="icon"
                  variant="ghost"
                  ref={panelButton}
                  className="shrink-0 max-md:size-11"
                  aria-label="Toggle panel"
                  aria-expanded={panel}
                  aria-controls="chat-panel"
                  onClick={() => (panel ? closePanel() : setPanel(true))}
                >
                  <PanelRight />
                </Button>
              </header>
              <div inert={reading !== null} className="flex min-h-0 flex-1">
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
                  search={searchN}
                  onCloseSearch={() => setSearch(null)}
                  post={post && postFound === `${bot.id}:${post}` ? post : null}
                  mentionables={
                    bot.kind === "room"
                      ? (bot.members ?? [])
                      : agents.filter((a) => a.id !== bot.id).map((a) => ({ id: a.id, name: a.name }))
                  }
                />
              </div>
              {reading && (
                <Reader
                  key={`${reading.kind}:${reading.id}`}
                  reading={reading}
                  chat={bot.id}
                  agents={agents}
                  teams={teams}
                  menu={menu}
                  onClose={closeReader}
                  onGone={() => {
                    closeReader()
                    setError("Not found. The linked artifact or skill was deleted or never existed.")
                  }}
                  onDeleted={() => {
                    closeReader()
                    setListsRev((n) => n + 1)
                  }}
                />
              )}
            </div>
          ) : (
            <div className="flex flex-1 flex-col">
              {menu && <header className="flex h-12 items-center px-4 max-md:h-14 max-md:px-3">{menu}</header>}
              <div className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
                Create a bot to start.
              </div>
            </div>
          )}
          {view !== "chat" && update.state && shown(update.state) && (
            <div className="@container">
              <UpdatePill update={update} bots={bots} floating />
            </div>
          )}
        </div>
        {view === "chat" && bot && panel && (
          <>
            {!over && (
              <ResizeHandle
                label="Resize panel"
                controls="chat-panel"
                edge="left"
                width={lay.width}
                min={PANEL.min}
                max={lay.max}
                initial={PANEL.initial}
                onWidth={setPanelWidth}
              />
            )}
            <aside
              id="chat-panel"
              aria-label="Chat panel"
              style={lay.mode === "sheet" ? undefined : { width: lay.width }}
              className={cn(
                "bg-sidebar text-sidebar-foreground flex shrink-0 flex-col",
                lay.mode === "overlay" && "absolute inset-y-0 right-0 z-30 border-l shadow-xl",
                lay.mode === "sheet" && "fixed inset-0 z-50 w-full pt-[env(safe-area-inset-top)]",
              )}
              ref={panelKeys}
            >
              <BotPanel
                bot={bot}
                agents={agents}
                version={(transcript?.rev ?? 0) + listsRev}
                onDeleted={() => setSelected(null)}
                tab={place?.tab ?? null}
                onTab={(tab) => go(chatUrl({ ...(place ?? { chat: bot.id }), tab }))}
                onRead={openReader}
                teams={teams}
                onOpenInbox={() => setScreen("inbox")}
                paused={draining ? pause : null}
                onClose={over ? closePanel : undefined}
              />
            </aside>
          </>
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
  return <BotHuesProvider bots={bots}>{layout}</BotHuesProvider>
}
