import type { Connector } from "./api"

// What the Connectors dialog shows and offers for each connector status. The server decides
// the status; this table is the one place that maps it to the badge and the buttons.
//   static:        connectors.json has the server's headers; nothing to sign in to.
//   needs-sign-in: an OAuth server without a token.
//   connected:     an OAuth server with a token.
//   expired:       an OAuth server whose token no longer works.
export type ConnectorView = {
  label: string
  // ok shows the badge as fine; otherwise it shows as a problem.
  ok: boolean
  // signIn is the sign-in button's text, or null when there is nothing to sign in to.
  signIn: "Connect" | "Reconnect" | null
  // disconnect tells whether the user can remove the token.
  disconnect: boolean
}

export const CONNECTOR_VIEWS: Record<Connector["status"], ConnectorView> = {
  static: { label: "token in file", ok: true, signIn: null, disconnect: false },
  "needs-sign-in": { label: "needs sign-in", ok: false, signIn: "Connect", disconnect: false },
  connected: { label: "connected", ok: true, signIn: "Reconnect", disconnect: true },
  expired: { label: "expired", ok: false, signIn: "Connect", disconnect: true },
}

// The dialog's list and its error:
//   loading: no list yet and no error; the first GET /api/connectors is under way.
//   ready:   the list is shown.
//   failed:  the error is shown, with the last list if there was one.
// error.from says what failed: a load clears only a load's error, so a sign-in or disconnect
// error stays until the user acts again.
export type ConnectorsState = {
  list: Connector[] | null
  error: { from: "load" | "action"; text: string } | null
}

export type ConnectorsEvent =
  | { type: "loaded"; list: Connector[] }
  | { type: "loadFailed"; text: string }
  | { type: "actionDone" }
  | { type: "actionFailed"; text: string }

export const NO_CONNECTORS: ConnectorsState = { list: null, error: null }

// connectorsStep applies an event to the dialog's state. Every event is valid in every state.
export function connectorsStep(s: ConnectorsState, e: ConnectorsEvent): ConnectorsState {
  if (e.type === "loaded") return { list: e.list, error: s.error?.from === "action" ? s.error : null }
  if (e.type === "loadFailed") return { list: s.list, error: { from: "load", text: e.text } }
  if (e.type === "actionDone") return { list: s.list, error: null }
  return { list: s.list, error: { from: "action", text: e.text } }
}

// connectorsPhase names the dialog's state, for its data attribute and its empty line.
export function connectorsPhase(s: ConnectorsState): "loading" | "ready" | "failed" {
  if (s.error) return "failed"
  return s.list ? "ready" : "loading"
}
