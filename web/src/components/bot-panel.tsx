import { useCallback, useEffect, useRef, useState } from "react"
import { Play, Trash2 } from "lucide-react"
import { api, type Bot, type Memory, type Routine, type Skill } from "@/lib/api"
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

// useLoad fetches data and fetches it again whenever key changes.
function useLoad<T>(load: () => Promise<T>, key: string) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const loadRef = useRef(load)
  loadRef.current = load
  const reload = useCallback(() => {
    loadRef.current().then(setData, (e: Error) => setError(e.message))
  }, [])
  useEffect(reload, [reload, key])
  return { data, error, reload, setError }
}

// AddFactForm saves a fact as the user's own; bots trust standing rules only from such facts.
function AddFactForm({ bot, onAdded, onError }: { bot: Bot; onAdded: () => void; onError: (message: string) => void }) {
  const [fact, setFact] = useState("")
  const [shared, setShared] = useState(false)
  const [always, setAlways] = useState(true)
  const add = async () => {
    try {
      await api.addMemory(bot.id, { scope: shared ? "user" : "agent", tier: always ? "profile" : "log", fact })
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
        <p className="text-muted-foreground text-sm">Nothing remembered yet. {bot.name} saves facts as you talk.</p>
      )}
      {data?.map((m: Memory) => (
        <div key={m.id} className="group flex items-start gap-2 rounded-md border p-2 text-sm">
          <div className="flex-1">
            <p>{m.fact}</p>
            <div className="mt-1 flex gap-1">
              <Badge variant={m.tier === "profile" ? "default" : "secondary"}>
                {m.tier === "log" ? "journal" : m.tier}
              </Badge>
              <Badge variant="outline">{m.scope === "user" ? "shared" : "this bot"}</Badge>
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

function MembersTab({ room, agents }: { room: Bot; agents: Bot[] }) {
  const [members, setMembers] = useState<Set<string>>(new Set((room.members ?? []).map((m) => m.id)))
  const [message, setMessage] = useState<string | null>(null)
  useEffect(() => setMembers(new Set((room.members ?? []).map((m) => m.id))), [room.id, room.members])
  const save = async () => {
    try {
      await api.setRoomMembers(room.id, [...members])
      setMessage("Saved.")
    } catch (e) {
      setMessage((e as Error).message)
    }
  }
  return (
    <div className="flex flex-col gap-2 text-sm">
      <p className="text-muted-foreground text-xs">
        Members take turns in the room. A post wakes the members it @mentions, or all members when it mentions none.
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
        {message && <span className="text-muted-foreground text-xs">{message}</span>}
      </div>
    </div>
  )
}

function SettingsTab({ bot, onDeleted }: { bot: Bot; onDeleted: () => void }) {
  const [name, setName] = useState(bot.name)
  const [instructions, setInstructions] = useState(bot.instructions)
  const [message, setMessage] = useState<string | null>(null)
  const [confirm, setConfirm] = useState(false)
  const isRoom = bot.kind === "room"
  const grants = useLoad(() => (isRoom ? Promise.resolve([]) : api.grants(bot.id)), `${bot.id}`)
  useEffect(() => {
    setName(bot.name)
    setInstructions(bot.instructions)
  }, [bot.id, bot.name, bot.instructions])
  const save = async () => {
    try {
      await api.updateBot(bot.id, { name, instructions })
      setMessage("Saved.")
    } catch (e) {
      setMessage((e as Error).message)
    }
  }
  const remove = async () => {
    try {
      await api.deleteBot(bot.id)
      onDeleted()
    } catch (e) {
      setMessage((e as Error).message)
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
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={() => void save()}>
          Save
        </Button>
        {message && <span className="text-muted-foreground text-xs">{message}</span>}
      </div>
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
          {isRoom ? "Delete room" : "Delete bot"}
        </Button>
      </div>
      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {bot.name}?</DialogTitle>
            <DialogDescription>
              {isRoom
                ? "This deletes the room and its conversation. The member bots stay."
                : "This deletes its conversation, its own memory, its routines and its working directory. Shared memory and skills stay."}
            </DialogDescription>
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

// BotPanel shows what the bot keeps: memory, routines, shared skills and settings.
// For a room it shows the members and settings. version changes whenever the
// transcript changes, which refreshes the lists.
export function BotPanel({
  bot,
  agents,
  version,
  onDeleted,
}: {
  bot: Bot
  agents: Bot[]
  version: number
  onDeleted: () => void
}) {
  if (bot.kind === "room") {
    return (
      <Tabs key={bot.id} defaultValue="members" className="flex h-full flex-col p-3">
        <TabsList className="w-full">
          <TabsTrigger value="members">Members</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>
        <div className="min-h-0 flex-1 overflow-y-auto pt-2">
          <TabsContent value="members">
            <MembersTab room={bot} agents={agents} />
          </TabsContent>
          <TabsContent value="settings">
            <SettingsTab bot={bot} onDeleted={onDeleted} />
          </TabsContent>
        </div>
      </Tabs>
    )
  }
  return (
    <Tabs key={bot.id} defaultValue="memory" className="flex h-full flex-col p-3">
      <TabsList className="w-full">
        <TabsTrigger value="memory">Memory</TabsTrigger>
        <TabsTrigger value="routines">Routines</TabsTrigger>
        <TabsTrigger value="skills">Skills</TabsTrigger>
        <TabsTrigger value="settings">Settings</TabsTrigger>
      </TabsList>
      <div className="min-h-0 flex-1 overflow-y-auto pt-2">
        <TabsContent value="memory">
          <MemoryTab bot={bot} version={version} />
        </TabsContent>
        <TabsContent value="routines">
          <RoutinesTab bot={bot} version={version} />
        </TabsContent>
        <TabsContent value="skills">
          <SkillsTab version={version} />
        </TabsContent>
        <TabsContent value="settings">
          <SettingsTab bot={bot} onDeleted={onDeleted} />
        </TabsContent>
      </div>
    </Tabs>
  )
}
