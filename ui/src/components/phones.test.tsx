import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import type { PhoneAccess } from "@/lib/api"
import { PairPage, PairRequestLine, Pairing, TurnOn } from "./phones"

const off: PhoneAccess = {
  on: false,
  addr: "127.0.0.1:7778",
  proxy: "cloudflare",
  hosts: [],
  teamDomain: "",
  aud: "",
  pairing: {},
}

// text is the words of markup, without its tags and with entities read.
function text(html: string) {
  return html
    .replace(/<[^>]+>/g, "")
    .replaceAll("&#x27;", "'")
    .replaceAll("&quot;", '"')
    .replaceAll("&amp;", "&")
    .replace(/\s+/g, " ")
}

afterEach(() => {
  vi.unstubAllGlobals()
})

test("P1 and P2: off, with the tunnel's steps and every warning before Turn on", () => {
  const t = text(renderToStaticMarkup(<TurnOn phone={off} onDone={() => {}} />))
  for (const line of [
    "Phone access is off. Turn it on to use Handful from your phone. Handful opens one extra address on this computer and never listens on your network.",
    "Cloudflare Tunnel. You need a Cloudflare account and a domain on Cloudflare.",
    "1. Create a tunnel for http://127.0.0.1:7778 and give it a hostname, for example phone.handful.dev.",
    "2. Add an Access application for that hostname, with a policy that allows only your email. Copy its AUD tag and your team domain into the fields below.",
    "3. Your phone address is https://phone.handful.dev Copy. Open it on your phone, sign in, and scan the pairing code.",
    "Cloudflare can read everything that passes through it: your chats, what bots print and your approvals. Pick this way only if you accept that.",
    "The address is on the public internet. Cloudflare's login and Handful's own pairing both protect it. Handful refuses any request without a valid Cloudflare login.",
    "The name becomes public. Its certificate is listed in public Certificate Transparency logs, so anyone can see that the name exists. They still can't open it without your Cloudflare login.",
    "Check the policy: if you ever see the pairing page without a Cloudflare login, turn phone access off.",
    "Not allowed: quick tunnels (trycloudflare.com), which have no login in front.",
  ])
    assert.include(t, line)
  assert.isBelow(
    t.indexOf("Not allowed:"),
    t.indexOf("Turn on", t.indexOf("Not allowed:")),
    "the warnings come before Turn on",
  )
})

test("P2b: the Tailscale steps and warnings, with the phone address's port", () => {
  const t = text(
    renderToStaticMarkup(
      <TurnOn phone={{ ...off, proxy: "tailscale", hosts: ["mac.tail1.ts.net"] }} onDone={() => {}} />,
    ),
  )
  for (const line of [
    "1. Put Tailscale on this computer and your phone and sign in to the same tailnet.",
    "2. Share Handful's phone address. On this computer, run tailscale serve --bg 7778 in Terminal. Copy command",
    "3. Your phone address is https://mac.tail1.ts.net Copy.",
    "The machine name becomes public. Tailscale's certificate for this name is listed in public Certificate Transparency logs, so anyone can see that the name exists. They still can't open it without being on your tailnet and paired.",
    "Don't use tailscale funnel. It puts this address on the public internet.",
    "Everyone on your tailnet can reach the pairing page, but can't do anything without a paired phone. If your tailnet is shared (a company, a family), use a tailnet of your own for this.",
  ])
    assert.include(t, line)
  assert.notInclude(t, "AUD tag", "Tailscale needs no Access fields")
})

test("P3: the scopes, with Chat and approve once chosen, and what a phone can do", () => {
  const html = renderToStaticMarkup(<Pairing phone={{ ...off, on: true }} onChange={() => {}} />)
  const t = text(html)
  for (const line of [
    "What this phone may do:",
    "View — only read.",
    "Chat — read and send messages. It can't approve a bot's requests.",
    "Chat and approve once — also Allow once or Deny a bot's request.",
    `A phone can send messages to your bots. A bot can then use any tools you've already always-allowed for it. A phone can never choose "Always allow" or change settings.`,
  ])
    assert.include(t, line)
  assert.match(html, /<input type="radio"[^>]*name="phone-scope" checked=""\/><span><strong>Chat and approve once/)
  assert.include(t, "Pair a phone")
})

test("P6: while pairing is paused, the computer says why and offers no code", () => {
  const t = text(
    renderToStaticMarkup(<Pairing phone={{ ...off, on: true, pairing: { lockedUntil: 1 } }} onChange={() => {}} />),
  )
  assert.include(t, "5 wrong pairing codes were tried. Pairing is paused for 10 minutes.")
  assert.include(t, "Pair again later")
  assert.notInclude(t, "What this phone may do:")
})

test("P4: a pair request names the phone and its scope, with Allow, Deny and the time left", () => {
  const request = { id: "r", name: "Pixel 8", scope: "chat+approve" as const, expiresAt: Date.now() + 112_500 }
  const t = text(renderToStaticMarkup(<PairRequestLine request={request} onDecided={() => {}} />))
  assert.match(t, /^Pixel 8 wants to pair as Chat and approve once\.AllowDeny1:5[23] left$/)
})

test("an ended pair request shows nothing", () => {
  const request = { id: "r", name: "Pixel 8", scope: "chat" as const, expiresAt: Date.now() - 1 }
  assert.equal(renderToStaticMarkup(<PairRequestLine request={request} onDecided={() => {}} />), "")
})

test("P4b: the pair page asks for the code, filled from the link or empty, and the phone's name", () => {
  vi.stubGlobal("location", { hash: "#K7QM2XPD9WTR", pathname: "/pair" })
  let html = renderToStaticMarkup(<PairPage paired={false} />)
  assert.include(text(html), "Enter the pairing code shown on your computer.")
  assert.match(html, /id="pair-code"[^>]*value="K7QM2XPD9WTR"/)
  assert.include(html, 'id="pair-name"')
  vi.stubGlobal("location", { hash: "", pathname: "/pair" })
  html = renderToStaticMarkup(<PairPage paired={false} />)
  assert.match(html, /id="pair-code"[^>]*value=""/)
  assert.notInclude(text(html), "This phone is paired.")
  assert.include(text(renderToStaticMarkup(<PairPage paired />)), "This phone is paired.")
})
