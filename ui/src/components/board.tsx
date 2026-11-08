import { useState } from "react"
import { api, type Bot, type Goal, type Item } from "@/lib/api"
import { useLoad } from "@/components/bot-panel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

const openStates: Goal["state"][] = ["active", "delivered", "paused"]

const goalBadge: Record<Goal["state"], string> = {
  active: "Goal",
  delivered: "Delivered",
  paused: "Paused",
  done: "Done",
  ended: "Ended",
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

// GoalBar shows a team's goal above its channel, with the controls of its budget.
export function GoalBar({ team, version }: { team: Bot; version: number }) {
  const board = useLoad(() => api.board(team.id), `${team.id}:${version}`)
  const [text, setText] = useState("")
  const [asking, setAsking] = useState(false)
  const [ending, setEnding] = useState(false)
  const goal = board.data?.goal
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
  const open = goal && openStates.includes(goal.state)
  return (
    <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2 text-sm">
      {open ? (
        <>
          <Badge variant={goal.state === "paused" ? "destructive" : "secondary"}>{goalBadge[goal.state]}</Badge>
          <span className="min-w-0 flex-1 truncate" title={goal.text}>
            {goal.text}
          </span>
          <span className="text-muted-foreground text-xs" title="Turns that the goal used of its budget">
            {goal.used}/{goal.budget} turns
          </span>
          {goal.state === "delivered" && (
            <Button size="xs" onClick={() => void act(() => api.acceptGoal(goal.id))}>
              Accept
            </Button>
          )}
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
              <Button
                size="xs"
                variant="destructive"
                onClick={() => void act(() => api.endGoal(goal.id)).then(() => setEnding(false))}
              >
                End goal
              </Button>
              <Button size="xs" variant="ghost" onClick={() => setEnding(false)}>
                Keep it
              </Button>
            </>
          ) : (
            <Button size="xs" variant="ghost" onClick={() => setEnding(true)}>
              End goal
            </Button>
          )}
        </>
      ) : asking ? (
        <form
          className="flex flex-1 items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            void act(async () => {
              await api.startGoal(team.id, text)
              setText("")
              setAsking(false)
            })
          }}
        >
          <Input
            aria-label="Goal"
            placeholder="What should the team do?"
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
          <Button size="xs" type="submit" disabled={!text.trim()}>
            Start
          </Button>
          <Button size="xs" variant="ghost" type="button" onClick={() => setAsking(false)}>
            Cancel
          </Button>
        </form>
      ) : (
        <>
          <span className="text-muted-foreground min-w-0 flex-1 truncate">
            {goal
              ? `Last goal (${goal.state}): ${goal.text}`
              : "No goal yet. Chat with the lead, or give the team a goal."}
          </span>
          <Button size="xs" variant="outline" onClick={() => setAsking(true)}>
            New goal
          </Button>
        </>
      )}
      {board.error && <span className="text-destructive w-full text-xs">{board.error}</span>}
    </div>
  )
}

function ItemCard({ team, item, teams }: { team: Bot; item: Item; teams: Record<string, string> }) {
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

// BoardTab lists the items of a team's goal, grouped by state.
export function BoardTab({ team, version }: { team: Bot; version: number }) {
  const board = useLoad(() => api.board(team.id), `${team.id}:${version}`)
  if (board.error) return <p className="text-destructive text-xs">{board.error}</p>
  if (!board.data) return null
  const { goal, items, requests, teams } = board.data
  return (
    <div className="flex flex-col gap-3">
      {requests.length > 0 && (
        <section className="flex flex-col gap-1.5">
          <h3 className="text-muted-foreground text-xs font-medium uppercase">
            Requests from other teams ({requests.length})
          </h3>
          {requests.map((r) => (
            <div key={r.id} className="flex flex-col gap-1">
              <span className="text-muted-foreground text-xs">
                From {teams[r.fromTeamId] ?? "another team"} · {stateLabel[r.state]}
              </span>
              <ItemCard team={team} item={r} teams={teams} />
            </div>
          ))}
        </section>
      )}
      {goal ? (
        <p className="text-sm">{goal.text}</p>
      ) : (
        <p className="text-muted-foreground text-sm">No goal yet. The lead adds items when the team works on one.</p>
      )}
      {goal && items.length === 0 && <p className="text-muted-foreground text-sm">No items yet.</p>}
      {stateOrder.map((state) => {
        const group = items.filter((i) => i.state === state)
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
    </div>
  )
}
