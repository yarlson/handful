// teamCommands are the commands of a team's chat. A skill with one of these
// ids cannot be invoked there: the command wins.
export const teamCommands = ["clear", "compact", "task", "goal"]

// offeredSkills returns the skills that a chat's "/" menu offers: all of them
// in a bot's own chat, those that no team command hides in a team's chat, and
// none in a plain room.
export function offeredSkills<S extends { id: string }>(skills: S[], chat: { kind: string; team?: object }): S[] {
  if (chat.kind !== "room") return skills
  if (!chat.team) return []
  return skills.filter((s) => !teamCommands.includes(s.id))
}
