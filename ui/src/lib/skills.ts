// teamCommands are the commands of a team's chat. A skill with one of these
// ids cannot be invoked there: the command wins.
export const teamCommands = ["clear", "compact", "task", "goal"]

// offeredSkills returns the skills that a chat's "/" menu offers, from the chat's skills list: in
// a bot's own chat, all but the shared ones that the bot's own skills replace; in a team's chat,
// those that no team command hides; in a plain room, none.
export function offeredSkills<S extends { id: string; replaced?: boolean }>(
  skills: S[],
  chat: { kind: string; team?: object },
): S[] {
  if (chat.kind !== "room") return skills.filter((s) => !s.replaced)
  if (!chat.team) return []
  return skills.filter((s) => !teamCommands.includes(s.id))
}

// skillLayerLabel says which bots a skill of a bot's skills list is for.
export function skillLayerLabel(skill: { layer: "shared" | "bot"; replaced?: boolean }) {
  if (skill.layer === "bot") return "This bot only"
  return skill.replaced ? "Replaced for this bot" : "Shared"
}
