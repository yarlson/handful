import { useEffect, useRef, useState } from "react"
import { ArrowDown, ArrowUp, Pin, PinOff } from "lucide-react"
import { api, type Board, type Bot, type Goal, type Item } from "@/lib/api"
import { PausedForUpdate } from "@/components/update-pill"
import {
  clock,
  SendTaskDialog,
  SentSection,
  SourceChip,
  BudgetSection,
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
import { fromUser, leadName, memberName, nextUp, taskBar, type Names } from "@/lib/tasks"
import { moved, placeNote } from "@/lib/queue"
import { cn } from "@/lib/utils"

export type { Act }

// taskWord names a task's state as text.
const taskWord: Record<Goal["state"], string> = {
  active: "Now",
  paused: "Paused",
  delivered: "Done, waiting for approval",
  done: "Approved",
  ended: "Ended",
  stopped: "Stopped",
  queued: "Planned",
  incoming: "In the inbox",
  later: "Later",
  declined: "Pushed back",
}

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

// TaskBar shows a team's Now task above its channel, with the controls of its budget, the
// way into the team's inbox, and the next task in the plan. "/task <text>" in the team's
// chat sends the lead a task, which adds a board event to the transcript: its new rev reloads
// the bar. onShowWork opens the Work tab, where the plan is.
export function TaskBar({
  team,
  version,
  onShowWork,
  onOpenInbox,
  teams = [],
  draining = false,
  pause = "Paused for update",
}: {
  team: Bot
  // version changes when the team's transcript or daily budget changes; it reloads the bar. It
  // leaves out who is running: a key that changes faster than the board loads never shows one.
  version: string
  onShowWork?: () => void
  onOpenInbox?: () => void
  // teams are the app's teams, to send tasks to.
  teams?: Bot[]
  // draining: the server starts no new turns until it restarts for an update.
  draining?: boolean
  // pause names the drain: for an update or for a restart.
  pause?: string
}) {
  const board = useLoad(() => api.board(team.id), `${team.id}:${version}`)
  const [sending, setSending] = useState(false)
  // stopping is the task whose Stop waits for a confirm; a confirm left open never stops the next task.
  const [stopping, setStopping] = useState<string | null>(null)
  const names = boardNames(board.data, teams, team)
  const goal = board.data?.goal
  const plan = board.data?.plan ?? []
  const inbox = board.data?.inbox?.length ?? 0
  const act = async (action: () => Promise<unknown>) => {
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
  if (!board.data) {
    return board.error ? <p className="text-destructive border-b px-4 py-2 text-xs">{board.error}</p> : null
  }
  const bar = taskBar(goal, plan.length)
  const upNext = nextUp(plan)
  return (
    <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2 text-sm" data-task-bar={bar.mode}>
      {goal && bar.mode === "now" ? (
        <>
          <Badge variant={goal.state === "paused" ? "destructive" : "secondary"}>{taskWord[goal.state]}</Badge>
          <FromChip goal={goal} names={names} />
          {draining && goal.state === "active" && <PausedForUpdate label={pause} />}
          <span className="min-w-0 flex-1 truncate" title={goal.text}>
            {goal.text}
          </span>
          {upNext && (
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground max-w-[30%] truncate text-xs underline-offset-2 hover:underline"
              title={upNext}
              onClick={onShowWork}
            >
              {upNext}
            </button>
          )}
          {board.data.budget?.paused && <ToneChip tone="attention">Today's turns used</ToneChip>}
          <span className="text-muted-foreground text-xs">
            {goal.used} {goal.used === 1 ? "turn" : "turns"} so far
          </span>
          {bar.actions.includes("approve") && (
            <Button
              size="xs"
              aria-label={`Approve: ${goal.text}`}
              onClick={() => void act(() => api.approveTask(goal.id))}
            >
              Approve
            </Button>
          )}
          {bar.actions.includes("changes") && (
            <TextAction
              id={`bar-changes-${goal.id}`}
              label="What should change? The task comes back as Now."
              open={`Ask for changes: ${goal.text}`}
              submit="Ask for changes"
              onSubmit={(text) => act(() => api.changesTask(goal.id, text))}
            />
          )}
          {bar.actions.includes("resume") && (
            <Button size="xs" onClick={() => void act(() => api.extendGoal(goal.id, 0))}>
              Resume
            </Button>
          )}
          {bar.actions.includes("stop") &&
            (stopping === goal.id ? (
              <>
                <span className="text-xs">Stop the task? Running turns finish, and its steps stay.</span>
                <Button
                  size="xs"
                  variant="destructive"
                  onClick={() => void act(() => api.stopTask(goal.id)).then(() => setStopping(null))}
                >
                  Stop task
                </Button>
                <Button size="xs" variant="ghost" onClick={() => setStopping(null)}>
                  Keep it
                </Button>
              </>
            ) : (
              <Button size="xs" variant="ghost" onClick={() => setStopping(goal.id)}>
                Stop…
              </Button>
            ))}
        </>
      ) : goal && bar.mode === "stopped" ? (
        <>
          <Badge variant="outline">Stopped</Badge>
          <span className="min-w-0 flex-1 truncate" title={goal.text}>
            {goal.text}
          </span>
          <Button size="xs" variant="outline" onClick={() => void act(() => api.reopenTask(goal.id))}>
            Reopen
          </Button>
          <span className="text-muted-foreground shrink-0 text-xs">Reopen puts it back in the inbox.</span>
        </>
      ) : bar.mode === "hold" && plan[0] ? (
        <>
          <Badge variant="outline">Plan on hold</Badge>
          <span className="min-w-0 flex-1 truncate" title={plan[0].text}>
            next: {plan[0].text}
          </span>
          <Button size="xs" onClick={() => void act(() => api.startNextGoal(team.id))}>
            Start now
          </Button>
        </>
      ) : (
        <>
          <span className="text-muted-foreground min-w-0 flex-1 truncate">Nothing now.</span>
          <span className="text-muted-foreground shrink-0 text-xs">
            To send the lead a task, type <code className="font-mono">/task &lt;what the team should do&gt;</code> in
            the chat.
          </span>
        </>
      )}
      <Button
        size="xs"
        variant="outline"
        aria-label={`Inbox, ${inbox} waiting`}
        onClick={onOpenInbox}
        disabled={!onOpenInbox}
      >
        Inbox <strong>{inbox}</strong>
      </Button>
      {teams.length > 1 && (
        <Button size="xs" variant="ghost" aria-haspopup="dialog" onClick={() => setSending(true)}>
          Send to a team…
        </Button>
      )}
      <SendTaskDialog open={sending} onOpenChange={setSending} from={team} teams={teams} onDone={board.reload} />
      {board.error && <span className="text-destructive w-full text-xs">{board.error}</span>}
    </div>
  )
}

// StepCard is one step of the Now task: its owner and reviewer, its change requests and its
// history.
function StepCard({ team, item }: { team: Bot; item: Item }) {
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
    <div className="flex flex-col gap-1 rounded-md border p-2 text-sm">
      <div className="flex items-start gap-2">
        <span className="flex-1 font-medium">{item.title}</span>
        {item.reviewRounds > 0 && toggle}
      </div>
      <p className="text-muted-foreground flex flex-wrap items-center gap-x-2 text-xs">
        {who}
        {item.reviewRounds === 0 && toggle}
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
    <section className="flex flex-col gap-1.5" aria-labelledby="plan-heading">
      <h3 id="plan-heading" className="text-muted-foreground text-xs font-medium uppercase">
        Next, in {leadName(team)}'s order ({plan.length})
      </h3>
      <ol className="flex flex-col gap-1.5">
        {plan.map((g, i) => (
          <li key={g.id} className="flex flex-col gap-1 rounded-md border p-2 text-sm">
            <div className="flex items-start gap-2">
              <span className="text-muted-foreground w-4 shrink-0 text-xs">{i + 1}</span>
              <span className="line-clamp-2 flex-1" title={g.text}>
                {g.text}
              </span>
              <FromChip goal={g} names={names} />
              {g.pinned && <Badge variant="outline">pinned</Badge>}
            </div>
            <div className="flex flex-wrap items-center justify-end gap-1">
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
            <TaskHistory goal={g} version={rev} />
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
        <p className="text-muted-foreground text-xs">The first one starts when the Now task is done or stopped.</p>
      )}
      <p className="sr-only" aria-live="polite">
        {note}
      </p>
    </section>
  )
}

// DoneList shows the tasks that are done and wait for approval by whoever asked. The team
// doesn't wait for it; the user can approve or ask for changes here.
function DoneList({ done, act, names, rev }: { done: Goal[]; act: Act; names: Names; rev: number }) {
  if (done.length === 0) return null
  return (
    <section className="flex flex-col gap-1.5" aria-labelledby="done-heading">
      <h3 id="done-heading" className="text-muted-foreground text-xs font-medium uppercase">
        Done, waiting for approval ({done.length})
      </h3>
      <ul className="flex flex-col gap-1.5">
        {done.map((g) => (
          <li key={g.id} className="flex flex-col gap-1.5 rounded-md border p-2 text-sm">
            <div className="flex items-start gap-2">
              <span className="flex-1 font-medium">{g.text}</span>
              <FromChip goal={g} names={names} />
            </div>
            <div className="flex flex-wrap gap-1.5">
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
            </div>
            <TaskHistory goal={g} version={rev} />
          </li>
        ))}
      </ul>
    </section>
  )
}

// StoppedList shows the team's stopped tasks: their steps stay, and Reopen puts one back in
// the inbox for the lead to decide on again. The latest task, when stopped, shows in Now.
function StoppedList({ stopped, act, names, rev }: { stopped: Goal[]; act: Act; names: Names; rev: number }) {
  if (stopped.length === 0) return null
  return (
    <section className="flex flex-col gap-1.5" aria-labelledby="stopped-heading">
      <h3 id="stopped-heading" className="text-muted-foreground text-xs font-medium uppercase">
        Stopped ({stopped.length})
      </h3>
      <ul className="flex flex-col gap-1.5">
        {stopped.map((g) => (
          <li key={g.id} className="flex flex-col gap-1.5 rounded-md border p-2 text-sm">
            <div className="flex items-start gap-2">
              <span className="flex-1 font-medium">{g.text}</span>
              <FromChip goal={g} names={names} />
            </div>
            <p className="text-muted-foreground text-xs">
              {g.used} {g.used === 1 ? "turn" : "turns"} used · Reopen puts it back in the inbox.
            </p>
            <Button
              size="xs"
              variant="outline"
              className="self-start"
              aria-label={`Reopen: ${g.text}`}
              onClick={() => void act(() => api.reopenTask(g.id))}
            >
              Reopen
            </Button>
            <TaskHistory goal={g} version={rev} />
          </li>
        ))}
      </ul>
    </section>
  )
}

// BoardTab is a team's Work tab: its Now task and steps, the tasks done and stopped, the plan,
// turns on no task, today's budget, the inbox, and what it sent other teams.
export function BoardTab({
  team,
  version,
  teams = [],
  onOpenInbox,
}: {
  team: Bot
  version: number
  teams?: Bot[]
  onOpenInbox?: () => void
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
    />
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
}) {
  const { goal, items } = data
  const names = boardNames(data, allTeams, team)
  const plan = data.plan ?? []
  const inbox = data.inbox ?? []
  const now = goal?.state === "active" || goal?.state === "paused"
  return (
    <div className="flex flex-col gap-3">
      {error && <p className="text-destructive text-xs">{error}</p>}
      <section className="flex flex-col gap-1.5" aria-labelledby="now-heading">
        <h3 id="now-heading" className="text-muted-foreground text-xs font-medium uppercase">
          {now || !goal ? "Now" : "Latest task"}
        </h3>
        {goal ? (
          <div className="flex flex-col gap-1 text-sm">
            <div className="flex flex-wrap items-start gap-2">
              <span className="flex-1 font-medium">{goal.text}</span>
              <FromChip goal={goal} names={names} />
              {!now && <Badge variant="outline">{taskWord[goal.state]}</Badge>}
              {goal.state === "paused" && <Badge variant="destructive">Paused</Badge>}
            </div>
            <p className="text-muted-foreground text-xs">
              {goal.used} {goal.used === 1 ? "turn" : "turns"} so far · started {clock(goal.createdAt)}
            </p>
            <TaskHistory goal={goal} steps={items} version={rev} />
          </div>
        ) : (
          <p className="text-muted-foreground text-sm">
            Nothing now. The lead splits a task into steps when it starts.
          </p>
        )}
        {goal && items.length === 0 && <p className="text-muted-foreground text-sm">No steps yet.</p>}
      </section>
      {stateOrder.map((state) => {
        const group = items.filter((i) => i.state === state)
        if (group.length === 0) return null
        return (
          <section key={state} className="flex flex-col gap-1.5">
            <h4 className="text-muted-foreground text-xs font-medium">
              {stateLabel[state]} ({group.length})
            </h4>
            {group.map((item) => (
              <StepCard key={item.id} team={team} item={item} />
            ))}
          </section>
        )
      })}
      <DoneList done={data.done ?? []} act={act} names={names} rev={rev} />
      <StoppedList stopped={(data.stopped ?? []).filter((g) => g.id !== goal?.id)} act={act} names={names} rev={rev} />
      {plan.length > 0 && (
        <PlanList team={team} plan={plan} held={!now && plan.length > 0} act={act} names={names} rev={rev} />
      )}
      {data.offTask && <OffTaskBox team={team} offTask={data.offTask} act={act} />}
      {data.budget && <BudgetSection team={team} budget={data.budget} act={act} />}
      <section className="flex flex-col gap-1.5" aria-labelledby="inbox-heading">
        <h3 id="inbox-heading" className="text-muted-foreground text-xs font-medium uppercase">
          Inbox ({inbox.length})
        </h3>
        <div className="flex items-center gap-2 text-xs">
          <span className="text-muted-foreground flex-1">
            {inbox.length === 0
              ? "Nothing waits for a decision."
              : `${inbox.length} ${inbox.length === 1 ? "task waits" : "tasks wait"} for ${leadName(team)}'s decision.`}
          </span>
          {onOpenInbox && (
            <Button size="xs" variant="outline" onClick={onOpenInbox}>
              Open inbox
            </Button>
          )}
        </div>
      </section>
      <SentSection goals={data.handedOver ?? []} names={names} act={act} version={rev} />
    </div>
  )
}
