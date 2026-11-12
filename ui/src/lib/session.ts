import { useSyncExternalStore } from "react"
import { api, type Session } from "./api"

// SessionStore holds what the server says about this browser's session: whether it is a paired
// phone. It asks once, when the first component watches it; until the answer comes, and if the
// question fails, the session is the computer's. That only shows more: the server is the lock,
// and it refuses what a phone may not do.
export type SessionStore = {
  phone: () => boolean
  subscribe: (onChange: () => void) => () => void
}

export function sessionStore(load: () => Promise<Session>): SessionStore {
  let phone = false
  let asked = false
  const listeners = new Set<() => void>()
  return {
    phone: () => phone,
    subscribe(onChange) {
      listeners.add(onChange)
      if (!asked) {
        asked = true
        load().then(
          (s) => {
            if (s.phone === phone) return
            phone = s.phone
            for (const l of listeners) l()
          },
          () => {},
        )
      }
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
