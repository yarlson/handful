// The load state of one chat's transcript. useYarGPT (store.ts) fetches a chat's entries when
// it opens and again after each reconnect; this file holds the states and transitions, so the
// chat can tell "still loading" and "couldn't load" from a chat that has no messages.
//   unloaded: the page never asked for this chat's entries (no entry in the map).
//   loading:  the first fetch, or a retry after a failure, is under way.
//   ready:    the transcript was loaded once; later fetches only add to it.
//   failed:   the last fetch failed before the transcript was ever loaded.
export type TranscriptStatus = "unloaded" | "loading" | "ready" | "failed"

// TranscriptEvent: fetch when a fetch starts (open or reconnect), loaded when every page
// arrived, failed when a page request failed.
export type TranscriptEvent = "fetch" | "loaded" | "failed"

// TRANSCRIPT is the transition table. A missing entry is invalid: a result can't arrive for a
// chat that was never fetched. A ready chat stays ready: its entries stay on screen, and a
// failed refresh shows only the app's error banner, as before.
export const TRANSCRIPT: Record<TranscriptStatus, Partial<Record<TranscriptEvent, TranscriptStatus>>> = {
  unloaded: { fetch: "loading" },
  loading: { fetch: "loading", loaded: "ready", failed: "failed" },
  ready: { fetch: "ready", loaded: "ready", failed: "ready" },
  failed: { fetch: "loading", loaded: "ready", failed: "failed" },
}

export type TranscriptStep =
  | { ok: true; status: TranscriptStatus }
  | { ok: false; status: TranscriptStatus; error: string }

// transcriptStep applies event to status. An invalid transition keeps the status and says why.
export function transcriptStep(status: TranscriptStatus, event: TranscriptEvent): TranscriptStep {
  const next = TRANSCRIPT[status][event]
  if (next === undefined) return { ok: false, status, error: `transcript: ${event} is not valid while ${status}` }
  return { ok: true, status: next }
}

// emptyText is what a chat without entries says in each status.
export function emptyText(status: TranscriptStatus): string {
  if (status === "ready") return "No messages yet."
  if (status === "failed") return "Couldn't load the messages."
  return "Loading messages…"
}
