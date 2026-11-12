// Phone access: the words and small rules of Settings › Phones, the pair request and the pair page
// (spec 6a748f8a, P1 to P10). The server decides what a phone may do; these only word it.

export type Scope = "view" | "chat" | "chat+approve"
export type Proxy = "cloudflare" | "tailscale"

// The three scopes, in the order Settings offers them, with the words of P3.
export const SCOPES: { id: Scope; name: string; help: string }[] = [
  { id: "view", name: "View", help: "only read." },
  { id: "chat", name: "Chat", help: "read and send messages. It can't approve a bot's requests." },
  { id: "chat+approve", name: "Chat and approve once", help: "also Allow once or Deny a bot's request." },
]

// DEFAULT_SCOPE is the scope a new pairing code starts with.
export const DEFAULT_SCOPE: Scope = "chat+approve"

// scopeName names a scope as Settings and the pair request show it.
export function scopeName(scope: string): string {
  return SCOPES.find((s) => s.id === scope)?.name ?? scope
}

// codeGroups shows a pairing code in groups of 4: K7QM-2XPD-9WTR.
export function codeGroups(code: string): string {
  return code.match(/.{1,4}/g)?.join("-") ?? ""
}

// pairLink is the address the QR holds: the code is in the fragment, so it never reaches a log.
export function pairLink(host: string, code: string): string {
  return `https://${host}/pair#${code}`
}

// codeOfHash reads the code from the pair page's fragment; "" when the link lost it.
export function codeOfHash(hash: string): string {
  return /^#([0-9A-Za-z-]{1,20})$/.exec(hash)?.[1] ?? ""
}

// timeLeft is the countdown of a pair request: "1:52 left"; it stops at 0:00.
export function timeLeft(expiresAt: number, now: number): string {
  const s = Math.max(0, Math.ceil((expiresAt - now) / 1000))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")} left`
}

// lockedText is P6, while pairing is paused after wrong codes.
export const LOCKED_TEXT = "5 wrong pairing codes were tried. Pairing is paused for 10 minutes."

// decidedOn is P7: the note on an approval card that a phone decided.
export function decidedOn(decision: string | undefined, device: string): string {
  return decision === "deny" ? `Denied from ${device}.` : `Approved once from ${device}.`
}

// ALWAYS_ON_COMPUTER is P8: why a phone has no Always allow.
export const ALWAYS_ON_COMPUTER = "Always allow can only be chosen on the computer."

// VIEW_ONLY is P10: what a phone paired for View shows instead of the message box.
export const VIEW_ONLY = "Your phone can only view."

// Rights is what this browser's session may do. A computer may do everything; a phone never
// changes settings or the shape of a team, and its scope decides messages and approvals.
export type Rights = { computer: boolean; send: boolean; approve: boolean }

export const COMPUTER: Rights = { computer: true, send: true, approve: true }

export function rightsOf(session: { phone: boolean; scope?: string }): Rights {
  if (!session.phone) return COMPUTER
  return {
    computer: false,
    send: session.scope === "chat" || session.scope === "chat+approve",
    approve: session.scope === "chat+approve",
  }
}

// splitHosts reads the phone addresses typed in Settings: one or more, split by commas or spaces.
export function splitHosts(text: string): string[] {
  return text.split(/[\s,]/).filter(Boolean)
}
