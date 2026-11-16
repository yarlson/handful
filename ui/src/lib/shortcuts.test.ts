import { assert, test } from "vitest"
import { shortcutKeys, shortcutLabel, shortcutOf, type KeyPress } from "./shortcuts"

// press is a key press with no modifier, no IME and no AltGr; altGraph turns AltGr on.
function press(code: string, keys: Partial<Omit<KeyPress, "getModifierState">> = {}, altGraph = false): KeyPress {
  return {
    code,
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    isComposing: false,
    keyCode: 0,
    getModifierState: (key) => key === "AltGraph" && altGraph,
    ...keys,
  }
}

test("each shortcut is ⌘ and its key on a Mac", () => {
  assert.equal(shortcutOf(press("KeyK", { metaKey: true }), true), "palette")
  assert.equal(shortcutOf(press("KeyF", { metaKey: true }), true), "search")
  assert.equal(shortcutOf(press("KeyB", { metaKey: true }), true), "sidebar")
  assert.equal(shortcutOf(press("KeyB", { metaKey: true, altKey: true }), true), "panel")
})

test("each shortcut is Ctrl and its key elsewhere", () => {
  assert.equal(shortcutOf(press("KeyK", { ctrlKey: true }), false), "palette")
  assert.equal(shortcutOf(press("KeyF", { ctrlKey: true }), false), "search")
  assert.equal(shortcutOf(press("KeyB", { ctrlKey: true }), false), "sidebar")
  assert.equal(shortcutOf(press("KeyB", { ctrlKey: true, altKey: true }), false), "panel")
})

test("shortcuts match the key's place on a layout where that key types another letter", () => {
  // Russian ЙЦУКЕН types "л" at KeyK, "а" at KeyF and "и" at KeyB; on a Mac, ⌥ with KeyB types
  // "∫". The press carries the key's place, not the letter.
  assert.equal(shortcutOf(press("KeyK", { metaKey: true }), true), "palette")
  assert.equal(shortcutOf(press("KeyB", { metaKey: true, altKey: true }), true), "panel")
  // Dvorak types "k" at KeyV and "b" at KeyN, so those are not the shortcuts.
  assert.isNull(shortcutOf(press("KeyV", { metaKey: true }), true))
  assert.isNull(shortcutOf(press("KeyN", { ctrlKey: true }), false))
})

test("a key with no Mod, the other platform's Mod, both or Shift is no shortcut", () => {
  assert.isNull(shortcutOf(press("KeyK"), true), "K alone")
  assert.isNull(shortcutOf(press("KeyB", { altKey: true }), true), "⌥B alone types ∫")
  assert.isNull(shortcutOf(press("KeyF", { ctrlKey: true }), true), "Ctrl+F on a Mac moves the cursor")
  assert.isNull(shortcutOf(press("KeyF", { metaKey: true }), false), "Meta+F elsewhere")
  assert.isNull(shortcutOf(press("KeyK", { metaKey: true, ctrlKey: true }), true))
  assert.isNull(shortcutOf(press("KeyK", { metaKey: true, ctrlKey: true }), false))
  assert.isNull(shortcutOf(press("KeyK", { metaKey: true, shiftKey: true }), true))
  assert.isNull(shortcutOf(press("KeyK", { metaKey: true, altKey: true }), true), "only B has an Alt shortcut")
  assert.isNull(shortcutOf(press("KeyG", { metaKey: true }), true))
})

test("a key that an IME takes is no shortcut", () => {
  assert.isNull(shortcutOf(press("KeyK", { metaKey: true, isComposing: true }), true))
  assert.isNull(shortcutOf(press("KeyB", { ctrlKey: true, keyCode: 229 }), false), "IME key without isComposing")
  assert.equal(shortcutOf(press("KeyB", { ctrlKey: true, keyCode: 66 }), false), "sidebar")
})

test("AltGr, which the browser reports as Ctrl+Alt, is no shortcut", () => {
  // Hungarian types "{" with AltGr at KeyB.
  assert.isNull(shortcutOf(press("KeyB", { ctrlKey: true, altKey: true }, true), false))
})

test("on a Mac, a ⌥ press that the browser marks as AltGraph is still ⌥", () => {
  assert.equal(shortcutOf(press("KeyB", { metaKey: true, altKey: true }, true), true), "panel")
  assert.equal(shortcutOf(press("KeyK", { metaKey: true }, true), true), "palette")
})

test("a shortcut shows as ⌘ symbols on a Mac and as Ctrl+ names elsewhere", () => {
  assert.equal(shortcutLabel("palette", true), "⌘K")
  assert.equal(shortcutLabel("panel", true), "⌥⌘B")
  assert.equal(shortcutLabel("search", false), "Ctrl+F")
  assert.equal(shortcutLabel("panel", false), "Ctrl+Alt+B")
  assert.equal(shortcutKeys("sidebar", true), "Meta+B")
  assert.equal(shortcutKeys("panel", true), "Meta+Alt+B")
  assert.equal(shortcutKeys("palette", false), "Control+K")
  assert.equal(shortcutKeys("panel", false), "Control+Alt+B")
})
