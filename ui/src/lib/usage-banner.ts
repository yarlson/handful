// The banner that says new turns wait for the Claude usage limit, and whether the user closed it.
// Close stores the message it closed in this browser, so the banner comes back only with another
// message: another end time of the pause.

import { loadStored, saveStored } from "./layout"

const CLOSED_KEY = "handful.usageBannerClosed"

// usageMessage is the banner's text for a pause that ends at pausedUntil, an RFC 3339 time.
export function usageMessage(pausedUntil: string): string {
  const until = new Date(pausedUntil).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })
  return `Claude usage limit reached. New turns wait until ${until}.`
}

// closedUsageMessage is the message that the user closed last in this browser.
export function closedUsageMessage(): string | null {
  return loadStored(CLOSED_KEY)
}

export function closeUsageBanner(message: string) {
  saveStored(CLOSED_KEY, message)
}

// usageBannerShown tells whether the banner shows message, given the message the user closed.
export function usageBannerShown(message: string | null, closed: string | null): message is string {
  return message !== null && message !== closed
}
