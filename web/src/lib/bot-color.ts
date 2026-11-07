// A bot's color, picked from its id so that it stays the same everywhere.
// The hues are far apart, and amber is left out because it marks attention.
// The class names are written out in full so Tailwind generates them.
const palette = [
  { text: "text-sky-600 dark:text-sky-400", avatar: "bg-sky-500/15 text-sky-600 dark:text-sky-400" },
  {
    text: "text-emerald-600 dark:text-emerald-400",
    avatar: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  },
  { text: "text-rose-600 dark:text-rose-400", avatar: "bg-rose-500/15 text-rose-600 dark:text-rose-400" },
  { text: "text-violet-600 dark:text-violet-400", avatar: "bg-violet-500/15 text-violet-600 dark:text-violet-400" },
  {
    text: "text-fuchsia-600 dark:text-fuchsia-400",
    avatar: "bg-fuchsia-500/15 text-fuchsia-600 dark:text-fuchsia-400",
  },
  { text: "text-lime-600 dark:text-lime-400", avatar: "bg-lime-500/15 text-lime-600 dark:text-lime-400" },
]

export function botColor(id: string) {
  let h = 5381
  for (let i = 0; i < id.length; i++) h = (h * 33 + id.charCodeAt(i)) >>> 0
  return palette[h % palette.length]
}
