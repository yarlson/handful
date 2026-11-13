// The tokens of a bot's latest turn, as the server recorded them from Claude Code, and the
// context window at which Claude Code compacts the bot's sessions.

export type TurnUsage = {
  inputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  outputTokens: number
  calls: number
  contextTokens: number // the context of the turn's last call
  cold: boolean // the first call wrote more than half of its context to the cache
}

export type Usage = { windowTokens: number; last: TurnUsage | null }

const thousands = (tokens: number) => `${Math.round(tokens / 1000)}k`

// contextLine is the Settings tab's line about the bot's context and last turn, or null before a
// turn is recorded.
export function contextLine(usage: Usage | null | undefined): string | null {
  const last = usage?.last
  if (!usage || !last) return null
  const calls = last.calls === 1 ? "1 call" : `${last.calls} calls`
  const cache = last.cold ? "rebuilt the cache" : "from cache"
  return `Context: ${thousands(last.contextTokens)} of ${thousands(usage.windowTokens)} · last turn: ${calls}, ${cache}`
}
