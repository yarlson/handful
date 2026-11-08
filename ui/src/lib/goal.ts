// goalCommand is the command that gives a team a new goal in its chat.
export const goalCommand = "/goal"

// parseGoalCommand returns the goal text of a "/goal <text>" message, with
// the space around it trimmed: "" for a bare "/goal", and null when the
// message is not the command, such as "/goals" or text that only mentions it.
export function parseGoalCommand(message: string): string | null {
  const text = message.trim()
  if (!text.startsWith(goalCommand)) return null
  const rest = text.slice(goalCommand.length)
  if (rest !== "" && !/^\s/.test(rest)) return null
  return rest.trim()
}
