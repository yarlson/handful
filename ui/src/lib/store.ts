import { useCallback, useEffect, useRef, useState } from "react"
import { api, type Bot, type ServerEvent } from "./api"
import {
  drainAnswered,
  drainAsked,
  drainChanged,
  linkStep,
  NO_DRAIN,
  type Drain,
  type Link,
  type LinkEvent,
} from "./connection"
import { ChatWindows } from "./chat-windows"
import { transcriptStep, type TranscriptEvent, type TranscriptStatus } from "./transcript"
import type { EntryWindow } from "./window"

// useHandful keeps the bot list and the open chats' windows (chat-windows.ts) in sync with
// the server's event stream.
export function useHandful() {
  const [bots, setBots] = useState<Bot[]>([])
  const [transcripts, setTranscripts] = useState<Map<string, EntryWindow>>(new Map())
  const [unread, setUnread] = useState<Set<string>>(new Set())
  // link is the event stream's state; connection.ts holds its transitions.
  const [link, setLink] = useState<Link>("connecting")
  const [error, setError] = useState<string | null>(null)
  const [pausedUntil, setPausedUntil] = useState<string | null>(null)
  // draining: the server starts no new turns until it restarts for an update. The stream's
  // drain events and GET /api/version both set it; drain.rev drops answers they overtook.
  const [draining, setDraining] = useState(false)
  // restartRev counts the server's settings-restart events, so the Settings page reloads.
  const [restartRev, setRestartRev] = useState(0)
  const drain = useRef<Drain>(NO_DRAIN)
  const setDrain = useCallback((d: Drain) => {
    drain.current = d
    setDraining(d.draining)
  }, [])
  const linkRef = useRef<Link>("connecting")
  const onLink = useCallback((event: LinkEvent) => {
    const step = linkStep(linkRef.current, event)
    if (!step.ok) console.warn(step.error)
    linkRef.current = step.state
    setLink(step.state)
  }, [])
  // status is each chat's load state; transcript.ts holds its transitions.
  const [status, setStatus] = useState<Map<string, TranscriptStatus>>(new Map())
  const onTranscript = useCallback((botId: string, event: TranscriptEvent) => {
    setStatus((prev) => {
      const step = transcriptStep(prev.get(botId) ?? "unloaded", event)
      if (!step.ok) console.warn(step.error)
      return new Map(prev).set(botId, step.status)
    })
  }, [])
  const selectedRef = useRef<string | null>(null)

  const refreshBots = useCallback(async () => {
    try {
      setBots(await api.listBots())
    } catch (e) {
      setError((e as Error).message)
    }
  }, [])

  const forget = useCallback((botId: string) => {
    setStatus((prev) => {
      const next = new Map(prev)
      next.delete(botId)
      return next
    })
  }, [])
  const [chats] = useState(
    () =>
      new ChatWindows({
        page: api.entryWindow,
        changes: api.entries,
        onWindows: setTranscripts,
        onStatus: onTranscript,
        onUnload: forget,
        onError: setError,
      }),
  )
  const loadOlder = useCallback((botId: string) => chats.loadOlder(botId), [chats])
  const reveal = useCallback((botId: string, entryId: string) => chats.reveal(botId, entryId), [chats])

  const select = useCallback(
    (botId: string | null) => {
      selectedRef.current = botId
      if (!botId) return
      setUnread((prev) => {
        const next = new Set(prev)
        next.delete(botId)
        return next
      })
      void chats.select(botId)
    },
    [chats],
  )

  // Many entries can arrive at once; refresh the list once after they settle.
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const scheduleBotsRefresh = useCallback(() => {
    if (refreshTimer.current) clearTimeout(refreshTimer.current)
    refreshTimer.current = setTimeout(() => void refreshBots(), 250)
  }, [refreshBots])

  useEffect(() => {
    const source = new EventSource("/api/events")
    source.addEventListener("hello", () => {
      onLink("hello")
      setPausedUntil(null)
      // A (re)connect may follow a restart, which ends drain mode; ask instead of assuming.
      const asked = drainAsked(drain.current)
      setDrain(asked)
      api.version().then(
        (v) => setDrain(drainAnswered(drain.current, asked.rev, v.draining)),
        () => setDrain(drainAnswered(drain.current, asked.rev, false)),
      )
      void refreshBots()
      chats.reconnected()
    })
    source.onmessage = (msg) => {
      const ev = JSON.parse(msg.data as string) as ServerEvent
      if (ev.type === "bots") {
        void refreshBots()
      } else if (ev.type === "state") {
        setBots((prev) => prev.map((b) => (b.id === ev.botId ? { ...b, running: ev.running } : b)))
      } else if (ev.type === "pause") {
        setPausedUntil(ev.pausedUntil ?? null)
      } else if (ev.type === "restart") {
        setRestartRev((n) => n + 1)
      } else if (ev.type === "drain") {
        setDrain(drainChanged(drain.current, ev.draining))
      } else if (ev.type === "tasks") {
        setBots((prev) => prev.map((b) => (b.id === ev.botId ? { ...b, tasks: ev.tasks } : b)))
      } else if (ev.type === "entry") {
        chats.update(ev.botId, [ev.entry])
        // These entries can change whether a bot waits on the user (attention).
        if (ev.entry.kind === "approval" || ev.entry.kind === "bot" || ev.entry.kind === "user") scheduleBotsRefresh()
        if (ev.botId !== selectedRef.current && (ev.entry.kind === "bot" || ev.entry.kind === "approval")) {
          setUnread((prev) => new Set(prev).add(ev.botId))
        }
      }
    }
    source.onerror = () => onLink("error")
    return () => source.close()
  }, [chats, refreshBots, scheduleBotsRefresh, onLink, setDrain])

  // reorder sets the order of one sidebar section (all bots, teams or rooms). The
  // list changes at once; a failed save reloads the order from the server.
  const reorder = useCallback(
    async (ids: string[]) => {
      const position = new Map(ids.map((id, i) => [id, i]))
      setBots((prev) => {
        const moved = prev.filter((b) => position.has(b.id)).sort((a, b) => position.get(a.id)! - position.get(b.id)!)
        let next = 0
        return prev.map((b) => (position.has(b.id) ? moved[next++] : b))
      })
      try {
        await api.reorderBots(ids)
      } catch (e) {
        setError((e as Error).message)
        void refreshBots()
      }
    },
    [refreshBots],
  )

  return {
    bots,
    transcripts,
    transcriptStatus: status,
    unread,
    link,
    error,
    setError,
    pausedUntil,
    draining,
    restartRev,
    select,
    loadOlder,
    reveal,
    refreshBots,
    reorder,
  }
}
