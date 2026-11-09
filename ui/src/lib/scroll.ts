// A pinned chat stays pinned while its view is within this many pixels of the bottom, so
// that a scrollbar drag or a layout shift does not unpin it.
export const PIN_THRESHOLD = 48
// An unpinned chat pins again only once its view is this close to the bottom: the user has
// scrolled all the way down. Browsers can leave a fraction of a pixel.
export const REPIN_THRESHOLD = 2

export type ScrollMetrics = { scrollTop: number; scrollHeight: number; clientHeight: number }

// atBottom tells whether the view is within threshold pixels of the bottom of the content.
export function atBottom(m: ScrollMetrics, threshold = PIN_THRESHOLD): boolean {
  return m.scrollHeight - m.scrollTop - m.clientHeight <= threshold
}

// Intent is a user's input on the transcript that may scroll it.
export type Intent = { kind: "wheel"; deltaY: number } | { kind: "key"; key: string } | { kind: "touch"; dy: number } // how far the finger moved down since the last touch event

const upKeys = new Set(["ArrowUp", "PageUp", "Home"])

// scrollsUp tells whether an input asks to see older messages. A small wheel or trackpad
// step stays within PIN_THRESHOLD, so the position alone cannot tell it apart from the
// bottom; the input itself says what the user wants.
export function scrollsUp(i: Intent): boolean {
  if (i.kind === "wheel") return i.deltaY < 0
  if (i.kind === "key") return upKeys.has(i.key)
  return i.dy > 0
}

// ScrollMemory keeps where the user left each chat. A chat it has no position for is
// pinned: it follows the bottom, and a chat opened for the first time starts there.
export class ScrollMemory {
  private saved = new Map<string, number>()

  // record notes the view of chat id after a scroll and returns whether it is now pinned.
  // Scrolling up unpins the chat; scrolling back to the bottom pins it again.
  record(id: string, m: ScrollMetrics): boolean {
    if (atBottom(m, this.pinned(id) ? PIN_THRESHOLD : REPIN_THRESHOLD)) {
      this.saved.delete(id)
      return true
    }
    this.saved.set(id, m.scrollTop)
    return false
  }

  // unpin stops chat id following the bottom at scrollTop, as when the user scrolls up.
  unpin(id: string, scrollTop: number) {
    this.saved.set(id, scrollTop)
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

// Box is the part of an element that ScrollFollower measures.
export type Box = { getBoundingClientRect(): { top: number; bottom: number }; readonly isConnected: boolean }
export type ScrollBox = Box & ScrollMetrics
// Item is an entry of the transcript: its id and its element.
export type Item = { key: string; box: Box }

// ScrollFollower keeps a chat's transcript where the user wants it whenever its content or its
// view changes size: a pinned chat stays at the bottom, and an unpinned one keeps the entry at
// the top of the view where it was. Browsers anchor scrolling differently (Safari not at
// all), so the transcript turns native anchoring off and this does it in every browser.
// It also remembers that entry per chat, so a chat reopens on the entry the user left it at
// even when images above it have not loaded yet.
export class ScrollFollower {
  // anchor is the entry at the top of the view on screen and its offset from the view's top.
  private anchor: { box: Box; offset: number } | null = null
  // left is, per unpinned chat, the entry at the top of its view and its offset.
  private left = new Map<string, { key: string; offset: number }>()
  // moved is the scrollTop this follower last scrolled the view to. The browser reports that
  // scroll later, when more content may have grown below, so its event is not the user's.
  private moved: number | null = null

  constructor(readonly memory: ScrollMemory) {}

  // scrolled notes a scroll of chat id's view over its items and returns whether the chat
  // is pinned now.
  scrolled(id: string, view: ScrollBox, items: Item[]): boolean {
    const ours = view.scrollTop === this.moved
    this.moved = null
    if (ours && this.memory.pinned(id)) return true
    const pinned = this.memory.record(id, view)
    if (!pinned) {
      this.capture(id, view, items)
      return false
    }
    this.anchor = null
    this.left.delete(id)
    return true
  }

  // intent notes a user's input on chat id's view and returns whether the chat is pinned
  // now. An input that scrolls up unpins it before its scroll arrives, so that no update of
  // the transcript in between pulls the view back down. A view already at the top, or one
  // with nothing to scroll, cannot move up, and no scroll would follow to pin it again.
  intent(id: string, view: ScrollBox, items: Item[], input: Intent): boolean {
    if (!scrollsUp(input) || view.scrollTop <= 0) return this.memory.pinned(id)
    this.memory.unpin(id, view.scrollTop)
    this.capture(id, view, items)
    return false
  }

  // capture remembers the item at the top of chat id's view and how far below the view's top
  // it is.
  capture(id: string, view: Box, items: Item[]) {
    const viewTop = view.getBoundingClientRect().top
    const rects = items.map((it) => it.box.getBoundingClientRect())
    const i = anchorIndex(rects, viewTop)
    if (i < 0) {
      this.anchor = null
      this.left.delete(id)
      return
    }
    const offset = rects[i].top - viewTop
    this.anchor = { box: items[i].box, offset }
    this.left.set(id, { key: items[i].key, offset })
  }

  // restore scrolls chat id's view back to the item it was left at, and returns false when
  // that item is not among items.
  restore(id: string, view: ScrollBox, items: Item[]): boolean {
    const at = this.left.get(id)
    const item = at && items.find((it) => it.key === at.key)
    if (!at || !item) return false
    this.anchor = { box: item.box, offset: at.offset }
    this.settle(id, view)
    return true
  }

  // scrollTo scrolls the view to top.
  scrollTo(view: ScrollBox, top: number) {
    view.scrollTop = top
    this.moved = view.scrollTop
  }

  // settle puts chat id's view back after its content or its size changed.
  settle(id: string, view: ScrollBox) {
    if (this.memory.pinned(id)) {
      this.scrollTo(view, view.scrollHeight)
      return
    }
    const a = this.anchor
    if (!a?.box.isConnected) return
    const moved = a.box.getBoundingClientRect().top - view.getBoundingClientRect().top - a.offset
    this.scrollTo(view, view.scrollTop + moved)
  }
}
