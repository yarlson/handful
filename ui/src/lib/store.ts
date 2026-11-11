import { useCallback, useEffect, useRef, useState } from "react"
import { api, type Bot, type Entry, type ServerEvent } from "./api"
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
import { loadEntriesAfter } from "./entries"
import { transcriptStep, type TranscriptEvent, type TranscriptStatus } from "./transcript"

type Transcript = { byId: Map<string, Entry>; rev: number }

// useHandful keeps the bot list and the loaded transcripts in sync with the
// server's event stream. On every (re)connect it refetches what changed after
// the highest rev it has, so missed events cannot leave gaps.
export function useHandful() {
  const [bots, setBots] = useState<Bot[]>([])
  const [transcripts, setTranscripts] = useState<Map<string, Transcript>>(new Map())
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
  const loaded = useRef<Map<string, number>>(new Map()) // bot id -> highest rev fetched
  // status is each chat's load state; transcript.ts holds its transitions.
  const [status, setStatus] = useState<Map<string, TranscriptStatus>>(new Map())
  const statusRef = useRef<Map<string, TranscriptStatus>>(new Map())
  const onTranscript = useCallback((botId: string, event: TranscriptEvent) => {
    const step = transcriptStep(statusRef.current.get(botId) ?? "unloaded", event)
    if (!step.ok) console.warn(step.error)
    statusRef.current = new Map(statusRef.current).set(botId, step.status)
    setStatus(statusRef.current)
  }, [])
  const selectedRef = useRef<string | null>(null)

  const refreshBots = useCallback(async () => {
    try {
      setBots(await api.listBots())
    } catch (e) {
      setError((e as Error).message)
    }
  }, [])

  const merge = useCallback((botId: string, entries: Entry[]) => {
    if (entries.length === 0) return
    setTranscripts((prev) => {
      const next = new Map(prev)
      const cur = next.get(botId) ?? { byId: new Map(), rev: 0 }
      const byId = new Map(cur.byId)
      let rev = cur.rev
      for (const e of entries) {
        const old = byId.get(e.id)
        if (!old || old.rev < e.rev) byId.set(e.id, e)
        rev = Math.max(rev, e.rev)
      }
      loaded.current.set(botId, Math.max(loaded.current.get(botId) ?? 0, rev))
      next.set(botId, { byId, rev })
      return next
    })
  }, [])

  const loadTranscript = useCallback(
    async (botId: string) => {
      onTranscript(botId, "fetch")
      try {
        const after = loaded.current.get(botId) ?? 0
        loaded.current.set(botId, after)
        await loadEntriesAfter(
          (rev) => api.entries(botId, rev),
          after,
          (entries) => merge(botId, entries),
        )
        onTranscript(botId, "loaded")
      } catch (e) {
        onTranscript(botId, "failed")
        setError((e as Error).message)
      }
    },
    [merge, onTranscript],
  )

  const select = useCallback(
    (botId: string | null) => {
      selectedRef.current = botId
      if (!botId) return
      setUnread((prev) => {
        const next = new Set(prev)
        next.delete(botId)
        return next
      })
      void loadTranscript(botId)
    },
    [loadTranscript],
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
      for (const botId of loaded.current.keys()) void loadTranscript(botId)
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
        if (loaded.current.has(ev.botId)) merge(ev.botId, [ev.entry])
        // These entries can change whether a bot waits on the user (attention).
        if (ev.entry.kind === "approval" || ev.entry.kind === "bot" || ev.entry.kind === "user") scheduleBotsRefresh()
        if (ev.botId !== selectedRef.current && (ev.entry.kind === "bot" || ev.entry.kind === "approval")) {
          setUnread((prev) => new Set(prev).add(ev.botId))
        }
      }
    }
    source.onerror = () => onLink("error")
    return () => source.close()
  }, [loadTranscript, merge, refreshBots, scheduleBotsRefresh, onLink, setDrain])

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
    refreshBots,
    reorder,
  }
}

export function orderedEntries(t: Transcript | undefined): Entry[] {
  if (!t) return []
  return [...t.byId.values()].sort((a, b) => a.seq - b.seq)
}
