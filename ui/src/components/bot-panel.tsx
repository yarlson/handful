import { useCallback, useEffect, useRef, useState } from "react"
import { Play, Trash2 } from "lucide-react"
import { api, type Artifact, type Bot, type Memory, type Routine, type Skill, type TeamSettings } from "@/lib/api"
import { ARTIFACT_KINDS, artifactAuthor } from "@/lib/artifacts"
import { latestOnly } from "@/lib/latest"
import { MarkdownText } from "@/components/chat"
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
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { budgetError, decisionError, inboxError } from "@/lib/tasks"
import { failed, modelError, saved, type SaveResult } from "@/lib/save"
import { cn } from "@/lib/utils"
import { BoardTab } from "@/components/board"
import { SendTaskDialog } from "@/components/tasks"
import { FALLBACK_ALIASES, ModelChip, ModelEffortFields, useDefaults } from "@/components/model"
import { ignoredOverrides, inheritedFromGlobal, overrides, sourceLine } from "@/lib/model"

function when(ms: number | null) {
  return ms
    ? new Date(ms).toLocaleString([], {
        weekday: "short",
        hour: "2-digit",
        minute: "2-digit",
        month: "short",
        day: "numeric",
      })
    : "—"
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

function MemoryTab({ bot, version }: { bot: Bot; version: number }) {
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
        <div key={m.id} className="group flex items-start gap-2 rounded-md border p-2 text-sm">
          <div className="flex-1">
            <p>{m.fact}</p>
            <div className="mt-1 flex gap-1">
              <Badge variant={m.tier === "profile" ? "default" : "secondary"}>
                {m.tier === "log" ? "journal" : m.tier}
              </Badge>
              <Badge variant="outline">{scopeLabel[m.scope]}</Badge>
              {m.learnedBy === "user" && <Badge variant="outline">from you</Badge>}
              <span className="text-muted-foreground text-xs">{new Date(m.createdAt).toLocaleDateString()}</span>
            </div>
          </div>
          <Button
            size="icon"
            variant="ghost"
            aria-label="Forget"
            className="opacity-0 group-hover:opacity-100"
            onClick={() => void remove(m.id)}
          >
            <Trash2 />
          </Button>
        </div>
      ))}
    </div>
  )
}

function RoutinesTab({ bot, version }: { bot: Bot; version: number }) {
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
        <div key={r.id} className="rounded-md border p-2 text-sm">
          <div className="flex items-center gap-2">
            <span className="flex-1 font-medium">{r.name}</span>
            <Switch
              aria-label="Enabled"
              checked={r.enabled}
              onCheckedChange={(v) => void act(() => api.setRoutineEnabled(r.id, v))}
            />
          </div>
          <p className="text-muted-foreground mt-1 text-xs">{r.prompt}</p>
          <div className="mt-2 flex items-center gap-2 text-xs">
            <code className="bg-muted shrink-0 rounded px-1 whitespace-nowrap">{r.schedule}</code>
            <span className="text-muted-foreground min-w-0">next {r.enabled ? when(r.nextRunAt) : "paused"}</span>
          </div>
          <div className="mt-1 flex items-center justify-end gap-1">
            <Button size="xs" variant="ghost" onClick={() => void act(() => api.runRoutine(r.id))}>
              <Play /> Run now
            </Button>
            <Button
              size="xs"
              variant="ghost"
              aria-label="Delete routine"
              onClick={() => void act(() => api.deleteRoutine(r.id))}
            >
              <Trash2 />
            </Button>
          </div>
        </div>
      ))}
    </div>
  )
}

function SkillsTab({ version }: { version: number }) {
  const { data, error, reload, setError } = useLoad(() => api.skills(), String(version))
  const [open, setOpen] = useState<Skill | null>(null)
  const show = async (id: string) => {
    try {
      setOpen(await api.skill(id))
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const remove = async (id: string) => {
    try {
      await api.deleteSkill(id)
      setOpen(null)
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
          No skills yet. Bots save a skill when you finish a process you will repeat.
        </p>
      )}
      {data?.map((s) => (
        <button
          key={s.id}
          type="button"
          className="hover:bg-muted rounded-md border p-2 text-left text-sm"
          onClick={() => void show(s.id)}
        >
          <span className="font-medium">{s.name}</span>
          <p className="text-muted-foreground text-xs">{s.description}</p>
        </button>
      ))}
      <Dialog open={open !== null} onOpenChange={(v) => !v && setOpen(null)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{open?.name}</DialogTitle>
            <DialogDescription>{open?.description}</DialogDescription>
          </DialogHeader>
          <pre className="bg-muted max-h-[60vh] overflow-auto rounded-md p-3 font-mono text-xs whitespace-pre-wrap">
            {open?.body}
          </pre>
          <DialogFooter>
            <Button variant="destructive" onClick={() => open && void remove(open.id)}>
              Delete for all bots
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// ArtifactsTab lists the documents that all bots share. Bots write them; the
// user reads and deletes them here.
function ArtifactsTab({ agents, version, teams }: { agents: Bot[]; version: number; teams: Bot[] }) {
  // handing is the artifact the user hands to a team.
  const [handing, setHanding] = useState<string | null>(null)
  const [kind, setKind] = useState("")
  const [query, setQuery] = useState("")
  const { data, error, reload, setError } = useLoad(() => api.artifacts({ kind, query }), `${version}:${kind}:${query}`)
  const [open, setOpen] = useState<Artifact | null>(null)
  const [confirm, setConfirm] = useState(false)
  const show = async (id: string) => {
    try {
      setConfirm(false)
      setOpen(await api.artifact(id))
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const remove = async (id: string) => {
    try {
      await api.deleteArtifact(id)
      setOpen(null)
      reload()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <div className="flex flex-col gap-2">
      <div className="flex gap-2">
        <select
          aria-label="Kind"
          className="border-input bg-background h-8 rounded-md border px-2 text-sm"
          value={kind}
          onChange={(e) => setKind(e.target.value)}
        >
          <option value="">All kinds</option>
          {ARTIFACT_KINDS.map((k) => (
            <option key={k} value={k}>
              {k}
            </option>
          ))}
        </select>
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
          className="hover:bg-muted rounded-md border p-2 text-left text-sm"
          onClick={() => void show(a.id)}
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
      <Dialog open={open !== null} onOpenChange={(v) => !v && setOpen(null)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{open?.title}</DialogTitle>
            <DialogDescription>
              {open && `${open.kind} from ${artifactAuthor(open.authorId, agents)}, updated ${when(open.updatedAt)}`}
            </DialogDescription>
          </DialogHeader>
          {(open?.tags.length ?? 0) > 0 && (
            <div className="flex flex-wrap gap-1">
              {open?.tags.map((t) => (
                <Badge key={t} variant="outline">
                  {t}
                </Badge>
              ))}
            </div>
          )}
          {open?.links.map((l) => (
            <a
              key={l}
              href={l}
              target="_blank"
              rel="noopener noreferrer"
              className="truncate text-xs text-blue-600 underline"
            >
              {l}
            </a>
          ))}
          <div className="max-h-[60vh] overflow-auto rounded-md border p-3 text-sm">
            <MarkdownText text={open?.content ?? ""} />
          </div>
          <DialogFooter>
            {confirm ? (
              <>
                <Button variant="ghost" onClick={() => setConfirm(false)}>
                  Cancel
                </Button>
                <Button variant="destructive" onClick={() => open && void remove(open.id)}>
                  Delete for all bots
                </Button>
              </>
            ) : (
              <>
                {teams.length > 0 && (
                  <Button
                    variant="outline"
                    aria-haspopup="dialog"
                    onClick={() => {
                      setHanding(open?.id ?? null)
                      setOpen(null)
                    }}
                  >
                    Send to a team…
                  </Button>
                )}
                <Button variant="destructive" onClick={() => setConfirm(true)}>
                  Delete
                </Button>
              </>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <SendTaskDialog
        open={handing !== null}
        onOpenChange={(v) => !v && setHanding(null)}
        from={null}
        teams={teams}
        artifactIds={handing ? [handing] : []}
      />
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
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="truncate font-medium">{m.name}</span>
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

// TeamSettingsForm edits what a team adds to its room: the lead, the working agreement, the
// turns per day, the decision time and the inbox limits. Its key changes with the saved
// settings, which resets it.
function TeamSettingsForm({ team, settings }: { team: Bot; settings: TeamSettings }) {
  const [lead, setLead] = useState(settings.leadId)
  const [agreement, setAgreement] = useState(settings.agreement)
  const [budget, setBudget] = useState(String(settings.dailyBudget ?? 0))
  const [decision, setDecision] = useState(String(settings.decisionMinutes ?? 30))
  const [inboxMax, setInboxMax] = useState(String(settings.inboxMax ?? 10))
  const [perSender, setPerSender] = useState(String(settings.inboxPerSender ?? 3))
  const [defaultModel, setDefaultModel] = useState(settings.defaultModel ?? "")
  const [defaultEffort, setDefaultEffort] = useState(settings.defaultEffort ?? "")
  const [result, setResult] = useState<SaveResult>(null)
  const defaults = useDefaults()
  const members = team.members ?? []
  const save = async () => {
    try {
      const badTask = budgetError(budget) ?? decisionError(decision) ?? inboxError(inboxMax, perSender)
      if (badTask) {
        setResult(failed(badTask))
        return
      }
      // The lead goes only when it changed, so that a fallback lead does not replace the chosen one.
      await api.updateTeam(team.id, {
        ...(lead !== settings.leadId && { leadId: lead }),
        agreement,
        dailyBudget: Number(budget),
        decisionMinutes: Number(decision),
        inboxMax: Number(inboxMax),
        inboxPerSender: Number(perSender),
        defaultModel,
        defaultEffort,
      })
      setResult(saved())
    } catch (e) {
      setResult(failed((e as Error).message))
    }
  }
  return (
    <div className="flex flex-col gap-3 border-t pt-3">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="team-settings-lead">Lead</Label>
        <select
          id="team-settings-lead"
          className="border-input h-9 rounded-md border bg-transparent px-2 text-sm"
          value={lead}
          onChange={(e) => setLead(e.target.value)}
        >
          {(team.members ?? []).map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </select>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="team-settings-agreement">Working agreement</Label>
        <Textarea
          id="team-settings-agreement"
          rows={5}
          placeholder="How the team works and when work is done."
          value={agreement}
          onChange={(e) => setAgreement(e.target.value)}
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
          value={budget}
          aria-describedby="team-settings-budget-help"
          onChange={(e) => setBudget(e.target.value)}
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
          value={decision}
          aria-describedby="team-settings-decision-help"
          onChange={(e) => setDecision(e.target.value)}
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
            value={inboxMax}
            onChange={(e) => setInboxMax(e.target.value)}
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
            value={perSender}
            onChange={(e) => setPerSender(e.target.value)}
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
          model={defaultModel}
          effort={defaultEffort}
          onModel={setDefaultModel}
          onEffort={setDefaultEffort}
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
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={() => void save()}>
          Save team
        </Button>
        <SaveNote result={result} />
      </div>
    </div>
  )
}

function SettingsTab({ bot, onDeleted }: { bot: Bot; onDeleted: () => void }) {
  const [name, setName] = useState(bot.name)
  const [instructions, setInstructions] = useState(bot.instructions)
  const [model, setModel] = useState(bot.model ?? "")
  const [effort, setEffort] = useState(bot.effort ?? "")
  const [result, setResult] = useState<SaveResult>(null)
  const [confirm, setConfirm] = useState(false)
  const defaults = useDefaults()
  const isRoom = bot.kind === "room"
  const what = bot.team ? "team" : isRoom ? "room" : "bot"
  const grants = useLoad(() => (isRoom ? Promise.resolve([]) : api.grants(bot.id)), `${bot.id}`)
  useEffect(() => {
    setName(bot.name)
    setInstructions(bot.instructions)
    setModel(bot.model ?? "")
    setEffort(bot.effort ?? "")
  }, [bot.id, bot.name, bot.instructions, bot.model, bot.effort])
  const save = async () => {
    try {
      await api.updateBot(bot.id, isRoom ? { name, instructions } : { name, instructions, model, effort })
      setResult(saved(isRoom ? "Saved." : "Saved. Applies from the next turn."))
    } catch (e) {
      setResult(failed((e as Error).message))
    }
  }
  const remove = async () => {
    try {
      await api.deleteBot(bot.id)
      onDeleted()
    } catch (e) {
      setResult(failed((e as Error).message))
    }
  }
  return (
    <div className="flex flex-col gap-3 text-sm">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="bot-name">Name</Label>
        <Input id="bot-name" value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="bot-instructions">{isRoom ? "Purpose" : "Instructions"}</Label>
        <Textarea
          id="bot-instructions"
          rows={6}
          value={instructions}
          onChange={(e) => setInstructions(e.target.value)}
        />
      </div>
      {!isRoom && (
        <fieldset className="flex flex-col gap-3">
          <legend className="mb-1 text-sm font-medium">Model and effort</legend>
          <ModelEffortFields
            key={bot.id}
            id="bot"
            model={model}
            effort={effort}
            onModel={setModel}
            onEffort={setEffort}
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
              {ignoredOverrides(defaults).join(", ")} ignored: YarGPT bots' helper subagents use the bot's model.
            </p>
          )}
        </fieldset>
      )}
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={() => void save()}>
          Save
        </Button>
        <SaveNote result={result} />
      </div>
      {bot.team && (
        <TeamSettingsForm
          key={`${bot.id}:${bot.team.leadId}:${bot.team.agreement}:${bot.team.dailyBudget}:${bot.team.defaultModel}:${bot.team.defaultEffort}:${bot.team.decisionMinutes}:${bot.team.inboxMax}:${bot.team.inboxPerSender}`}
          team={bot}
          settings={bot.team}
        />
      )}
      <div className={isRoom ? "hidden" : "flex flex-col gap-1.5"}>
        <Label>Always allowed tools</Label>
        {grants.data?.length === 0 && (
          <p className="text-muted-foreground text-xs">None. Each command and file change asks first.</p>
        )}
        <div className="flex flex-wrap gap-1">
          {grants.data?.map((g) => (
            <Badge key={g} variant="secondary" className="gap-1">
              {g}
              <button
                type="button"
                aria-label={`Revoke ${g}`}
                onClick={() => void api.revokeGrant(bot.id, g).then(grants.reload)}
              >
                ×
              </button>
            </Badge>
          ))}
        </div>
      </div>
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
  show,
  teams = [],
  onOpenInbox,
}: {
  bot: Bot
  agents: Bot[]
  // teams are the app's teams, to send tasks to.
  teams?: Bot[]
  // onOpenInbox opens the team's inbox.
  onOpenInbox?: () => void
  version: number
  onDeleted: () => void
  // show asks for a tab; its n changes with every ask, so the same tab can be asked again.
  show?: { tab: string; n: number }
}) {
  // A new ask remounts the tabs on the asked tab.
  const first = show?.tab ?? (bot.kind === "room" ? (bot.team ? "board" : "members") : "memory")
  const key = `${bot.id}:${show?.n ?? 0}`
  if (bot.kind === "room") {
    return (
      <Tabs key={key} defaultValue={first} className="flex h-full flex-col p-3">
        <TabsList className="w-full">
          {bot.team && <TabsTrigger value="board">Work</TabsTrigger>}
          <TabsTrigger value="members">Members</TabsTrigger>
          {bot.team && <TabsTrigger value="memory">Memory</TabsTrigger>}
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>
        <div className="relative min-h-0 flex-1 overflow-y-auto pt-2">
          <TabsContent value="members">
            <MembersTab room={bot} agents={agents} />
          </TabsContent>
          {bot.team && (
            <TabsContent value="board">
              <BoardTab team={bot} version={version} teams={teams} onOpenInbox={onOpenInbox} />
            </TabsContent>
          )}
          {bot.team && (
            <TabsContent value="memory">
              <MemoryTab bot={bot} version={version} />
            </TabsContent>
          )}
          <TabsContent value="settings">
            <SettingsTab bot={bot} onDeleted={onDeleted} />
          </TabsContent>
        </div>
      </Tabs>
    )
  }
  return (
    <Tabs key={key} defaultValue={first} className="flex h-full flex-col p-3">
      <TabsList className="w-full">
        {/* text-xs fits five tabs into the panel's width. */}
        <TabsTrigger value="memory" className="text-xs">
          Memory
        </TabsTrigger>
        <TabsTrigger value="routines" className="text-xs">
          Routines
        </TabsTrigger>
        <TabsTrigger value="skills" className="text-xs">
          Skills
        </TabsTrigger>
        <TabsTrigger value="artifacts" className="text-xs">
          Artifacts
        </TabsTrigger>
        <TabsTrigger value="settings" className="text-xs">
          Settings
        </TabsTrigger>
      </TabsList>
      <div className="relative min-h-0 flex-1 overflow-y-auto pt-2">
        <TabsContent value="memory">
          <MemoryTab bot={bot} version={version} />
        </TabsContent>
        <TabsContent value="routines">
          <RoutinesTab bot={bot} version={version} />
        </TabsContent>
        <TabsContent value="skills">
          <SkillsTab version={version} />
        </TabsContent>
        <TabsContent value="artifacts">
          <ArtifactsTab agents={agents} version={version} teams={teams} />
        </TabsContent>
        <TabsContent value="settings">
          <SettingsTab bot={bot} onDeleted={onDeleted} />
        </TabsContent>
      </div>
    </Tabs>
  )
}
