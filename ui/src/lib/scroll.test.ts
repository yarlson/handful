import { assert, test } from "vitest"
import { anchorIndex, atBottom, PIN_THRESHOLD, ScrollFollower, ScrollMemory } from "./scroll.ts"

// view returns the metrics of a 1000px transcript in a 200px viewport, scrolled to top.
const view = (scrollTop: number) => ({ scrollTop, scrollHeight: 1000, clientHeight: 200 })
const bottom = 800

test("the view is at the bottom within the threshold, and not beyond it", () => {
  assert.isTrue(atBottom(view(bottom)))
  assert.isTrue(atBottom(view(bottom - PIN_THRESHOLD)))
  assert.isFalse(atBottom(view(bottom - PIN_THRESHOLD - 1)))
  assert.isFalse(atBottom(view(0)))
})

test("the threshold is small", () => {
  assert.isAtLeast(PIN_THRESHOLD, 20)
  assert.isAtMost(PIN_THRESHOLD, 80)
})

test("a chat opened for the first time starts at the bottom and follows it", () => {
  const m = new ScrollMemory()
  assert.isTrue(m.pinned("a"))
  assert.equal(m.target("a", true, 1000), 1000)
  assert.equal(m.target("a", false, 1200), 1200)
})

test("while pinned, new content keeps the view at the bottom", () => {
  const m = new ScrollMemory()
  assert.isTrue(m.record("a", view(bottom)))
  assert.isTrue(m.pinned("a"))
  assert.equal(m.target("a", false, 1300), 1300)
})

test("scrolling up unpins the chat, and new content does not move the view", () => {
  const m = new ScrollMemory()
  assert.isFalse(m.record("a", view(300)))
  assert.isFalse(m.pinned("a"))
  assert.isNull(m.target("a", false, 1300))
})

test("scrolling back to the bottom pins the chat again and forgets its position", () => {
  const m = new ScrollMemory()
  m.record("a", view(300))
  assert.isTrue(m.record("a", view(bottom - 10)))
  assert.isTrue(m.pinned("a"))
  assert.equal(m.target("a", true, 1000), 1000)
})

test("switching back to a chat restores its own position", () => {
  const m = new ScrollMemory()
  m.record("a", view(300))
  m.record("b", view(120))
  m.record("c", view(bottom))
  assert.equal(m.target("a", true, 1000), 300)
  assert.equal(m.target("b", true, 1000), 120)
  assert.equal(m.target("c", true, 1000), 1000)
})

test("the latest scroll replaces a chat's saved position", () => {
  const m = new ScrollMemory()
  m.record("a", view(300))
  m.record("a", view(50))
  assert.equal(m.target("a", true, 1000), 50)
})

test("sending a message pins the chat", () => {
  const m = new ScrollMemory()
  m.record("a", view(300))
  m.record("b", view(300))
  m.pin("a")
  assert.isTrue(m.pinned("a"))
  assert.equal(m.target("a", false, 1100), 1100)
  assert.isFalse(m.pinned("b"))
})

test("the anchor is the first entry that reaches into the view", () => {
  const rects = [{ bottom: -50 }, { bottom: 0 }, { bottom: 1 }, { bottom: 300 }]
  assert.equal(anchorIndex(rects, 0), 2)
  assert.equal(anchorIndex(rects, -100), 0)
  assert.equal(anchorIndex(rects, 300), -1)
  assert.equal(anchorIndex([], 0), -1)
})

// Layout is a fake transcript: entries of given heights stacked in a view of a given height
// whose top edge is at y=100 on the page. It lays out like a browser without scroll anchoring.
class Layout {
  heights: number[]
  viewHeight: number
  connected = true
  readonly isConnected = true
  private top = 0
  constructor(heights: number[], viewHeight: number) {
    this.heights = heights
    this.viewHeight = viewHeight
  }
  // The layout is its own scroller; setting scrollTop clamps it like a browser does.
  get view() {
    return this
  }
  get scrollTop() {
    return this.top
  }
  set scrollTop(v: number) {
    this.top = Math.max(0, Math.min(v, this.scrollHeight - this.viewHeight))
  }
  get scrollHeight() {
    return this.heights.reduce((a, b) => a + b, 0)
  }
  get clientHeight() {
    return this.viewHeight
  }
  getBoundingClientRect() {
    return { top: 100, bottom: 100 + this.viewHeight }
  }
  // entries returns the items of the transcript, keyed e0, e1, ….
  entries() {
    return this.heights.map((_, i) => ({
      key: `e${i}`,
      box: {
        layout: this,
        get isConnected() {
          return this.layout.connected
        },
        getBoundingClientRect: () => {
          const top = 100 - this.scrollTop + this.heights.slice(0, i).reduce((a, b) => a + b, 0)
          return { top, bottom: top + this.heights[i] }
        },
      },
    }))
  }
  // topOf is where entry i is, relative to the view's top edge.
  topOf(i: number) {
    return this.entries()[i].box.getBoundingClientRect().top - 100
  }
}

test("unpinned: an entry above the view that grows does not move what the user reads", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 950
  assert.isFalse(f.scrolled("a", l.view, l.entries()))
  const before = l.topOf(9)
  l.heights[3] += 108
  assert.equal(l.topOf(9), before + 108)
  f.settle("a", l.view)
  assert.equal(l.topOf(9), before)
  assert.equal(l.scrollTop, 1058)
})

test("unpinned: a reflow to another width keeps the top entry in place", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 1030
  f.scrolled("a", l.view, l.entries())
  // Entry 10 is at the top of the view, 30px of it scrolled out.
  assert.equal(l.topOf(10), -30)
  l.heights = l.heights.map((h) => h * 2)
  f.settle("a", l.view)
  assert.equal(l.topOf(10), -30)
})

test("unpinned: content that does not move the anchor leaves the view alone", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 500
  f.scrolled("a", l.view, l.entries())
  l.heights.push(400)
  f.settle("a", l.view)
  assert.equal(l.scrollTop, 500)
})

test("unpinned: an anchor that left the page is not followed", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 500
  f.scrolled("a", l.view, l.entries())
  l.heights[0] += 200
  l.connected = false
  f.settle("a", l.view)
  assert.equal(l.scrollTop, 500)
})

test("pinned: growing content or a shrinking view keeps the chat at the bottom", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 1700
  assert.isTrue(f.scrolled("a", l.view, l.entries()))
  l.heights[5] += 50
  l.heights.push(120)
  f.settle("a", l.view)
  assert.equal(l.scrollTop, l.scrollHeight - l.viewHeight)
  // The goal bar or a taller draft takes room from the view.
  l.viewHeight = 263
  f.settle("a", l.view)
  assert.equal(l.scrollTop, l.scrollHeight - l.viewHeight)
})

test("scrolling back to the bottom drops the anchor and follows again", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 500
  f.scrolled("a", l.view, l.entries())
  l.scrollTop = 1700
  f.scrolled("a", l.view, l.entries())
  l.heights.push(100)
  f.settle("a", l.view)
  assert.equal(l.scrollTop, 1800)
})

test("each chat settles by its own state", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 500
  f.scrolled("a", l.view, l.entries())
  // Chat b was never scrolled, so it is pinned.
  f.settle("b", l.view)
  assert.equal(l.scrollTop, 1700)
})

test("capture without an entry in view keeps no anchor", () => {
  const l = new Layout([], 300)
  const f = new ScrollFollower(new ScrollMemory())
  f.memory.record("a", { scrollTop: 0, scrollHeight: 1000, clientHeight: 300 })
  f.capture("a", l.view, l.entries())
  l.scrollTop = 0
  f.settle("a", l.view)
  assert.equal(l.scrollTop, 0)
})

test("a chat reopens on the entry it was left at, even if images above have not loaded", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 930
  f.scrolled("a", l.view, l.entries())
  assert.equal(l.topOf(9), -30)
  // Back in the chat, an image above has no height yet: scrollTop 930 would show entry 11.
  l.heights[2] = 0
  l.scrollTop = 0
  assert.isTrue(f.restore("a", l.view, l.entries()))
  assert.equal(l.topOf(9), -30)
  // The image loads and the entry stays put.
  l.heights[2] = 100
  f.settle("a", l.view)
  assert.equal(l.topOf(9), -30)
})

test("restore is not possible without a remembered entry on the page", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  assert.isFalse(f.restore("a", l.view, l.entries()))
  l.scrollTop = 930
  f.scrolled("a", l.view, l.entries())
  assert.isFalse(f.restore("b", l.view, l.entries()))
  assert.isFalse(f.restore("a", l.view, l.entries().slice(10)))
  assert.equal(l.scrollTop, 930)
  // Back at the bottom, the chat forgets the entry.
  l.scrollTop = 1700
  f.scrolled("a", l.view, l.entries())
  assert.isFalse(f.restore("a", l.view, l.entries()))
})

test("a view scrolled past every entry forgets the chat's entry", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 930
  f.scrolled("a", l.view, l.entries())
  f.memory.record("a", { scrollTop: 0, scrollHeight: 5000, clientHeight: 300 })
  f.capture("a", l.view, [])
  assert.isFalse(f.restore("a", l.view, l.entries()))
})

test("the follower's own scroll, reported after more content grew, keeps the chat pinned", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  f.settle("a", l.view)
  assert.equal(l.scrollTop, 1700)
  // An image loads before the browser reports the scroll: the view is now 300px short.
  l.heights[3] += 300
  assert.isTrue(f.scrolled("a", l.view, l.entries()))
  f.settle("a", l.view)
  assert.equal(l.scrollTop, 2000)
  // The user's own scroll up still unpins it.
  l.scrollTop = 1000
  assert.isFalse(f.scrolled("a", l.view, l.entries()))
})

test("only the latest scroll of the follower counts as its own", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  f.scrollTo(l.view, 1700)
  assert.isTrue(f.scrolled("a", l.view, l.entries()))
  // The same position reported again is the user's: the content grew and they stayed.
  l.heights[3] += 300
  assert.isFalse(f.scrolled("a", l.view, l.entries()))
})

test("the follower's own scroll of an unpinned chat is recorded like any other", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 500
  f.scrolled("a", l.view, l.entries())
  f.scrollTo(l.view, 1700)
  assert.isTrue(f.scrolled("a", l.view, l.entries()))
})

test("a chat left at its first entry reopens there", () => {
  const l = new Layout(Array(20).fill(100), 300)
  const f = new ScrollFollower(new ScrollMemory())
  l.scrollTop = 30
  f.scrolled("a", l.view, l.entries())
  l.scrollTop = 500
  assert.isTrue(f.restore("a", l.view, l.entries()))
  assert.equal(l.scrollTop, 30)
})
