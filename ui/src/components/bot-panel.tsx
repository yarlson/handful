import { useCallback, useEffect, useRef, useState } from "react"
import { RadioGroup as RadioGroupPrimitive } from "radix-ui"
import { Play, Trash2, X } from "lucide-react"
import { api, type Bot, type Memory, type Routine } from "@/lib/api"
import { ARTIFACT_KINDS, artifactAuthor } from "@/lib/artifacts"
import { latestOnly } from "@/lib/latest"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Confirm } from "@/components/confirm"
import { useUnsavedEdits } from "@/components/leave"
import { OptionSelect } from "@/components/option-select"
import { RadioGroup } from "@/components/ui/radio-group"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { draftOf, rebase, savePlan, unsaved, type BotDraft, type TeamDraft } from "@/lib/bot-settings"
import { failed, modelError, saved, type SaveResult } from "@/lib/save"
import { panelTabs, shownTab, type PanelTab, type Reading } from "@/lib/place"
import { contextLine, usageKey, type Usage } from "@/lib/usage"
import { cn } from "@/lib/utils"
import { botColor, hues } from "@/lib/bot-color"
import { ChatIcon } from "@/components/bot-colors"
import { BotDot } from "@/components/bot-dot"
import { useRights } from "@/lib/session"
import { BoardTab } from "@/components/board"
import { FALLBACK_ALIASES, ModelChip, ModelEffortFields, useDefaults } from "@/components/model"
import { ignoredOverrides, inheritedFromGlobal, overrides, sourceLine } from "@/lib/model"
import { forgetHistory, withStorage } from "@/lib/history"
import { dateTime } from "@/lib/dates"
import { useNow } from "@/components/now"

// useWhen formats a time as a day and a time; its clock ticks each minute, so Today becomes
// Yesterday after midnight.
function useWhen() {
  const now = useNow(60_000)
  return (ms: number | null) => (ms ? dateTime(ms, now) : "—")
}

// useLoad fetches data and fetches it again whenever key changes. Only the
// newest request's result applies, and a success clears an earlier error.
export function useLoad<T>(load: () => Promise<T>, key: string) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  // rev counts the loads that succeeded, so views of the data can reload what they derive from it.
  const [rev, setRev] = useState(0)
  const loadRef = useRef(load)
  loadRef.current = load
  const [run] = useState(latestOnly)
  const reload = useCallback(() => {
    run(
      loadRef.current(),
      (d) => {
        setData(d)
        setError(null)
        setRev((r) => r + 1)
      },
      (e) => setError(e.message),
    )
  }, [run])
  useEffect(reload, [reload, key])
  return { data, error, reload, setError, rev }
}

// AddFactForm saves a fact as the user's own; bots trust standing rules only from such facts.
function AddFactForm({ bot, onAdded, onError }: { bot: Bot; onAdded: () => void; onError: (message: string) => void }) {
  const [fact, setFact] = useState("")
  const [shared, setShared] = useState(false)
  const [always, setAlways] = useState(true)
  const own = bot.team ? "team" : "agent"
  const add = async () => {
    try {
      await api.addMemory(bot.id, {
        scope: shared ? "user" : own,
        tier: always ? "profile" : "log",
        fact,
      })
      setFact("")
      onAdded()
    } catch (e) {
      onError((e as Error).message)
    }
  }
  return (
    <div className="flex flex-col gap-2 rounded-md border p-2">
      <Textarea
        placeholder={`A rule or fact for ${bot.name}, from you`}
        value={fact}
        onChange={(e) => setFact(e.target.value)}
        rows={2}
      />
      <div className="flex flex-wrap items-center gap-3 text-xs">
        <label className="flex items-center gap-1">
          <Checkbox checked={always} onCheckedChange={(v) => setAlways(v === true)} />
          Always in mind
        </label>
        <label className="flex items-center gap-1">
          <Checkbox checked={shared} onCheckedChange={(v) => setShared(v === true)} />
          Shared with all bots
        </label>
        <Button size="xs" className="ml-auto" disabled={!fact.trim()} onClick={() => void add()}>
          Add
        </Button>
      </div>
    </div>
  )
}

const scopeLabel: Record<Memory["scope"], string> = {
  agent: "this bot",
  team: "this team",
  user: "shared",
}

// revealed shows a row's control on hover, on keyboard focus, on touch screens and while
// its confirm is open.
const revealed =
  "opacity-0 group-hover:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100 data-[state=open]:opacity-100"

// MemoryFact is one remembered fact: its text, tier, scope, author and date, and Forget.
export function MemoryFact({
  fact: m,
  when,
  onForget,
}: {
  fact: Memory
  when: (ms: number | null) => string
  onForget: () => void
}) {
  return (
    <div className="group flex items-start gap-2 rounded-md border p-2 text-sm">
      <div className="flex-1">
        <p>{m.fact}</p>
        <div className="mt-1 flex gap-1">
          <Badge variant={m.tier === "profile" ? "default" : "secondary"}>{m.tier}</Badge>
          <Badge variant="outline">{scopeLabel[m.scope]}</Badge>
          {m.learnedBy === "user" && <Badge variant="outline">from you</Badge>}
          <span className="text-muted-foreground text-xs">{when(m.createdAt)}</span>
        </div>
      </div>
      <Confirm question="Forget this fact?" action="Forget" onConfirm={onForget}>
        <Button size="icon" variant="ghost" aria-label="Forget" className={revealed}>
          <Trash2 />
        </Button>
      </Confirm>
    </div>
  )
}

function MemoryTab({ bot, version }: { bot: Bot; version: number }) {
  const when = useWhen()
  const { data, error, reload, setError } = useLoad(() => api.memory(bot.id), `${bot.id}:${version}`)
  const remove = async (id: string) => {
    try {
      await api.deleteMemory(id)
      reload()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <div className="flex flex-col gap-2">
      <AddFactForm bot={bot} onAdded={reload} onError={setError} />
      {error && <p className="text-destructive text-xs">{error}</p>}
      {data?.length === 0 && (
        <p className="text-muted-foreground text-sm">
          Nothing remembered yet.{" "}
          {bot.team ? "The members save team facts as they work." : `${bot.name} saves facts as you talk.`}
        </p>
      )}
      {data?.map((m: Memory) => (
        <MemoryFact key={m.id} fact={m} when={when} onForget={() => void remove(m.id)} />
      ))}
    </div>
  )
}

// RoutineItem is one routine: its name, prompt, schedule and next run, with its actions.
export function RoutineItem({
  routine: r,
  when,
  onEnabled,
  onRun,
  onDelete,
}: {
  routine: Routine
  when: (ms: number | null) => string
  onEnabled: (enabled: boolean) => void
  onRun: () => void
  onDelete: () => void
}) {
  return (
    <div className="rounded-md border p-2 text-sm">
      <div className="flex items-center gap-2">
        <span className="flex-1 font-medium">{r.name}</span>
        <Switch aria-label="Enabled" checked={r.enabled} onCheckedChange={onEnabled} />
      </div>
      <p className="text-muted-foreground mt-1 text-xs">{r.prompt}</p>
      <div className="mt-2 flex items-center gap-2 text-xs">
        <code className="bg-sidebar-foreground/5 shrink-0 rounded-sm px-1 whitespace-nowrap">{r.schedule}</code>
        <span className="text-muted-foreground min-w-0">next {r.enabled ? when(r.nextRunAt) : "paused"}</span>
      </div>
      <div className="mt-1 flex items-center justify-end gap-1">
        <Button size="xs" variant="ghost" onClick={onRun}>
          <Play /> Run now
        </Button>
        <Confirm question="Delete this routine?" action="Delete" onConfirm={onDelete}>
          <Button size="xs" variant="ghost" aria-label="Delete routine">
            <Trash2 />
          </Button>
        </Confirm>
      </div>
    </div>
  )
}

function RoutinesTab({ bot, version }: { bot: Bot; version: number }) {
  const when = useWhen()
  const { data, error, reload, setError } = useLoad(() => api.routines(bot.id), `${bot.id}:${version}`)
  const act = async (f: () => Promise<unknown>) => {
    try {
      await f()
      reload()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <div className="flex flex-col gap-2">
      {error && <p className="text-destructive text-xs">{error}</p>}
      {data?.length === 0 && (
        <p className="text-muted-foreground text-sm">
          No routines. Ask {bot.name} for anything recurring, like "every weekday at 8:10, brief me".
        </p>
      )}
      {data?.map((r: Routine) => (
        <RoutineItem
          key={r.id}
          routine={r}
          when={when}
          onEnabled={(v) => void act(() => api.setRoutineEnabled(r.id, v))}
          onRun={() => void act(() => api.runRoutine(r.id))}
          onDelete={() => void act(() => api.deleteRoutine(r.id))}
        />
      ))}
    </div>
  )
}

function SkillsTab({ version, onRead }: { version: number; onRead: (r: Reading) => void }) {
  const { data, error } = useLoad(() => api.skills(), String(version))
  return (
    <div className="flex flex-col gap-2">
      {error && <p className="text-destructive text-xs">{error}</p>}
      {data?.length === 0 && (
        <p className="text-muted-foreground text-sm">
          No skills yet. Bots save a skill when you finish a process you will repeat.
        </p>
      )}
      {data?.map((s) => (
        <button
          key={s.id}
          type="button"
          className="hover:bg-muted transition-colors duration-(--duration-fast) rounded-md border p-2 text-left text-sm"
          onClick={() => onRead({ kind: "skill", id: s.id })}
        >
          <span className="font-medium">{s.name}</span>
          <p className="text-muted-foreground text-xs">{s.description}</p>
        </button>
      ))}
    </div>
  )
}

// ArtifactsTab lists the documents that all bots share. Bots write them; the user reads, sends
// and deletes them in the reader.
function ArtifactsTab({ agents, version, onRead }: { agents: Bot[]; version: number; onRead: (r: Reading) => void }) {
  const when = useWhen()
  const [kind, setKind] = useState("")
  const [query, setQuery] = useState("")
  const { data, error } = useLoad(() => api.artifacts({ kind, query }), `${version}:${kind}:${query}`)
  return (
    <div className="flex flex-col gap-2">
      <div className="flex gap-2">
        <OptionSelect
          label="Kind"
          className="w-auto shrink-0"
          value={kind}
          options={[{ value: "", label: "All kinds" }, ...ARTIFACT_KINDS.map((k) => ({ value: k, label: k }))]}
          onChange={setKind}
        />
        <Input
          aria-label="Search artifacts"
          placeholder="Search"
          className="h-8"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>
      {error && <p className="text-destructive text-xs">{error}</p>}
      {data?.length === 0 && (
        <p className="text-muted-foreground text-sm">
          {kind || query.trim()
            ? "No artifacts match."
            : "No artifacts yet. Bots save plans, reviews and reports here for each other."}
        </p>
      )}
      {data?.map((a) => (
        <button
          key={a.id}
          type="button"
          className="hover:bg-muted transition-colors duration-(--duration-fast) rounded-md border p-2 text-left text-sm"
          onClick={() => onRead({ kind: "artifact", id: a.id })}
        >
          <span className="font-medium">{a.title}</span>
          <div className="mt-1 flex items-center gap-1 text-xs">
            <Badge variant="secondary">{a.kind}</Badge>
            <span className="text-muted-foreground">
              {artifactAuthor(a.authorId, agents)} · {when(a.updatedAt)}
            </span>
          </div>
        </button>
      ))}
    </div>
  )
}

// SaveNote shows a form's last save: plain when it worked, as an error when it failed.
export function SaveNote({ result }: { result: SaveResult }) {
  if (!result) return null
  return (
    <output
      data-save={result.ok ? "saved" : "failed"}
      className={cn("text-xs", result.ok ? "text-muted-foreground" : "text-destructive")}
    >
      {result.text}
    </output>
  )
}

function MembersTab({ room, agents }: { room: Bot; agents: Bot[] }) {
  const [members, setMembers] = useState<Set<string>>(new Set((room.members ?? []).map((m) => m.id)))
  const [result, setResult] = useState<SaveResult>(null)
  useEffect(() => setMembers(new Set((room.members ?? []).map((m) => m.id))), [room.id, room.members])
  const save = async () => {
    try {
      await api.setRoomMembers(room.id, [...members])
      setResult(saved())
    } catch (e) {
      setResult(failed((e as Error).message))
    }
  }
  const defaults = useDefaults()
  const aliases = defaults?.modelAliases ?? FALLBACK_ALIASES
  const seated = (room.members ?? []).filter((m) => m.effective)
  return (
    <div className="flex flex-col gap-2 text-sm">
      {seated.length > 0 && (
        <ul
          className="flex flex-col gap-2 border-b pb-3"
          aria-label={room.team ? "Members' models in this team" : "Members' models"}
        >
          {seated.map((m) => (
            <li key={m.id} className="flex min-w-0 flex-col gap-0.5">
              <div className="flex min-w-0 items-center gap-2">
                <ChatIcon chat={{ id: m.id, kind: "agent" }} />
                <span className="flex-1 truncate font-medium">{m.name}</span>
                {m.effective && <ModelChip effective={m.effective} aliases={aliases} who={m.name} />}
              </div>
              {m.effective && <span className="text-muted-foreground text-xs">{sourceLine(m.effective)}</span>}
            </li>
          ))}
        </ul>
      )}
      <p className="text-muted-foreground text-xs">
        {room.team
          ? "A post from you wakes the members it @mentions, or the lead when it mentions none. @here wakes every member."
          : "Members take turns in the room. A post wakes the members it @mentions, or all members when it mentions none."}
      </p>
      {agents.map((a) => (
        <label key={a.id} className="flex items-center gap-2">
          <Checkbox
            checked={members.has(a.id)}
            onCheckedChange={(v) =>
              setMembers((prev) => {
                const next = new Set(prev)
                if (v === true) next.add(a.id)
                else next.delete(a.id)
                return next
              })
            }
          />
          <ChatIcon chat={a} />
          {a.name}
        </label>
      ))}
      <div className="flex items-center gap-2">
        <Button size="sm" disabled={members.size === 0} onClick={() => void save()}>
          Save members
        </Button>
        <SaveNote result={result} />
      </div>
    </div>
  )
}

const deleteNote = {
  bot: "This deletes its conversation, its own memory, its routines and its working directory. Shared memory and skills stay.",
  room: "This deletes the room and its conversation. The member bots stay.",
  team: "This deletes the team, its conversation, its team memory and its shared folder. The member bots stay.",
}

// TeamFields edits what a team adds to its room: the lead, the working agreement, the turns
// per day, the decision time, the inbox limits and the member defaults.
function TeamFields({
  team,
  draft,
  onDraft,
}: {
  team: Bot
  draft: TeamDraft
  onDraft: (edit: Partial<TeamDraft>) => void
}) {
  const defaults = useDefaults()
  const members = team.members ?? []
  return (
    <div className="flex flex-col gap-3 border-t pt-3">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="team-settings-lead">Lead</Label>
        <OptionSelect
          id="team-settings-lead"
          value={draft.leadId}
          options={members.map((m) => {
            const label = (
              <>
                <ChatIcon chat={{ id: m.id, kind: "agent" }} />
                {m.name}
              </>
            )
            return { value: m.id, label }
          })}
          onChange={(leadId) => onDraft({ leadId })}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="team-settings-agreement">Working agreement</Label>
        <Textarea
          id="team-settings-agreement"
          rows={5}
          placeholder="How the team works and when work is done."
          value={draft.agreement}
          onChange={(e) => onDraft({ agreement: e.target.value })}
        />
        <p className="text-muted-foreground text-xs">
          In the team, it overrides each member's instructions on how work is done and checked.
        </p>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="team-settings-budget">Turns per day</Label>
        <Input
          id="team-settings-budget"
          type="number"
          min={0}
          max={10000}
          value={draft.budget}
          aria-describedby="team-settings-budget-help"
          onChange={(e) => onDraft({ budget: e.target.value })}
        />
        <p id="team-settings-budget-help" className="text-muted-foreground text-xs">
          Every turn of the team's members counts, chat included; the lead's decisions don't. At the limit the team
          pauses until midnight. 0 is no limit: the Work tab then shows the day's count and, after a week, a suggested
          limit.
        </p>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="team-settings-decision">Decision time, in minutes</Label>
        <Input
          id="team-settings-decision"
          type="number"
          min={5}
          max={1440}
          value={draft.decision}
          aria-describedby="team-settings-decision-help"
          onChange={(e) => onDraft({ decision: e.target.value })}
        />
        <p id="team-settings-decision-help" className="text-muted-foreground text-xs">
          How long a task may wait in the inbox for the lead's decision, counting only time when the lead can act. After
          that, it needs you.
        </p>
      </div>
      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1 text-sm font-medium">Inbox limits</legend>
        <div className="flex items-center gap-2 text-sm">
          <Label htmlFor="team-settings-inbox-max" className="font-normal">
            Holds at most
          </Label>
          <Input
            id="team-settings-inbox-max"
            type="number"
            min={1}
            max={50}
            className="w-16"
            value={draft.inboxMax}
            onChange={(e) => onDraft({ inboxMax: e.target.value })}
          />
          <Label htmlFor="team-settings-inbox-sender" className="font-normal">
            From one team, at most
          </Label>
          <Input
            id="team-settings-inbox-sender"
            type="number"
            min={1}
            max={50}
            className="w-16"
            value={draft.perSender}
            onChange={(e) => onDraft({ perSender: e.target.value })}
          />
        </div>
        <p className="text-muted-foreground text-xs">
          Other teams can't send more while the inbox is full. Your own tasks always land.
        </p>
      </fieldset>
      <fieldset className="flex flex-col gap-3">
        <legend className="mb-1 text-sm font-medium">Member defaults</legend>
        <ModelEffortFields
          id="team-default"
          modelLabel="Default model for members"
          effortLabel="Default effort for members"
          model={draft.defaultModel}
          effort={draft.defaultEffort}
          onModel={(defaultModel) => onDraft({ defaultModel })}
          onEffort={(defaultEffort) => onDraft({ defaultEffort })}
          inherited={
            defaults
              ? inheritedFromGlobal(defaults)
              : {
                  model: { value: "", source: "claude-code" },
                  effort: { value: "", source: "claude-code" },
                }
          }
          defaults={defaults}
        />
        <p className="text-muted-foreground text-xs">
          Members without their own model or effort use these in this team. {overrides(members)} of {members.length}{" "}
          members override the model.
        </p>
      </fieldset>
    </div>
  )
}

// ContextNote is the line about the bot's context and its last turn; nothing before a turn is
// recorded.
export function ContextNote({ usage }: { usage: Usage | null | undefined }) {
  const line = contextLine(usage)
  return line && <p className="text-muted-foreground text-xs">{line}</p>
}

// HueSwatches picks a bot's colour from the palette; the choice saves with the other settings.
function HueSwatches({ hue, onHue }: { hue: string; onHue: (hue: string) => void }) {
  return (
    <div className="flex flex-col gap-1.5">
      <span id="bot-hue-label" className="mb-1 text-sm font-medium">
        Colour
      </span>
      <RadioGroup aria-labelledby="bot-hue-label" className="flex gap-1" value={hue} onValueChange={onHue}>
        {hues.map((h) => {
          const name = h[0].toUpperCase() + h.slice(1)
          return (
            <RadioGroupPrimitive.Item
              key={h}
              value={h}
              title={name}
              aria-label={name}
              className="focus-visible:ring-ring/50 data-[state=checked]:border-foreground hover:bg-muted grid size-9 cursor-pointer place-items-center rounded-full border-2 border-transparent transition-colors outline-none focus-visible:ring-3"
            >
              <BotDot className={cn("size-5", botColor("", h).text)} />
            </RadioGroupPrimitive.Item>
          )
        })}
      </RadioGroup>
    </div>
  )
}

// AllowedTools lists the tools a bot may use without asking, each with Revoke; null is
// still loading.
export function AllowedTools({ grants, onRevoke }: { grants: string[] | null; onRevoke: (grant: string) => void }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label>Always allowed tools</Label>
      {grants?.length === 0 && (
        <p className="text-muted-foreground text-xs">None. Each command and file change asks first.</p>
      )}
      <div className="flex flex-wrap gap-1">
        {grants?.map((g) => (
          <Badge key={g} variant="secondary" className="gap-1">
            {g}
            <Button
              variant="ghost"
              size="icon-xs"
              className="-mr-1.5 size-4 rounded-full"
              aria-label={`Revoke ${g}`}
              onClick={() => onRevoke(g)}
            >
              <X />
            </Button>
          </Badge>
        ))}
      </div>
    </div>
  )
}

export function SettingsTab({ bot, version, onDeleted }: { bot: Bot; version: number; onDeleted: () => void }) {
  // base is the saved values: the bot's, or what this tab saved before the bot reloads. A
  // reload gives each field the user did not edit its new value.
  const fresh = draftOf(bot)
  const freshKey = JSON.stringify(fresh)
  const [loaded, setLoaded] = useState(freshKey)
  const [base, setBase] = useState(fresh)
  const [draft, setDraft] = useState(fresh)
  if (loaded !== freshKey) {
    setLoaded(freshKey)
    setBase(fresh)
    setDraft(rebase(draft, base, fresh))
  }
  const dirty = unsaved(draft, base)
  useUnsavedEdits(dirty)
  const [result, setResult] = useState<SaveResult>(null)
  const edit = (e: Partial<BotDraft>) => {
    setDraft((d) => ({ ...d, ...e }))
    setResult(null)
  }
  const editTeam = (e: Partial<TeamDraft>) => {
    setDraft((d) => ({ ...d, team: d.team && { ...d.team, ...e } }))
    setResult(null)
  }
  const [confirm, setConfirm] = useState(false)
  const defaults = useDefaults()
  const isRoom = bot.kind === "room"
  const what = bot.team ? "team" : isRoom ? "room" : "bot"
  const grants = useLoad(() => (isRoom ? Promise.resolve([]) : api.grants(bot.id)), `${bot.id}`)
  const usage = useLoad(() => (isRoom ? Promise.resolve(null) : api.usage(bot.id)), usageKey(bot, version))
  const save = async () => {
    const plan = savePlan(draft, base, isRoom)
    if ("error" in plan) return setResult(failed(plan.error))
    try {
      if (plan.bot) {
        await api.updateBot(bot.id, plan.bot)
        setBase((b) => ({ ...draft, team: b.team }))
      }
      if (plan.team) {
        await api.updateTeam(bot.id, plan.team)
        setBase(draft)
      }
      setResult(saved(isRoom ? "Saved." : "Saved. Applies from the next turn."))
    } catch (e) {
      setResult(failed((e as Error).message))
    }
  }
  const remove = async () => {
    try {
      await api.deleteBot(bot.id)
      withStorage((store) => forgetHistory(store, bot.id), undefined)
      onDeleted()
    } catch (e) {
      setResult(failed((e as Error).message))
    }
  }
  return (
    <div className="flex flex-col gap-3 text-sm">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="bot-name">Name</Label>
        <Input id="bot-name" value={draft.name} onChange={(e) => edit({ name: e.target.value })} />
      </div>
      {!isRoom && <HueSwatches hue={draft.hue} onHue={(hue) => edit({ hue })} />}
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="bot-instructions">{isRoom ? "Purpose" : "Instructions"}</Label>
        <Textarea
          id="bot-instructions"
          rows={6}
          value={draft.instructions}
          onChange={(e) => edit({ instructions: e.target.value })}
        />
      </div>
      {!isRoom && (
        <fieldset className="flex flex-col gap-3">
          <legend className="mb-1 text-sm font-medium">Model and effort</legend>
          <ModelEffortFields
            key={bot.id}
            id="bot"
            model={draft.model}
            effort={draft.effort}
            onModel={(model) => edit({ model })}
            onEffort={(effort) => edit({ effort })}
            inherited={
              defaults
                ? inheritedFromGlobal(defaults)
                : {
                    model: { value: "", source: "claude-code" },
                    effort: { value: "", source: "claude-code" },
                  }
            }
            defaults={defaults}
            invalid={modelError(result)}
          />
          {bot.effective?.modelId && (
            <p className="text-muted-foreground text-xs">
              Now runs on <code>{bot.effective.modelId}</code>
              {bot.effective.effort ? `; requested effort ${bot.effective.effort}.` : "."}
            </p>
          )}
          <p className="text-muted-foreground text-xs">
            Applies from the bot's next turn. Switching models makes that turn slower and costlier.
          </p>
          {defaults && ignoredOverrides(defaults).length > 0 && (
            <p className="text-muted-foreground text-xs">
              {ignoredOverrides(defaults).join(", ")} ignored: Handful bots' helper subagents use the bot's model.
            </p>
          )}
          <ContextNote usage={usage.data} />
        </fieldset>
      )}
      {bot.team && draft.team && <TeamFields team={bot} draft={draft.team} onDraft={editTeam} />}
      <div className="flex items-center gap-2">
        <Button size="sm" disabled={!dirty} onClick={() => void save()}>
          Save
        </Button>
        <SaveNote result={result} />
      </div>
      {!isRoom && (
        <AllowedTools grants={grants.data} onRevoke={(g) => void api.revokeGrant(bot.id, g).then(grants.reload)} />
      )}
      <div className="border-t pt-3">
        <Button size="sm" variant="destructive" onClick={() => setConfirm(true)}>
          Delete {what}
        </Button>
      </div>
      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {bot.name}?</DialogTitle>
            <DialogDescription>{deleteNote[what]}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirm(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => void remove()}>
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// BotPanel shows what the bot keeps: memory, routines, shared skills, shared
// artifacts and settings.
// For a room it shows the members and settings. version changes whenever the
// transcript changes, which refreshes the lists.
export function BotPanel({
  bot,
  agents,
  version,
  onDeleted,
  tab,
  onTab,
  onRead,
  teams = [],
  onOpenInbox,
  paused,
  onClose,
}: {
  bot: Bot
  agents: Bot[]
  // onClose closes the panel; its button shows only where the panel covers the chat.
  onClose?: () => void
  // teams are the app's teams, to send tasks to.
  teams?: Bot[]
  // onOpenInbox opens the team's inbox.
  onOpenInbox?: () => void
  // paused names a drain that holds the Now task (an update or a restart), or is null.
  paused?: string | null
  version: number
  onDeleted: () => void
  // tab is the tab asked for; the panel shows its first tab when it has no such tab.
  tab: PanelTab | null
  onTab: (tab: PanelTab) => void
  // onRead opens an artifact or a skill in the reader.
  onRead: (r: Reading) => void
}) {
  const rights = useRights()
  const tabs = panelTabs(bot, rights.computer)
  const shown = shownTab(tabs, tab)
  const content = (t: PanelTab) => {
    switch (t) {
      case "board":
        return <BoardTab team={bot} version={version} teams={teams} onOpenInbox={onOpenInbox} paused={paused} />
      case "members":
        return <MembersTab room={bot} agents={agents} />
      case "memory":
        return <MemoryTab bot={bot} version={version} />
      case "routines":
        return <RoutinesTab bot={bot} version={version} />
      case "skills":
        return <SkillsTab version={version} onRead={onRead} />
      case "artifacts":
        return <ArtifactsTab agents={agents} version={version} onRead={onRead} />
      case "settings":
        return <SettingsTab bot={bot} version={version} onDeleted={onDeleted} />
    }
  }
  return (
    <Tabs value={shown} onValueChange={(v) => onTab(v as PanelTab)} className="flex h-full flex-col gap-0">
      <div className="flex h-12 shrink-0 items-center gap-1 border-b px-3 max-md:h-14">
        <TabsList className="bg-sidebar-foreground/5 w-full max-md:h-12">
          {/* text-xs fits five tabs into the panel's narrowest width, beside the Close panel button. */}
          {tabs.map((t) => (
            <TabsTrigger key={t.tab} value={t.tab} className={cn(tabs.length > 4 && "px-0.5 text-xs")}>
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>
        {onClose && (
          <Button
            size="icon-sm"
            variant="ghost"
            className="shrink-0 max-md:size-11"
            aria-label="Close panel"
            onClick={onClose}
          >
            <X />
          </Button>
        )}
      </div>
      <div className="relative min-h-0 flex-1 overflow-y-auto p-3">
        <TabsContent key={`${bot.id}:${shown}`} value={shown}>
          {content(shown)}
        </TabsContent>
      </div>
    </Tabs>
  )
}
