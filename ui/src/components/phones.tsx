import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { encode } from "uqr"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useLoad } from "@/components/bot-panel"
import { api, type PairRequest, type PhoneAccess } from "@/lib/api"
import {
  codeGroups,
  codeOfHash,
  DEFAULT_SCOPE,
  LOCKED_TEXT,
  pairLink,
  SCOPES,
  scopeName,
  splitHosts,
  timeLeft,
  type Proxy,
  type Scope,
} from "@/lib/phones"
import { cn } from "@/lib/utils"

// EXAMPLE_HOST is the phone address the setup text shows before the user types one.
const EXAMPLE_HOST = "phone.handful.dev"

// Qr draws text as a QR code, dark on white in both color modes, so any camera reads it.
export function Qr({ text, label }: { text: string; label: string }) {
  const src = useMemo(() => {
    const qr = encode(text, { border: 2 })
    let path = ""
    qr.data.forEach((row, y) =>
      row.forEach((dark, x) => {
        if (dark) path += `M${x} ${y}h1v1h-1z`
      }),
    )
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${qr.size} ${qr.size}" shape-rendering="crispEdges"><rect width="100%" height="100%" fill="#fff"/><path d="${path}" fill="#000"/></svg>`
    return `data:image/svg+xml,${encodeURIComponent(svg)}`
  }, [text])
  return <img src={src} alt={label} className="size-44 shrink-0 rounded-md bg-white [image-rendering:pixelated]" />
}

// CopyButton copies text and says so for two seconds.
function CopyButton({ text, label = "Copy", what }: { text: string; label?: string; what: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <Button
      size="xs"
      variant="outline"
      aria-label={`${label}: ${what}`}
      onClick={() =>
        void navigator.clipboard.writeText(text).then(() => {
          setCopied(true)
          setTimeout(() => setCopied(false), 2000)
        })
      }
    >
      {copied ? "Copied" : label}
    </Button>
  )
}

const code = (s: string) => <code className="bg-muted rounded px-1 font-mono text-[0.85em]">{s}</code>

// portOf is the port of a host:port address.
function portOf(addr: string) {
  return addr.slice(addr.lastIndexOf(":") + 1)
}

// CloudflareText is P2: how to set up a Cloudflare Tunnel, and what it costs in privacy.
function CloudflareText({ addr, host }: { addr: string; host: string }) {
  return (
    <div className="flex flex-col gap-2 text-sm">
      <p>
        <strong>Cloudflare Tunnel.</strong> You need a Cloudflare account and a domain on Cloudflare.
      </p>
      <p>
        <strong>1.</strong> Create a tunnel for {code(`http://${addr}`)} and give it a hostname, for example{" "}
        {code(EXAMPLE_HOST)}.
      </p>
      <p>
        <strong>2.</strong> Add an Access application for that hostname, with a policy that allows only your email. Copy
        its <strong>AUD tag</strong> and your <strong>team domain</strong> into the fields below.
      </p>
      <p>
        <strong>3.</strong> Your phone address is {code(`https://${host}`)}{" "}
        <CopyButton text={`https://${host}`} what="phone address" />. Open it on your phone, sign in, and scan the
        pairing code.
      </p>
      <Warnings>
        <p>
          <strong>Cloudflare can read everything that passes through it:</strong> your chats, what bots print and your
          approvals. Pick this way only if you accept that.
        </p>
        <p>
          <strong>The address is on the public internet.</strong> Cloudflare's login and Handful's own pairing both
          protect it. Handful refuses any request without a valid Cloudflare login.
        </p>
        <p>
          <strong>The name becomes public.</strong> Its certificate is listed in public Certificate Transparency logs,
          so anyone can see that the name exists. They still can't open it without your Cloudflare login.
        </p>
        <p>
          <strong>Check the policy:</strong> if you ever see the pairing page without a Cloudflare login, turn phone
          access off.
        </p>
        <p>
          <strong>Not allowed:</strong> quick tunnels ({code("trycloudflare.com")}), which have no login in front.
        </p>
      </Warnings>
    </div>
  )
}

// TailscaleText is P2b: how to share the phone address on a tailnet, and what to avoid.
function TailscaleText({ addr, host }: { addr: string; host: string }) {
  const command = `tailscale serve --bg ${portOf(addr)}`
  return (
    <div className="flex flex-col gap-2 text-sm">
      <p>
        <strong>1. Put Tailscale on this computer and your phone</strong> and sign in to the same tailnet.
      </p>
      <p>
        <strong>2. Share Handful's phone address.</strong> On this computer, run {code(command)} in Terminal.{" "}
        <CopyButton text={command} label="Copy command" what={command} />
      </p>
      <p>
        <strong>3. Your phone address</strong> is {code(`https://${host}`)}{" "}
        <CopyButton text={`https://${host}`} what="phone address" />.
      </p>
      <Warnings>
        <p>
          <strong>The machine name becomes public.</strong> Tailscale's certificate for this name is listed in public
          Certificate Transparency logs, so anyone can see that the name exists. They still can't open it without being
          on your tailnet and paired.
        </p>
        <p>
          <strong>Don't use {code("tailscale funnel")}.</strong> It puts this address on the public internet.
        </p>
        <p>
          <strong>Everyone on your tailnet can reach the pairing page</strong>, but can't do anything without a paired
          phone. If your tailnet is shared (a company, a family), use a tailnet of your own for this.
        </p>
      </Warnings>
    </div>
  )
}

function Warnings({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-amber-500/40 bg-amber-50 px-4 py-3 text-amber-900 dark:bg-amber-950 dark:text-amber-200">
      {children}
    </div>
  )
}

function Field({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      {children}
    </div>
  )
}

// TurnOn is P1 with the way in: the setup text of the chosen way, its fields, and Turn on.
export function TurnOn({ phone, onDone }: { phone: PhoneAccess; onDone: () => void }) {
  const [proxy, setProxy] = useState<Proxy>(phone.proxy)
  const [hosts, setHosts] = useState(phone.hosts.join(", "))
  const [teamDomain, setTeamDomain] = useState(phone.teamDomain)
  const [aud, setAud] = useState(phone.aud)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const host = splitHosts(hosts)[0] ?? (proxy === "cloudflare" ? EXAMPLE_HOST : "<name>")
  const turnOn = async () => {
    setBusy(true)
    setError(null)
    try {
      await api.phoneOn({ proxy, hosts: splitHosts(hosts), teamDomain, aud })
      onDone()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm">
        <strong>Phone access is off.</strong> Turn it on to use Handful from your phone. Handful opens one extra address
        on this computer and never listens on your network.
      </p>
      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1 text-sm font-medium">The way in</legend>
        {(
          [
            ["cloudflare", "Cloudflare Tunnel"],
            ["tailscale", "Tailscale"],
          ] as const
        ).map(([id, name]) => (
          <label key={id} className="flex min-h-8 items-center gap-2 text-sm max-md:min-h-11">
            <input type="radio" name="phone-proxy" checked={proxy === id} onChange={() => setProxy(id)} />
            {name}
          </label>
        ))}
      </fieldset>
      {proxy === "cloudflare" ? (
        <CloudflareText addr={phone.addr} host={host} />
      ) : (
        <TailscaleText addr={phone.addr} host={host} />
      )}
      <div className="grid gap-3 sm:grid-cols-2">
        <Field id="phone-hosts" label="Phone address">
          <Input
            id="phone-hosts"
            value={hosts}
            placeholder={proxy === "cloudflare" ? EXAMPLE_HOST : "mac.tail1234.ts.net"}
            onChange={(e) => setHosts(e.target.value)}
          />
        </Field>
        {proxy === "cloudflare" && (
          <>
            <Field id="phone-team" label="Team domain">
              <Input
                id="phone-team"
                value={teamDomain}
                placeholder="you.cloudflareaccess.com"
                onChange={(e) => setTeamDomain(e.target.value)}
              />
            </Field>
            <Field id="phone-aud" label="AUD tag">
              <Input id="phone-aud" value={aud} className="font-mono" onChange={(e) => setAud(e.target.value)} />
            </Field>
          </>
        )}
      </div>
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
      <Button className="self-start" disabled={busy} onClick={() => void turnOn()}>
        Turn on
      </Button>
    </div>
  )
}

// Pairing is P3, or P6 while pairing is paused: the code with its QR, and the scope.
export function Pairing({ phone, onChange }: { phone: PhoneAccess; onChange: () => void }) {
  const [scope, setScope] = useState<Scope>(phone.pairing.scope ?? DEFAULT_SCOPE)
  const [code, setCode] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  // The code shows until it is used or ends; then Pair a phone makes a new one.
  const now = useNow(code !== null)
  const open = code !== null && now < (phone.pairing.codeExpiresAt ?? 0)
  if (phone.pairing.lockedUntil) {
    return (
      <div className="flex flex-col items-start gap-2 text-sm">
        <output>{LOCKED_TEXT}</output>
        <Button size="sm" disabled>
          Pair again later
        </Button>
      </div>
    )
  }
  const start = async () => {
    setError(null)
    try {
      const r = await api.startPairing(scope)
      setCode(r.code)
      onChange()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const choose = async (s: Scope) => {
    setScope(s)
    if (!open) return
    try {
      await api.setPairingScope(s)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const host = phone.hosts[0] ?? EXAMPLE_HOST
  return (
    <div className="flex flex-col gap-3 text-sm">
      {open && code ? (
        <div className="flex flex-wrap items-start gap-4">
          <Qr text={pairLink(host, code)} label="Pairing code for your phone's camera" />
          <p className="min-w-0 flex-1 basis-60">
            <strong>Pair a phone.</strong> Scan this code with your phone's camera, or type it on the phone:{" "}
            <strong className="font-mono">{codeGroups(code)}</strong>{" "}
            <CopyButton text={codeGroups(code)} what="pairing code" />. The code works once and expires in 5 minutes.
            Then approve the phone here.
          </p>
        </div>
      ) : (
        <Button size="sm" className="self-start" onClick={() => void start()}>
          Pair a phone
        </Button>
      )}
      <fieldset className="flex flex-col gap-1">
        <legend className="mb-1 font-medium">What this phone may do:</legend>
        {SCOPES.map((s) => (
          <label key={s.id} className="flex min-h-8 items-start gap-2 max-md:min-h-11">
            <input
              type="radio"
              name="phone-scope"
              className="mt-1"
              checked={scope === s.id}
              onChange={() => void choose(s.id)}
            />
            <span>
              <strong>{s.name}</strong> — {s.help}
            </span>
          </label>
        ))}
      </fieldset>
      <p className="text-muted-foreground">
        A phone can send messages to your bots. A bot can then use any tools you've already always-allowed for it. A
        phone can never choose "Always allow" or change settings.
      </p>
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}

// PhonesList is P5: each paired phone with Revoke, then Revoke all and the kill switch.
function PhonesList({ rev, onOff }: { rev: number; onOff: () => void }) {
  const list = useLoad(() => api.phoneSessions(), String(rev))
  const act = (action: () => Promise<void>) => void action().then(list.reload, (e: Error) => list.setError(e.message))
  const phones = (list.data ?? []).filter((p) => !p.revokedAt)
  return (
    <div className="flex flex-col gap-2 text-sm">
      <h3 className="font-medium">Phones</h3>
      {phones.length === 0 ? (
        <p className="text-muted-foreground">No phone is paired.</p>
      ) : (
        <ul className="flex flex-col divide-y rounded-md border">
          {phones.map((p) => (
            <li key={p.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
              <span className="font-medium">{p.name}</span>
              <span>{scopeName(p.scope)}</span>
              {p.pairedBy && <span className="text-muted-foreground">paired by {p.pairedBy}</span>}
              <span className="text-muted-foreground">
                last used {new Date(p.lastSeenAt).toLocaleString([], { dateStyle: "medium", timeStyle: "short" })}
              </span>
              <Button
                size="xs"
                variant="outline"
                className="ml-auto max-md:h-11"
                aria-label={`Revoke ${p.name}`}
                onClick={() => act(() => api.revokePhone(p.id))}
              >
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      )}
      {list.error && (
        <p role="alert" className="text-destructive">
          {list.error}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={phones.length === 0} onClick={() => act(api.revokeAllPhones)}>
          Revoke all
        </Button>
        <Button size="sm" variant="destructive" onClick={() => act(() => api.phoneOff().then(onOff))}>
          Turn off phone access
        </Button>
      </div>
    </div>
  )
}

// PhonesSection is Settings › Phones. pairRev changes with each pairing event, which reloads it.
export function PhonesSection({ pairRev }: { pairRev: number }) {
  const phone = useLoad(() => api.phone(), String(pairRev))
  const view = phone.data
  if (!view) {
    return <p className="text-muted-foreground text-sm">{phone.error ?? "Loading…"}</p>
  }
  if (!view.on) return <TurnOn phone={view} onDone={phone.reload} />
  const host = view.hosts[0] ?? EXAMPLE_HOST
  return (
    <div className="flex flex-col gap-6">
      {view.proxy === "cloudflare" ? (
        <CloudflareText addr={view.addr} host={host} />
      ) : (
        <TailscaleText addr={view.addr} host={host} />
      )}
      <Pairing phone={view} onChange={phone.reload} />
      <PhonesList rev={pairRev} onOff={phone.reload} />
    </div>
  )
}

// useNow ticks every second while on is true, for a countdown.
function useNow(on: boolean) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!on) return
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [on])
  return now
}

// PairRequestLine is P4: a phone that waits for the user's decision, with its countdown.
export function PairRequestLine({
  request,
  onDecided,
  className,
}: {
  request: PairRequest
  onDecided: () => void
  className?: string
}) {
  const now = useNow(true)
  const [error, setError] = useState<string | null>(null)
  const decide = (d: "allow" | "deny") =>
    void api.decidePairing(request.id, d).then(onDecided, (e: Error) => setError(e.message))
  if (now >= request.expiresAt) return null
  return (
    <div role="alert" className={cn("flex flex-wrap items-center gap-2 text-sm", className)}>
      <p className="min-w-0 flex-1 basis-60">
        <strong>{request.name}</strong> wants to pair as <em>{scopeName(request.scope)}</em>.
      </p>
      <Button size="sm" className="max-md:h-11" onClick={() => decide("allow")}>
        Allow
      </Button>
      <Button size="sm" variant="outline" className="max-md:h-11" onClick={() => decide("deny")}>
        Deny
      </Button>
      <span className="text-muted-foreground tabular-nums">{timeLeft(request.expiresAt, now)}</span>
      {error && <p className="text-destructive basis-full">{error}</p>}
    </div>
  )
}

// usePairRequest is the pair request that waits on the computer, reloaded on each pairing event;
// on a phone, which can't pair others, there is none. The desktop app hears of each change through
// the window's "handful:pair-request" event, whose detail is 1 while a request waits and 0 after.
export function usePairRequest(pairRev: number, computer: boolean) {
  const phone = useLoad(() => (computer ? api.phone() : Promise.resolve(null)), `${pairRev}:${computer}`)
  const request = phone.data?.pairing.request ?? null
  const now = useNow(request !== null)
  const live = request && now < request.expiresAt ? request : null
  const waiting = live ? 1 : 0
  const told = useRef(0)
  useEffect(() => {
    if (told.current === waiting) return
    told.current = waiting
    window.dispatchEvent(new CustomEvent("handful:pair-request", { detail: waiting }))
  }, [waiting])
  return { request: live, reload: phone.reload }
}

// PairBanner is P4 at the top of every screen of the computer while a phone waits.
export function PairBanner({ request, onDecided }: { request: PairRequest | null; onDecided: () => void }) {
  if (!request) return null
  return (
    <PairRequestLine
      request={request}
      onDecided={onDecided}
      className="border-b border-amber-500/40 bg-amber-50 px-4 py-2 text-amber-900 dark:bg-amber-950 dark:text-amber-200"
    />
  )
}

// PairPage is P4b, the phone's pairing page: the typed code, or the one the link brought, and the
// phone's name; then it waits for the user's decision on the computer.
export function PairPage({ paired }: { paired: boolean }) {
  const [code, setCode] = useState(() => codeOfHash(location.hash))
  const [name, setName] = useState("")
  const [state, setState] = useState<"form" | "waiting" | "denied">("form")
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    // The code leaves the address bar, so it isn't kept in the history.
    if (location.hash) history.replaceState(null, "", location.pathname)
  }, [])
  useEffect(() => {
    if (state !== "waiting") return
    const t = setInterval(() => {
      api.pairStatus().then(
        (s) => {
          if (s.state === "approved") location.assign("/")
          else if (s.state === "denied") setState("denied")
        },
        (e: Error) => setError(e.message),
      )
    }, 2000)
    return () => clearInterval(t)
  }, [state])
  const pair = async () => {
    setError(null)
    try {
      await api.pair(code, name)
      setState("waiting")
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <div className="bg-background text-foreground flex min-h-dvh items-start justify-center px-4 pt-[max(3rem,env(safe-area-inset-top))]">
      <div className="flex w-full max-w-sm flex-col gap-4">
        <img src="/logo.svg" alt="Handful" className="size-10" />
        {paired && state === "form" && (
          <p className="text-sm">
            This phone is paired. <a href="/">Open Handful</a>
          </p>
        )}
        {state === "waiting" && <output>Waiting for approval on your computer…</output>}
        {state === "denied" && <output>Not approved. Ask for a new code.</output>}
        {state !== "waiting" && (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault()
              void pair()
            }}
          >
            <p className="text-sm">Enter the pairing code shown on your computer.</p>
            <Field id="pair-code" label="Pairing code">
              <Input
                id="pair-code"
                value={code}
                autoComplete="one-time-code"
                autoCapitalize="characters"
                spellCheck={false}
                className="h-11 font-mono text-base"
                onChange={(e) => setCode(e.target.value)}
              />
            </Field>
            <Field id="pair-name" label="This phone's name">
              <Input
                id="pair-name"
                value={name}
                placeholder="Pixel 8"
                maxLength={60}
                className="h-11 text-base"
                onChange={(e) => setName(e.target.value)}
              />
            </Field>
            {error && (
              <p role="alert" className="text-destructive text-sm">
                {error}
              </p>
            )}
            <Button type="submit" className="h-11" disabled={!code.trim() || !name.trim()}>
              Pair
            </Button>
          </form>
        )}
      </div>
    </div>
  )
}

// PairAgain is the page of a phone whose session ended: revoked, expired or never made.
export function PairAgain() {
  return (
    <div className="bg-background text-foreground flex min-h-dvh items-start justify-center px-4 pt-[max(3rem,env(safe-area-inset-top))]">
      <div className="flex w-full max-w-sm flex-col gap-4">
        <img src="/logo.svg" alt="Handful" className="size-10" />
        <p>This phone isn't paired with Handful.</p>
        <Button className="h-11" onClick={() => location.assign("/pair")}>
          Pair again
        </Button>
      </div>
    </div>
  )
}
