import { useCallback, useEffect, useEffectEvent, useRef, useState, type ReactNode } from "react"
import { ArrowLeft, Link as LinkIcon, Send, Trash2 } from "lucide-react"
import { api, isNotFound, type Artifact, type Bot, type Skill } from "@/lib/api"
import { artifactAuthor } from "@/lib/artifacts"
import { artifactUrl, type Reading } from "@/lib/place"
import { skillLayerLabel } from "@/lib/skills"
import { dateTime } from "@/lib/dates"
import { useRights } from "@/lib/session"
import { MarkdownText } from "@/components/chat"
import { copyLabel, useCopy } from "@/components/copy"
import { CopyIcon } from "@/components/code-block"
import { SendTaskDialog } from "@/components/tasks"
import { useNow } from "@/components/now"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

type Loaded = { kind: "artifact"; item: Artifact } | { kind: "skill"; item: Skill }

// Reader shows an artifact or a skill over the chat, as a page to read. Escape and Back to chat
// close it; onGone says that it is not there (a 404), and onDeleted that the user deleted it. Key
// it by the item, so that another item starts from its loading state.
export function Reader({
  reading,
  chat,
  agents,
  teams,
  menu,
  onClose,
  onGone,
  onDeleted,
}: {
  reading: Reading
  // chat is the chat the reader opened in; an artifact's link opens it there again, and a
  // bot-skill reading is that chat's own skill.
  chat: string
  agents: Bot[]
  teams: Bot[]
  menu: ReactNode
  onClose: () => void
  onGone: () => void
  onDeleted: () => void
}) {
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [confirm, setConfirm] = useState(false)
  const [handing, setHanding] = useState(false)
  const [linkState, copyLink] = useCopy()
  const rights = useRights()
  const now = useNow(60_000)
  const title = useRef<HTMLHeadingElement>(null)
  const gone = useEffectEvent(onGone)
  const botChat = agents.some((a) => a.id === chat)
  useEffect(() => {
    let live = true
    const load: Promise<Loaded> =
      reading.kind === "artifact"
        ? api.artifact(reading.id).then((item) => ({ kind: "artifact", item }))
        : reading.kind === "bot-skill"
          ? api.botSkill(chat, reading.id).then((item) => ({ kind: "skill", item }))
          : // In a bot's chat, the bot's list says whether its own skill replaces the shared one.
            Promise.all([api.skill(reading.id), botChat ? api.botSkills(chat) : []]).then(([item, listed]) => ({
              kind: "skill",
              item: { ...item, replaced: listed.some((s) => s.layer === "shared" && s.id === item.id && s.replaced) },
            }))
    load.then(
      (l) => live && setLoaded(l),
      (e: Error) => {
        if (!live) return
        if (isNotFound(e)) gone()
        else setError(e.message)
      },
    )
    return () => {
      live = false
    }
  }, [reading.kind, reading.id, chat, botChat])
  // The keyboard goes to the title when the item shows, so Tab reaches its links and controls.
  useEffect(() => {
    if (loaded) title.current?.focus({ preventScroll: true })
  }, [loaded])
  // closesOnEscape: a dialog of the reader is in a portal, so its Escape closes only the dialog.
  const closesOnEscape = useCallback(
    (el: HTMLElement | null) => {
      if (!el) return
      const onKey = (e: KeyboardEvent) => {
        if (e.key !== "Escape" || e.defaultPrevented) return
        e.preventDefault()
        onClose()
      }
      el.addEventListener("keydown", onKey)
      return () => el.removeEventListener("keydown", onKey)
    },
    [onClose],
  )
  const remove = async () => {
    if (!loaded) return
    try {
      if (loaded.kind === "artifact") await api.deleteArtifact(loaded.item.id)
      else if (loaded.item.layer === "bot") await api.deleteBotSkill(chat, loaded.item.id)
      else await api.deleteSkill(loaded.item.id)
      onDeleted()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const noun = reading.kind === "artifact" ? "artifact" : "skill"
  return (
    <section
      ref={closesOnEscape}
      aria-label={loaded ? (loaded.kind === "artifact" ? loaded.item.title : loaded.item.name) : `Loading ${noun}`}
      className="bg-background absolute inset-0 z-20 flex flex-col"
    >
      <header className="flex h-12 shrink-0 items-center gap-2 border-b px-4 max-md:h-14 max-md:px-3">
        {menu}
        <Button variant="ghost" size="sm" className="-ml-2 max-md:h-11" onClick={onClose}>
          <ArrowLeft /> Back to chat
        </Button>
        <span className="flex-1" />
        {loaded?.kind === "artifact" && (
          <Button
            variant="ghost"
            size="sm"
            className="max-md:size-11 max-md:px-0"
            aria-label={copyLabel(linkState, "Copy link")}
            title={copyLabel(linkState, "Copy link")}
            onClick={() => copyLink(artifactUrl(location.origin, chat, loaded.item.id))}
          >
            {linkState === "idle" ? <LinkIcon /> : <CopyIcon state={linkState} />}
            <span className="max-md:hidden">{copyLabel(linkState, "Copy link")}</span>
          </Button>
        )}
        {loaded && rights.computer && !confirm && (
          <>
            {loaded.kind === "artifact" && teams.length > 0 && (
              <Button
                variant="ghost"
                size="sm"
                className="max-md:size-11 max-md:px-0"
                aria-haspopup="dialog"
                aria-label="Send to a team…"
                onClick={() => setHanding(true)}
              >
                <Send />
                <span className="max-md:hidden">Send to a team…</span>
              </Button>
            )}
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive max-md:size-11 max-md:px-0"
              aria-label={`Delete ${noun}`}
              onClick={() => setConfirm(true)}
            >
              <Trash2 />
              <span className="max-md:hidden">Delete</span>
            </Button>
          </>
        )}
        {confirm && (
          <>
            <Button variant="ghost" size="sm" className="max-md:h-11" onClick={() => setConfirm(false)}>
              Cancel
            </Button>
            <Button variant="destructive" size="sm" className="max-md:h-11" onClick={() => void remove()}>
              {loaded?.kind === "skill" && loaded.item.layer === "bot" ? "Delete for this bot" : "Delete for all bots"}
            </Button>
          </>
        )}
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <article className="mx-auto max-w-3xl px-6 py-6 max-md:px-4">
          {error && (
            <p role="alert" className="text-destructive mb-4 text-sm">
              {error}
            </p>
          )}
          {!loaded && !error && <p className="text-muted-foreground text-sm">Loading…</p>}
          {loaded?.kind === "artifact" && (
            <>
              <h1 ref={title} tabIndex={-1} className="text-xl font-semibold outline-none">
                {loaded.item.title}
              </h1>
              <p className="text-foreground/70 mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
                <Badge variant="secondary">{loaded.item.kind}</Badge>
                <span>{artifactAuthor(loaded.item.authorId, agents)}</span>
                <span aria-hidden>·</span>
                <span>Updated {dateTime(loaded.item.updatedAt, now)}</span>
              </p>
              {loaded.item.tags.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1">
                  {loaded.item.tags.map((t) => (
                    <Badge key={t} variant="outline">
                      {t}
                    </Badge>
                  ))}
                </div>
              )}
              {loaded.item.links.length > 0 && (
                <ul className="mt-2 flex flex-col gap-0.5 text-sm">
                  {loaded.item.links.map((l) => (
                    <li key={l} className="truncate">
                      <a href={l} target="_blank" rel="noopener noreferrer" className="text-info-foreground underline">
                        {l}
                      </a>
                    </li>
                  ))}
                </ul>
              )}
              <div className="mt-6 border-t pt-6">
                <MarkdownText text={loaded.item.content ?? ""} />
              </div>
            </>
          )}
          {loaded?.kind === "skill" && (
            <>
              <h1 ref={title} tabIndex={-1} className="text-xl font-semibold outline-none">
                {loaded.item.name}
              </h1>
              <p className="text-foreground/70 mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
                <Badge variant={loaded.item.layer === "bot" ? "secondary" : "outline"}>
                  {skillLayerLabel(loaded.item)}
                </Badge>
                <span>{loaded.item.description}</span>
              </p>
              <div className="mt-6 border-t pt-6">
                <MarkdownText text={loaded.item.body ?? ""} />
              </div>
            </>
          )}
        </article>
      </div>
      {loaded?.kind === "artifact" && (
        <SendTaskDialog
          open={handing}
          onOpenChange={setHanding}
          from={null}
          teams={teams}
          artifactIds={[loaded.item.id]}
        />
      )}
    </section>
  )
}
