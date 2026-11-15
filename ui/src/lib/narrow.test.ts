import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import { isNarrow, narrowQuery, useNarrow, useWindowWidth, watchNarrow, watchWidth } from "./narrow"

afterEach(() => {
  vi.unstubAllGlobals()
})

// media stubs window.matchMedia: narrow tells whether the md query matches, and it records the
// queries asked and the listeners added and removed.
function media(narrow: boolean) {
  const asked: string[] = []
  const listeners: string[] = []
  vi.stubGlobal("window", {
    matchMedia: (q: string) => {
      asked.push(q)
      return {
        matches: narrow && q === narrowQuery,
        addEventListener: (type: string) => listeners.push(`+${type}`),
        removeEventListener: (type: string) => listeners.push(`-${type}`),
      }
    },
  })
  return { asked, listeners }
}

test("a window narrower than 48rem is narrow, and a wider one isn't", () => {
  assert.equal(narrowQuery, "(max-width: 47.99rem)")
  const m = media(true)
  assert.isTrue(isNarrow())
  assert.deepEqual(m.asked, [narrowQuery])
  media(false)
  assert.isFalse(isNarrow())
})

test("watching the breakpoint listens for changes until it is stopped", () => {
  const m = media(false)
  const stop = watchNarrow(() => {})
  assert.deepEqual(m.listeners, ["+change"])
  stop()
  assert.deepEqual(m.listeners, ["+change", "-change"])
})

test("a server render is not narrow", () => {
  const Probe = () => String(useNarrow())
  assert.equal(renderToStaticMarkup(createElement(Probe)), "false")
})

// resizable stubs a window innerWidth wide that records the listeners added and removed.
function resizable(innerWidth: number) {
  const listeners: string[] = []
  vi.stubGlobal("window", {
    innerWidth,
    addEventListener: (type: string) => listeners.push(`+${type}`),
    removeEventListener: (type: string) => listeners.push(`-${type}`),
  })
  return listeners
}

test("watching the width listens for resizes until it is stopped", () => {
  const listeners = resizable(1024)
  const stop = watchWidth(() => {})
  assert.deepEqual(listeners, ["+resize"])
  stop()
  assert.deepEqual(listeners, ["+resize", "-resize"])
})

test("the window's width is its inner width in CSS pixels", () => {
  resizable(1024)
  const Probe = () => String(useWindowWidth())
  assert.equal(renderToStaticMarkup(createElement(Probe)), "1024")
})
