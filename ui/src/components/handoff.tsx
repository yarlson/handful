import { useEffect, useState } from "react"
import { api, type Artifact, type Bot, type Goal } from "@/lib/api"
import {
  awaitsSender,
  canWithdrawHandoff,
  firstLine,
  handedOverState,
  handoffError,
  incomingState,
  teamOption,
} from "@/lib/handoff"
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

export type Act = (action: () => Promise<unknown>) => Promise<boolean>

function when(ms: number) {
  return new Date(ms).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })
}

// TextAction asks for a text, such as a reason or an answer, before it acts.
function TextAction({
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
  onOpen: () => void
  onClose: () => void
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
          onOpen()
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
            onClose()
          }}
        >
          Cancel
        </Button>
      </div>
    </div>
  )
}

// IncomingCard is a handoff that waits for this team: the user can accept it, decline it
// with a reason, or answer it once the leads handed it to the user.
function IncomingCard({ g, name, act }: { g: Goal; name: (id: string) => string; act: Act }) {
  const [typing, setTyping] = useState(false)
  const from = g.fromTeamId ? name(g.fromTeamId) : "you"
  return (
    <li className="flex flex-col gap-1 rounded-md border border-sky-500/40 p-2 text-sm">
      <div className="flex items-start gap-2">
        <span className="flex-1 font-medium">{g.text}</span>
        <Badge variant={g.needsUser ? "destructive" : "outline"}>{incomingState(g)}</Badge>
      </div>
      <p className="text-muted-foreground text-xs">
        From {from} · {when(g.createdAt)}
      </p>
      {firstLine(g.brief) && <p className="text-xs">{firstLine(g.brief)}</p>}
      {g.intakeNote && <p className="text-xs">Question: {g.intakeNote}</p>}
      {(g.artifactIds?.length ?? 0) > 0 && (
        <p className="text-muted-foreground text-xs">
          {g.artifactIds?.length === 1 ? "1 artifact" : `${g.artifactIds?.length} artifacts`} attached; see the
          Artifacts tab of any bot.
        </p>
      )}
      <div className="flex flex-wrap gap-1.5">
        {!typing && (
          <Button size="xs" aria-label={`Accept: ${g.text}`} onClick={() => void act(() => api.acceptHandoff(g.id))}>
            Accept
          </Button>
        )}
        {g.needsUser && (
          <TextAction
            id={`answer-${g.id}`}
            label={`Your answer to: ${g.intakeNote ?? g.text}`}
            open={`Answer: ${g.text}`}
            submit="Send answer"
            onOpen={() => setTyping(true)}
            onClose={() => setTyping(false)}
            onSubmit={(text) => act(() => api.answerHandoff(g.id, text))}
          />
        )}
        <TextAction
          id={`decline-${g.id}`}
          label="Why decline it? The sender sees this."
          open={`Decline: ${g.text}`}
          submit="Decline"
          onOpen={() => setTyping(true)}
          onClose={() => setTyping(false)}
          onSubmit={(text) => act(() => api.declineHandoff(g.id, text))}
        />
      </div>
    </li>
  )
}

// IncomingSection lists the handoffs that wait for this team, oldest first.
export function IncomingSection({ incoming, name, act }: { incoming: Goal[]; name: (id: string) => string; act: Act }) {
  if (incoming.length === 0) return null
  return (
    <section aria-labelledby="incoming-heading" className="flex flex-col gap-1.5">
      <h3 id="incoming-heading" className="text-muted-foreground text-xs font-medium uppercase">
        Incoming from other teams ({incoming.length})
      </h3>
      <ul className="flex flex-col gap-1.5">
        {incoming.map((g) => (
          <IncomingCard key={g.id} g={g} name={name} act={act} />
        ))}
      </ul>
    </section>
  )
}

// HandedOverCard is a handoff this team gave another team, as the sender follows it.
function HandedOverCard({ g, name, act }: { g: Goal; name: (id: string) => string; act: Act }) {
  const [confirm, setConfirm] = useState(false)
  const to = name(g.teamId)
  const declined = g.state === "declined"
  return (
    <li className="flex flex-col gap-1 rounded-md border p-2 text-sm">
      <div className="flex items-start gap-2">
        <span className="flex-1 font-medium">{g.text}</span>
        <Badge variant={declined ? "destructive" : "outline"}>{handedOverState(g, to)}</Badge>
      </div>
      <p className="text-muted-foreground text-xs">
        To {to} · {when(g.createdAt)}
      </p>
      {g.intakeNote && (
        <p className={declined ? "text-destructive text-xs" : "text-xs"}>
          {declined ? "Reason" : "Question"}: {g.intakeNote}
        </p>
      )}
      <div className="flex flex-wrap gap-1.5">
        {awaitsSender(g) && (
          <TextAction
            id={`answer-${g.id}`}
            label={`Your answer to ${to}: ${g.intakeNote ?? ""}`}
            open={`Answer: ${g.text}`}
            submit="Send answer"
            onOpen={() => {}}
            onClose={() => {}}
            onSubmit={(text) => act(() => api.answerHandoff(g.id, text))}
          />
        )}
        {canWithdrawHandoff(g) &&
          (confirm ? (
            <span className="flex items-center gap-2 text-xs">
              Withdraw it? {to} won't work on it.
              <Button
                size="xs"
                variant="destructive"
                onClick={() => void act(() => api.withdrawHandoff(g.id)).then(() => setConfirm(false))}
              >
                Withdraw
              </Button>
              <Button size="xs" variant="ghost" onClick={() => setConfirm(false)}>
                Keep it
              </Button>
            </span>
          ) : (
            <Button size="xs" variant="ghost" aria-label={`Withdraw: ${g.text}`} onClick={() => setConfirm(true)}>
              Withdraw…
            </Button>
          ))}
      </div>
    </li>
  )
}

// HandedOverSection lists this team's handoffs of the last 30 days, newest first.
export function HandedOverSection({ goals, name, act }: { goals: Goal[]; name: (id: string) => string; act: Act }) {
  if (goals.length === 0) return null
  return (
    <section aria-labelledby="handed-over-heading" className="flex flex-col gap-1.5">
      <h3 id="handed-over-heading" className="text-muted-foreground text-xs font-medium uppercase">
        Handed over ({goals.length})
      </h3>
      <ul className="flex flex-col gap-1.5">
        {goals.map((g) => (
          <HandedOverCard key={g.id} g={g} name={name} act={act} />
        ))}
      </ul>
    </section>
  )
}

// ArtifactPicker lets the user attach artifacts to a hand-over. A long title wraps to two
// lines inside the panel, and its full text is in the tooltip.
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

// HandOverDialog gives another team a piece of work as a goal of its own, from a team or
// from no team.
export function HandOverDialog({
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
    const bad = handoffError(to, text)
    if (bad) {
      setError(bad)
      return
    }
    try {
      await api.handOver(to, {
        ...(from && { fromTeamId: from.id }),
        text: text.trim(),
        ...(brief.trim() && { brief: brief.trim() }),
        ...(picked.length > 0 && { artifactIds: picked }),
      })
      setTo("")
      setText("")
      setBrief("")
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
          a team name or the goal would push the panel wider than the window. */}
      <DialogContent className="grid-cols-[minmax(0,1fr)]">
        <DialogHeader>
          <DialogTitle>Hand over to another team</DialogTitle>
          <DialogDescription className="[overflow-wrap:anywhere]">
            The team takes it in as a goal of its own and works on it from its own budget.
            {from ? ` Follow it under Handed over on ${from.name}'s board.` : ""}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="handoff-to">Team</Label>
          <select
            id="handoff-to"
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
          <Label htmlFor="handoff-text">What should the team do?</Label>
          <Textarea
            id="handoff-text"
            rows={3}
            className="max-h-48 min-w-0 [overflow-wrap:anywhere]"
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="handoff-brief">Brief (optional)</Label>
          <Textarea
            id="handoff-brief"
            rows={3}
            className="max-h-48 min-w-0 [overflow-wrap:anywhere]"
            placeholder="Context the team needs: links, constraints, what done looks like."
            value={brief}
            onChange={(e) => setBrief(e.target.value)}
          />
        </div>
        <ArtifactPicker artifacts={artifacts} picked={picked} onPick={setPicked} />
        {error && <output className="text-destructive text-sm [overflow-wrap:anywhere]">{error}</output>}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => void send()}>Hand over</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
