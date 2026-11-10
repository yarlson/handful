// taskCommand is the command that sends a team's lead a task in the team's chat. /goal is
// its old name and still works.
export const taskCommand = "/task"
const COMMANDS = [taskCommand, "/goal"]

// parseTaskCommand returns the task text of a "/task <text>" (or "/goal <text>") message,
// with the space around it trimmed: "" for a bare command, and null when the message is not
// the command, such as "/tasks" or text that only mentions it.
export function parseTaskCommand(message: string): string | null {
  const text = message.trim()
  const command = COMMANDS.find((c) => text.startsWith(c))
  if (command === undefined) return null
  const rest = text.slice(command.length)
  if (rest !== "" && !/^\s/.test(rest)) return null
  return rest.trim()
}
