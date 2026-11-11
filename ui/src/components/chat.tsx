import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
import Markdown, { type Components } from "react-markdown"
import remarkGfm from "remark-gfm"
import {
  AlertTriangle,
  ArrowDownLeft,
  ArrowDown,
  ArrowUp,
  ArrowUpRight,
  Bot as BotIcon,
  Check,
  ChevronRight,
  Copy,
  Loader2,
  Paperclip,
  Play,
  SmilePlus,
  ShieldQuestion,
  Sparkles,
  Square,
  TerminalSquare,
  Users,
  Wrench,
  X,
} from "lucide-react"
import { api, type BackgroundTask, type Bot, type Decision, type Entry, type Member, type Skill } from "@/lib/api"
import { applyCompletion, findMentionQuery, findSlashQuery, matchNames, type MentionQuery } from "@/lib/completion"
import { Popover } from "radix-ui"
import { REACTIONS, toggled } from "@/lib/reactions"
import { imageHost, isExternalLink, isLocalImage } from "@/lib/links"
import { taskCommand } from "@/lib/task-command"
import { answers, sendPlan } from "@/lib/send"
import { offeredSkills, teamSkillOf } from "@/lib/skills"
import { CREATE_SETUP, setupProposal, type SetupProposal } from "@/lib/setup"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Textarea } from "@/components/ui/textarea"
import { MediaGallery } from "@/components/media"
import { cn } from "@/lib/utils"
import { botColor, colorId } from "@/lib/bot-color"
import { FALLBACK_ALIASES, Hint, openSettings } from "@/components/model"
import { postTooltip, setupLine, teamSetupLine } from "@/lib/model"
import { AnswerChip, QuestionStrip } from "@/components/questions"
import { answersFor, questionOf, stateLabel, type Asked, type OpenQuestion } from "@/lib/questions"
import { ScrollFollower, ScrollMemory, type Intent, type Item } from "@/lib/scroll"
import { emptyText, type TranscriptStatus } from "@/lib/transcript"

function time(ms: number) {
  return new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

// markdownComponents opens links to other sites in a new tab, so the app
// stays open. The desktop app sends such links to the default browser. An
// image from another site doesn't load: it shows as a link to it, which the
// user can open.
const markdownComponents: Components = {
  img: ({ node: _node, src, alt, ...props }) => {
    const url = typeof src === "string" ? src : undefined
    if (isLocalImage(url, window.location.origin)) return <img src={url} alt={alt} {...props} />
    if (!url) return alt ? <span>{alt}</span> : null
    return (
      <a href={url} target="_blank" rel="noopener noreferrer" title={url}>
        {alt ? `${alt} ` : "Image "}({imageHost(url, window.location.origin)})
      </a>
    )
  },
  a: ({ node: _node, href, children, ...props }) =>
    isExternalLink(href, window.location.origin) ? (
      <a href={href} target="_blank" rel="noopener noreferrer" {...props}>
        {children}
      </a>
    ) : (
      <a href={href} {...props}>
        {children}
      </a>
    ),
}

export function MarkdownText({ text }: { text: string }) {
  return (
    <div className="markdown text-sm leading-relaxed">
      <Markdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
        {text}
      </Markdown>
    </div>
  )
}

function ActivityRow({ entry, chat }: { entry: Extract<Entry, { kind: "activity" }>; chat: Bot }) {
  const [open, setOpen] = useState(false)
  const b = entry.body
  return (
    <div className={cn("text-muted-foreground text-xs", b.subagent && "ml-5")}>
      <button
        type="button"
        className="hover:text-foreground flex w-full items-center gap-1.5 text-left"
        onClick={() => setOpen((v) => !v)}
        disabled={!b.output}
      >
        <ChevronRight
          className={cn("size-3 shrink-0 transition-transform", open && "rotate-90", !b.output && "opacity-0")}
        />
        {b.status === "running" ? (
          <Loader2 className="size-3 shrink-0 animate-spin" />
        ) : b.status === "error" ? (
          <X className="text-destructive size-3 shrink-0" />
        ) : (
          <Wrench className="size-3 shrink-0" />
        )}
        {b.bot && <span className={cn("shrink-0 font-medium", botColor(colorId(b, chat)).text)}>{b.bot}</span>}
        <span className="text-foreground/80 shrink-0 font-medium">{b.subagent ? `↳ ${b.tool}` : b.tool}</span>
        <span className="truncate font-mono">{b.summary}</span>
      </button>
      {open && b.output && (
        <pre className="bg-muted mt-1 ml-5 max-h-60 overflow-auto rounded-md p-2 font-mono text-[11px] whitespace-pre-wrap">
          {b.output}
        </pre>
      )}
    </div>
  )
}

function ApprovalCard({ entry }: { entry: Extract<Entry, { kind: "approval" }> }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const b = entry.body
  const decide = async (d: Decision) => {
    setBusy(true)
    setError(null)
    try {
      await api.resolveApproval(entry.id, d)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const setup = b.tool === CREATE_SETUP ? setupProposal(b.input) : null
  if (setup) {
    return (
      <Card size="sm" className="max-w-xl">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm">
            <ShieldQuestion className="size-4" /> Create this setup?
            {b.status !== "pending" && (
              <Badge variant={b.status === "allowed" ? "secondary" : "outline"} className="ml-auto">
                {b.status === "allowed" ? "confirmed" : b.status === "denied" ? "cancelled" : b.status}
              </Badge>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <SetupSummary setup={setup} />
          {error && <p className="text-destructive mt-2 text-xs">{error}</p>}
        </CardContent>
        {b.status === "pending" && (
          <CardFooter className="gap-2">
            <Button size="sm" disabled={busy} aria-label="Create this setup" onClick={() => void decide("once")}>
              <Check /> Create
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              aria-label="Cancel this setup"
              onClick={() => void decide("deny")}
            >
              Cancel
            </Button>
          </CardFooter>
        )}
      </Card>
    )
  }
  const input = typeof b.input === "object" && b.input !== null ? (b.input as Record<string, unknown>) : {}
  const command = typeof input.command === "string" ? input.command : JSON.stringify(b.input, null, 2)
  return (
    <Card size="sm" className="max-w-xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm">
          <ShieldQuestion className="size-4" /> {b.bot ? `Allow ${b.bot} to use ${b.tool}?` : `Allow ${b.tool}?`}
          {b.status !== "pending" && (
            <Badge variant={b.status === "allowed" ? "secondary" : "outline"} className="ml-auto">
              {b.status === "allowed" ? (b.decision === "always" ? "always allowed" : "allowed") : b.status}
            </Badge>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <pre className="bg-muted max-h-48 overflow-auto rounded-md p-2 font-mono text-xs whitespace-pre-wrap">
          {command}
        </pre>
        {error && <p className="text-destructive mt-2 text-xs">{error}</p>}
      </CardContent>
      {b.status === "pending" && (
        <CardFooter className="gap-2">
          <Button size="sm" disabled={busy} onClick={() => void decide("once")}>
            <Check /> Allow once
          </Button>
          <Button size="sm" variant="secondary" disabled={busy} onClick={() => void decide("always")}>
            Always allow {b.tool}
          </Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => void decide("deny")}>
            Deny
          </Button>
        </CardFooter>
      )}
    </Card>
  )
}

// SetupSummary lists everything that a setup creates, with each new bot's
// whole instructions, so that the user can read what they confirm. It has no
// scroll box of its own: nothing that the setup creates is out of sight.
function SetupSummary({ setup }: { setup: SetupProposal }) {
  const heading = "text-muted-foreground mb-1 text-xs font-medium uppercase"
  return (
    <div className="space-y-3 text-sm">
      {setup.bots.length > 0 && (
        <section>
          <h4 className={heading}>New bots</h4>
          <ul className="space-y-2">
            {setup.bots.map((bot, i) => (
              <li key={i}>
                <span className="font-medium">{bot.name}</span>
                <p className="text-muted-foreground text-xs whitespace-pre-wrap">{bot.instructions}</p>
                {setupLine(bot.model, bot.effort, FALLBACK_ALIASES) && (
                  <p className="text-muted-foreground text-xs">{setupLine(bot.model, bot.effort, FALLBACK_ALIASES)}</p>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
      {setup.teams.length > 0 && (
        <section>
          <h4 className={heading}>Teams</h4>
          <ul className="space-y-2">
            {setup.teams.map((team, i) => (
              <li key={i}>
                <span className="font-medium">{team.name}</span>, led by {team.lead}, with {team.members.join(", ")}
                {team.purpose && <p className="text-muted-foreground text-xs whitespace-pre-wrap">{team.purpose}</p>}
                {team.agreement && (
                  <p className="text-muted-foreground text-xs whitespace-pre-wrap">Agreement: {team.agreement}</p>
                )}
                {teamSetupLine(team.defaultModel, team.defaultEffort, FALLBACK_ALIASES) && (
                  <p className="text-muted-foreground text-xs">
                    {teamSetupLine(team.defaultModel, team.defaultEffort, FALLBACK_ALIASES)}
                  </p>
                )}
                {team.goalBudget !== undefined && (
                  <p className="text-muted-foreground text-xs">{team.goalBudget} turns per task</p>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
      {setup.rooms.length > 0 && (
        <section>
          <h4 className={heading}>Rooms</h4>
          <ul className="space-y-2">
            {setup.rooms.map((room, i) => (
              <li key={i}>
                <span className="font-medium">{room.name}</span>, with {room.members.join(", ")}
                {room.purpose && <p className="text-muted-foreground text-xs whitespace-pre-wrap">{room.purpose}</p>}
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}

function EventLine({ entry }: { entry: Extract<Entry, { kind: "event" }> }) {
  const b = entry.body
  if (b.type === "context-cleared") {
    return (
      <div className="text-muted-foreground my-2 flex items-center gap-3 text-xs">
        <span className="bg-border h-px flex-1" />
        <span>{b.text}</span>
        <span className="bg-border h-px flex-1" />
      </div>
    )
  }
  if (b.type === "model-error" || b.type === "model-mismatch") {
    const error = b.type === "model-error"
    return (
      <div
        role="alert"
        className={cn(
          "flex flex-wrap items-center justify-center gap-1.5 text-xs",
          error ? "text-destructive" : "text-amber-700 dark:text-amber-400",
        )}
      >
        <AlertTriangle className="size-3 shrink-0" aria-hidden />
        <span className="max-w-xl">{b.text}</span>
        {b.botId && (
          <Button size="xs" variant="outline" onClick={() => openSettings(b.botId ?? "")}>
            Open settings
          </Button>
        )}
        <span className="opacity-60">· {time(entry.createdAt)}</span>
      </div>
    )
  }
  const failed = b.type === "turn-failed"
  const text = b.type === "renamed" ? `Renamed from ${b.from} to ${b.to}` : (b.text ?? b.type)
  return (
    <div
      className={cn(
        "flex items-center justify-center gap-1.5 text-xs",
        failed ? "text-destructive" : "text-muted-foreground",
      )}
    >
      {failed && <AlertTriangle className="size-3" />}
      <span className="max-w-xl truncate" title={text}>
        {text}
      </span>
      <span className="opacity-60">· {time(entry.createdAt)}</span>
    </div>
  )
}

function taskLabel(t: BackgroundTask) {
  return t.bot ? `${t.bot}: ${t.description}` : t.description
}

// CopyButton copies a message's markdown to the clipboard and shows the
// result for a moment. It stays hidden until its message is hovered or focused.
function CopyButton({ text }: { text: string }) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle")
  useEffect(() => {
    if (state === "idle") return
    const timer = setTimeout(() => setState("idle"), 2000)
    return () => clearTimeout(timer)
  }, [state])
  const copy = () =>
    navigator.clipboard.writeText(text).then(
      () => setState("copied"),
      () => setState("failed"),
    )
  const label = state === "copied" ? "Copied" : state === "failed" ? "Copy failed" : "Copy message"
  return (
    <Button
      size="icon-xs"
      variant="ghost"
      aria-label={label}
      title={label}
      className={cn(
        "text-muted-foreground opacity-0 transition-opacity group-focus-within/message:opacity-100 group-hover/message:opacity-100 focus-visible:opacity-100",
        state !== "idle" && "opacity-100",
      )}
      onClick={() => void copy()}
    >
      {state === "copied" ? <Check /> : state === "failed" ? <X /> : <Copy />}
    </Button>
  )
}

// Reactions shows the user's reactions on a message as chips, with a picker
// to add one. A click on a chip removes it. The message updates when the
// server sends the changed entry; reactions never wake a bot.
function Reactions({ entryId, reactions, copyText }: { entryId: string; reactions?: string[]; copyText?: string }) {
  const [open, setOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const set = async (emoji: string) => {
    setOpen(false)
    setError(null)
    try {
      await api.react(entryId, emoji, toggled(reactions, emoji))
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <div className="flex flex-wrap items-center gap-1">
      {reactions?.map((emoji) => (
        <Button
          key={emoji}
          size="xs"
          variant="outline"
          aria-pressed
          aria-label={`Remove ${emoji} reaction`}
          title={`Remove ${emoji} reaction`}
          className="rounded-full"
          onClick={() => void set(emoji)}
        >
          <span>{emoji}</span> <span className="text-muted-foreground">1</span>
        </Button>
      ))}
      {copyText && <CopyButton text={copyText} />}
      <Popover.Root open={open} onOpenChange={setOpen}>
        <Popover.Trigger asChild>
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="Add reaction"
            title="Add reaction"
            className={cn(
              "text-muted-foreground opacity-0 transition-opacity group-focus-within/message:opacity-100 group-hover/message:opacity-100 focus-visible:opacity-100",
              open && "opacity-100",
            )}
          >
            <SmilePlus />
          </Button>
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            sideOffset={4}
            aria-label="Reactions"
            className="bg-popover text-popover-foreground z-50 flex gap-0.5 rounded-lg border p-1 shadow-md"
          >
            {REACTIONS.map((emoji) => {
              const on = reactions?.includes(emoji) ?? false
              return (
                <Button
                  key={emoji}
                  size="icon-sm"
                  variant={on ? "secondary" : "ghost"}
                  aria-pressed={on}
                  aria-label={on ? `Remove ${emoji} reaction` : `React with ${emoji}`}
                  className="text-base"
                  onClick={() => void set(emoji)}
                >
                  {emoji}
                </Button>
              )
            })}
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
      {error && <span className="text-destructive text-xs">{error}</span>}
    </div>
  )
}

function AgentMessage({ entry }: { entry: Extract<Entry, { kind: "agent" }> }) {
  const b = entry.body
  const incoming = b.direction === "in"
  return (
    <div className={cn("group/message flex flex-col", incoming ? "items-start" : "items-end")}>
      <div className="border-primary/30 bg-muted/40 max-w-[75%] rounded-xl border border-dashed px-3.5 py-2">
        <div className="text-muted-foreground mb-1 flex items-center gap-1.5 text-xs">
          {incoming ? <ArrowDownLeft className="size-3" /> : <ArrowUpRight className="size-3" />}
          {incoming ? "From" : "To"} <span className={cn("font-semibold", botColor(b.peerId).text)}>{b.peerName}</span>{" "}
          · {time(entry.createdAt)}
          {b.priority && !b.held && <Badge variant="secondary">priority</Badge>}
          {b.held && <Badge variant="outline">next turn</Badge>}
        </div>
        <MarkdownText text={b.text} />
      </div>
      {b.text && <Reactions entryId={entry.id} reactions={b.reactions} copyText={b.text} />}
    </div>
  )
}

export function EntryView({
  entry,
  chat,
  skill,
  onAnswer,
}: {
  entry: Entry
  chat: Bot
  skill: string | null
  onAnswer?: (q: Asked) => void
}) {
  switch (entry.kind) {
    case "user":
      return (
        <div className="group/message flex flex-col items-end">
          {skill && (
            <Badge variant="outline" className="mb-1 font-mono" title="This message invoked a skill">
              <Sparkles className="size-3" />
              skill: {skill}
            </Badge>
          )}
          {entry.body.media && entry.body.media.length > 0 && (
            <div className="mb-1 flex max-w-[75%] justify-end">
              <MediaGallery names={entry.body.media} />
            </div>
          )}
          {entry.body.text && (
            <div className="bg-primary text-primary-foreground max-w-[75%] rounded-2xl rounded-br-sm px-3.5 py-2 text-sm whitespace-pre-wrap">
              {entry.body.text}
            </div>
          )}
          {(entry.body.text || (entry.body.media && entry.body.media.length > 0)) && (
            <Reactions entryId={entry.id} reactions={entry.body.reactions} copyText={entry.body.text} />
          )}
        </div>
      )
    case "bot": {
      const speaker = entry.body.speakerName ?? chat.name
      const color = botColor(entry.body.speakerId ?? entry.botId)
      const qState = entry.body.questionState
      const open = qState === "open"
      const asked = questionOf(entry.id, entry.body)
      return (
        <div
          id={qState ? `entry-${entry.id}` : undefined}
          tabIndex={qState ? -1 : undefined}
          className={cn(
            "group/message flex max-w-[85%] gap-2.5 rounded-md outline-none",
            open && "border-l-2 border-amber-500 pl-2",
          )}
        >
          <div
            className={cn("grid size-7 shrink-0 place-items-center rounded-full text-xs font-semibold", color.avatar)}
            aria-hidden
          >
            {speaker.charAt(0).toUpperCase()}
          </div>
          <div className="min-w-0 flex-1">
            <div className="mb-0.5 flex items-baseline gap-2">
              {postTooltip(entry.body, FALLBACK_ALIASES) ? (
                <Hint text={postTooltip(entry.body, FALLBACK_ALIASES)}>
                  <button type="button" className={cn("cursor-default text-sm font-semibold", color.text)}>
                    {speaker}
                  </button>
                </Hint>
              ) : (
                <span className={cn("text-sm font-semibold", color.text)}>{speaker}</span>
              )}
              <span className="text-muted-foreground text-xs">{time(entry.createdAt)}</span>
              {stateLabel(qState) && (
                <span
                  className={cn(
                    "text-xs",
                    open ? "font-medium text-amber-700 dark:text-amber-400" : "text-muted-foreground",
                  )}
                >
                  {stateLabel(qState)}
                </span>
              )}
              {open && onAnswer && (
                <Button
                  size="xs"
                  variant="outline"
                  aria-label={`Answer: ${asked.question}`}
                  onClick={() => onAnswer(asked)}
                >
                  Answer
                </Button>
              )}
            </div>
            {entry.body.text && <MarkdownText text={entry.body.text} />}
            {entry.body.media && entry.body.media.length > 0 && (
              <div className={cn(entry.body.text && "mt-2")}>
                <MediaGallery names={entry.body.media} />
              </div>
            )}
            {(entry.body.text || (entry.body.media && entry.body.media.length > 0)) && (
              <Reactions entryId={entry.id} reactions={entry.body.reactions} copyText={entry.body.text} />
            )}
          </div>
        </div>
      )
    }
    case "activity":
      return <ActivityRow entry={entry} chat={chat} />
    case "approval":
      return <ApprovalCard entry={entry} />
    case "event":
      return <EventLine entry={entry} />
    case "agent":
      return <AgentMessage entry={entry} />
  }
}

type Suggestion = {
  token: string
  label: string
  detail?: string
  kind: "bot" | "everyone" | "command" | "skill"
}

const commands: Suggestion[] = [
  {
    token: "/clear",
    label: "/clear",
    detail: "Start a fresh context. The chat stays visible.",
    kind: "command",
  },
  {
    token: "/compact",
    label: "/compact",
    detail: "Summarize the context to free space. The chat stays visible.",
    kind: "command",
  },
]

// teamCommands are the commands that only a team's chat offers.
const teamCommands: Suggestion[] = [
  {
    token: taskCommand,
    label: taskCommand,
    detail: "Send the team's lead a task: /task <what the team should do>",
    kind: "command",
  },
]

// suggestionsFor returns what the typed query can complete to: bot names
// after "@" (and "here" for everyone in a room), commands and skills after a
// leading "/". A team's chat also offers the team commands.
function suggestionsFor(
  query: MentionQuery & { mode: "mention" | "slash" },
  mentionables: Member[],
  skills: Skill[],
  inRoom: boolean,
  inTeam: boolean,
): Suggestion[] {
  if (query.mode === "mention") {
    const names = matchNames(
      mentionables.map((m) => m.name),
      query.query,
    ).map((name) => ({ token: `@${name}`, label: name, kind: "bot" as const }))
    const here = inRoom && matchNames(["here"], query.query).length > 0
    return here
      ? [
          {
            token: "@here",
            label: "here",
            detail: "All members of the room",
            kind: "everyone" as const,
          },
          ...names,
        ]
      : names
  }
  const all = [
    ...(inTeam ? teamCommands : []),
    ...commands,
    ...skills.map((sk) => ({
      token: `/${sk.id}`,
      label: `/${sk.id}`,
      detail: sk.description,
      kind: "skill" as const,
    })),
  ]
  const byKey = new Map(all.map((s) => [s.token.slice(1), s]))
  return matchNames([...byKey.keys()], query.query).map((key) => byKey.get(key)!)
}

// A draft is a per-browser convenience. Storage can be unavailable (for
// example in a private window); a draft then lasts only while its chat is open.
function loadDraft(botId: string) {
  try {
    return localStorage.getItem(`handful.draft.${botId}`) ?? ""
  } catch {
    return ""
  }
}

function saveDraft(botId: string, text: string) {
  try {
    if (text) localStorage.setItem(`handful.draft.${botId}`, text)
    else localStorage.removeItem(`handful.draft.${botId}`)
  } catch {
    // Not saved: the draft is lost when its chat closes, which is acceptable.
  }
}

// PendingMedia is a file waiting to be sent. url is an object URL for its
// preview; revoke it when the file leaves the composer.
type PendingMedia = { key: string; file: File; url: string }

function readBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const dataUrl = reader.result as string
      resolve(dataUrl.slice(dataUrl.indexOf(",") + 1))
    }
    reader.onerror = () => reject(reader.error ?? new Error(`could not read ${file.name}`))
    reader.readAsDataURL(file)
  })
}

function Composer({
  bot,
  mentionables,
  media,
  attachError,
  onAttach,
  onMediaChange,
  onSent,
  answering,
  onAnswer,
}: {
  bot: Bot
  mentionables: Member[]
  media: PendingMedia[]
  attachError: string | null
  onAttach: (files: File[]) => void
  onMediaChange: (update: (media: PendingMedia[]) => PendingMedia[]) => void
  onSent: (botId: string) => void
  // answering is the question that the next message answers, set by an Answer button.
  answering: Asked | null
  onAnswer: (q: Asked | null) => void
}) {
  const [text, setText] = useState(() => loadDraft(bot.id))
  useEffect(() => saveDraft(bot.id, text), [bot.id, text])
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)
  const [query, setQuery] = useState<(MentionQuery & { mode: "mention" | "slash" }) | null>(null)
  const [active, setActive] = useState(0)
  const [dismissedAt, setDismissedAt] = useState<number | null>(null)
  const [skills, setSkills] = useState<Skill[]>([])
  const input = useRef<HTMLTextAreaElement>(null)
  const filePicker = useRef<HTMLInputElement>(null)
  const isRoom = bot.kind === "room"

  // Skills are invoked in a bot's own chat and in a team's chat; a plain room
  // offers only the commands.
  const loadSkills = () => {
    if (isRoom && !bot.team) return
    api.skills().then(setSkills, (e: Error) => setError(`Could not load skills: ${e.message}`))
  }
  useEffect(loadSkills, [bot.id])

  const suggestions = query
    ? suggestionsFor(query, mentionables, offeredSkills(skills, bot), isRoom, Boolean(bot.team))
    : []
  const open = suggestions.length > 0 && query !== null && query.start !== dismissedAt

  const trackCaret = (el: HTMLTextAreaElement) => {
    const slash = findSlashQuery(el.value, el.selectionStart)
    const mention = slash ? null : findMentionQuery(el.value, el.selectionStart)
    const next = slash
      ? { ...slash, mode: "slash" as const }
      : mention
        ? { ...mention, mode: "mention" as const }
        : null
    if (next?.mode === "slash" && query?.mode !== "slash") loadSkills()
    if (next?.start !== query?.start || next?.query !== query?.query) setActive(0)
    if (next === null) setDismissedAt(null)
    setQuery(next)
  }
  const accept = (s: Suggestion) => {
    const el = input.current
    if (!el || !query) return
    const result = applyCompletion(text, query, el.selectionStart, s.token)
    setText(result.text)
    setQuery(null)
    requestAnimationFrame(() => {
      el.focus()
      el.setSelectionRange(result.caret, result.caret)
    })
  }
  const send = async () => {
    if (sending) return
    // sendPlan (lib/send.ts) decides what the draft does: a message, a command or nothing.
    const plan = sendPlan(text, media.length, Boolean(bot.team))
    if (plan.kind === "skip") return
    if (plan.kind === "invalid") {
      setError(plan.error)
      return
    }
    setSending(true)
    setError(null)
    try {
      if (plan.kind === "task") await api.userTask(bot.id, { text: plan.task })
      else if (plan.kind === "clear") await api.clearContext(bot.id)
      else if (plan.kind === "compact") await api.compactContext(bot.id)
      else
        await api.send(
          bot.id,
          plan.text,
          await Promise.all(media.map((m) => readBase64(m.file))),
          answersFor(answering, false),
        )
      if (answers(plan)) onAnswer(null)
      setText("")
      onSent(bot.id)
      for (const m of media) URL.revokeObjectURL(m.url)
      onMediaChange(() => [])
      setQuery(null)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSending(false)
    }
  }
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.nativeEvent.isComposing) return
    if (open) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault()
        const step = e.key === "ArrowDown" ? 1 : -1
        setActive((i) => (i + step + suggestions.length) % suggestions.length)
        return
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault()
        accept(suggestions[Math.min(active, suggestions.length - 1)])
        return
      }
      if (e.key === "Escape") {
        e.preventDefault()
        setDismissedAt(query?.start ?? null)
        return
      }
    }
    if (e.key === "Escape" && answering) {
      e.preventDefault()
      onAnswer(null)
      return
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      void send()
    }
  }
  return (
    <div className="border-t p-3">
      {(error ?? attachError) && <p className="text-destructive mb-2 text-xs">{error ?? attachError}</p>}
      <div className="relative">
        {open && (
          <ul
            id="completion-list"
            role="listbox"
            aria-label={query?.mode === "slash" ? "Commands and skills" : "Mention a bot"}
            className="bg-popover text-popover-foreground absolute bottom-full left-0 z-10 mb-2 max-h-72 w-96 overflow-y-auto rounded-lg border p-1 shadow-md"
          >
            {suggestions.map((s, i) => (
              <li
                key={s.token}
                id={`completion-${i}`}
                role="option"
                aria-selected={i === active}
                className={cn(
                  "flex cursor-pointer items-start gap-2 rounded-md px-2 py-1.5 text-sm",
                  i === active && "bg-accent text-accent-foreground",
                )}
                onMouseEnter={() => setActive(i)}
                onMouseDown={(e) => {
                  // Keep the focus in the textarea, then insert.
                  e.preventDefault()
                  accept(s)
                }}
              >
                {s.kind === "bot" ? (
                  <BotIcon className="mt-0.5 size-3.5 shrink-0" />
                ) : s.kind === "everyone" ? (
                  <Users className="mt-0.5 size-3.5 shrink-0" />
                ) : s.kind === "command" ? (
                  <TerminalSquare className="mt-0.5 size-3.5 shrink-0" />
                ) : (
                  <Sparkles className="mt-0.5 size-3.5 shrink-0" />
                )}
                <span className="min-w-0">
                  <span className={cn((s.kind === "command" || s.kind === "skill") && "font-mono")}>{s.label}</span>
                  {s.detail && <span className="text-muted-foreground block truncate text-xs">{s.detail}</span>}
                </span>
              </li>
            ))}
          </ul>
        )}
        {bot.tasks.length > 0 && (
          <div className="text-muted-foreground flex items-center gap-1.5 px-2 pb-1.5 text-xs">
            <TerminalSquare className="size-3.5 shrink-0" />
            <Loader2 className="size-3 shrink-0 animate-spin" />
            <span className="truncate" title={bot.tasks.map(taskLabel).join("\n")}>
              In the background: {bot.tasks.map(taskLabel).join(" · ")}
            </span>
          </div>
        )}
        <div className="bg-background focus-within:ring-ring/50 rounded-xl border p-2 focus-within:ring-2">
          {answering && <AnswerChip answering={answering} onClear={() => onAnswer(null)} />}
          {media.length > 0 && (
            <div className="flex flex-wrap gap-2 px-1 pt-1 pb-2">
              {media.map((m) => (
                <div key={m.key} className="relative">
                  {m.file.type.startsWith("video/") ? (
                    <>
                      <video
                        src={m.url}
                        preload="metadata"
                        muted
                        playsInline
                        aria-label="Video to send"
                        className="size-16 rounded-lg border bg-black object-cover"
                      />
                      <Play className="pointer-events-none absolute inset-0 m-auto size-5 fill-white text-white drop-shadow" />
                    </>
                  ) : (
                    <img src={m.url} alt="Image to send" className="size-16 rounded-lg border object-cover" />
                  )}
                  <button
                    type="button"
                    aria-label="Remove file"
                    className="bg-foreground text-background absolute -top-1.5 -right-1.5 grid size-5 place-items-center rounded-full"
                    onClick={() => {
                      URL.revokeObjectURL(m.url)
                      onMediaChange((list) => list.filter((i) => i.key !== m.key))
                    }}
                  >
                    <X className="size-3" />
                  </button>
                </div>
              ))}
            </div>
          )}
          <div className="flex items-end gap-2">
            <input
              ref={filePicker}
              type="file"
              accept="image/png,image/jpeg,image/gif,image/webp,video/mp4,video/webm,video/quicktime"
              multiple
              hidden
              onChange={(e) => {
                onAttach([...(e.target.files ?? [])])
                // Clear the choice, so that choosing the same file again fires change.
                e.target.value = ""
              }}
            />
            <Button
              size="icon-lg"
              variant="ghost"
              aria-label="Attach images or videos"
              title="Attach images or videos"
              onClick={() => filePicker.current?.click()}
            >
              <Paperclip />
            </Button>
            <Textarea
              ref={input}
              id="composer-input"
              aria-label="Message"
              aria-autocomplete="list"
              aria-expanded={open}
              aria-controls={open ? "completion-list" : undefined}
              aria-activedescendant={open ? `completion-${active}` : undefined}
              className="max-h-48 min-h-9 resize-none border-0 shadow-none focus-visible:ring-0"
              placeholder={
                bot.team
                  ? `Message ${bot.name}… (the lead answers; @Name, @here, / for commands)`
                  : isRoom
                    ? `Message ${bot.name}… (@Name asks one member, @here all, / for commands)`
                    : `Message ${bot.name}… (/ for commands and skills, @ for bots)`
              }
              value={text}
              onChange={(e) => {
                setText(e.target.value)
                setError(null)
                trackCaret(e.target)
              }}
              onPaste={(e) => {
                const files = [...e.clipboardData.files]
                if (files.length === 0) return
                e.preventDefault()
                onAttach(files)
              }}
              onSelect={(e) => trackCaret(e.currentTarget)}
              onBlur={() => setQuery(null)}
              onKeyDown={onKeyDown}
            />
            {(bot.running || bot.tasks.length > 0) && (
              <Button
                size="icon-lg"
                variant="outline"
                aria-label={bot.running ? "Stop" : "Stop background tasks"}
                title={bot.running ? "Stop" : "Stop background tasks"}
                onClick={() => void api.interrupt(bot.id)}
              >
                <Square />
              </Button>
            )}
            <Button
              size="icon-lg"
              aria-label="Send"
              disabled={(!text.trim() && media.length === 0) || sending}
              onClick={() => void send()}
            >
              <ArrowUp />
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

// itemsOf pairs the entries with their elements, which come first in the transcript's column.
function itemsOf(column: HTMLElement | null, entries: Entry[]): Item[] {
  const kids = column?.children
  return kids ? entries.flatMap((e, i) => (kids[i] ? [{ key: e.id, box: kids[i] }] : [])) : []
}

// mentionables are the bots that @ can complete: room members, or the other bots.
export function Chat({
  bot,
  entries,
  status = "ready",
  mentionables,
  answering = null,
  onAnswer = () => {},
  pill = null,
  draining = false,
  pause = "Paused for update",
}: {
  bot: Bot
  entries: Entry[]
  // status is the transcript's load state, so an empty chat can say it is still loading.
  status?: TranscriptStatus
  mentionables: Member[]
  // pill is the update pill, shown above the message box; draining says the server waits
  // to restart for an update.
  pill?: ReactNode
  draining?: boolean
  pause?: string
  // answering is the question the next message in this chat answers.
  answering?: Asked | null
  onAnswer?: (q: Asked | null) => void
}) {
  // The chat's open questions, for the strip; reloaded as the transcript changes.
  const [questions, setQuestions] = useState<OpenQuestion[]>([])
  const [questionsAt, setQuestionsAt] = useState(0)
  const transcriptRev = entries.reduce((max, e) => Math.max(max, e.rev), 0)
  useEffect(() => {
    let live = true
    api.chatQuestions(bot.id).then(
      (qs) => live && setQuestions(qs),
      () => live && setQuestions([]),
    )
    return () => {
      live = false
    }
  }, [bot.id, transcriptRev, questionsAt])
  // The transcript follows new messages only while the user keeps it at the bottom, and each
  // chat keeps its own position across a switch to another chat.
  const scroller = useRef<HTMLDivElement>(null)
  const column = useRef<HTMLDivElement>(null)
  const [follower] = useState(() => new ScrollFollower(new ScrollMemory()))
  const memory = follower.memory
  // following re-renders the "Jump to latest" button when the view pins or unpins.
  const [, setFollowing] = useState(true)
  // shown is the chat on screen, as of the last layout.
  const shown = useRef<string | null>(null)
  const toBottom = () => {
    const el = scroller.current
    if (el) follower.scrollTo(el, el.scrollHeight)
  }
  const items = () => itemsOf(column.current, entries)
  useLayoutEffect(() => {
    const el = scroller.current
    if (!el) return
    const opened = shown.current !== bot.id
    const top = memory.target(bot.id, opened, el.scrollHeight)
    shown.current = bot.id
    if (top === null) return
    // A chat the user left scrolled up reopens on the entry they left it at; its saved
    // scrollTop is only the fallback, since images above may not have loaded again yet.
    if (opened && !memory.pinned(bot.id) && follower.restore(bot.id, el, itemsOf(column.current, entries))) return
    follower.scrollTo(el, top)
    if (opened && !memory.pinned(bot.id)) follower.capture(bot.id, el, itemsOf(column.current, entries))
  }, [entries, bot.id, memory, follower])
  // An entry that grows (an update, a loaded image, a reflow to another width) or a view that
  // shrinks (the goal bar, a taller draft, a shorter window) would move what the user reads.
  useLayoutEffect(() => {
    const el = scroller.current
    if (!el || !column.current) return
    const observer = new ResizeObserver(() => shown.current && follower.settle(shown.current, el))
    observer.observe(el)
    observer.observe(column.current)
    return () => observer.disconnect()
  }, [follower])
  const onScroll = (el: HTMLDivElement) => setFollowing(follower.scrolled(bot.id, el, items()))
  // A wheel, key or touch input that scrolls up unpins the chat before its scroll arrives,
  // even a trackpad step too small to leave the bottom.
  useEffect(() => {
    const el = scroller.current
    if (!el) return
    const intent = (input: Intent) => setFollowing(follower.intent(bot.id, el, itemsOf(column.current, entries), input))
    let touchY: number | null = null
    const onWheel = (e: WheelEvent) => intent({ kind: "wheel", deltaY: e.deltaY })
    const onKey = (e: globalThis.KeyboardEvent) => intent({ kind: "key", key: e.key })
    const onTouchStart = (e: TouchEvent) => (touchY = e.touches[0]?.clientY ?? null)
    const onTouchMove = (e: TouchEvent) => {
      const y = e.touches[0]?.clientY
      if (y === undefined) return
      if (touchY !== null) intent({ kind: "touch", dy: y - touchY })
      touchY = y
    }
    const opts = { passive: true }
    el.addEventListener("wheel", onWheel, opts)
    el.addEventListener("keydown", onKey)
    el.addEventListener("touchstart", onTouchStart, opts)
    el.addEventListener("touchmove", onTouchMove, opts)
    return () => {
      el.removeEventListener("wheel", onWheel)
      el.removeEventListener("keydown", onKey)
      el.removeEventListener("touchstart", onTouchStart)
      el.removeEventListener("touchmove", onTouchMove)
    }
  }, [follower, bot.id, entries])
  // pin makes a chat follow the bottom again: on "Jump to latest" or the user's own send.
  // A send that finishes after a switch to another chat holds the old render's bot id, so
  // compare with the chat on screen now, which shown holds.
  const pin = (id: string) => {
    memory.pin(id)
    if (id !== shown.current) return
    setFollowing(true)
    toBottom()
  }

  // answer sets the Answer chip and moves the keyboard to the message box.
  const answer = (q: Asked) => {
    onAnswer(q)
    requestAnimationFrame(() => document.getElementById("composer-input")?.focus())
  }

  // Files waiting to be sent, per chat, so they survive a switch to another chat.
  const [pending, setPending] = useState(() => new Map<string, PendingMedia[]>())
  const [attachError, setAttachError] = useState<string | null>(null)
  const [dropping, setDropping] = useState(false)
  useEffect(() => setAttachError(null), [bot.id])
  // A user's post in a team's chat that invokes a skill shows it.
  const [skillIds, setSkillIds] = useState<string[]>([])
  const isTeam = Boolean(bot.team)
  useEffect(() => {
    if (!isTeam) return
    api.skills().then(
      (list) => setSkillIds(list.map((sk) => sk.id)),
      () => setSkillIds([]),
    )
  }, [bot.id, isTeam])
  const memberNames = (bot.members ?? []).map((m) => m.name)
  const skillOf = (e: Entry) => (isTeam && e.kind === "user" ? teamSkillOf(e.body.text, skillIds, memberNames) : null)
  const updateMedia = (botId: string, update: (media: PendingMedia[]) => PendingMedia[]) =>
    setPending((m) => new Map(m).set(botId, update(m.get(botId) ?? [])))
  const attach = (files: File[]) => {
    const media = files.filter((f) => f.type.startsWith("image/") || f.type.startsWith("video/"))
    setAttachError(
      media.length < files.length ? "Only images and videos can be attached; other files were skipped." : null,
    )
    if (media.length === 0) return
    updateMedia(bot.id, (list) => [
      ...list,
      ...media.map((file) => ({ key: crypto.randomUUID(), file, url: URL.createObjectURL(file) })),
    ])
  }

  return (
    <div
      className="relative flex min-h-0 min-w-0 flex-1 flex-col"
      onDragOver={(e) => {
        if (!e.dataTransfer.types.includes("Files")) return
        e.preventDefault()
        setDropping(true)
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDropping(false)
      }}
      onDrop={(e) => {
        if (!e.dataTransfer.types.includes("Files")) return
        e.preventDefault()
        setDropping(false)
        attach([...e.dataTransfer.files])
      }}
    >
      {dropping && (
        <div className="border-primary/50 bg-background/80 text-muted-foreground pointer-events-none absolute inset-2 z-20 flex items-center justify-center rounded-xl border-2 border-dashed text-sm">
          Drop images or videos to attach
        </div>
      )}
      <div
        ref={scroller}
        data-transcript={status}
        className={cn("min-h-0 flex-1 overflow-y-auto [overflow-anchor:none]", pill && "pb-11")}
        onScroll={(e) => onScroll(e.currentTarget)}
      >
        <div ref={column} className="mx-auto flex max-w-3xl flex-col gap-3 px-4 py-6">
          {entries.length === 0 && <p className="text-muted-foreground text-center text-sm">{emptyText(status)}</p>}
          {entries.map((e) => (
            <EntryView key={e.id} entry={e} chat={bot} skill={skillOf(e)} onAnswer={answer} />
          ))}
          {bot.running && (
            <div className="text-muted-foreground flex items-center gap-2 text-xs">
              <Loader2 className="size-3 animate-spin" />{" "}
              {draining
                ? `${bot.kind === "room" ? "Members are" : `${bot.name} is`} finishing. ${pause}: new turns wait.`
                : bot.kind === "room"
                  ? "Members are working…"
                  : `${bot.name} is working…`}
            </div>
          )}
        </div>
      </div>
      {!memory.pinned(bot.id) && (
        <div className="pointer-events-none relative mx-auto w-full max-w-3xl">
          <Button
            size="icon"
            variant="outline"
            className={cn(
              "pointer-events-auto absolute right-4 z-10 rounded-full shadow-md",
              pill ? "bottom-12" : "bottom-3",
            )}
            aria-label="Jump to latest"
            title="Jump to latest"
            onClick={() => pin(bot.id)}
          >
            <ArrowDown />
          </Button>
        </div>
      )}
      <div className="@container relative mx-auto w-full max-w-3xl">
        {pill}
        <QuestionStrip
          questions={questions}
          room={bot.kind === "room"}
          onAnswer={answer}
          onChange={() => setQuestionsAt((n) => n + 1)}
        />
        <Composer
          key={bot.id}
          answering={answering}
          onAnswer={onAnswer}
          bot={bot}
          mentionables={mentionables}
          media={pending.get(bot.id) ?? []}
          attachError={attachError}
          onAttach={attach}
          onMediaChange={(update) => updateMedia(bot.id, update)}
          onSent={pin}
        />
      </div>
    </div>
  )
}
