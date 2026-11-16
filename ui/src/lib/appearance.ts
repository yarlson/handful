import { loadStored, saveStored } from "./layout"

// Appearance is this browser's choice of theme, chat width and text size. The script in
// index.html shows the stored theme before the first paint; it reads THEME_KEY the same way.

export const THEMES = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
] as const
export const CHAT_WIDTHS = [
  { value: "normal", label: "Normal" },
  { value: "wide", label: "Wide" },
] as const
export const TEXT_SIZES = [
  { value: "medium", label: "Medium" },
  { value: "large", label: "Large" },
] as const

export type Appearance = {
  theme: (typeof THEMES)[number]["value"]
  chatWidth: (typeof CHAT_WIDTHS)[number]["value"]
  textSize: (typeof TEXT_SIZES)[number]["value"]
}

const OPTIONS: Record<keyof Appearance, readonly { value: string }[]> = {
  theme: THEMES,
  chatWidth: CHAT_WIDTHS,
  textSize: TEXT_SIZES,
}

export const THEME_KEY = "handful.theme"
const KEYS: Record<keyof Appearance, string> = {
  theme: THEME_KEY,
  chatWidth: "handful.chatWidth",
  textSize: "handful.textSize",
}

// oneOf is the stored value when it is one of options, else the first option, the default.
function oneOf<T extends string>(options: readonly { value: T }[], stored: string | null): T {
  return options.find((o) => o.value === stored)?.value ?? options[0].value
}

// loadAppearance reads the stored choice; a missing, old or unknown value gives its default.
export function loadAppearance(): Appearance {
  return {
    theme: oneOf(THEMES, loadStored(KEYS.theme)),
    chatWidth: oneOf(CHAT_WIDTHS, loadStored(KEYS.chatWidth)),
    textSize: oneOf(TEXT_SIZES, loadStored(KEYS.textSize)),
  }
}

// choose stores value as the choice for key and gives a with it; an unknown value gives the
// key's default. A choice that storage refuses holds until the page reloads.
export function choose(a: Appearance, key: keyof Appearance, value: string): Appearance {
  const next = { ...a, [key]: oneOf(OPTIONS[key], value) }
  saveStored(KEYS[key], next[key])
  return next
}

type Root = { classList: { toggle: (name: string, on: boolean) => unknown }; dataset: DOMStringMap }

// showAppearance applies a to root, the page's <html>; systemDark is whether the system's
// colour scheme is dark. index.css reads data-chat-width and data-text-size.
export function showAppearance(root: Root, a: Appearance, systemDark: boolean) {
  root.classList.toggle("dark", a.theme === "dark" || (a.theme === "system" && systemDark))
  root.dataset.chatWidth = a.chatWidth
  root.dataset.textSize = a.textSize
}

type SystemScheme = Pick<MediaQueryList, "matches" | "addEventListener">

// followAppearance shows the stored choice on root now, and again when the system's colour
// scheme changes or another tab of this browser stores a choice (page's storage event).
export function followAppearance(root: Root, system: SystemScheme, page: Pick<Window, "addEventListener">) {
  const show = () => showAppearance(root, loadAppearance(), system.matches)
  show()
  system.addEventListener("change", show)
  page.addEventListener("storage", show)
}
