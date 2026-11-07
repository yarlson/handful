import { useEffect, useRef, useState, type KeyboardEvent } from "react"
import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"
import {
  AlertTriangle,
  ArrowDownLeft,
  ArrowUp,
  ArrowUpRight,
  Bot as BotIcon,
  Check,
  ChevronRight,
  Copy,
  Loader2,
  Paperclip,
  Play,
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
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Textarea } from "@/components/ui/textarea"
import { MediaGallery } from "@/components/media"
import { cn } from "@/lib/utils"
import { botColor } from "@/lib/bot-color"

function time(ms: number) {
  return new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

function MarkdownText({ text }: { text: string }) {
  return (
    <div className="markdown text-sm leading-relaxed">
      <Markdown remarkPlugins={[remarkGfm]}>{text}</Markdown>
    </div>
  )
}

function ActivityRow({ entry }: { entry: Extract<Entry, { kind: "activity" }> }) {
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
        {b.bot && <span className={cn("shrink-0 font-medium", botColor(b.bot).text)}>{b.bot}</span>}
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
      <CopyButton text={b.text} />
    </div>
  )
}

function EntryView({ entry, botName }: { entry: Entry; botName: string }) {
  switch (entry.kind) {
    case "user":
      return (
        <div className="group/message flex flex-col items-end">
          {entry.body.media && entry.body.media.length > 0 && (
            <div className="mb-1 flex max-w-[75%] justify-end">
              <MediaGallery names={entry.body.media} />
            </div>
          )}
          {entry.body.text && (
            <>
              <div className="bg-primary text-primary-foreground max-w-[75%] rounded-2xl rounded-br-sm px-3.5 py-2 text-sm whitespace-pre-wrap">
                {entry.body.text}
              </div>
              <CopyButton text={entry.body.text} />
            </>
          )}
        </div>
      )
    case "bot": {
      const speaker = entry.body.speakerName ?? botName
      const color = botColor(entry.body.speakerId ?? entry.botId)
      return (
        <div className="group/message flex max-w-[85%] gap-2.5">
          <div
            className={cn("grid size-7 shrink-0 place-items-center rounded-full text-xs font-semibold", color.avatar)}
            aria-hidden
          >
            {speaker.charAt(0).toUpperCase()}
          </div>
          <div className="min-w-0 flex-1">
            <div className="mb-0.5 flex items-baseline gap-2">
              <span className={cn("text-sm font-semibold", color.text)}>{speaker}</span>
              <span className="text-muted-foreground text-xs">{time(entry.createdAt)}</span>
            </div>
            {entry.body.text && <MarkdownText text={entry.body.text} />}
            {entry.body.media && entry.body.media.length > 0 && (
              <div className={cn(entry.body.text && "mt-2")}>
                <MediaGallery names={entry.body.media} />
              </div>
            )}
            {entry.body.text && <CopyButton text={entry.body.text} />}
          </div>
        </div>
      )
    }
    case "activity":
      return <ActivityRow entry={entry} />
    case "approval":
      return <ApprovalCard entry={entry} />
    case "event":
      return <EventLine entry={entry} />
    case "agent":
      return <AgentMessage entry={entry} />
  }
}

type Suggestion = { token: string; label: string; detail?: string; kind: "bot" | "everyone" | "command" | "skill" }

const commands: Suggestion[] = [
  { token: "/clear", label: "/clear", detail: "Start a fresh context. The chat stays visible.", kind: "command" },
  {
    token: "/compact",
    label: "/compact",
    detail: "Summarize the context to free space. The chat stays visible.",
    kind: "command",
  },
]

// suggestionsFor returns what the typed query can complete to: bot names
// after "@" (and "here" for everyone in a room), commands and skills after a
// leading "/".
function suggestionsFor(
  query: MentionQuery & { mode: "mention" | "slash" },
  mentionables: Member[],
  skills: Skill[],
  inRoom: boolean,
): Suggestion[] {
  if (query.mode === "mention") {
    const names = matchNames(
      mentionables.map((m) => m.name),
      query.query,
    ).map((name) => ({ token: `@${name}`, label: name, kind: "bot" as const }))
    const here = inRoom && matchNames(["here"], query.query).length > 0
    return here
      ? [{ token: "@here", label: "here", detail: "All members of the room", kind: "everyone" as const }, ...names]
      : names
  }
  const all = [
    ...commands,
    ...skills.map((sk) => ({ token: `/${sk.id}`, label: `/${sk.id}`, detail: sk.description, kind: "skill" as const })),
  ]
  const byKey = new Map(all.map((s) => [s.token.slice(1), s]))
  return matchNames([...byKey.keys()], query.query).map((key) => byKey.get(key)!)
}

// A draft is a per-browser convenience. Storage can be unavailable (for
// example in a private window); a draft then lasts only while its chat is open.
function loadDraft(botId: string) {
  try {
    return localStorage.getItem(`yargpt.draft.${botId}`) ?? ""
  } catch {
    return ""
  }
}

function saveDraft(botId: string, text: string) {
  try {
    if (text) localStorage.setItem(`yargpt.draft.${botId}`, text)
    else localStorage.removeItem(`yargpt.draft.${botId}`)
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
}: {
  bot: Bot
  mentionables: Member[]
  media: PendingMedia[]
  attachError: string | null
  onAttach: (files: File[]) => void
  onMediaChange: (update: (media: PendingMedia[]) => PendingMedia[]) => void
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

  // Skills are invoked in a bot's own chat; a room offers only the commands.
  const loadSkills = () => {
    if (isRoom) return
    api.skills().then(setSkills, (e: Error) => setError(`Could not load skills: ${e.message}`))
  }
  useEffect(loadSkills, [bot.id])

  const suggestions = query ? suggestionsFor(query, mentionables, isRoom ? [] : skills, isRoom) : []
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
    const value = text.trim()
    if ((!value && media.length === 0) || sending) return
    setSending(true)
    setError(null)
    try {
      if (media.length === 0 && value === "/clear") await api.clearContext(bot.id)
      else if (media.length === 0 && value === "/compact") await api.compactContext(bot.id)
      else await api.send(bot.id, value, await Promise.all(media.map((m) => readBase64(m.file))))
      setText("")
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
              aria-label="Message"
              aria-autocomplete="list"
              aria-expanded={open}
              aria-controls={open ? "completion-list" : undefined}
              aria-activedescendant={open ? `completion-${active}` : undefined}
              className="max-h-48 min-h-9 resize-none border-0 shadow-none focus-visible:ring-0"
              placeholder={
                isRoom
                  ? `Message ${bot.name}… (@Name asks one member, @here all, / for commands)`
                  : `Message ${bot.name}… (/ for commands and skills, @ for bots)`
              }
              value={text}
              onChange={(e) => {
                setText(e.target.value)
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

// mentionables are the bots that @ can complete: room members, or the other bots.
export function Chat({ bot, entries, mentionables }: { bot: Bot; entries: Entry[]; mentionables: Member[] }) {
  const bottom = useRef<HTMLDivElement>(null)
  const last = entries[entries.length - 1]
  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "end" })
  }, [entries.length, last?.rev, bot.id])

  // Files waiting to be sent, per chat, so they survive a switch to another chat.
  const [pending, setPending] = useState(() => new Map<string, PendingMedia[]>())
  const [attachError, setAttachError] = useState<string | null>(null)
  const [dropping, setDropping] = useState(false)
  useEffect(() => setAttachError(null), [bot.id])
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
      {/* An image grows the transcript when it loads, after the scroll above. React also passes
          up the load events of the media viewer, which renders outside this element. */}
      <div
        className="min-h-0 flex-1 overflow-y-auto"
        onLoadCapture={(e) =>
          e.currentTarget.contains(e.target as Node) && bottom.current?.scrollIntoView({ block: "end" })
        }
      >
        <div className="mx-auto flex max-w-3xl flex-col gap-3 px-4 py-6">
          {entries.length === 0 && <p className="text-muted-foreground text-center text-sm">No messages yet.</p>}
          {entries.map((e) => (
            <EntryView key={e.id} entry={e} botName={bot.name} />
          ))}
          {bot.running && (
            <div className="text-muted-foreground flex items-center gap-2 text-xs">
              <Loader2 className="size-3 animate-spin" />{" "}
              {bot.kind === "room" ? "Members are working…" : `${bot.name} is working…`}
            </div>
          )}
          <div ref={bottom} />
        </div>
      </div>
      <div className="mx-auto w-full max-w-3xl">
        <Composer
          key={bot.id}
          bot={bot}
          mentionables={mentionables}
          media={pending.get(bot.id) ?? []}
          attachError={attachError}
          onAttach={attach}
          onMediaChange={(update) => updateMedia(bot.id, update)}
        />
      </div>
    </div>
  )
}
