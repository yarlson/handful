import { MessagesSquare } from "lucide-react"
import type { Bot } from "@/lib/api"
import { botColor } from "@/lib/bot-color"
import { cn } from "@/lib/utils"
import { BotDot } from "@/components/bot-dot"
import { TeamDots } from "@/components/team-dots"

// useBotColor returns the function that gives a bot or a team its colour by id.
export function useBotColor() {
  return botColor
}

// ChatIcon is the mark of a chat: a bot is a BotDot and a team is TeamDots, in its colour; a room
// is a plain icon.
export function ChatIcon({ chat, className }: { chat: Pick<Bot, "id" | "kind" | "team">; className?: string }) {
  const color = useBotColor()
  const Icon = chat.team ? TeamDots : chat.kind === "room" ? MessagesSquare : BotDot
  return (
    <Icon className={cn("size-4 shrink-0", (chat.kind !== "room" || chat.team) && color(chat.id).text, className)} />
  )
}
