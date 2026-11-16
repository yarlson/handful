// A Shortcut is Mod (⌘ on a Mac, Ctrl elsewhere), with or without Alt, and the key at code. The
// key's place counts, not its letter, so a shortcut works in every keyboard layout. key is the
// letter the UI shows.
type Shortcut = { code: string; alt: boolean; key: string }

export const SHORTCUTS = {
  palette: { code: "KeyK", alt: false, key: "K" },
  search: { code: "KeyF", alt: false, key: "F" },
  sidebar: { code: "KeyB", alt: false, key: "B" },
  panel: { code: "KeyB", alt: true, key: "B" },
} as const satisfies Record<string, Shortcut>

export type ShortcutName = keyof typeof SHORTCUTS

export type KeyPress = {
  code: string
  metaKey: boolean
  ctrlKey: boolean
  altKey: boolean
  shiftKey: boolean
  isComposing: boolean
  keyCode: number
  getModifierState: (key: string) => boolean
}

// MAC says the shortcuts use ⌘, not Ctrl.
export const MAC = /Mac|iPhone|iPad/.test(navigator.userAgent)

// shortcutOf names the shortcut that a key press is, or gives null. Ctrl on a Mac is not Mod, so
// Ctrl+F still moves the cursor in a Mac text field. A key that an IME takes is no shortcut;
// keyCode 229 marks it in browsers that don't set isComposing. Off a Mac, AltGr, which some
// layouts need to type a character, is Ctrl+Alt to the browser, so it is no shortcut either. On a
// Mac some browsers report AltGraph for every ⌥ press, and AltGr does not exist there.
export function shortcutOf(e: KeyPress, mac: boolean): ShortcutName | null {
  if (e.isComposing || e.keyCode === 229 || (!mac && e.getModifierState("AltGraph"))) return null
  const mod = mac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey
  if (!mod || e.shiftKey) return null
  const names = Object.keys(SHORTCUTS) as ShortcutName[]
  return names.find((n) => SHORTCUTS[n].code === e.code && SHORTCUTS[n].alt === e.altKey) ?? null
}

// shortcutLabel is how the UI shows a shortcut: ⌥⌘B on a Mac, Ctrl+Alt+B elsewhere.
export function shortcutLabel(name: ShortcutName, mac: boolean): string {
  const s = SHORTCUTS[name]
  return mac ? `${s.alt ? "⌥" : ""}⌘${s.key}` : `Ctrl+${s.alt ? "Alt+" : ""}${s.key}`
}

// shortcutKeys is a shortcut as aria-keyshortcuts names it.
export function shortcutKeys(name: ShortcutName, mac: boolean): string {
  const s = SHORTCUTS[name]
  return `${mac ? "Meta" : "Control"}+${s.alt ? "Alt+" : ""}${s.key}`
}
