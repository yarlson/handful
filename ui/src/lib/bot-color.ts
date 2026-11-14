// hues are the bot colours, in the server's order (internal/db/migrations/026_bot_hues.sql): the
// server gives each new bot the least used one. Amber is left out because it marks attention, and
// rose because it sits next to the brand pink and the error red.
export const hues = ["sky", "emerald", "violet", "fuchsia", "lime"] as const
export type Hue = (typeof hues)[number]

// The class names are written out in full so Tailwind generates them. Each text is at least
// 4.5:1 on the background, the sidebar, cards and muted rows in its theme.
const palette: Record<Hue, { text: string }> = {
  sky: { text: "text-sky-700 dark:text-sky-400" },
  emerald: { text: "text-emerald-700 dark:text-emerald-400" },
  violet: { text: "text-violet-700 dark:text-violet-400" },
  fuchsia: { text: "text-fuchsia-700 dark:text-fuchsia-400" },
  lime: { text: "text-lime-700 dark:text-lime-400" },
}

function isHue(name: string | undefined): name is Hue {
  return hues.includes(name as Hue)
}

// botColor returns the colour of a bot from its stored hue. A team, or a bot without a known hue
// (an older server), gets one picked from its id, so it stays the same everywhere.
export function botColor(id: string, hue?: string) {
  if (isHue(hue)) return palette[hue]
  let h = 5381
  for (let i = 0; i < id.length; i++) h = (h * 33 + id.charCodeAt(i)) >>> 0
  return palette[hues[h % hues.length]]
}

// colorId returns the id to color a row that names a bot, such as a tool call, so it gets the
// same color as that bot's posts. A row carries the bot's name and, from newer servers, its
// id. Without the id the name is looked up among the chat's members; a row in a bot's own
// chat, or one without a name, belongs to the chat's bot. A name that matches no one, as
// after a rename, is colored by the name itself.
export function colorId(
  row: { botId?: string; bot?: string },
  chat: { id: string; name: string; members?: { id: string; name: string }[] },
): string {
  if (row.botId) return row.botId
  if (!row.bot || row.bot === chat.name) return chat.id
  return chat.members?.find((m) => m.name === row.bot)?.id ?? row.bot
}
