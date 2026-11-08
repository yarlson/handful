// The emoji the user can put on a message, in display order. The server
// accepts only these; internal/core/reactions.go keeps the same list.
export const REACTIONS = ["👍", "👎", "❤️", "😂", "👀", "✅"]

// toggled returns the state a click on the emoji asks for: on when the
// message does not have it yet.
export function toggled(current: string[] | undefined, emoji: string): boolean {
  return !current?.includes(emoji)
}
