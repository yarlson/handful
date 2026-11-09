// A chat follows new messages while its view is within this many pixels of the bottom.
export const PIN_THRESHOLD = 48

export type ScrollMetrics = { scrollTop: number; scrollHeight: number; clientHeight: number }

// atBottom tells whether the view is at or near the bottom of the content.
export function atBottom(m: ScrollMetrics): boolean {
  return m.scrollHeight - m.scrollTop - m.clientHeight <= PIN_THRESHOLD
}

// ScrollMemory keeps where the user left each chat. A chat it has no position for is
// pinned: it follows the bottom, and a chat opened for the first time starts there.
export class ScrollMemory {
  private saved = new Map<string, number>()

  // record notes the view of chat id after a scroll and returns whether it is now pinned.
  // Scrolling up unpins the chat; scrolling back to the bottom pins it again.
  record(id: string, m: ScrollMetrics): boolean {
    if (atBottom(m)) {
      this.saved.delete(id)
      return true
    }
    this.saved.set(id, m.scrollTop)
    return false
  }

  // pin makes chat id follow the bottom again, as when the user sends a message.
  pin(id: string) {
    this.saved.delete(id)
  }

  pinned(id: string): boolean {
    return !this.saved.has(id)
  }

  // target returns the scrollTop to move chat id to after its content changed, or null to
  // leave the view alone. opened says the chat has just been switched to.
  target(id: string, opened: boolean, scrollHeight: number): number | null {
    const top = this.saved.get(id)
    if (top === undefined) return scrollHeight
    return opened ? top : null
  }
}

// anchorIndex returns the first of the transcript's entries, top to bottom, that reaches into
// the view whose top edge is at viewTop, or -1 when none does. An unpinned view keeps that
// entry where it is when media above it loads and grows the transcript.
export function anchorIndex(rects: { bottom: number }[], viewTop: number): number {
  return rects.findIndex((r) => r.bottom > viewTop)
}
