import type { MouseEvent } from "react"
import { useState } from "react"
import { useLoad } from "@/components/bot-panel"
import { openSettings } from "@/components/model"
import { Button } from "@/components/ui/button"
import { api, type Bot } from "@/lib/api"
import type { Zone } from "@/lib/dates"
import { chipText } from "@/lib/model"
import { chatUrl } from "@/lib/place"
import type { PlanWindow } from "@/lib/usage-banner"
import {
  asOfLine,
  botShares,
  helperRunsText,
  PERIODS,
  resetLine,
  shownWindows,
  usedLine,
  windowPercent,
  windowTitle,
  type BotShare,
  type Period,
} from "@/lib/usage-view"
import { cn } from "@/lib/utils"

// UsageSection is Settings → Usage: the plan's usage windows, then each bot's share of the spend
// of the Handful bots on this computer in the chosen period.
export function UsageSection({ windows, bots, aliases }: { windows: PlanWindow[]; bots: Bot[]; aliases: string[] }) {
  const [period, setPeriod] = useState<Period>("24h")
  const spend = useLoad(() => api.spend(period), period)
  return (
    <>
      <PlanWindowBars windows={windows} />
      <h3 id="usage-spend-heading" className="mt-8 text-sm font-semibold">
        What's contributing to your usage?
      </h3>
      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2">
        <fieldset aria-labelledby="usage-spend-heading" className="bg-muted inline-flex rounded-lg p-[3px]">
          {PERIODS.map((p) => (
            <Button
              key={p.id}
              size="xs"
              variant="ghost"
              aria-pressed={p.id === period}
              className={cn("rounded-md px-2.5", p.id === period && "bg-background shadow-sm hover:bg-background")}
              onClick={() => setPeriod(p.id)}
            >
              {p.label}
            </Button>
          ))}
        </fieldset>
        <p className="text-muted-foreground text-xs">Approximate · Handful bots on this machine only</p>
      </div>
      {spend.error ? (
        <p role="alert" className="text-destructive mt-3 text-sm">
          Couldn't load the spend: {spend.error}
        </p>
      ) : spend.data === null ? (
        <p className="text-muted-foreground mt-3 text-sm">Loading…</p>
      ) : (
        <SpendList shares={botShares(spend.data)} bots={bots} aliases={aliases} />
      )}
      <p className="text-muted-foreground mt-4 text-xs">
        Shares use Claude Code's cost estimate at list price. It is not a bill and not the plan limit.
      </p>
    </>
  )
}

// PlanWindowBars shows a bar for each usage window of the plan, with how much is used, when it
// resets and when Claude Code reported it.
export function PlanWindowBars({ windows, zone = {} }: { windows: PlanWindow[]; zone?: Zone }) {
  if (windows.length === 0)
    return (
      <p className="text-muted-foreground mt-3 text-sm">
        No plan usage reported yet. It shows after a bot's next turn.
      </p>
    )
  return (
    <ul className="mt-3 flex flex-col gap-5">
      {shownWindows(windows).map((w) => {
        const title = windowTitle(w.window)
        const percent = windowPercent(w)
        const reset = resetLine(w, zone.timeZone)
        return (
          <li key={w.window} className="flex flex-col gap-1.5">
            <div className="flex items-baseline justify-between gap-3 text-sm">
              <span className="font-medium">{title}</span>
              <span className="text-muted-foreground tabular-nums">{usedLine(w)}</span>
            </div>
            {/* The line above says the percent, so the bar is hidden from screen readers. */}
            <div aria-hidden className="bg-muted h-2 overflow-hidden rounded-full">
              <div
                className={cn("h-full rounded-full", w.status === "allowed" ? "bg-primary" : "bg-attention-foreground")}
                style={{ width: `${percent ?? 0}%` }}
              />
            </div>
            <p className="text-muted-foreground text-xs">
              {reset && <>{reset} · </>}
              {asOfLine(w, zone)}
            </p>
          </li>
        )
      })}
    </ul>
  )
}

// SpendList has a row for each bot with a turn in the period: its share, its name, which opens
// its Settings, its model and effort, and its helper runs.
export function SpendList({ shares, bots, aliases }: { shares: BotShare[]; bots: Bot[]; aliases: string[] }) {
  if (shares.length === 0) return <p className="text-muted-foreground mt-3 text-sm">No bot turns in this period.</p>
  return (
    <ul className="mt-3 flex flex-col divide-y rounded-lg border text-sm">
      {shares.map((s) => {
        const bot = bots.find((b) => b.id === s.botId)
        return (
          <li key={s.botId} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-3 py-2">
            <span className="w-10 shrink-0 text-right font-medium tabular-nums">{s.percent}%</span>
            {bot ? (
              <a
                href={chatUrl({ chat: bot.id, tab: "settings" })}
                className="font-medium underline-offset-4 hover:underline"
                onClick={(e) => openInApp(e, bot.id)}
              >
                {bot.name}
              </a>
            ) : (
              <span className="text-muted-foreground">A deleted bot</span>
            )}
            {bot?.effective && <span className="text-muted-foreground">{chipText(bot.effective, aliases)}</span>}
            <span className="text-muted-foreground ml-auto tabular-nums">{helperRunsText(s.helpers)}</span>
          </li>
        )
      })}
    </ul>
  )
}

// openInApp opens the bot's Settings in this page on a plain click; a click with a modifier
// opens the link as the browser does.
function openInApp(e: MouseEvent, botId: string) {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
  e.preventDefault()
  openSettings(botId)
}
