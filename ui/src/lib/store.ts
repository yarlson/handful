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
import { EMPTY_WINDOW, keptChats, WINDOW_PAGE, withPage, withUpdates, type EntryWindow } from "./window"

// KEPT_CHATS is how many chats keep their loaded window: the open one and the ones opened
// just before it. The others unload, and load their newest page again when reopened.
export const KEPT_CHATS = 4

// Load is what the store knows about one loaded chat besides its window.
type Load = {
  ready: boolean // its newest page arrived
  buffer: Entry[] // events that arrived before it, applied after it
  refresh: boolean // a reconnect came before it: fetch the changes after it
  older: boolean // an older page is on its way
}

// useHandful keeps the bot list and the open chats' windows in sync with the server's event
// stream. A chat loads its newest page once when it opens, and older pages on demand; on
// every reconnect it fetches only what changed after the highest rev it has, so missed events
// cannot leave gaps.
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
  // windows mirrors transcripts, so that loads read the window they add to.
  const windows = useRef<Map<string, EntryWindow>>(new Map())
  const loads = useRef<Map<string, Load>>(new Map())
  const recent = useRef<string[]>([])
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

  // put stores chat botId's window, or drops it when w is undefined.
  const put = useCallback((botId: string, w: EntryWindow | undefined) => {
    const next = new Map(windows.current)
    if (w) next.set(botId, w)
    else next.delete(botId)
    windows.current = next
    setTranscripts(next)
  }, [])

  // update applies new and changed entries to a loaded chat; before its newest page arrives,
  // they wait for it.
  const update = useCallback(
    (botId: string, entries: Entry[]) => {
      const load = loads.current.get(botId)
      if (!load) return
      if (!load.ready) {
        load.buffer.push(...entries)
        return
      }
      put(botId, withUpdates(windows.current.get(botId) ?? EMPTY_WINDOW, entries))
    },
    [put],
  )

  // refresh fetches the changes of a loaded chat after the highest rev it has.
  const refresh = useCallback(
    async (botId: string) => {
      const after = windows.current.get(botId)?.rev ?? 0
      try {
        await loadEntriesAfter(
          (rev) => api.entries(botId, rev),
          after,
          (entries) => update(botId, entries),
        )
      } catch (e) {
        setError((e as Error).message)
      }
    },
    [update],
  )

  // open loads a chat's newest page, once: a loaded chat stays current through the stream.
  const open = useCallback(
    async (botId: string) => {
      if (loads.current.has(botId)) return
      const load: Load = { ready: false, buffer: [], refresh: false, older: false }
      loads.current.set(botId, load)
      onTranscript(botId, "fetch")
      try {
        const page = await api.entryWindow(botId, WINDOW_PAGE)
        if (loads.current.get(botId) !== load) return
        load.ready = true
        put(botId, withUpdates(withPage(EMPTY_WINDOW, page, WINDOW_PAGE), load.buffer))
        load.buffer = []
        onTranscript(botId, "loaded")
        if (load.refresh) void refresh(botId)
      } catch (e) {
        if (loads.current.get(botId) !== load) return
        loads.current.delete(botId)
        onTranscript(botId, "failed")
        setError((e as Error).message)
      }
    },
    [onTranscript, put, refresh],
  )

  // loadOlder loads the page before a chat's oldest loaded entry, unless one is on its way or
  // the window reaches the chat's first entry.
  const loadOlder = useCallback(
    async (botId: string) => {
      const load = loads.current.get(botId)
      const w = windows.current.get(botId)
      if (!load?.ready || load.older || !w || w.complete) return
      load.older = true
      try {
        const page = await api.entryWindow(botId, WINDOW_PAGE, w.entries[0].seq)
        if (loads.current.get(botId) === load) put(botId, withPage(windows.current.get(botId) ?? w, page, WINDOW_PAGE))
      } catch (e) {
        setError((e as Error).message)
      } finally {
        load.older = false
      }
    },
    [put],
  )

  // reveal loads older pages of a chat until entryId is loaded, and returns whether it is.
  const reveal = useCallback(
    async (botId: string, entryId: string) => {
      const has = () => windows.current.get(botId)?.entries.some((e) => e.id === entryId) ?? false
      for (;;) {
        if (has()) return true
        const before = windows.current.get(botId)
        await loadOlder(botId)
        if (windows.current.get(botId) === before) return false
      }
    },
    [loadOlder],
  )

  // unload drops a chat's window, so that it loads its newest page again when reopened.
  const unload = useCallback(
    (botId: string) => {
      loads.current.delete(botId)
      put(botId, undefined)
      const next = new Map(statusRef.current)
      next.delete(botId)
      statusRef.current = next
      setStatus(next)
    },
    [put],
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
      const kept = keptChats(recent.current, botId, KEPT_CHATS)
      for (const id of recent.current) if (!kept.includes(id)) unload(id)
      recent.current = kept
      void open(botId)
    },
    [open, unload],
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
      for (const [botId, load] of loads.current) {
        if (load.ready) void refresh(botId)
        else load.refresh = true
      }
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
        update(ev.botId, [ev.entry])
        // These entries can change whether a bot waits on the user (attention).
        if (ev.entry.kind === "approval" || ev.entry.kind === "bot" || ev.entry.kind === "user") scheduleBotsRefresh()
        if (ev.botId !== selectedRef.current && (ev.entry.kind === "bot" || ev.entry.kind === "approval")) {
          setUnread((prev) => new Set(prev).add(ev.botId))
        }
      }
    }
    source.onerror = () => onLink("error")
    return () => source.close()
  }, [refresh, update, refreshBots, scheduleBotsRefresh, onLink, setDrain])

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
