import { useSyncExternalStore } from "react"
import { api, type Session } from "./api"
import { rightsOf, type Rights } from "./phones"

// SessionStore holds what the server says about this browser's session: whether it is a paired
// phone, its scope and name, or a phone whose session no longer works. It asks when the first
// component watches it, and again on recheck; until an answer comes, and if a question fails,
// the session is the computer's. That only shows more: the server is the lock, and it refuses
// what a phone may not do.
export type SessionStore = {
  session: () => Session
  phone: () => boolean
  recheck: () => void
  subscribe: (onChange: () => void) => () => void
}

const COMPUTER_SESSION: Session = { phone: false }

function same(a: Session, b: Session) {
  return a.phone === b.phone && a.scope === b.scope && a.device === b.device && a.lost === b.lost
}

export function sessionStore(load: () => Promise<Session>): SessionStore {
  let session = COMPUTER_SESSION
  let asked = false
  const listeners = new Set<() => void>()
  const ask = () => {
    asked = true
    load().then(
      (s) => {
        if (same(s, session)) return
        session = s
        for (const l of listeners) l()
      },
      () => {},
    )
  }
  return {
    session: () => session,
    phone: () => session.phone,
    // recheck asks again after a sign that the session may have ended, such as a lost event
    // stream; before the first question it does nothing.
    recheck: () => {
      if (asked) ask()
    },
    subscribe(onChange) {
      listeners.add(onChange)
      if (!asked) ask()
      return () => {
        listeners.delete(onChange)
      }
    },
  }
}

// phoneSession is this page's session, as the server tells it.
export const phoneSession = sessionStore(() => api.session())

// usePhoneSession tells whether this browser is a paired phone, so the UI leaves out what the
// server would refuse it. It is never decided by the window's width; a server render is no phone.
export function usePhoneSession(): boolean {
  return useSyncExternalStore(phoneSession.subscribe, phoneSession.phone, () => false)
}

// useSession is this browser's whole session: on a phone its scope and name, or lost.
export function useSession(): Session {
  return useSyncExternalStore(phoneSession.subscribe, phoneSession.session, () => COMPUTER_SESSION)
}

// useRights is what this browser may do, from its session.
export function useRights(): Rights {
  return rightsOf(useSession())
}
