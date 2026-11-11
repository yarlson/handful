import type { Entry } from "./api"
import { loadEntriesAfter } from "./entries"
import type { TranscriptEvent } from "./transcript"
import { keptChats, openWindow, WINDOW_PAGE, withOlderPage, withUpdates, type EntryWindow } from "./window"

// KEPT_CHATS is how many chats keep their loaded window: the open one and the ones opened
// just before it. The others unload, and load their newest page again when reopened.
export const KEPT_CHATS = 4

// Server is what ChatWindows reads from the server and tells the page.
export type Server = {
  // page returns a chat's newest limit entries, below seq before when given, in chat order.
  page: (id: string, limit: number, before?: number) => Promise<Entry[]>
  // changes returns a page of a chat's entries with a rev above after.
  changes: (id: string, after: number) => Promise<Entry[]>
  onWindows: (windows: Map<string, EntryWindow>) => void
  onStatus: (id: string, event: TranscriptEvent) => void
  onUnload: (id: string) => void
  onError: (message: string) => void
}

// Load is one loaded chat.
type Load = {
  opened: Promise<void> // its newest page, on its way or arrived
  window: EntryWindow | null // null until its newest page arrives
  buffer: Entry[] // updates that arrived before the newest page, applied after it
  refresh: boolean // a reconnect came before the newest page: fetch the changes after it
  older: Promise<void> | null // the older page on its way
  below: Entry[] // updates older than the window that arrived while that page was on its way
}

// ChatWindows loads the windows of the open chats: a chat loads its newest page once when it
// opens, older pages on demand, and, after each reconnect, only the changes after its rev.
export class ChatWindows {
  private loads = new Map<string, Load>()
  private recent: string[] = []

  constructor(private server: Server) {}

  get(id: string): EntryWindow | undefined {
    return this.loads.get(id)?.window ?? undefined
  }

  // select opens chat id, and unloads the chats beyond the KEPT_CHATS opened most recently.
  select(id: string): Promise<void> {
    const kept = keptChats(this.recent, id, KEPT_CHATS)
    for (const old of this.recent) if (!kept.includes(old)) this.unload(old)
    this.recent = kept
    return this.open(id)
  }

  // open loads chat id's newest page, once: a loaded chat stays current through updates.
  open(id: string): Promise<void> {
    const loaded = this.loads.get(id)
    if (loaded) return loaded.opened
    const load: Load = { opened: Promise.resolve(), window: null, buffer: [], refresh: false, older: null, below: [] }
    this.loads.set(id, load)
    load.opened = this.fetchNewest(id, load)
    return load.opened
  }

  private async fetchNewest(id: string, load: Load) {
    this.server.onStatus(id, "fetch")
    try {
      const page = await this.server.page(id, WINDOW_PAGE)
      if (this.loads.get(id) !== load) return
      const w = withUpdates(openWindow(page, WINDOW_PAGE), load.buffer)
      this.put(load, w)
      this.server.onStatus(id, "loaded")
      if (load.refresh) await this.refresh(id, w.rev)
    } catch (e) {
      if (this.loads.get(id) !== load) return
      this.loads.delete(id)
      this.server.onStatus(id, "failed")
      this.server.onError((e as Error).message)
    }
  }

  // update applies new and changed entries of chat id. Before its newest page arrives they
  // wait for it; an update older than the window waits for the older page on its way, which
  // may hold the entry's previous version.
  update(id: string, entries: Entry[]) {
    const load = this.loads.get(id)
    if (!load) return
    const w = load.window
    if (!w) {
      load.buffer.push(...entries)
      return
    }
    // The older page applies them again; for the entries inside the window that is a no-op.
    if (load.older) load.below.push(...entries)
    this.put(load, withUpdates(w, entries))
  }

  // reconnected fetches, for each loaded chat, the changes after its rev; a chat whose newest
  // page is on its way does it once the page arrives.
  reconnected() {
    for (const [id, load] of this.loads) {
      if (load.window) void this.refresh(id, load.window.rev)
      else load.refresh = true
    }
  }

  private async refresh(id: string, after: number) {
    try {
      await loadEntriesAfter(
        (rev) => this.server.changes(id, rev),
        after,
        (entries) => this.update(id, entries),
      )
    } catch (e) {
      this.server.onError((e as Error).message)
    }
  }

  // loadOlder loads the page above chat id's window, or returns the one on its way. It does
  // nothing once the window reaches the chat's first entry.
  loadOlder(id: string): Promise<void> {
    const load = this.loads.get(id)
    if (!load?.window || load.window.complete) return Promise.resolve()
    load.older ??= this.fetchOlder(id, load, load.window.entries[0].seq).finally(() => {
      load.older = null
    })
    return load.older
  }

  private async fetchOlder(id: string, load: Load, before: number) {
    try {
      const page = await this.server.page(id, WINDOW_PAGE, before)
      // A chat unloaded meanwhile keeps its window out of the loaded ones, so the page is
      // dropped with it.
      const below = load.below
      load.below = []
      // An older page starts only from a loaded window, and a load's window never goes back
      // to null.
      this.put(load, withUpdates(withOlderPage(load.window!, page, WINDOW_PAGE), below))
    } catch (e) {
      this.server.onError((e as Error).message)
    }
  }

  // reveal loads older pages of chat id until entryId is loaded, and reports whether it is.
  async reveal(id: string, entryId: string): Promise<boolean> {
    await this.loads.get(id)?.opened
    for (;;) {
      const w = this.get(id)
      if (w?.entries.some((e) => e.id === entryId)) return true
      await this.loadOlder(id)
      if (this.get(id) === w) return false
    }
  }

  private unload(id: string) {
    this.loads.delete(id)
    this.changed()
    this.server.onUnload(id)
  }

  private put(load: Load, w: EntryWindow) {
    load.window = w
    this.changed()
  }

  // changed tells the page the loaded windows: at most KEPT_CHATS of them.
  private changed() {
    const windows = new Map<string, EntryWindow>()
    for (const [id, load] of this.loads) if (load.window) windows.set(id, load.window)
    this.server.onWindows(windows)
  }
}
