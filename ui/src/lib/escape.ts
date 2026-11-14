// EscapePress is the composer when Escape is pressed. handled: a layer above it (a menu or a
// dialog) took the key already. stopping: a stop of this turn is on its way.
export type EscapePress = {
  handled: boolean
  menuOpen: boolean
  browsing: boolean
  answering: boolean
  running: boolean
  stopping: boolean
}

// escapeAction tells what Escape in the composer does: it closes the innermost open thing first,
// the / or @ menu, then the history walk, then the answer to a question, and only then stops the
// running turn. It returns null when there is nothing to do.
export function escapeAction(p: EscapePress): "menu" | "browse" | "answer" | "stop" | null {
  if (p.handled) return null
  if (p.menuOpen) return "menu"
  if (p.browsing) return "browse"
  if (p.answering) return "answer"
  if (p.running && !p.stopping) return "stop"
  return null
}

// turnStoppable tells whether the chat shows Stop turn beside Stop: when stopping only the turn
// keeps something that Stop ends, the background tasks or, in a room or a team, the queued
// turns and the board.
export function turnStoppable(bot: { running: boolean; kind: "agent" | "room"; tasks: unknown[] }): boolean {
  return bot.running && (bot.kind === "room" || bot.tasks.length > 0)
}
