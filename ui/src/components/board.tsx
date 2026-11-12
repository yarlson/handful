import { useEffect, useRef, useState, type ReactNode } from "react"
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  ChevronRight,
  Circle,
  CircleDot,
  Eye,
  MinusCircle,
  Pin,
  PinOff,
  Play,
  Send,
  Square,
  type LucideIcon,
} from "lucide-react"
import { api, type Board, type Bot, type Goal, type Item } from "@/lib/api"
import { PausedForUpdate } from "@/components/update-pill"
import {
  clock,
  SendTaskDialog,
  SourceChip,
  BudgetSection,
  LimitAction,
  OffTaskBox,
  TaskHistory,
  TextAction,
  ToneChip,
  when,
  WithdrawAction,
  type Act,
} from "@/components/tasks"
import { actorOf, eventLabel, eventTone, historyToggle, type ItemEvent } from "@/lib/questions"
import { useLoad } from "@/components/bot-panel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  awaitsAnswer,
  canWithdraw,
  fromUser,
  leadName,
  memberName,
  sentState,
  turnsLine,
  type Names,
} from "@/lib/tasks"
import { moved, placeNote } from "@/lib/queue"
import { cn } from "@/lib/utils"

export type { Act }

// stateOrder lists step states in the order the board shows them: what needs
// attention first, what is over last.
const stateOrder: Item["state"][] = ["blocked", "review", "doing", "todo", "done", "dropped"]

const stateLabel: Record<Item["state"], string> = {
  blocked: "Blocked",
  review: "In review",
  doing: "Doing",
  todo: "To do",
  done: "Done",
  dropped: "Dropped",
}

// boardNames names other teams from the board's names, then from the app's teams, and
// members from the team's own.
function boardNames(board: { teams: Record<string, string> } | null | undefined, teams: Bot[], team: Bot): Names {
  return {
    team: (id) => board?.teams[id] ?? teams.find((t) => t.id === id)?.name ?? "a deleted team",
    member: (id) => memberName(team, id),
    at: when,
  }
}

// FromChip marks a task that came from another team or a member, not from the user.
function FromChip({ goal, names }: { goal: Goal; names: Names }) {
  return fromUser(goal) ? null : <SourceChip goal={goal} names={names} />
}

// NowChip stands in for the Work tab in the chat header while the panel is closed: what the team
// does now and how many things need the user. It opens the Work tab. On a narrow window it fills
// the header and shows the title and the count only.
export function NowChip({ team, version, onOpen }: { team: Bot; version: string; onOpen: () => void }) {
  const board = useLoad(() => api.board(team.id), `${team.id}:${version}`)
  return board.data ? <NowChipView data={board.data} onOpen={onOpen} /> : null
}

// NowChipView is the Now chip of a loaded board.
export function NowChipView({ data, onOpen }: { data: Board; onOpen: () => void }) {
  const now = nowTask(data.goal)
  const needs = needsOf(data).length
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label={`Work: ${now ? `now, ${now.text}` : "nothing now"}${needs > 0 ? `; ${needs} need you` : ""}. Opens the Work tab.`}
      title={now?.text}
      className="hover:bg-muted flex max-w-md min-w-0 items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs max-md:min-h-11 max-md:max-w-none max-md:flex-1"
    >
      <span
        aria-hidden
        className={cn(
          "size-1.5 shrink-0 rounded-full",
          !now ? "bg-muted-foreground/40" : now.state === "paused" ? "bg-amber-500" : "bg-emerald-500",
        )}
      />
      <span className={cn("text-muted-foreground shrink-0", now && "max-md:hidden")}>
        {now ? "Now" : "Nothing now"}
      </span>
      {now && <span className="min-w-0 truncate">{now.text}</span>}
      {needs > 0 && (
        <ToneChip tone="attention">
          <span className="max-md:hidden">{needs} need you</span>
          <span className="md:hidden">{needs}</span>
        </ToneChip>
      )}
    </button>
  )
}

// nowTask is the task the team works on: active, or paused by its budget. A stopped or finished
// latest task is not Now; it shows under Earlier.
function nowTask(goal: Goal | null): Goal | null {
  return goal && (goal.state === "active" || goal.state === "paused") ? goal : null
}

// Need is one thing in a team's Work tab that only the user settles.
type Need =
  | { kind: "approve"; goal: Goal } // the team's own task is done and waits for approval
  | { kind: "sent-done"; goal: Goal } // a task the team sent is done at the other team
  | { kind: "answer"; goal: Goal } // a team the task was sent to asked a question
  | { kind: "pushed"; goal: Goal } // a team the task was sent to pushed it back
  | { kind: "decide"; goal: Goal } // the lead missed a decision; only the user decides now
  | { kind: "budget" } // today's turns are used

// needsOf lists what needs the user in a team's board, most pressing first.
export function needsOf(data: Board): Need[] {
  const sent = data.handedOver ?? []
  return [
    ...(data.budget?.paused ? [{ kind: "budget" } as const] : []),
    ...(data.inbox ?? []).filter((g) => g.needsUser).map((goal) => ({ kind: "decide", goal }) as const),
    ...sent.filter((g) => awaitsAnswer(g)).map((goal) => ({ kind: "answer", goal }) as const),
    ...(data.done ?? []).map((goal) => ({ kind: "approve", goal }) as const),
    ...sent.filter((g) => g.state === "delivered").map((goal) => ({ kind: "sent-done", goal }) as const),
    ...sent.filter((g) => g.state === "declined" && !g.dismissedAt).map((goal) => ({ kind: "pushed", goal }) as const),
  ]
}

// sentOpen tells whether a sent task is still with the other team: not over, and not waiting on
// the user (those are in Needs you).
function sentOpen(g: Goal): boolean {
  return ["incoming", "later", "queued", "active", "paused"].includes(g.state) && !awaitsAnswer(g)
}

// sentOver tells whether a sent task is finished for the sender: done, stopped, withdrawn, or
// pushed back and put away.
function sentOver(g: Goal): boolean {
  return ["done", "stopped", "ended"].includes(g.state) || (g.state === "declined" && Boolean(g.dismissedAt))
}

// StepRow is one step of the Now task, as one line: its state, title and owner. It opens to the
// step's change requests, history and details.
function StepRow({ team, item }: { team: Bot; item: Item }) {
  const [open, setOpen] = useState(false)
  const id = `step-${item.id}`
  const over = item.state === "done" || item.state === "dropped"
  const who = item.ownerId ? memberName(team, item.ownerId) : "No owner yet"
  const Mark = stepMark[item.state]
  return (
    <li className="text-sm">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((v) => !v)}
        className="hover:bg-sidebar-foreground/5 flex w-full items-start gap-2 rounded-md px-1.5 py-1 text-left max-md:min-h-11 max-md:py-2"
      >
        <Mark.icon aria-hidden className={cn("mt-0.5 size-4 shrink-0", Mark.className)} />
        <span className="min-w-0 flex-1">
          <span className={cn("block", over ? "text-muted-foreground" : "font-medium")}>{item.title}</span>
          <span className="text-muted-foreground block text-xs">
            {stateLabel[item.state]} · {who}
            {item.reviewRounds > 0 &&
              ` · ${item.reviewRounds} ${item.reviewRounds === 1 ? "change request" : "change requests"}`}
          </span>
        </span>
        <ChevronRight
          aria-hidden
          className={cn("text-muted-foreground mt-0.5 size-3.5 shrink-0 transition-transform", open && "rotate-90")}
        />
      </button>
      {open && (
        <div id={id} className="mt-1 mb-2 ml-7">
          <StepDetails team={team} item={item} />
        </div>
      )}
    </li>
  )
}

// stepMark is the icon of a step's state; the state is also written next to it.
const stepMark: Record<Item["state"], { icon: LucideIcon; className: string }> = {
  blocked: { icon: AlertTriangle, className: "text-amber-600 dark:text-amber-400" },
  review: { icon: Eye, className: "text-violet-600 dark:text-violet-400" },
  doing: { icon: CircleDot, className: "text-blue-600 dark:text-blue-400" },
  todo: { icon: Circle, className: "text-muted-foreground" },
  done: { icon: CheckCircle2, className: "text-emerald-600 dark:text-emerald-400" },
  dropped: { icon: MinusCircle, className: "text-muted-foreground" },
}

// StepDetails is an opened step: its reviewer, change requests, history and details.
export function StepDetails({ team, item }: { team: Bot; item: Item }) {
  const who = `${item.ownerId ? memberName(team, item.ownerId) : "No owner yet"}${item.reviewerId ? ` · reviewed by ${memberName(team, item.reviewerId)}` : ""}`
  const [history, setHistory] = useState(false)
  const historyId = `history-${item.id}`
  const count = item.eventCount ?? 0
  const toggle = (item.reviewRounds > 0 || count > 0) && (
    <Button
      size="xs"
      variant={item.reviewRounds > 0 ? "outline" : "link"}
      className={item.reviewRounds > 0 ? "" : "text-muted-foreground h-auto p-0 text-xs"}
      aria-expanded={history}
      aria-controls={historyId}
      onClick={() => setHistory((v) => !v)}
    >
      {historyToggle(item.reviewRounds, count)}
    </Button>
  )
  return (
    <div className="flex flex-col gap-1.5 text-sm">
      <p className="text-muted-foreground flex flex-wrap items-center gap-x-2 text-xs">
        {who}
        {toggle}
      </p>
      {item.latestChange && (
        <div className="rounded-md border border-amber-500/50 bg-amber-500/10 p-2 text-xs">
          <p className="font-medium text-amber-800 dark:text-amber-300">
            Changes asked · round {item.latestChange.round} · {item.latestChange.by} · {time(item.latestChange.at)}
          </p>
          <p className="mt-1 whitespace-pre-wrap">{item.latestChange.text}</p>
        </div>
      )}
      {history && <ItemHistory id={historyId} item={item} />}
      {item.note && (item.state === "blocked" || item.state === "dropped") && <p className="text-xs">{item.note}</p>}
      <details className="text-xs">
        <summary className="text-muted-foreground cursor-pointer">Details</summary>
        <p className="mt-1 whitespace-pre-wrap">{item.brief}</p>
        <p className="text-muted-foreground mt-1 whitespace-pre-wrap">Done when: {item.acceptance}</p>
        {item.result && <p className="mt-1 whitespace-pre-wrap">Result: {item.result}</p>}
      </details>
    </div>
  )
}

function time(ms: number) {
  return new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

function day(ms: number) {
  return new Date(ms).toLocaleDateString([], { month: "short", day: "numeric" })
}

// ItemHistory shows every event of an item, oldest first: each round of changes with its
// whole text, who asked and when.
export function ItemHistory({ id, item }: { id: string; item: Item }) {
  const events = useLoad(() => api.itemEvents(item.id), `${item.id}:${item.eventCount ?? 0}`)
  if (events.error) return <p className="text-destructive text-xs">{events.error}</p>
  if (!events.data) return null
  return <HistoryList id={id} item={item} events={events.data} />
}

export function HistoryList({ id, item, events }: { id: string; item: Item; events: ItemEvent[] }) {
  return (
    <div id={id} className="text-xs">
      {item.historyFrom && <p className="text-muted-foreground mb-1">History starts on {day(item.historyFrom)}.</p>}
      <ol aria-label={`History of ${item.title}`} className="flex flex-col gap-1.5 border-l pl-3">
        {events.map((e) => {
          const tone = eventTone(e.kind)
          return (
            <li
              key={e.id}
              className={cn(
                "relative",
                tone === "changes" && "rounded-sm bg-amber-500/10 p-1.5",
                tone === "passed" && "rounded-sm bg-emerald-500/10 p-1.5",
              )}
            >
              <p>
                <span
                  className={cn(
                    "font-medium",
                    tone === "changes" && "text-amber-800 dark:text-amber-300",
                    tone === "passed" && "text-emerald-800 dark:text-emerald-300",
                  )}
                >
                  {eventLabel(e)}
                </span>{" "}
                <span className="text-muted-foreground">
                  · {actorOf(e)} · {day(e.createdAt)} {time(e.createdAt)}
                </span>
              </p>
              {e.text && <p className="mt-0.5 whitespace-pre-wrap">{e.text}</p>}
            </li>
          )
        })}
      </ol>
    </div>
  )
}

// reveal shows a planned task's controls while the pointer is on it or the keyboard is in it, on
// a touch screen, and while its history or its Withdraw confirm is open: they are a lot of chrome
// for what is mostly status.
const reveal =
  "opacity-0 transition-opacity group-hover/plan:opacity-100 group-focus-within/plan:opacity-100 pointer-coarse:opacity-100 has-[[aria-expanded=true]]:opacity-100 has-[[data-variant=destructive]]:opacity-100"

// PlanList shows a team's planned tasks in the lead's order. The user moves them, pins one
// where it is so the lead can't move it, starts the first when the plan holds, or withdraws one.
function PlanList({
  team,
  plan,
  held,
  act,
  names,
  rev,
}: {
  team: Bot
  plan: Goal[]
  held: boolean
  act: Act
  names: Names
  rev: number
}) {
  const [note, setNote] = useState("")
  // focus is the task whose move button keeps the keyboard once the list shows the new order.
  const focus = useRef<{ id: string; step: -1 | 1; to: number } | null>(null)
  const ids = plan.map((g) => g.id)
  useEffect(() => {
    const f = focus.current
    if (!f) return
    const at = ids.indexOf(f.id)
    // Wait for the reloaded board, which has the task at its new place.
    if (at !== f.to) return
    // At an end the button it used is disabled; the other one takes the keyboard.
    const atEnd = f.step < 0 ? at === 0 : at === ids.length - 1
    const dir = f.step < 0 !== atEnd ? "up" : "down"
    document.getElementById(`plan-${f.id}-${dir}`)?.focus()
    focus.current = null
  })
  const move = async (g: Goal, i: number, step: -1 | 1) => {
    const order = moved(ids, i, step)
    if (!order) return
    const ok = await act(() => api.orderPlan(team.id, order))
    if (!ok) return
    setNote(placeNote(g.text, i + step + 1, ids.length))
    focus.current = { id: g.id, step, to: i + step }
  }
  return (
    <>
      <ol aria-labelledby="next-heading" className="flex flex-col gap-1.5">
        {plan.map((g, i) => (
          <li key={g.id} className="group/plan flex flex-col gap-1 rounded-md border p-2 text-sm">
            <div className="flex flex-wrap items-start gap-2">
              <span className="text-muted-foreground w-4 shrink-0 text-xs">{i + 1}</span>
              <span className="line-clamp-2 min-w-[60%] flex-1" title={g.text}>
                {g.text}
              </span>
              <FromChip goal={g} names={names} />
              {g.pinned && <Badge variant="outline">pinned</Badge>}
            </div>
            <div className={cn("flex flex-wrap items-center gap-1", reveal)}>
              <TaskHistory goal={g} version={rev} />
              <span className="flex-1" />
              <Button
                id={`plan-${g.id}-up`}
                size="icon-xs"
                variant="ghost"
                aria-label={`Move ${g.text} up`}
                disabled={i === 0}
                onClick={() => void move(g, i, -1)}
              >
                <ArrowUp />
              </Button>
              <Button
                id={`plan-${g.id}-down`}
                size="icon-xs"
                variant="ghost"
                aria-label={`Move ${g.text} down`}
                disabled={i === plan.length - 1}
                onClick={() => void move(g, i, 1)}
              >
                <ArrowDown />
              </Button>
              <Button
                size="icon-xs"
                variant="ghost"
                aria-label={g.pinned ? `Unpin ${g.text}` : `Pin ${g.text} at #${i + 1}`}
                aria-pressed={g.pinned === true}
                title={g.pinned ? "Unpin: the lead can move it again" : "Pin: the lead can't move it"}
                onClick={() => void act(() => api.pinTask(g.id, !g.pinned))}
              >
                {g.pinned ? <PinOff /> : <Pin />}
              </Button>
              <WithdrawAction g={g} to={team.name} act={act} />
            </div>
          </li>
        ))}
      </ol>
      {held ? (
        <div className="flex items-center gap-2 text-xs">
          <span className="text-muted-foreground flex-1">On hold.</span>
          <Button size="xs" onClick={() => void act(() => api.startNextGoal(team.id))}>
            Start now
          </Button>
        </div>
      ) : (
        <p className="text-muted-foreground text-xs">The first one starts when the Now task is done.</p>
      )}
      <p className="sr-only" aria-live="polite">
        {note}
      </p>
    </>
  )
}

// BoardTab is a team's Work tab. It answers, in order: what needs me, what the team does now,
// what comes next, what it waits on from other teams, and what is over.
export function BoardTab({
  team,
  version,
  teams = [],
  onOpenInbox,
  paused = null,
}: {
  team: Bot
  version: number
  teams?: Bot[]
  onOpenInbox?: () => void
  paused?: string | null
}) {
  const board = useLoad(() => api.board(team.id), `${team.id}:${version}`)
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
  if (!board.data) return board.error ? <p className="text-destructive text-xs">{board.error}</p> : null
  return (
    <BoardView
      team={team}
      data={board.data}
      rev={board.rev}
      error={board.error}
      act={act}
      teams={teams}
      onOpenInbox={onOpenInbox}
      paused={paused}
    />
  )
}

// Heading is a Work tab section's title, with its count and one optional action at the right.
function Heading({
  id,
  count,
  action,
  children,
}: {
  id: string
  count?: number
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="flex min-h-7 items-center gap-2">
      <h3 id={id} className="text-muted-foreground text-xs font-medium uppercase">
        {children}
        {count !== undefined && ` (${count})`}
      </h3>
      {action && <span className="ml-auto">{action}</span>}
    </div>
  )
}

// BoardView draws a loaded board; act runs a change and reloads it.
export function BoardView({
  team,
  data,
  error,
  act,
  teams: allTeams = [],
  onOpenInbox,
  paused = null,
  rev = 0,
}: {
  team: Bot
  data: Board
  // rev counts the board's loads; it reloads the open task histories with the board.
  rev?: number
  error: string | null
  act: Act
  teams?: Bot[]
  onOpenInbox?: () => void
  paused?: string | null
}) {
  const [sending, setSending] = useState(false)
  const names = boardNames(data, allTeams, team)
  const plan = data.plan ?? []
  const inbox = data.inbox ?? []
  const sent = data.handedOver ?? []
  const needs = needsOf(data)
  const waiting = sent.filter(sentOpen)
  const now = nowTask(data.goal)
  const stopped = (data.stopped ?? []).concat(data.goal && !now && data.goal.state === "stopped" ? [data.goal] : [])
  const earlier = [...new Map([...stopped, ...sent.filter(sentOver)].map((g) => [g.id, g])).values()]
  const lead = leadName(team)
  return (
    <div className="flex flex-col gap-5">
      {error && <p className="text-destructive text-xs">{error}</p>}
      <NowSection team={team} goal={now} data={data} names={names} act={act} rev={rev} paused={paused} />
      {needs.length > 0 && (
        <section
          aria-labelledby="needs-heading"
          className="flex flex-col gap-2 rounded-lg border border-amber-500/50 p-2.5"
        >
          <h3 id="needs-heading" className="text-xs font-medium text-amber-800 uppercase dark:text-amber-300">
            Needs you ({needs.length})
          </h3>
          <ul className="flex flex-col divide-y divide-amber-500/20">
            {needs.map((n) => (
              <NeedRow
                key={n.kind === "budget" ? "budget" : `${n.kind}:${n.goal.id}`}
                n={n}
                team={team}
                names={names}
                act={act}
                onOpenInbox={onOpenInbox}
                budget={data.budget}
              />
            ))}
          </ul>
        </section>
      )}
      {(plan.length > 0 || inbox.length > 0) && (
        <section aria-labelledby="next-heading" className="flex flex-col gap-1.5">
          <Heading id="next-heading" count={plan.length}>
            Next
          </Heading>
          {plan.length > 0 && (
            <>
              <p className="text-muted-foreground -mt-1 text-xs">In {lead}'s order. Move or pin to change it.</p>
              <PlanList team={team} plan={plan} held={!now} act={act} names={names} rev={rev} />
            </>
          )}
          {inbox.length > 0 && (
            <div className="flex items-center gap-2 rounded-md border border-dashed px-2.5 py-1.5 text-xs">
              <span className="flex-1">
                {`Inbox: ${inbox.length} ${inbox.length === 1 ? "task waits" : "tasks wait"} for ${lead}'s decision.`}
              </span>
              {onOpenInbox && (
                <Button size="xs" variant="outline" onClick={onOpenInbox}>
                  Open inbox
                </Button>
              )}
            </div>
          )}
        </section>
      )}
      <section aria-labelledby="sent-heading" className="flex flex-col gap-1.5">
        <Heading
          id="sent-heading"
          count={waiting.length}
          action={
            allTeams.length > 1 && (
              <Button size="xs" variant="ghost" aria-haspopup="dialog" onClick={() => setSending(true)}>
                <Send /> Send a task…
              </Button>
            )
          }
        >
          Waiting on other teams
        </Heading>
        {waiting.length === 0 ? (
          <p className="text-muted-foreground text-xs">Nothing sent is open.</p>
        ) : (
          <ul className="flex flex-col gap-1.5">
            {waiting.map((g) => (
              <SentRow key={g.id} g={g} names={names} act={act} rev={rev} />
            ))}
          </ul>
        )}
      </section>
      {earlier.length > 0 && (
        <details className="group/earlier">
          <summary className="text-muted-foreground hover:text-foreground flex min-h-7 cursor-pointer list-none items-center max-md:min-h-11 gap-1 text-xs font-medium uppercase">
            <ChevronRight aria-hidden className="size-3.5 transition-transform group-open/earlier:rotate-90" />
            Earlier ({earlier.length})
          </summary>
          <ul className="mt-1.5 flex flex-col gap-1.5">
            {earlier.map((g) =>
              g.teamId === team.id ? (
                <StoppedRow key={g.id} g={g} names={names} act={act} rev={rev} />
              ) : (
                <SentRow key={g.id} g={g} names={names} act={act} rev={rev} />
              ),
            )}
          </ul>
        </details>
      )}
      {(data.budget || (data.offTask?.count ?? 0) > 0) && (
        <footer className="flex flex-col gap-3 border-t pt-3">
          {data.offTask && <OffTaskBox team={team} offTask={data.offTask} act={act} />}
          {data.budget && <BudgetSection team={team} budget={data.budget} act={act} />}
        </footer>
      )}
      <SendTaskDialog
        open={sending}
        onOpenChange={setSending}
        from={team}
        teams={allTeams}
        onDone={() => void act(async () => {})}
      />
    </div>
  )
}

// NowSection is the task the team works on, with its steps nested under it.
function NowSection({
  team,
  goal,
  data,
  names,
  act,
  rev,
  paused,
}: {
  team: Bot
  goal: Goal | null
  data: Board
  names: Names
  act: Act
  rev: number
  paused: string | null
}) {
  const [stopping, setStopping] = useState(false)
  const items = [...data.items].sort((a, b) => stateOrder.indexOf(a.state) - stateOrder.indexOf(b.state))
  const done = data.items.filter((i) => i.state === "done").length
  const plan = data.plan ?? []
  return (
    <section aria-labelledby="now-heading" className="flex flex-col gap-1.5">
      <Heading id="now-heading">Now</Heading>
      {goal ? (
        <div className="bg-background/60 flex flex-col gap-2 rounded-lg border p-2.5">
          <div className="flex flex-wrap items-start gap-2">
            <span className="min-w-[60%] flex-1 text-sm font-medium">{goal.text}</span>
            <FromChip goal={goal} names={names} />
          </div>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
            {goal.state === "paused" && <Badge variant="destructive">Paused</Badge>}
            {paused && goal.state === "active" && <PausedForUpdate label={paused} />}
            <span className="text-muted-foreground">
              {[
                items.length > 0 ? `${done} of ${items.length} steps done` : "No steps yet",
                `${goal.used} ${goal.used === 1 ? "turn" : "turns"}`,
                `since ${clock(goal.createdAt)}`,
              ].join(" · ")}
            </span>
          </div>
          {items.length > 0 && (
            <ul aria-label={`Steps of ${goal.text}`} className="-mx-1.5 flex flex-col">
              {items.map((item) => (
                <StepRow key={item.id} team={team} item={item} />
              ))}
            </ul>
          )}
          <div className="flex flex-wrap items-center gap-2 border-t pt-2">
            <TaskHistory goal={goal} steps={data.items} version={rev} />
            <span className="flex-1" />
            {goal.state === "paused" && !stopping && (
              <Button
                size="xs"
                aria-label={`Resume: ${goal.text}`}
                onClick={() => void act(() => api.extendGoal(goal.id, 0))}
              >
                <Play /> Resume
              </Button>
            )}
            {stopping ? (
              <span className="flex flex-wrap items-center gap-1.5 text-xs">
                Stop the task? Running turns finish, and its steps stay.
                <Button
                  size="xs"
                  variant="destructive"
                  onClick={() => void act(() => api.stopTask(goal.id)).then(() => setStopping(false))}
                >
                  Stop task
                </Button>
                <Button size="xs" variant="ghost" onClick={() => setStopping(false)}>
                  Keep it
                </Button>
              </span>
            ) : (
              <Button size="xs" variant="ghost" aria-label={`Stop: ${goal.text}`} onClick={() => setStopping(true)}>
                <Square /> Stop…
              </Button>
            )}
          </div>
        </div>
      ) : (
        <p className="text-muted-foreground text-sm">
          {plan.length > 0
            ? "Nothing now. The plan is on hold: start the next task below."
            : "Nothing now. To send the lead a task, type /task <what the team should do> in the chat."}
        </p>
      )}
    </section>
  )
}

// NeedRow is one thing in the Work tab that only the user settles, with the buttons that settle it.
function NeedRow({
  n,
  team,
  names,
  act,
  onOpenInbox,
  budget,
}: {
  n: Need
  team: Bot
  names: Names
  act: Act
  onOpenInbox?: () => void
  budget?: Board["budget"]
}) {
  if (n.kind === "budget") {
    return (
      <li className="flex flex-col gap-1.5 py-2 text-sm first:pt-0 last:pb-0">
        <p>
          <strong>Today's turns are used.</strong> New turns wait until midnight or a higher limit.
        </p>
        <div className="flex flex-wrap gap-1.5">
          <LimitAction team={team} open={`Raise: ${team.name}`} value={(budget?.limit ?? 0) + 50} act={act} />
        </div>
      </li>
    )
  }
  const g = n.goal
  const to = names.team(g.teamId)
  const lead =
    n.kind === "approve"
      ? "Done, approve it:"
      : n.kind === "sent-done"
        ? `Done at ${to}, approve it:`
        : n.kind === "answer"
          ? `${to} asks:`
          : n.kind === "pushed"
            ? `${to} pushed back:`
            : `${leadName(team)} didn't decide in time:`
  return (
    <li className="flex flex-col gap-1.5 py-2 text-sm first:pt-0 last:pb-0">
      <p>
        <span className="text-muted-foreground">{lead}</span> <span className="font-medium">{g.text}</span>
      </p>
      {(n.kind === "answer" || n.kind === "pushed") && g.intakeNote && (
        <p className="text-xs">
          <strong>{to}:</strong> {g.intakeNote}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        {(n.kind === "approve" || n.kind === "sent-done") && (
          <>
            <Button size="xs" aria-label={`Approve: ${g.text}`} onClick={() => void act(() => api.approveTask(g.id))}>
              Approve
            </Button>
            <TextAction
              id={`changes-${g.id}`}
              label="What should change? The task comes back as Now, first in the plan."
              open={`Ask for changes: ${g.text}`}
              submit="Ask for changes"
              onSubmit={(text) => act(() => api.changesTask(g.id, text))}
            />
          </>
        )}
        {n.kind === "answer" && (
          <TextAction
            id={`answer-${g.id}`}
            label={`Your answer to ${to}: ${g.intakeNote ?? ""}`}
            open={`Answer: ${g.text}`}
            submit="Send answer"
            onSubmit={(text) => act(() => api.answerTask(g.id, text))}
          />
        )}
        {n.kind === "pushed" && (
          <Button
            size="xs"
            variant="outline"
            aria-label={`Dismiss: ${g.text}`}
            onClick={() => void act(() => api.dismissSent(g.id))}
          >
            Dismiss
          </Button>
        )}
        {n.kind === "decide" && onOpenInbox && (
          <Button size="xs" variant="outline" onClick={onOpenInbox}>
            Open inbox
          </Button>
        )}
        {(n.kind === "answer" || n.kind === "pushed") && <TaskHistory goal={g} version={0} />}
      </div>
    </li>
  )
}

// SentRow is a task the team sent: where it is at the other team, and Withdraw while it hasn't
// started.
function SentRow({ g, names, act, rev }: { g: Goal; names: Names; act: Act; rev: number }) {
  const to = names.team(g.teamId)
  const state = sentState(g, to, when)
  return (
    <li className="flex flex-col gap-1 rounded-md border p-2 text-sm">
      <div className="flex items-start gap-2">
        <span className="line-clamp-2 min-w-0 flex-1" title={g.text}>
          {g.text}
        </span>
        <ToneChip tone={state.tone}>{state.label}</ToneChip>
      </div>
      <p className="text-muted-foreground text-xs">
        {[
          `To ${to}`,
          g.passedBy,
          turnsLine(g.used, to),
          g.state === "later" && g.intakeNote ? `Note: ${g.intakeNote}` : "",
        ]
          .filter(Boolean)
          .join(" · ")}
      </p>
      <div className="flex flex-wrap items-center gap-1.5">
        <TaskHistory goal={g} version={rev} />
        {canWithdraw(g) && <WithdrawAction g={g} to={to} act={act} />}
      </div>
    </li>
  )
}

// StoppedRow is a team's own stopped task under Earlier: its steps stay, and Reopen puts it back
// in the inbox for the lead to decide on again.
function StoppedRow({ g, names, act, rev }: { g: Goal; names: Names; act: Act; rev: number }) {
  return (
    <li className="flex flex-col gap-1 rounded-md border p-2 text-sm">
      <div className="flex flex-wrap items-start gap-2">
        <span className="text-muted-foreground line-clamp-2 min-w-[60%] flex-1">{g.text}</span>
        <FromChip goal={g} names={names} />
        <ToneChip tone="over">Stopped</ToneChip>
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        <TaskHistory goal={g} version={rev} />
        <Button
          size="xs"
          variant="outline"
          aria-label={`Reopen: ${g.text}`}
          onClick={() => void act(() => api.reopenTask(g.id))}
        >
          Reopen
        </Button>
      </div>
    </li>
  )
}
