// teamCommands are the commands of a team's chat. A skill with one of these
// ids cannot be invoked there: the command wins.
export const teamCommands = ["clear", "compact", "goal"]

// offeredSkills returns the skills that a chat's "/" menu offers: all of them
// in a bot's own chat, those that no team command hides in a team's chat, and
// none in a plain room.
export function offeredSkills<S extends { id: string }>(skills: S[], chat: { kind: string; team?: object }): S[] {
  if (chat.kind !== "room") return skills
  if (!chat.team) return []
  return skills.filter((s) => !teamCommands.includes(s.id))
}

// teamSkillOf returns the id of the skill that a user's post in a team's chat
// invokes, as the server reads it: after any @mentions of members or @here,
// the post starts with "/<skill-id>" for an offered skill. It returns null for
// any other post.
export function teamSkillOf(text: string, skillIds: string[], memberNames: string[]): string | null {
  const rest = withoutMentions(text, memberNames)
  if (!rest.startsWith("/")) return null
  const id = rest.slice(1).split(" ", 1)[0]
  return skillIds.includes(id) && !teamCommands.includes(id) ? id : null
}

// withoutMentions returns text without the @mentions that it starts with.
export function withoutMentions(text: string, memberNames: string[]): string {
  let rest = text.trim()
  while (rest.startsWith("@")) {
    const after = afterMention(rest.slice(1), memberNames)
    if (after === null) return rest
    rest = after.replace(/^[\s,:]+/, "")
  }
  return rest
}

// afterMention returns the text after the member name or "here" that text
// starts with, ignoring case, or null. The name must end where a word ends,
// and the longest name wins, so "Forge Dev" wins over "Forge".
function afterMention(text: string, memberNames: string[]): string | null {
  const names = ["here", ...memberNames].toSorted((a, b) => b.length - a.length)
  const lower = text.toLowerCase()
  for (const name of names) {
    if (lower.startsWith(name.toLowerCase()) && !/^[\p{L}\p{N}]/u.test(text.slice(name.length))) {
      return text.slice(name.length)
    }
  }
  return null
}
