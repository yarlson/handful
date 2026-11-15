import { readFileSync } from "node:fs"
import { join } from "node:path"
import { runInNewContext } from "node:vm"
import { afterEach, assert, test, vi } from "vitest"
import { choose, followAppearance, loadAppearance, showAppearance, type Appearance } from "./appearance"

afterEach(() => vi.unstubAllGlobals())

// store stubs localStorage with entries and gives the map that setItem writes to.
function store(entries: Record<string, string>): Map<string, string> {
  const map = new Map(Object.entries(entries))
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => map.set(k, v),
  })
  return map
}

// fakeRoot stands for <html>: its classes and its data attributes.
function fakeRoot() {
  const classes = new Set<string>()
  return {
    classes,
    classList: { toggle: (name: string, on: boolean) => (on ? classes.add(name) : classes.delete(name)) },
    dataset: {} as DOMStringMap,
  }
}

const defaults: Appearance = { theme: "system", chatWidth: "normal", textSize: "medium" }

test("with nothing stored, appearance is the System theme, Normal width and Medium text", () => {
  store({})
  assert.deepEqual(loadAppearance(), defaults)
})

test("a stored choice loads", () => {
  store({ "handful.theme": "dark", "handful.chatWidth": "wide", "handful.textSize": "large" })
  assert.deepEqual(loadAppearance(), { theme: "dark", chatWidth: "wide", textSize: "large" })
})

test("an unknown or old stored value falls back to its default", () => {
  store({ "handful.theme": "auto", "handful.chatWidth": "64rem", "handful.textSize": "16" })
  assert.deepEqual(loadAppearance(), defaults)
})

test("storage that the browser blocks gives the defaults", () => {
  vi.stubGlobal("localStorage", {
    getItem: () => {
      throw new Error("SecurityError")
    },
  })
  assert.deepEqual(loadAppearance(), defaults)
})

test("choose saves the choice for its key only, and gives the new appearance", () => {
  const saved = store({})
  const next = choose(defaults, "chatWidth", "wide")
  assert.deepEqual(next, { theme: "system", chatWidth: "wide", textSize: "medium" })
  assert.deepEqual([...saved], [["handful.chatWidth", "wide"]])
  assert.deepEqual(loadAppearance(), next)
})

test("choose saves each key under its own name", () => {
  const saved = store({})
  choose(choose(defaults, "theme", "light"), "textSize", "large")
  assert.deepEqual(Object.fromEntries(saved), { "handful.theme": "light", "handful.textSize": "large" })
})

test("choose turns an unknown value into the key's default", () => {
  const saved = store({})
  assert.deepEqual(choose({ ...defaults, theme: "dark" }, "theme", "sepia"), defaults)
  assert.equal(saved.get("handful.theme"), "system")
})

test("showAppearance sets the dark class by theme and system scheme, and the width and size attributes", () => {
  const cases: [Appearance["theme"], boolean, boolean][] = [
    ["dark", false, true],
    ["dark", true, true],
    ["light", true, false],
    ["light", false, false],
    ["system", true, true],
    ["system", false, false],
  ]
  for (const [theme, systemDark, dark] of cases) {
    const root = fakeRoot()
    // The class starts in the other state, so the test sees showAppearance change it.
    if (!dark) root.classes.add("dark")
    showAppearance(root, { theme, chatWidth: "wide", textSize: "large" }, systemDark)
    assert.equal(root.classes.has("dark"), dark, `theme ${theme}, system dark ${systemDark}`)
    assert.equal(root.dataset.chatWidth, "wide")
    assert.equal(root.dataset.textSize, "large")
  }
})

// fakeSystem stands for matchMedia("(prefers-color-scheme: dark)"); turn() changes the scheme.
function fakeSystem(matches: boolean) {
  const listeners: (() => void)[] = []
  const system = {
    matches,
    addEventListener: (type: string, listener: () => void) => {
      if (type === "change") listeners.push(listener)
    },
    turn(dark: boolean) {
      system.matches = dark
      for (const l of listeners) l()
    },
  }
  return system
}

test("with the System theme, the page follows the system's colour scheme as it changes", () => {
  store({ "handful.textSize": "large" })
  const root = fakeRoot()
  const system = fakeSystem(false)
  followAppearance(root, system as unknown as MediaQueryList)
  assert.isFalse(root.classes.has("dark"))
  assert.equal(root.dataset.textSize, "large")
  system.turn(true)
  assert.isTrue(root.classes.has("dark"))
  system.turn(false)
  assert.isFalse(root.classes.has("dark"))
})

test("a Light or Dark theme stays when the system's colour scheme changes", () => {
  for (const theme of ["light", "dark"]) {
    store({ "handful.theme": theme })
    const root = fakeRoot()
    const system = fakeSystem(theme === "light")
    followAppearance(root, system as unknown as MediaQueryList)
    system.turn(theme !== "light")
    assert.equal(root.classes.has("dark"), theme === "dark", theme)
  }
})

test("a choice made after load shows on the next change of the system's colour scheme", () => {
  store({})
  const root = fakeRoot()
  const system = fakeSystem(false)
  followAppearance(root, system as unknown as MediaQueryList)
  choose(defaults, "theme", "dark")
  system.turn(false)
  assert.isTrue(root.classes.has("dark"))
})

// The script in index.html runs before the bundle, so it repeats the theme rule; it must give the
// dark class that showAppearance gives for every stored theme and system scheme.
const html = readFileSync(join(import.meta.dirname, "../../index.html"), "utf8")
const script = /<script>([\s\S]*?)<\/script>/.exec(html)?.[1] ?? ""

function runScript(storage: Pick<Storage, "getItem">, systemDark: boolean) {
  const root = fakeRoot()
  const matchMedia = (q: string) => ({ matches: q === "(prefers-color-scheme: dark)" && systemDark })
  runInNewContext(script, { localStorage: storage, matchMedia, document: { documentElement: root } })
  return root.classes.has("dark")
}

test("the index.html script shows the stored theme as showAppearance does", () => {
  assert.include(script, "handful.theme")
  for (const stored of [null, "system", "light", "dark", "auto"]) {
    for (const systemDark of [false, true]) {
      const map = store(stored === null ? {} : { "handful.theme": stored })
      const root = fakeRoot()
      showAppearance(root, loadAppearance(), systemDark)
      const dark = runScript({ getItem: (k) => map.get(k) ?? null }, systemDark)
      assert.equal(dark, root.classes.has("dark"), `stored ${stored}, system dark ${systemDark}`)
    }
  }
})

test("the index.html script follows the system when storage is blocked", () => {
  const blocked = {
    getItem: (): string | null => {
      throw new Error("SecurityError")
    },
  }
  assert.isTrue(runScript(blocked, true))
  assert.isFalse(runScript(blocked, false))
})
