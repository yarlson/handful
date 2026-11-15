import { useSyncExternalStore } from "react"

// narrowQuery is the width below which the UI uses its phone layout: Tailwind's md breakpoint,
// 48rem. It decides layout only; what a phone may do comes from the server (session.ts).
export const narrowQuery = "(max-width: 47.99rem)"

// isNarrow tells whether the window is narrower than the md breakpoint now.
export function isNarrow(): boolean {
  return window.matchMedia(narrowQuery).matches
}

// watchNarrow calls onChange when the window crosses the md breakpoint, until it is stopped.
export function watchNarrow(onChange: () => void): () => void {
  const m = window.matchMedia(narrowQuery)
  m.addEventListener("change", onChange)
  return () => m.removeEventListener("change", onChange)
}

// useNarrow re-renders a component when the window crosses the md breakpoint. A server render
// is not narrow.
export function useNarrow(): boolean {
  return useSyncExternalStore(watchNarrow, isNarrow, () => false)
}

// watchWidth calls onChange when the window is resized, until it is stopped.
export function watchWidth(onChange: () => void): () => void {
  window.addEventListener("resize", onChange)
  return () => window.removeEventListener("resize", onChange)
}

// useWindowWidth re-renders a component when the window's width changes, and gives it in CSS
// pixels.
export function useWindowWidth(): number {
  return useSyncExternalStore(watchWidth, windowWidth, windowWidth)
}

function windowWidth() {
  return window.innerWidth
}
