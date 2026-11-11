// The connection to the server's event stream (/api/events) and the drain flag that the
// stream and GET /api/version both report. useHandful (store.ts) drives these machines; this
// file holds their states and every transition, so the UI, tests and bots read one place.

// Link is the state of the event stream:
//   connecting:   the page has not received the stream's hello yet.
//   live:         the stream is open; events arrive.
//   reconnecting: the stream failed after or before a hello; the browser retries it.
export type Link = "connecting" | "live" | "reconnecting"

// LinkEvent is what the stream reports: hello when a connection opens, error when it fails.
export type LinkEvent = "hello" | "error"

// LINKS is the transition table of the event stream. A missing entry is an invalid
// transition: the server sends hello once per connection, so a second hello without an
// error in between means the stream and the UI disagree.
export const LINKS: Record<Link, Partial<Record<LinkEvent, Link>>> = {
  connecting: { hello: "live", error: "reconnecting" },
  live: { error: "reconnecting" },
  reconnecting: { hello: "live", error: "reconnecting" },
}

export type Step = { ok: true; state: Link } | { ok: false; state: Link; error: string }

// linkStep applies event to state. An invalid transition keeps the state and says why.
export function linkStep(state: Link, event: LinkEvent): Step {
  const next = LINKS[state][event]
  if (next === undefined) return { ok: false, state, error: `connection: ${event} is not valid while ${state}` }
  return { ok: true, state: next }
}

// linkLabel is how the sidebar names the connection.
export function linkLabel(state: Link): string {
  if (state === "live") return "Connected"
  if (state === "connecting") return "Connecting…"
  return "Reconnecting…"
}

// Drain is whether the server starts no new turns until it restarts for an update. rev
// counts every change and every question, so an answer that a newer event or a newer
// question overtook is ignored.
export type Drain = { draining: boolean; rev: number }

export const NO_DRAIN: Drain = { draining: false, rev: 0 }

// drainAsked starts a question to GET /api/version. Its rev is the token to answer with.
export function drainAsked(d: Drain): Drain {
  return { draining: d.draining, rev: d.rev + 1 }
}

// drainChanged applies a drain event from the stream; it always wins over open questions.
export function drainChanged(d: Drain, draining: boolean): Drain {
  return { draining, rev: d.rev + 1 }
}

// drainAnswered applies the answer to the question with token asked, unless a drain event
// or a newer question came after it.
export function drainAnswered(d: Drain, asked: number, draining: boolean): Drain {
  return asked === d.rev ? { draining, rev: d.rev } : d
}
