import { useEffect, useState, type ReactNode } from "react"
import {
  api,
  type Artifact,
  type Board,
  type Bot,
  type Goal,
  type Item,
  type NeedsYouItem,
  type PairRequest,
  type OffTask,
} from "@/lib/api"
import { actorOf, eventTone, type ItemEvent } from "@/lib/questions"
import { useLoad } from "@/components/bot-panel"
import { PairRequestLine } from "@/components/phones"
import { useRights } from "@/lib/session"
import {
  awaitsAnswer,
  budgetError,
  budgetLine,
  budgetShare,
  clockLine,
  clockStop,
  countsLine,
  firstLine,
  fromUser,
  leadName,
  needsYouLine,
  sourceLabel,
  historyKey,
  taskError,
  taskTurns,
  taskEventLabel,
  teamOption,
  teamSummary,
  type Budget,
  type ClockStop,
  type Names,
  type Tone,
} from "@/lib/tasks"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { cn } from "@/lib/utils"

export type Act = (action: () => Promise<unknown>) => Promise<boolean>

// clock is a time of day for a card: "04:00".
export function clock(ms: number) {
  return new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

// when is a time within the week: "Wed 04:00".
export function when(ms: number) {
  return new Date(ms).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })
}

const toneClass: Record<Tone, string> = {
  progress: "border-success/50 text-success-foreground",
  waiting: "border-sky-500/50 text-sky-700 dark:text-sky-400",
  attention: "border-attention/50 text-attention-foreground",
  over: "text-muted-foreground",
}

// ToneChip is a state chip: its text names the state, and its colour only repeats it.
export function ToneChip({ tone, className, children }: { tone: Tone; className?: string; children: ReactNode }) {
  return (
    <Badge variant="outline" className={cn("shrink-0", toneClass[tone], className)}>
      {children}
    </Badge>
  )
}

// SourceChip names who a task came from. A long team name is cut short, so the chip takes at
// most half of its row and leaves the task's title room; the whole name is in its title.
export function SourceChip({ goal, names }: { goal: Goal; names: Names }) {
  const label = sourceLabel(goal, names)
  return (
    <ToneChip tone="waiting" className="max-w-1/2 shrink">
      <span className="min-w-0 truncate" title={label}>
        {label}
      </span>
    </ToneChip>
  )
}

// TextAction asks for a text, such as a reason or an answer, before it acts. open is the
// button's accessible name, "Push back: <task>"; its text is the part before the colon.
export function TextAction({
  id,
  label,
  open,
  submit,
  onOpen,
  onClose,
  onSubmit,
}: {
  id: string
  label: string
  open: string
  submit: string
  onOpen?: () => void
  onClose?: () => void
  onSubmit: (text: string) => Promise<boolean>
}) {
  const [text, setText] = useState<string | null>(null)
  if (text === null) {
    return (
      <Button
        size="xs"
        variant="outline"
        aria-label={open}
        onClick={() => {
          setText("")
          onOpen?.()
        }}
      >
        {open.split(":")[0]}…
      </Button>
    )
  }
  return (
    <div className="flex w-full flex-col gap-1.5">
      <Label htmlFor={id} className="text-xs">
        {label}
      </Label>
      <Textarea id={id} rows={2} value={text} onChange={(e) => setText(e.target.value)} />
      <div className="flex gap-1.5">
        <Button
          size="xs"
          disabled={!text.trim()}
          onClick={() =>
            void onSubmit(text.trim()).then((ok) => {
              if (ok) setText(null)
            })
          }
        >
          {submit}
        </Button>
        <Button
          size="xs"
          variant="ghost"
          onClick={() => {
            setText(null)
            onClose?.()
          }}
        >
          Cancel
        </Button>
      </div>
    </div>
  )
}

// OffTaskBox lists the team's turns in the last hour that were on no task: who, when and the
// first line. Make a task… turns them into a task the team takes at once, so the count drops.
export function OffTaskBox({ team, offTask, act }: { team: Bot; offTask: OffTask; act: Act }) {
  const [shown, setShown] = useState(false)
  const rights = useRights()
  if (offTask.count === 0) return null
  const who = [...new Set(offTask.turns.map((t) => t.botName))].join(", ")
  const turns = offTask.count === 1 ? "1 turn" : `${offTask.count} turns`
  const picked = taskTurns(offTask.turns)
  return (
    <section aria-label="Not on a task" className="flex flex-col gap-1.5">
      <div className="flex flex-col gap-1.5 rounded-md border border-dashed p-2 text-xs">
        <p>
          <strong>{`${turns} in the last hour`}</strong>
          {` ${offTask.count === 1 ? "wasn't" : "weren't"} on any task: ${who}.`}
        </p>
        <div className="flex flex-wrap gap-1.5">
          <Button
            size="xs"
            variant="outline"
            aria-expanded={shown}
            aria-controls="off-task-turns"
            onClick={() => setShown((v) => !v)}
          >
            {shown ? "Hide them" : "Show them"}
          </Button>
          {rights.computer && (
            <TextAction
              id={`make-task-${team.id}`}
              label={
                picked.left > 0
                  ? `What is the task? ${team.name} takes it at once, with the oldest ${picked.take.length} of these ${turns}; make another for the other ${picked.left}.`
                  : `What is the task? ${team.name} takes it at once, with these ${turns}.`
              }
              open={`Make a task: ${turns} on no task`}
              submit="Make a task"
              onSubmit={(text) => act(() => api.makeTask(team.id, { text, turnIds: picked.take.map((t) => t.turnId) }))}
            />
          )}
          {rights.send && (
            <Button
              size="xs"
              variant="ghost"
              aria-label={`Dismiss: ${turns} on no task`}
              title="They stay in the chat; they only leave this count."
              onClick={() => void act(() => api.dismissOffTask(team.id))}
            >
              Dismiss
            </Button>
          )}
        </div>
        {shown && (
          <ul id="off-task-turns" className="flex flex-col gap-1">
            {offTask.turns.map((t) => (
              <li key={t.turnId}>
                <span className="font-medium">{t.botName}</span>
                <span className="text-muted-foreground">{` · ${clock(t.startedAt)}`}</span>
                {t.firstLine && (
                  <span className="block truncate" title={t.firstLine}>
                    {t.firstLine}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}

// LimitAction sets a team's turns per day: Raise… on a budget, or the suggested limit.
export function LimitAction({ team, open, value, act }: { team: Bot; open: string; value: number; act: Act }) {
  const [text, setText] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const rights = useRights()
  const id = `limit-${team.id}`
  if (!rights.computer) return null
  if (text === null)
    return (
      <Button size="xs" variant="outline" aria-label={open} onClick={() => setText(String(value))}>
        {open.split(":")[0]}…
      </Button>
    )
  const save = () => {
    const bad = budgetError(text)
    setError(bad)
    if (bad) return
    void act(() => api.updateTeam(team.id, { dailyBudget: Number(text) })).then((ok) => {
      if (ok) setText(null)
    })
  }
  return (
    <span className="flex flex-wrap items-center gap-1.5 text-xs">
      <Label htmlFor={id} className="text-xs font-normal">
        Turns per day
      </Label>
      <input
        id={id}
        type="number"
        min={0}
        max={10000}
        className="border-input h-6 w-20 rounded-md border bg-transparent px-1 text-xs"
        value={text}
        aria-invalid={error ? true : undefined}
        onChange={(e) => setText(e.target.value)}
      />
      <Button size="xs" onClick={save}>
        Save
      </Button>
      <Button size="xs" variant="ghost" onClick={() => setText(null)}>
        Cancel
      </Button>
      {error && <span className="text-destructive w-full">{error}</span>}
    </span>
  )
}

// BudgetSection is a team's day in its Work tab: turns used against the daily limit, the
// paused state, and the suggested limit once the first week of watching is over.
export function BudgetSection({ team, budget, act }: { team: Bot; budget: Budget; act: Act }) {
  const share = budgetShare(budget)
  const rights = useRights()
  return (
    <section aria-labelledby="budget-heading" className="flex flex-col gap-1.5 text-xs">
      <h3 id="budget-heading" className="text-muted-foreground font-medium">
        Today
      </h3>
      <p>
        {budgetLine(budget)}
        <span className="text-muted-foreground"> · resets at midnight</span>
      </p>
      {share !== null && (
        // The line above says the same in words, so the bar is only for the eye.
        <div aria-hidden="true" className="bg-muted h-1.5 overflow-hidden rounded-full">
          <div
            className={cn("h-full", budget.paused || budget.warn ? "bg-attention" : "bg-foreground")}
            style={{ width: `${share}%` }}
          />
        </div>
      )}
      {budget.paused && (
        <p className="text-attention-foreground">
          Paused: new turns wait until midnight or a higher limit. The lead's decisions still run.
        </p>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        {budget.suggested > 0 && budget.limit === 0 && rights.computer && (
          <>
            <span className="text-muted-foreground">{`Suggested limit: ${budget.suggested} turns a day.`}</span>
            <Button
              size="xs"
              variant="outline"
              onClick={() => void act(() => api.updateTeam(team.id, { dailyBudget: budget.suggested }))}
            >
              {`Use ${budget.suggested}`}
            </Button>
          </>
        )}
        <LimitAction
          team={team}
          open={`${budget.limit === 0 ? "Set a limit" : "Change the limit"}: ${team.name}`}
          value={budget.limit || budget.suggested || 100}
          act={act}
        />
      </div>
    </section>
  )
}

// TaskHistoryList shows a task's events, oldest first: each decision and step event with who
// made it, when, and its text.
export function TaskHistoryList({
  id,
  goal,
  events,
  steps = [],
}: {
  id: string
  goal: Goal
  events: ItemEvent[]
  steps?: Pick<Item, "id" | "title">[]
}) {
  const title = (itemId: string) => steps.find((s) => s.id === itemId)?.title
  return (
    <section id={id} aria-labelledby={`${id}-heading`} className="flex flex-col gap-1 text-xs">
      <h4 id={`${id}-heading`} className="font-medium">
        History of {goal.text}
      </h4>
      {events.length === 0 ? (
        <p className="text-muted-foreground">Nothing has happened to it yet.</p>
      ) : (
        <ol className="flex flex-col gap-1.5 border-l pl-3">
          {events.map((e) => {
            const tone = eventTone(e.goalId ? "" : e.kind)
            return (
              <li
                key={e.id}
                className={cn(
                  tone === "changes" && "rounded-sm bg-attention-surface p-1.5",
                  tone === "passed" && "rounded-sm bg-success/10 p-1.5",
                )}
              >
                <p>
                  <span className="font-medium">{taskEventLabel(e, title(e.itemId))}</span>{" "}
                  <span className="text-muted-foreground">{`· ${actorOf(e)} · ${when(e.createdAt)}`}</span>
                </p>
                {e.text && <p className="mt-0.5 whitespace-pre-wrap">{e.text}</p>}
              </li>
            )
          })}
        </ol>
      )}
    </section>
  )
}

// TaskHistory is a task's History button, and its history once opened. The history loads
// when it opens, and again when version changes: pass the rev of the board the card is on,
// so a decision the board reloads for shows in an open history too.
export function TaskHistory({
  goal,
  steps,
  version,
}: {
  goal: Goal
  steps?: Pick<Item, "id" | "title">[]
  version: number
}) {
  const [open, setOpen] = useState(false)
  const id = `task-history-${goal.id}`
  return (
    <>
      <Button
        size="xs"
        variant="link"
        className="text-muted-foreground h-auto self-start p-0 text-xs"
        aria-expanded={open}
        aria-controls={id}
        aria-label={`History: ${goal.text}`}
        onClick={() => setOpen((v) => !v)}
      >
        History
      </Button>
      {open && <LoadedHistory id={id} goal={goal} steps={steps} version={version} />}
    </>
  )
}

function LoadedHistory({
  id,
  goal,
  steps,
  version,
}: {
  id: string
  goal: Goal
  steps?: Pick<Item, "id" | "title">[]
  version: number
}) {
  const events = useLoad(() => api.taskEvents(goal.id), historyKey(goal.id, version))
  if (events.error) return <p className="text-destructive text-xs">{events.error}</p>
  if (!events.data) return null
  return <TaskHistoryList id={id} goal={goal} events={events.data} steps={steps} />
}

// WithdrawAction takes back a task that hasn't started, after a confirm.
export function WithdrawAction({ g, to, act }: { g: Goal; to: string; act: Act }) {
  const [confirm, setConfirm] = useState(false)
  const rights = useRights()
  if (!rights.computer) return null
  if (!confirm) {
    return (
      <Button size="xs" variant="ghost" aria-label={`Withdraw: ${g.text}`} onClick={() => setConfirm(true)}>
        Withdraw…
      </Button>
    )
  }
  return (
    <span className="flex flex-wrap items-center gap-2 text-xs">
      Withdraw it? {to} won't work on it.
      <Button
        size="xs"
        variant="destructive"
        onClick={() => void act(() => api.withdrawTask(g.id)).then(() => setConfirm(false))}
      >
        Withdraw
      </Button>
      <Button size="xs" variant="ghost" onClick={() => setConfirm(false)}>
        Keep it
      </Button>
    </span>
  )
}

// InboxContext is what every card of a team's inbox shares.
export type InboxContext = {
  team: Bot
  lead: string
  minutes: number
  planned: number
  stop: ClockStop
  loadedAt: number
  names: Names
  act: Act
}

// InboxCard is a task that waits for the lead's decision. The user decides on it only once
// its lead couldn't: then the user takes it, at a place in the plan, or pushes it back.
function InboxCard({ g, view }: { g: Goal; view: InboxContext }) {
  const [typing, setTyping] = useState(false)
  const [place, setPlace] = useState(view.planned + 1)
  const rights = useRights()
  const sender = g.fromTeamId ? view.names.team(g.fromTeamId) : "you"
  const titleId = `inbox-${g.id}`
  const asked = (g.askedAt ?? 0) > 0
  const line = clockLine(
    g,
    { minutes: view.minutes, lead: view.lead, sender, stop: view.stop, loadedAt: view.loadedAt },
    when,
  )
  return (
    <li
      aria-labelledby={titleId}
      className={cn("flex flex-col gap-1.5 rounded-md border p-3 text-sm", g.needsUser && "border-attention/50")}
    >
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <SourceChip goal={g} names={view.names} />
        <span className="text-muted-foreground">
          {g.fromBotId ? `${view.names.member(g.fromBotId)} · ` : ""}
          {clock(g.createdAt)}
        </span>
        {g.waitsOnItemId && <ToneChip tone="attention">{`${sender}'s task waits on this`}</ToneChip>}
        <span className={cn("ml-auto", g.needsUser ? "text-attention-foreground" : "text-muted-foreground")}>
          {line}
        </span>
      </div>
      <h3 id={titleId} className="font-medium">
        {g.text}
      </h3>
      {firstLine(g.brief) && <p className="text-muted-foreground text-xs">{firstLine(g.brief)}</p>}
      {g.passedBy && <p className="text-muted-foreground text-xs">{g.passedBy}</p>}
      {(g.artifactIds?.length ?? 0) > 0 && (
        <p className="text-muted-foreground text-xs">
          {g.artifactIds?.length === 1 ? "1 artifact" : `${g.artifactIds?.length} artifacts`} attached; see the
          Artifacts tab of any bot.
        </p>
      )}
      {asked && g.intakeNote && (
        <>
          <p className="text-xs">
            <strong>{view.lead} asked:</strong> {g.intakeNote}
          </p>
          {!g.needsUser && (
            <p className="text-muted-foreground text-xs">
              Waiting for {sender === "you" ? "your" : `${sender}'s`} answer. The decision clock is paused.
            </p>
          )}
        </>
      )}
      {g.state === "later" && g.intakeNote && (
        <p className="text-xs">
          <strong>{view.lead}'s note:</strong> {g.intakeNote}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        {g.needsUser && !typing && rights.computer && (
          <>
            <select
              aria-label={`Place in the plan: ${g.text}`}
              className="border-input h-6 rounded-md border bg-transparent px-1 text-xs"
              value={place}
              onChange={(e) => setPlace(Number(e.target.value))}
            >
              {Array.from({ length: view.planned + 1 }, (_, i) => (
                <option key={i} value={i + 1}>
                  {`#${i + 1} in the plan`}
                </option>
              ))}
            </select>
            <Button
              size="xs"
              aria-label={`Take: ${g.text}`}
              onClick={() => void view.act(() => api.takeTask(g.id, place === view.planned + 1 ? 0 : place))}
            >
              Take
            </Button>
          </>
        )}
        {g.needsUser && rights.computer && (
          <TextAction
            id={`push-back-${g.id}`}
            label={`Reason ${sender === "you" ? "you keep" : `${sender} will see`} (required)`}
            open={`Push back: ${g.text}`}
            submit="Push back"
            onOpen={() => setTyping(true)}
            onClose={() => setTyping(false)}
            onSubmit={(text) => view.act(() => api.pushBack(g.id, text))}
          />
        )}
        {awaitsAnswer(g) && fromUser(g) && rights.send && (
          <TextAction
            id={`answer-${g.id}`}
            label={`Your answer to ${view.lead}: ${g.intakeNote ?? ""}`}
            open={`Answer: ${g.text}`}
            submit="Send answer"
            onSubmit={(text) => view.act(() => api.answerTask(g.id, text))}
          />
        )}
        {fromUser(g) && !g.needsUser && <WithdrawAction g={g} to={view.team.name} act={view.act} />}
      </div>
      <TaskHistory goal={g} version={view.loadedAt} />
    </li>
  )
}

// InboxView is a team's inbox: every task that waits for its lead's decision, with the
// decision clock as text.
export function InboxView({
  board,
  view,
  error,
  onBack,
  menu,
}: {
  board: Board
  view: InboxContext
  error: string | null
  onBack: () => void
  // menu is the phone layout's button that opens the sidebar; null above the phone width.
  menu?: ReactNode
}) {
  const inbox = board.inbox ?? []
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-12 items-center gap-2 border-b px-4 max-md:h-14 max-md:px-3">
        {menu}
        <h2 className="shrink-0 font-medium max-md:min-w-0 max-md:flex-1 max-md:truncate">{view.team.name} · Inbox</h2>
        <span className="text-muted-foreground min-w-0 flex-1 truncate text-sm max-md:hidden">
          {view.lead} decides what the team takes. {inbox.length} waiting.
        </span>
        <Button size="sm" variant="outline" className="max-md:h-11" onClick={onBack}>
          Back to team
        </Button>
      </header>
      <div className="flex max-w-3xl flex-col gap-3 overflow-y-auto p-4 max-md:px-3">
        {error && <p className="text-destructive text-xs">{error}</p>}
        <p className="text-muted-foreground text-sm md:hidden">
          {view.lead} decides what the team takes. {inbox.length} waiting.
        </p>
        {inbox.length === 0 ? (
          <p className="text-muted-foreground text-sm">
            The inbox is empty. Tasks from you, from other teams and from members land here, and {view.lead} decides on
            each.
          </p>
        ) : (
          <ul className="flex flex-col gap-3" aria-label={`${view.team.name}'s inbox`}>
            {inbox.map((g) => (
              <InboxCard key={g.id} g={g} view={view} />
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

// NeedsYouRow is one thing only the user settles, with the buttons that settle it.
function NeedsYouRow({
  n,
  chat,
  act,
  onOpen,
  team,
  limit,
}: {
  n: NeedsYouItem
  chat: string
  act: Act
  onOpen: (chatId: string) => void
  // team and limit are the team a budget item is about, and its daily limit.
  team?: Bot
  limit?: number
}) {
  const line = needsYouLine(n, chat)
  const id = n.goalId
  const rights = useRights()
  return (
    <li
      className={cn(
        "flex flex-wrap items-center gap-2 rounded-md border px-3 py-2 text-sm",
        n.kind !== "approve" && n.kind !== "question" && "border-attention/50",
      )}
    >
      <p className="min-w-0 flex-1 max-md:basis-full">
        <strong>{line.lead}</strong> {line.rest}
      </p>
      {n.kind === "approve" && id && rights.send && (
        <>
          <Button size="xs" aria-label={`Approve: ${n.text}`} onClick={() => void act(() => api.approveTask(id))}>
            Approve
          </Button>
          <TextAction
            id={`changes-${id}`}
            label="What should change?"
            open={`Ask for changes: ${n.text}`}
            submit="Ask for changes"
            onSubmit={(text) => act(() => api.changesTask(id, text))}
          />
        </>
      )}
      {(n.kind === "missed" || n.kind === "stuck") && id && rights.computer && (
        <>
          <Button size="xs" aria-label={`Take: ${n.text}`} onClick={() => void act(() => api.takeTask(id))}>
            Take
          </Button>
          <TextAction
            id={`push-back-${id}`}
            label="Reason the sender will see (required)"
            open={`Push back: ${n.text}`}
            submit="Push back"
            onSubmit={(text) => act(() => api.pushBack(id, text))}
          />
        </>
      )}
      {(n.kind === "budget_warning" || n.kind === "budget_reached") && team && (
        <LimitAction team={team} open={`Raise: ${team.name}`} value={(limit ?? 0) + 50} act={act} />
      )}
      {n.kind === "budget_reached" && rights.send && (
        <Button size="xs" variant="outline" onClick={() => void act(() => api.waitForBudget(n.chatId))}>
          Wait until midnight
        </Button>
      )}
      <Button size="xs" variant="ghost" onClick={() => onOpen(n.chatId)}>
        Open chat
      </Button>
    </li>
  )
}

// AcrossTeams is the user's view across teams: what needs the user, and what each team does.
export function AcrossTeams({
  needs,
  pairRequest = null,
  onPairDecided = () => {},
  boards,
  teams,
  error,
  act,
  onOpen,
  onNewTask,
  chatName,
  menu,
}: {
  needs: NeedsYouItem[]
  // pairRequest is a phone that waits for the user's decision, shown first in Needs you.
  pairRequest?: PairRequest | null
  onPairDecided?: () => void
  boards: Map<string, Board>
  teams: Bot[]
  error: string | null
  act: Act
  onOpen: (chatId: string) => void
  onNewTask: () => void
  chatName: (id: string) => string
  // menu is the phone layout's button that opens the sidebar; null above the phone width.
  menu?: ReactNode
}) {
  const rights = useRights()
  const count = needs.length + (pairRequest ? 1 : 0)
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-12 items-center gap-2 border-b px-4 max-md:h-14 max-md:px-3">
        {menu}
        <h2 className="shrink-0 font-medium max-md:flex-1">Across teams</h2>
        <span className="text-muted-foreground min-w-0 flex-1 truncate text-sm max-md:hidden">
          What each team is doing, and what needs you
        </span>
        {rights.computer && (
          <Button
            size="sm"
            className="max-md:h-11"
            aria-haspopup="dialog"
            onClick={onNewTask}
            disabled={teams.length === 0}
          >
            New task…
          </Button>
        )}
      </header>
      <div className="flex max-w-5xl flex-col gap-5 overflow-y-auto p-4 max-md:px-3">
        {error && <p className="text-destructive text-xs">{error}</p>}
        <section aria-labelledby="needs-you-heading" className="flex flex-col gap-1.5">
          <h3 id="needs-you-heading" className="text-muted-foreground text-xs font-medium">
            Needs you ({count})
          </h3>
          {count === 0 ? (
            <p className="text-muted-foreground text-sm">Nothing needs you.</p>
          ) : (
            <ul className="flex flex-col gap-1.5">
              {pairRequest && (
                <li className="rounded-md border border-attention/50 px-3 py-2">
                  <PairRequestLine request={pairRequest} onDecided={onPairDecided} />
                </li>
              )}
              {needs.map((n) => (
                <NeedsYouRow
                  key={`${n.kind}:${n.goalId ?? n.entryId}`}
                  n={n}
                  chat={chatName(n.chatId)}
                  act={act}
                  onOpen={onOpen}
                  team={teams.find((t) => t.id === n.chatId)}
                  limit={boards.get(n.chatId)?.budget?.limit}
                />
              ))}
            </ul>
          )}
        </section>
        <section aria-labelledby="teams-heading" className="flex flex-col gap-1.5">
          <h3 id="teams-heading" className="text-muted-foreground text-xs font-medium">
            Teams
          </h3>
          {teams.length === 0 ? (
            <p className="text-muted-foreground text-sm">No teams yet.</p>
          ) : (
            <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {teams.map((t) => {
                const board = boards.get(t.id)
                const s = board ? teamSummary(board) : null
                return (
                  <li key={t.id}>
                    <button
                      type="button"
                      className={cn(
                        "hover:bg-accent flex h-full w-full flex-col gap-1 rounded-md border p-3 text-left text-sm",
                        s?.paused && "border-attention/50",
                      )}
                      onClick={() => onOpen(t.id)}
                    >
                      <span className="flex items-center gap-2">
                        <span className="flex-1 font-medium">{t.name}</span>
                        {s?.paused && <ToneChip tone="attention">paused</ToneChip>}
                      </span>
                      {s && (
                        <>
                          <span className="text-xs">
                            <strong>Now:</strong> {s.now ?? "nothing"}
                          </span>
                          <span className="text-muted-foreground text-xs">{countsLine(s)}</span>
                          {s.budget && (
                            <span className="text-muted-foreground text-xs">Today {budgetLine(s.budget)}</span>
                          )}
                        </>
                      )}
                    </button>
                  </li>
                )
              })}
            </ul>
          )}
        </section>
      </div>
    </div>
  )
}

// ArtifactPicker lets the user attach artifacts to a task. A long title wraps to two lines
// inside the panel, and its full text is in the tooltip.
export function ArtifactPicker({
  artifacts,
  picked,
  onPick,
}: {
  artifacts: Pick<Artifact, "id" | "title">[]
  picked: string[]
  onPick: (update: (picked: string[]) => string[]) => void
}) {
  if (artifacts.length === 0) return null
  return (
    <fieldset className="flex max-h-32 min-w-0 flex-col gap-1 overflow-y-auto text-sm">
      <legend className="mb-1 text-sm font-medium">Artifacts (optional)</legend>
      {artifacts.map((a) => (
        <label key={a.id} className="flex min-w-0 items-start gap-2">
          <Checkbox
            className="mt-0.5"
            checked={picked.includes(a.id)}
            onCheckedChange={(v) => onPick((p) => (v === true ? [...p, a.id] : p.filter((id) => id !== a.id)))}
          />
          <span className="line-clamp-2 min-w-0 flex-1 [overflow-wrap:anywhere]" title={a.title}>
            {a.title}
          </span>
        </label>
      ))}
    </fieldset>
  )
}

// SendTaskDialog sends a team a task: from another team, which follows it under Sent to other
// teams, or from the user, who may start it at once.
export function SendTaskDialog({
  open,
  onOpenChange,
  from,
  teams,
  artifactIds = [],
  onDone,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  from: Bot | null
  teams: Bot[]
  artifactIds?: string[]
  onDone?: () => void
}) {
  const targets = teams.filter((t) => t.id !== from?.id)
  const [to, setTo] = useState("")
  const [text, setText] = useState("")
  const [brief, setBrief] = useState("")
  const [startNow, setStartNow] = useState(false)
  const [picked, setPicked] = useState<string[]>(artifactIds)
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    if (!open) return
    setPicked(artifactIds)
    api.artifacts({ kind: "", query: "" }).then(
      (list) => setArtifacts(list.slice(0, 20)),
      () => setArtifacts([]),
    )
    // artifactIds is what the dialog opens with; later changes don't reset the choice.
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [open])
  const send = async () => {
    const bad = taskError(to, text)
    if (bad) {
      setError(bad)
      return
    }
    const task = {
      text: text.trim(),
      ...(brief.trim() && { brief: brief.trim() }),
      ...(picked.length > 0 && { artifactIds: picked }),
    }
    try {
      if (from) await api.handOver(to, { fromTeamId: from.id, ...task })
      else await api.userTask(to, { ...task, ...(startNow && { startNow }) })
      setTo("")
      setText("")
      setBrief("")
      setStartNow(false)
      setError(null)
      onOpenChange(false)
      onDone?.()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* A grid column sizes to its widest content unless capped, so a long word in a title,
          a team name or the task would push the panel wider than the window. */}
      <DialogContent className="grid-cols-[minmax(0,1fr)]">
        <DialogHeader>
          <DialogTitle>{from ? "Send a task to another team" : "Send a task to a team"}</DialogTitle>
          <DialogDescription className="[overflow-wrap:anywhere]">
            It lands in the team's inbox, and its lead decides on it. The team works on it from its own budget.
            {from ? ` Follow it under Sent to other teams on ${from.name}'s Work tab.` : ""}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="send-task-to">Team</Label>
          <select
            id="send-task-to"
            className="border-input h-9 w-full min-w-0 truncate rounded-md border bg-transparent px-2 text-sm"
            value={to}
            onChange={(e) => setTo(e.target.value)}
          >
            <option value="">Choose a team</option>
            {targets.map((t) => (
              <option key={t.id} value={t.id}>
                {teamOption(t)}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="send-task-text">What should the team do?</Label>
          <Textarea
            id="send-task-text"
            rows={3}
            className="max-h-48 min-w-0 [overflow-wrap:anywhere]"
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="send-task-brief">Brief (optional)</Label>
          <Textarea
            id="send-task-brief"
            rows={3}
            className="max-h-48 min-w-0 [overflow-wrap:anywhere]"
            placeholder="Context the team needs: links, constraints, what done looks like."
            value={brief}
            onChange={(e) => setBrief(e.target.value)}
          />
        </div>
        <ArtifactPicker artifacts={artifacts} picked={picked} onPick={setPicked} />
        {!from && (
          <div className="flex items-start gap-2 text-sm">
            <Checkbox
              id="send-task-now"
              className="mt-0.5"
              checked={startNow}
              aria-describedby="send-task-now-help"
              onCheckedChange={(v) => setStartNow(v === true)}
            />
            <div className="flex flex-col">
              <Label htmlFor="send-task-now">Start now</Label>
              <span id="send-task-now-help" className="text-muted-foreground text-xs">
                Skip the inbox: it goes to the top of the team's plan, and the lead is told.
              </span>
            </div>
          </div>
        )}
        {error && <output className="text-destructive text-sm [overflow-wrap:anywhere]">{error}</output>}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => void send()}>Send</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// InboxScreen loads a team's board and shows its inbox. version reloads it when the team's
// transcript changes; botName names the senders, who may be in other teams.
export function InboxScreen({
  team,
  version,
  teams,
  botName,
  usageLimit,
  draining,
  onBack,
  menu,
}: {
  team: Bot
  version: string
  teams: Bot[]
  botName: (id: string) => string
  usageLimit: boolean
  draining: boolean
  onBack: () => void
  // menu is the phone layout's button that opens the sidebar; null above the phone width.
  menu?: ReactNode
}) {
  const board = useLoad(() => api.board(team.id).then((b) => ({ board: b, at: Date.now() })), `${team.id}:${version}`)
  const act: Act = async (action) => {
    try {
      await action()
      board.setError(null)
      board.reload()
      return true
    } catch (e) {
      board.setError((e as Error).message)
      return false
    }
  }
  if (!board.data) return board.error ? <p className="text-destructive p-4 text-xs">{board.error}</p> : null
  const data = board.data.board
  const view: InboxContext = {
    team,
    lead: leadName(team),
    minutes: team.team?.decisionMinutes ?? 30,
    planned: data.plan?.length ?? 0,
    stop: clockStop(usageLimit, draining, data.goal),
    loadedAt: board.data.at,
    names: {
      team: (id) => data.teams[id] ?? teams.find((t) => t.id === id)?.name ?? "a deleted team",
      member: botName,
      at: when,
    },
    act,
  }
  return <InboxView board={data} view={view} error={board.error} onBack={onBack} menu={menu} />
}

// AcrossScreen loads what needs the user and every team's board, for Across teams.
export function AcrossScreen({
  needs,
  pairRequest,
  onPairDecided,
  teams,
  version,
  chatName,
  onOpen,
  onChange,
  menu,
}: {
  needs: NeedsYouItem[]
  pairRequest?: PairRequest | null
  onPairDecided?: () => void
  teams: Bot[]
  version: string
  chatName: (id: string) => string
  onOpen: (chatId: string) => void
  onChange: () => void
  // menu is the phone layout's button that opens the sidebar; null above the phone width.
  menu?: ReactNode
}) {
  const [sending, setSending] = useState(false)
  const boards = useLoad(
    () => Promise.all(teams.map((t) => api.board(t.id).then((b) => [t.id, b] as const))).then((l) => new Map(l)),
    version,
  )
  const act: Act = async (action) => {
    try {
      await action()
      boards.setError(null)
      boards.reload()
      onChange()
      return true
    } catch (e) {
      boards.setError((e as Error).message)
      return false
    }
  }
  return (
    <>
      <AcrossTeams
        needs={needs}
        pairRequest={pairRequest}
        onPairDecided={onPairDecided}
        boards={boards.data ?? new Map()}
        teams={teams}
        error={boards.error}
        act={act}
        onOpen={onOpen}
        onNewTask={() => setSending(true)}
        chatName={chatName}
        menu={menu}
      />
      <SendTaskDialog
        open={sending}
        onOpenChange={setSending}
        from={null}
        teams={teams}
        onDone={() => {
          boards.reload()
          onChange()
        }}
      />
    </>
  )
}
