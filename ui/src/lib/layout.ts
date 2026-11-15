// The widths of the left sidebar, the chat and the right panel. All widths are CSS pixels.

export const SIDEBAR = { min: 180, max: 480, initial: 240 }
export const PANEL = { min: 280, max: 560, initial: 320 }
// CHAT_MIN is the narrowest chat beside an inline panel; the chat's minimum wins over the panel's
// width.
export const CHAT_MIN = 480
// SEPARATOR is the width of a resize handle (w-1).
export const SEPARATOR = 4
// STEP is how far an arrow key moves a resize handle.
export const STEP = 16
// WIDE is 64rem: below it the panel covers the chat. NARROW is md, 48rem: below it the panel is
// a full-screen sheet and the sidebar a drawer.
const WIDE = 1024
const NARROW = 768
// OVERLAY_GAP is the strip of the chat that a panel over it leaves visible.
const OVERLAY_GAP = 48

export const SIDEBAR_WIDTH_KEY = "handful.sidebarWidth"
export const PANEL_WIDTH_KEY = "handful.panelWidth"
export const SIDEBAR_COLLAPSED_KEY = "handful.sidebarCollapsed"

function clamp(w: number, min: number, max: number) {
  return Math.round(Math.min(max, Math.max(min, w)))
}

export function clampSidebar(w: number) {
  return clamp(w, SIDEBAR.min, SIDEBAR.max)
}

export function clampPanel(w: number) {
  return clamp(w, PANEL.min, PANEL.max)
}

// PanelLayout is how the right panel shows: inline beside the chat, as an overlay over it, or as
// a full-screen sheet. width is the panel's width now; max is the widest an inline panel may be.
export type PanelLayout = { mode: "inline" | "overlay" | "sheet"; width: number; max: number }

// panelLayout places the panel in a window of windowWidth, beside a sidebar that takes sidebar
// pixels with its handle (0 when it is collapsed), at the width the user set. An inline panel
// leaves the chat CHAT_MIN; where it can't be PANEL.min wide then, it covers the chat.
export function panelLayout(windowWidth: number, sidebar: number, set: number): PanelLayout {
  if (windowWidth < NARROW) return { mode: "sheet", width: windowWidth, max: windowWidth }
  const max = Math.min(PANEL.max, windowWidth - sidebar - SEPARATOR - CHAT_MIN)
  if (windowWidth < WIDE || max < PANEL.min) {
    const over = Math.min(PANEL.max, windowWidth - OVERLAY_GAP)
    return { mode: "overlay", width: clamp(set, PANEL.min, over), max: over }
  }
  return { mode: "inline", width: clamp(set, PANEL.min, max), max }
}

// A layout choice is a per-browser preference. Storage can be unavailable (for example in a
// private window); the choice then resets on reload.
export function loadStored(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

export function saveStored(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Not saved: the choice resets on reload, which is acceptable.
  }
}

// loadWidth reads a stored width through clampWidth, or gives initial when none is stored.
export function loadWidth(key: string, clampWidth: (w: number) => number, initial: number) {
  const saved = Number(loadStored(key))
  return saved ? clampWidth(saved) : initial
}
