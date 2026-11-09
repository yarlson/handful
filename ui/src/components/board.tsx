import { useEffect, useRef, useState, type ReactNode } from "react"
import { ArrowDown, ArrowUp, X } from "lucide-react"
import { api, type Board, type Bot, type Goal, type Item, type Request } from "@/lib/api"
import { useLoad } from "@/components/bot-panel"
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
import {
  canWithdraw,
  grouped,
  intakeLabel,
  moved,
  nextUp,
  onHold,
  openGoal,
  placeNote,
  reasonLine,
  showsPosition,
  slotHeader,
} from "@/lib/queue"
import { cn } from "@/lib/utils"

const goalBadge: Record<Goal["state"], string> = {
  active: "Goal",
  delivered: "Delivered",
  paused: "Paused",
  done: "Done",
  ended: "Ended",
  queued: "Queued",
}

// stateOrder lists item states in the order the board shows them: what needs
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

function memberName(team: Bot, id: string) {
  return team.members?.find((m) => m.id === id)?.name ?? "a former member"
}

// GoalBar shows a team's goal above its channel, with the controls of its
// budget and of the goals queued after it. A goal starts with "/goal <text>" in
// the team's chat, which adds a board event to the transcript: its new rev
// reloads the bar. onShowBoard opens the board tab, where the queue is.
export function GoalBar({ team, version, onShowBoard }: { team: Bot; version: number; onShowBoard?: () => void }) {
  const board = useLoad(() => api.board(team.id), `${team.id}:${version}`)
  const [ending, setEnding] = useState(false)
  const [startNext, setStartNext] = useState(true)
  const [accepting, setAccepting] = useState(false)
  const goal = board.data?.goal
  const queue = board.data?.queue ?? []
  const next = queue[0]
  const act = async (action: () => Promise<unknown>) => {
    try {
      await action()
      board.setError(null)
      board.reload()
    } catch (e) {
      board.setError((e as Error).message)
    }
  }
  if (!board.data) {
    return board.error ? <p className="text-destructive border-b px-4 py-2 text-xs">{board.error}</p> : null
  }
  const open = goal && openGoal(goal.state)
  const upNext = nextUp(queue)
  return (
    <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2 text-sm">
      {open ? (
        <>
          <Badge variant={goal.state === "paused" ? "destructive" : "secondary"}>{goalBadge[goal.state]}</Badge>
          <span className="min-w-0 flex-1 truncate" title={goal.text}>
            {goal.text}
          </span>
          {upNext && (
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground max-w-[30%] truncate text-xs underline-offset-2 hover:underline"
              title={upNext}
              onClick={onShowBoard}
            >
              {upNext}
            </button>
          )}
          <span className="text-muted-foreground text-xs" title="Turns that the goal used of its budget">
            {goal.used}/{goal.budget} turns
          </span>
          {goal.state === "delivered" &&
            (next ? (
              <Button size="xs" aria-haspopup="dialog" onClick={() => setAccepting(true)}>
                Accept and start next…
              </Button>
            ) : (
              <Button size="xs" onClick={() => void act(() => api.acceptGoal(goal.id))}>
                Accept
              </Button>
            ))}
          {goal.state === "paused" && goal.used < goal.budget && (
            <Button size="xs" onClick={() => void act(() => api.extendGoal(goal.id, 0))}>
              Resume
            </Button>
          )}
          {goal.state !== "delivered" && (
            <Button size="xs" variant="outline" onClick={() => void act(() => api.extendGoal(goal.id, 10))}>
              Add 10 turns
            </Button>
          )}
          {ending ? (
            <>
              <span className="text-xs">End the goal? Its work stops for good.</span>
              {next && (
                <label className="flex items-center gap-1.5 text-xs">
                  <Checkbox checked={startNext} onCheckedChange={(v) => setStartNext(v === true)} />
                  Start the next goal: {next.text}
                </label>
              )}
              <Button
                size="xs"
                variant="destructive"
                onClick={() => void act(() => api.endGoal(goal.id, startNext)).then(() => setEnding(false))}
              >
                End goal
              </Button>
              <Button size="xs" variant="ghost" onClick={() => setEnding(false)}>
                Keep it
              </Button>
            </>
          ) : (
            <Button
              size="xs"
              variant="ghost"
              onClick={() => {
                setStartNext(true)
                setEnding(true)
              }}
            >
              End goal
            </Button>
          )}
          {goal.state === "delivered" && next && (
            <span className="text-muted-foreground w-full text-xs">Next goal starts when you accept {goal.text}.</span>
          )}
        </>
      ) : onHold(goal?.state, queue.length) && next ? (
        <>
          <Badge variant="outline">Queue on hold</Badge>
          <span className="min-w-0 flex-1 truncate" title={next.text}>
            next: {next.text}
          </span>
          <Button size="xs" onClick={() => void act(() => api.startNextGoal(team.id))}>
            Start now
          </Button>
          <span className="text-muted-foreground shrink-0 text-xs">
            Or type <code className="font-mono">/goal &lt;what the team should do&gt;</code> to queue another.
          </span>
        </>
      ) : (
        <>
          <span className="text-muted-foreground min-w-0 flex-1 truncate" title={goal?.text}>
            {goal ? `Last goal (${goal.state}): ${goal.text}` : "No goal yet."}
          </span>
          <span className="text-muted-foreground shrink-0 text-xs">
            To start one, type <code className="font-mono">/goal &lt;what the team should do&gt;</code> in the chat.
          </span>
        </>
      )}
      {board.error && <span className="text-destructive w-full text-xs">{board.error}</span>}
      {goal && next && (
        <Dialog open={accepting} onOpenChange={setAccepting}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Accept {goal.text}?</DialogTitle>
              <DialogDescription>
                The next goal starts and uses its own budget: <strong>{next.text}</strong> (
                {team.team?.goalBudget ?? 30} turns).
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button
                variant="outline"
                onClick={() => void act(() => api.acceptGoal(goal.id, false)).then(() => setAccepting(false))}
              >
                Accept, hold the queue
              </Button>
              <Button onClick={() => void act(() => api.acceptGoal(goal.id, true)).then(() => setAccepting(false))}>
                Accept and start next
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}

function ItemCard({
  team,
  item,
  teams,
  children,
}: {
  team: Bot
  item: Item
  teams: Record<string, string>
  children?: ReactNode
}) {
  const who =
    item.teamId !== team.id
      ? `Asked of team ${teams[item.teamId] ?? "a deleted team"}`
      : `${item.ownerId ? memberName(team, item.ownerId) : "No owner yet"}${item.reviewerId ? ` · reviewed by ${memberName(team, item.reviewerId)}` : ""}`
  return (
    <div className="flex flex-col gap-1 rounded-md border p-2 text-sm">
      <div className="flex items-start gap-2">
        <span className="flex-1 font-medium">{item.title}</span>
        {item.reviewRounds > 0 && (
          <Badge variant="outline" title="Times that the reviewer asked for changes">
            {item.reviewRounds === 1 ? "1 change request" : `${item.reviewRounds} change requests`}
          </Badge>
        )}
      </div>
      <p className="text-muted-foreground text-xs">{who}</p>
      {children}
      {item.note && <p className="text-xs">{item.note}</p>}
      <details className="text-xs">
        <summary className="text-muted-foreground cursor-pointer">Details</summary>
        <p className="mt-1 whitespace-pre-wrap">{item.brief}</p>
        <p className="text-muted-foreground mt-1 whitespace-pre-wrap">Done when: {item.acceptance}</p>
        {item.result && <p className="mt-1 whitespace-pre-wrap">Result: {item.result}</p>}
      </details>
    </div>
  )
}

// QueueList shows a team's queued goals, which only the user orders, starts or cancels.
function QueueList({ team, board, held, act }: { team: Bot; board: Board; held: boolean; act: Act }) {
  const [note, setNote] = useState("")
  // focus is the goal whose move button keeps the keyboard once the list shows the new order.
  const focus = useRef<{ id: string; step: -1 | 1; to: number } | null>(null)
  const ids = board.queue.map((g) => g.id)
  useEffect(() => {
    const f = focus.current
    if (!f) return
    const at = ids.indexOf(f.id)
    // Wait for the reloaded board, which has the goal at its new place.
    if (at !== f.to) return
    // At an end the button it used is disabled; the other one takes the keyboard.
    const atEnd = f.step < 0 ? at === 0 : at === ids.length - 1
    const dir = f.step < 0 !== atEnd ? "up" : "down"
    document.getElementById(`queue-${f.id}-${dir}`)?.focus()
    focus.current = null
  })
  const move = async (g: Goal, i: number, step: -1 | 1) => {
    const order = moved(ids, i, step)
    if (!order) return
    const ok = await act(() => api.reorderGoals(team.id, order))
    if (!ok) return
    setNote(placeNote(g.text, i + step + 1, ids.length))
    focus.current = { id: g.id, step, to: i + step }
  }
  return (
    <section className="flex flex-col gap-1.5" aria-labelledby="queue-heading">
      <h3 id="queue-heading" className="text-muted-foreground text-xs font-medium uppercase">
        Next up ({board.queue.length})
      </h3>
      <ol className="flex flex-col gap-1.5">
        {board.queue.map((g, i) => (
          <li key={g.id} className="flex items-start gap-2 rounded-md border p-2 text-sm">
            <span className="text-muted-foreground w-4 shrink-0 text-xs">{i + 1}</span>
            <span className="line-clamp-2 flex-1" title={g.text}>
              {g.text}
            </span>
            <Button
              id={`queue-${g.id}-up`}
              size="icon-xs"
              variant="ghost"
              aria-label={`Move ${g.text} up`}
              disabled={i === 0}
              onClick={() => void move(g, i, -1)}
            >
              <ArrowUp />
            </Button>
            <Button
              id={`queue-${g.id}-down`}
              size="icon-xs"
              variant="ghost"
              aria-label={`Move ${g.text} down`}
              disabled={i === board.queue.length - 1}
              onClick={() => void move(g, i, 1)}
            >
              <ArrowDown />
            </Button>
            <Button
              size="icon-xs"
              variant="ghost"
              aria-label={`Cancel goal ${g.text}`}
              title="Cancel goal"
              onClick={() => void act(() => api.cancelGoal(g.id))}
            >
              <X />
            </Button>
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
        <p className="text-muted-foreground text-xs">Starts when you accept or end the current goal.</p>
      )}
      <p className="sr-only" aria-live="polite">
        {note}
      </p>
    </section>
  )
}

export type Act = (action: () => Promise<unknown>) => Promise<boolean>

// RequestCard shows a request to the team: who asked, where it waits and why.
function RequestCard({ team, r, teams, act }: { team: Bot; r: Request; teams: Record<string, string>; act: Act }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-muted-foreground flex items-center gap-1.5 text-xs">
        From {teams[r.fromTeamId] ?? "another team"}
        {showsPosition(r) && (
          <Badge variant="outline" title={`Position ${r.position} of ${r.total}`}>
            #{r.position}
          </Badge>
        )}
      </span>
      <ItemCard team={team} item={r} teams={teams}>
        {reasonLine(r.reason) && <p className="text-xs">{reasonLine(r.reason)}</p>}
      </ItemCard>
      {r.intake === "deferred" && (
        <Button
          size="xs"
          variant="outline"
          className="self-start"
          onClick={() => void act(() => api.undeferItem(r.id))}
        >
          Bring back
        </Button>
      )}
    </div>
  )
}

// AskedCard shows a request this team made of another team, as the asking team sees it.
function AskedCard({ r, teams, act }: { r: Request; teams: Record<string, string>; act: Act }) {
  const [confirm, setConfirm] = useState(false)
  const label = intakeLabel(r)
  const declined = r.intake === "declined"
  return (
    <div className={cn("flex flex-col gap-1 rounded-md border p-2 text-sm", declined && "border-destructive/50")}>
      <div className="flex items-start gap-2">
        <span className="flex-1 font-medium">{r.title}</span>
        <Badge variant={declined ? "destructive" : "outline"}>{label[0].toUpperCase() + label.slice(1)}</Badge>
        {showsPosition(r) && <Badge variant="outline">#{r.position}</Badge>}
      </div>
      <p className="text-muted-foreground text-xs">Asked of team {teams[r.teamId] ?? "a deleted team"}</p>
      {reasonLine(r.reason) && <p className="text-xs">{reasonLine(r.reason)}</p>}
      {declined && <p className="text-destructive text-xs">Items waiting on it are blocked.</p>}
      {canWithdraw(r) &&
        (confirm ? (
          <div className="flex items-center gap-2 text-xs">
            <span>Withdraw it? Team {teams[r.teamId] ?? "the other team"} stops working on it.</span>
            <Button
              size="xs"
              variant="destructive"
              onClick={() => void act(() => api.withdrawItem(r.id)).then(() => setConfirm(false))}
            >
              Withdraw
            </Button>
            <Button size="xs" variant="ghost" onClick={() => setConfirm(false)}>
              Keep it
            </Button>
          </div>
        ) : (
          <Button size="xs" variant="ghost" className="self-start" onClick={() => setConfirm(true)}>
            Withdraw…
          </Button>
        ))}
    </div>
  )
}

// BoardTab lists a team's goal and its items, the goals queued after it, the
// requests of other teams to it and what it asked of other teams.
export function BoardTab({ team, version }: { team: Bot; version: number }) {
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
  return <BoardView team={team} data={board.data} error={board.error} act={act} />
}

// BoardView draws a loaded board; act runs a change and reloads it.
export function BoardView({ team, data, error, act }: { team: Bot; data: Board; error: string | null; act: Act }) {
  const { goal, items, requests, teams } = data
  const queue = data.queue ?? []
  const asked = data.asked ?? []
  const groups = grouped(requests)
  const own = items.filter((i) => i.teamId === team.id || !asked.some((a) => a.id === i.id))
  return (
    <div className="flex flex-col gap-3">
      {error && <p className="text-destructive text-xs">{error}</p>}
      {goal ? (
        <p className="text-sm">{goal.text}</p>
      ) : (
        <p className="text-muted-foreground text-sm">No goal yet. The lead adds items when the team works on one.</p>
      )}
      {goal && own.length === 0 && <p className="text-muted-foreground text-sm">No items yet.</p>}
      {stateOrder.map((state) => {
        const group = own.filter((i) => i.state === state)
        if (group.length === 0) return null
        return (
          <section key={state} className="flex flex-col gap-1.5">
            <h3 className="text-muted-foreground text-xs font-medium uppercase">
              {stateLabel[state]} ({group.length})
            </h3>
            {group.map((item) => (
              <ItemCard key={item.id} team={team} item={item} teams={teams} />
            ))}
          </section>
        )
      })}
      {queue.length > 0 && (
        <QueueList team={team} board={{ ...data, queue }} held={onHold(goal?.state, queue.length)} act={act} />
      )}
      {requests.length > 0 && data.slots && (
        <section className="flex flex-col gap-2" aria-labelledby="requests-heading">
          <h3 id="requests-heading" className="text-muted-foreground text-xs font-medium uppercase">
            {slotHeader(data.slots, requests)}
          </h3>
          {groups.map((g) =>
            g.group === "closed" ? (
              <details key={g.group} className="text-xs">
                <summary className="text-muted-foreground cursor-pointer font-medium uppercase">
                  {g.label} ({g.items.length})
                </summary>
                <div className="mt-1.5 flex flex-col gap-1.5">
                  {g.items.map((r) => (
                    <RequestCard key={r.id} team={team} r={r} teams={teams} act={act} />
                  ))}
                </div>
              </details>
            ) : (
              <div key={g.group} className="flex flex-col gap-1.5">
                <h4 className="text-xs font-medium">
                  {g.label} ({g.items.length})
                </h4>
                {g.items.map((r) => (
                  <RequestCard key={r.id} team={team} r={r} teams={teams} act={act} />
                ))}
              </div>
            ),
          )}
        </section>
      )}
      {asked.length > 0 && (
        <section className="flex flex-col gap-1.5" aria-labelledby="asked-heading">
          <h3 id="asked-heading" className="text-muted-foreground text-xs font-medium uppercase">
            Asked of other teams ({asked.length})
          </h3>
          {asked.map((r) => (
            <AskedCard key={r.id} r={r} teams={teams} act={act} />
          ))}
        </section>
      )}
    </div>
  )
}
