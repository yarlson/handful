import { useCallback, useEffect, useRef, useState } from "react"
import { api, type Bot, type Entry, type ServerEvent } from "./api"
import { loadEntriesAfter } from "./entries"

type Transcript = { byId: Map<string, Entry>; rev: number }

// useYarGPT keeps the bot list and the loaded transcripts in sync with the
// server's event stream. On every (re)connect it refetches what changed after
// the highest rev it has, so missed events cannot leave gaps.
export function useYarGPT() {
  const [bots, setBots] = useState<Bot[]>([])
  const [transcripts, setTranscripts] = useState<Map<string, Transcript>>(new Map())
  const [unread, setUnread] = useState<Set<string>>(new Set())
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pausedUntil, setPausedUntil] = useState<string | null>(null)
  // draining: the server starts no new turns until it restarts for an update.
  const [draining, setDraining] = useState(false)
  const loaded = useRef<Map<string, number>>(new Map()) // bot id -> highest rev fetched
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
      try {
        const after = loaded.current.get(botId) ?? 0
        loaded.current.set(botId, after)
        await loadEntriesAfter(
          (rev) => api.entries(botId, rev),
          after,
          (entries) => merge(botId, entries),
        )
      } catch (e) {
        setError((e as Error).message)
      }
    },
    [merge],
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
      setConnected(true)
      setPausedUntil(null)
      // A (re)connect may follow a restart, which ends drain mode; ask instead of assuming.
      api.version().then(
        (v) => setDraining(v.draining),
        () => setDraining(false),
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
      } else if (ev.type === "drain") {
        setDraining(ev.draining)
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
    source.onerror = () => setConnected(false)
    return () => source.close()
  }, [loadTranscript, merge, refreshBots, scheduleBotsRefresh])

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

  return { bots, transcripts, unread, connected, error, setError, pausedUntil, draining, select, refreshBots, reorder }
}

export function orderedEntries(t: Transcript | undefined): Entry[] {
  if (!t) return []
  return [...t.byId.values()].sort((a, b) => a.seq - b.seq)
}
