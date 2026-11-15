import { afterEach, assert, test, vi } from "vitest"
import { clampPanel, clampSidebar, loadStored, loadWidth, panelLayout, saveStored } from "./layout"

afterEach(() => {
  vi.unstubAllGlobals()
})

test("the panel's width stays from 280 to 560 px", () => {
  assert.equal(clampPanel(279), 280)
  assert.equal(clampPanel(280), 280)
  assert.equal(clampPanel(560), 560)
  assert.equal(clampPanel(561), 560)
  assert.equal(clampPanel(400.4), 400)
})

test("the sidebar's width stays from 180 to 480 px", () => {
  assert.equal(clampSidebar(179), 180)
  assert.equal(clampSidebar(481), 480)
  assert.equal(clampSidebar(300), 300)
})

test("an inline panel leaves the chat at least 480 px", () => {
  // 1440 - (240 + 4) sidebar - 4 handle - 480 chat = 712, more than 560.
  assert.deepEqual(panelLayout(1440, 244, 561), { mode: "inline", width: 560, max: 560 })
  assert.deepEqual(panelLayout(1440, 244, 279), { mode: "inline", width: 280, max: 560 })
  // 1280 - 244 - 4 - 480 = 552: the chat's minimum wins over 560.
  assert.deepEqual(panelLayout(1280, 244, 560), { mode: "inline", width: 552, max: 552 })
  assert.deepEqual(panelLayout(1280, 244, 320), { mode: "inline", width: 320, max: 552 })
  // 1024 - 244 - 4 - 480 = 296.
  assert.deepEqual(panelLayout(1024, 244, 320), { mode: "inline", width: 296, max: 296 })
})

test("a collapsed sidebar leaves the panel more room", () => {
  // 1024 - 0 - 4 - 480 = 540.
  assert.deepEqual(panelLayout(1024, 0, 560), { mode: "inline", width: 540, max: 540 })
})

test("where the sidebar leaves no room for a 280 px panel and a 480 px chat, the panel covers the chat", () => {
  // A 480 px sidebar in a 1024 px window: 1024 - 484 - 4 - 480 = 56.
  assert.deepEqual(panelLayout(1024, 484, 320), { mode: "overlay", width: 320, max: 560 })
  // 280 px is just enough: 1248 - 484 - 4 - 480 = 280; 1247 leaves 279.
  assert.equal(panelLayout(1248, 484, 320).mode, "inline")
  assert.equal(panelLayout(1247, 484, 320).mode, "overlay")
})

test("below 64rem the panel covers the chat, at most the window less 48 px; below md it is a sheet", () => {
  assert.deepEqual(panelLayout(1023, 0, 320), { mode: "overlay", width: 320, max: 560 })
  assert.deepEqual(panelLayout(800, 0, 560), { mode: "overlay", width: 560, max: 560 })
  assert.deepEqual(panelLayout(768, 244, 279), { mode: "overlay", width: 280, max: 560 })
  assert.deepEqual(panelLayout(767, 0, 320), { mode: "sheet", width: 767, max: 767 })
  assert.deepEqual(panelLayout(390, 0, 320), { mode: "sheet", width: 390, max: 390 })
})

test("a stored width comes back clamped, and a missing or broken one gives the initial width", () => {
  const store = new Map<string, string>()
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => store.set(k, v),
  })
  assert.equal(loadWidth("w", clampPanel, 320), 320, "nothing stored")
  saveStored("w", "999")
  assert.equal(loadWidth("w", clampPanel, 320), 560)
  saveStored("w", "300")
  assert.equal(loadWidth("w", clampPanel, 320), 300)
  saveStored("w", "wide")
  assert.equal(loadWidth("w", clampPanel, 320), 320, "not a number")
})

test("storage that refuses reads and writes loses the choice, without an error", () => {
  vi.stubGlobal("localStorage", {
    getItem: () => {
      throw new Error("denied")
    },
    setItem: () => {
      throw new Error("denied")
    },
  })
  assert.isNull(loadStored("w"))
  saveStored("w", "300")
  assert.equal(loadWidth("w", clampPanel, 320), 320)
})
