import { useEffect, useState, type ReactNode } from "react"
import { CornerDownLeft, X } from "lucide-react"
import { api, type Bot } from "@/lib/api"
import {
  byChat,
  stateLabel,
  stripCollapsed,
  UNDO_MS,
  waitingCount,
  type Asked,
  type OpenQuestion,
  type Waiting,
} from "@/lib/questions"
import { Button } from "@/components/ui/button"
import { useRights } from "@/lib/session"

function time(ms: number) {
  return new Date(ms).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })
}

// goTo scrolls to a question's message, highlights it and moves the keyboard there.
export function goTo(entryId: string) {
  const el = document.getElementById(`entry-${entryId}`)
  if (!el) return
  el.scrollIntoView({ block: "center" })
  el.focus({ preventScroll: true })
  el.classList.add("ring-2", "ring-amber-500/60")
  setTimeout(() => el.classList.remove("ring-2", "ring-amber-500/60"), 1500)
}

// useDismiss dismisses questions and offers Undo for 10 seconds after each.
export function useDismiss(onChange: () => void) {
  const [undo, setUndo] = useState<{ entryId: string; question: string } | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    if (!undo) return
    const t = setTimeout(() => setUndo(null), UNDO_MS)
    return () => clearTimeout(t)
  }, [undo])
  const dismiss = async (q: { entryId: string; question: string }) => {
    try {
      await api.dismissQuestion(q.entryId)
      setError(null)
      setUndo(q)
      onChange()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const reopen = async () => {
    if (!undo) return
    try {
      await api.reopenQuestion(undo.entryId)
      setError(null)
      setUndo(null)
      onChange()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return { dismiss, reopen, undo, error }
}

// UndoNote says a question was dismissed and offers to take that back.
export function UndoNote({ undo, onUndo, error }: { undo: Asked | null; onUndo: () => void; error: string | null }) {
  return (
    <output className="block text-xs">
      {undo && (
        <span className="flex items-center gap-2">
          <span className="text-muted-foreground min-w-0 truncate">Dismissed: {undo.question}</span>
          <Button size="xs" variant="outline" onClick={onUndo}>
            Undo
          </Button>
        </span>
      )}
      {error && <span className="text-destructive">{error}</span>}
    </output>
  )
}

// QuestionStrip lists a chat's open questions above its composer, with Go to, Answer and
// Dismiss on each. It's hidden when nothing is open. onGoTo shows a question's message; the
// chat passes one that loads the message first when it isn't loaded.
export function QuestionStrip({
  questions,
  room,
  onAnswer,
  onChange,
  onGoTo = goTo,
}: {
  questions: OpenQuestion[]
  room: boolean
  onAnswer: (q: Asked) => void
  onChange: () => void
  onGoTo?: (entryId: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const { dismiss, reopen, undo, error } = useDismiss(onChange)
  // A phone paired for View only reads questions.
  const { send } = useRights()
  if (questions.length === 0) return <UndoNote undo={undo} onUndo={() => void reopen()} error={error} />
  const collapsed = stripCollapsed(questions.length) && !expanded
  const shown = collapsed ? questions.slice(0, 1) : questions
  return (
    <section aria-labelledby="open-questions-heading" className="mx-3 mt-2 rounded-lg border border-amber-500/40 p-2">
      <div className="flex items-center gap-2">
        <h3 id="open-questions-heading" className="flex-1 text-xs font-medium">
          Open questions ({questions.length})
        </h3>
        {stripCollapsed(questions.length) && (
          <Button size="xs" variant="ghost" aria-expanded={expanded} onClick={() => setExpanded((v) => !v)}>
            {expanded ? "Show less" : `Show all ${questions.length}`}
          </Button>
        )}
      </div>
      <ul className="mt-1 flex flex-col gap-1">
        {shown.map((q) => (
          <li key={q.entryId} className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
            <span className="min-w-0 flex-1 truncate" title={q.question}>
              {q.question}
            </span>
            <span className="text-muted-foreground shrink-0">
              {room ? `${q.askerName} · ` : ""}
              {time(q.createdAt)}
            </span>
            <span className="flex shrink-0 gap-1">
              <Button size="xs" variant="ghost" aria-label={`Go to: ${q.question}`} onClick={() => onGoTo(q.entryId)}>
                Go to
              </Button>
              {send && (
                <>
                  <Button
                    size="xs"
                    variant="outline"
                    aria-label={`Answer: ${q.question}`}
                    onClick={() => onAnswer({ entryId: q.entryId, question: q.question })}
                  >
                    Answer
                  </Button>
                  <Button
                    size="xs"
                    variant="ghost"
                    aria-label={`Dismiss: ${q.question}`}
                    onClick={() => void dismiss({ entryId: q.entryId, question: q.question })}
                  >
                    Dismiss
                  </Button>
                </>
              )}
            </span>
          </li>
        ))}
      </ul>
      <UndoNote undo={undo} onUndo={() => void reopen()} error={error} />
    </section>
  )
}

// AnswerChip shows the question that the next message answers, with a labelled ✕.
export function AnswerChip({ answering, onClear }: { answering: Asked; onClear: () => void }) {
  return (
    <div className="bg-muted mb-1.5 flex items-center gap-1.5 rounded-md px-2 py-1 text-xs">
      <CornerDownLeft className="size-3 shrink-0" aria-hidden />
      <span className="min-w-0 flex-1 truncate" title={answering.question}>
        Answering: {answering.question}
      </span>
      <button
        type="button"
        className="hover:text-foreground text-muted-foreground shrink-0"
        aria-label={`Stop answering: ${answering.question}`}
        onClick={onClear}
      >
        <X className="size-3" />
      </button>
    </div>
  )
}

// WaitingView lists everything that waits for the user across chats: open questions grouped
// by chat, oldest first, and the chats with approvals or paused tasks.
export function WaitingView({
  waiting,
  bots,
  onOpen,
  onAnswer,
  onChange,
  menu,
}: {
  waiting: Waiting
  bots: Bot[]
  onOpen: (chatId: string) => void
  onAnswer: (chatId: string, q: Asked) => void
  onChange: () => void
  // menu is the phone layout's button that opens the sidebar; null above the phone width.
  menu?: ReactNode
}) {
  const { dismiss, reopen, undo, error } = useDismiss(onChange)
  // A phone paired for View only reads questions.
  const { send } = useRights()
  const name = (id: string) => bots.find((b) => b.id === id)?.name ?? "A deleted chat"
  const approvals = bots.filter((b) => b.attention === "approval")
  const paused = bots.filter((b) => b.attention === "goal")
  const groups = byChat(waiting.questions)
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-5 overflow-y-auto px-4 py-6 max-md:px-3 max-md:pt-1.5">
      <div className="flex items-center gap-2">
        {menu}
        <h2 className="text-lg font-medium">Waiting for you</h2>
      </div>
      {waitingCount(waiting) === 0 && <p className="text-muted-foreground text-sm">Nothing is waiting for you.</p>}
      {groups.map((g) => (
        <section key={g.chatId} aria-labelledby={`waiting-${g.chatId}`} className="flex flex-col gap-2">
          <h3 id={`waiting-${g.chatId}`} className="flex items-center gap-2 text-sm font-medium">
            {name(g.chatId)}
            <Button size="xs" variant="ghost" onClick={() => onOpen(g.chatId)}>
              Open chat
            </Button>
          </h3>
          <ul className="flex flex-col gap-2">
            {g.questions.map((q) => (
              <li key={q.entryId} className="rounded-md border border-amber-500/40 p-2 text-sm">
                <p>{q.question}</p>
                <p className="text-muted-foreground text-xs">
                  {q.askerName} · {time(q.createdAt)}
                </p>
                {send && (
                  <div className="mt-1.5 flex gap-1">
                    <Button
                      size="xs"
                      aria-label={`Answer: ${q.question}`}
                      onClick={() => onAnswer(g.chatId, { entryId: q.entryId, question: q.question })}
                    >
                      Answer
                    </Button>
                    <Button
                      size="xs"
                      variant="ghost"
                      aria-label={`Dismiss: ${q.question}`}
                      onClick={() => void dismiss({ entryId: q.entryId, question: q.question })}
                    >
                      Dismiss
                    </Button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        </section>
      ))}
      {(approvals.length > 0 || paused.length > 0) && (
        <section aria-labelledby="waiting-other" className="flex flex-col gap-1.5">
          <h3 id="waiting-other" className="text-sm font-medium">
            Approvals and paused tasks
          </h3>
          {approvals.map((b) => (
            <Button key={b.id} size="sm" variant="ghost" className="justify-start" onClick={() => onOpen(b.id)}>
              {b.name}: needs your approval
            </Button>
          ))}
          {paused.map((b) => (
            <Button key={b.id} size="sm" variant="ghost" className="justify-start" onClick={() => onOpen(b.id)}>
              {b.name}: the task is paused
            </Button>
          ))}
        </section>
      )}
      <UndoNote undo={undo} onUndo={() => void reopen()} error={error} />
      <ClosedList waiting={waiting} name={name} onOpen={onOpen} />
    </div>
  )
}

// ClosedList counts the questions closed in the last 7 days and shows them on request,
// newest first, each with how it closed.
function ClosedList({
  waiting,
  name,
  onOpen,
}: {
  waiting: Waiting
  name: (id: string) => string
  onOpen: (chatId: string) => void
}) {
  const [shown, setShown] = useState(false)
  const closed = waiting.closed ?? []
  return (
    <section className="flex flex-col gap-1.5 text-xs">
      <p className="text-muted-foreground flex items-center gap-2">
        Answered or dismissed in the last 7 days: {waiting.closedRecently}
        {closed.length > 0 && (
          <Button
            size="xs"
            variant="ghost"
            aria-expanded={shown}
            aria-controls="waiting-closed"
            onClick={() => setShown((v) => !v)}
          >
            {shown ? "Hide" : "Show"}
          </Button>
        )}
      </p>
      {shown && (
        <ul id="waiting-closed" aria-label="Closed in the last 7 days" className="flex flex-col gap-1">
          {closed.map((q) => (
            <li key={q.entryId} className="flex flex-wrap items-baseline gap-x-2 rounded-md border p-2">
              <span className="min-w-0 flex-1">{q.question}</span>
              <span className="text-muted-foreground">
                {stateLabel(q.state)} · {name(q.chatId)} · {time(q.closedAt ?? q.createdAt)}
              </span>
              <Button
                size="xs"
                variant="ghost"
                aria-label={`Open chat: ${q.question}`}
                onClick={() => onOpen(q.chatId)}
              >
                Open chat
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
