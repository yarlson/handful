import { createContext, useContext, useMemo, type ReactNode } from "react"
import { MessagesSquare } from "lucide-react"
import type { Bot } from "@/lib/api"
import { botColor } from "@/lib/bot-color"
import { cn } from "@/lib/utils"
import { BotDot } from "@/components/bot-dot"
import { TeamDots } from "@/components/team-dots"

// BotHues holds each bot's stored hue by id, so that every place that shows a bot uses it.
const BotHues = createContext<ReadonlyMap<string, string>>(new Map())

export function BotHuesProvider({ bots, children }: { bots: Bot[]; children: ReactNode }) {
  const hues = useMemo(() => new Map(bots.flatMap((b) => (b.hue ? [[b.id, b.hue] as const] : []))), [bots])
  return <BotHues value={hues}>{children}</BotHues>
}

// useBotColor returns the function that gives a bot or a team its colour by id.
export function useBotColor() {
  const hues = useContext(BotHues)
  return (id: string) => botColor(id, hues.get(id))
}

// ChatIcon is the mark of a chat: a bot is a BotDot and a team is TeamDots, in its colour; a room
// is a plain icon in the muted colour.
export function ChatIcon({ chat, className }: { chat: Pick<Bot, "id" | "kind" | "team">; className?: string }) {
  const color = useBotColor()
  const Icon = chat.team ? TeamDots : chat.kind === "room" ? MessagesSquare : BotDot
  const tint = chat.kind === "room" && !chat.team ? "text-muted-foreground" : color(chat.id).text
  return <Icon className={cn("size-4 shrink-0", tint, className)} />
}
