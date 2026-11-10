// A team's plan: how the UI moves a planned task and announces its new place. The server
// keeps the order.

// moved returns ids with the one at index moved by step (-1 up, 1 down), or null when it
// can't move that way.
export function moved(ids: string[], index: number, step: -1 | 1): string[] | null {
  const to = index + step
  if (index < 0 || index >= ids.length || to < 0 || to >= ids.length) return null
  const next = [...ids]
  ;[next[index], next[to]] = [next[to], next[index]]
  return next
}

// placeNote announces where a moved goal is now, for screen readers.
export function placeNote(title: string, position: number, total: number): string {
  return `${title} is now ${position} of ${total}.`
}
