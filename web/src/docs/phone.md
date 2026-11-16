# Phone access

After this page, you can read your chats, message your bots and answer their approval cards from your phone, and you know what that opens up.

Phone access is off by default. While it is off, Handful opens no other port. Your computer must be on, with Handful running, for the phone to work.

## How it works

Handful never listens on your network itself. When you turn on phone access, it opens a second address on this computer, the phone address, `127.0.0.1:7778` by default. A proxy that you run on this computer forwards your phone's requests to it. Handful supports two:

- A Cloudflare Tunnel, with a Cloudflare Access login in front. You need a Cloudflare account and a domain on Cloudflare. Handful checks the Access login on every request, so the public address answers nothing without it. Cloudflare sees the traffic: your chats, what bots print and your approvals.
- Tailscale, with `tailscale serve`. Your phone and this computer must be on the same tailnet. The machine name becomes public in certificate logs, but nobody can open it without being on your tailnet and paired. Never use `tailscale funnel`: it puts the address on the public internet.

Every request through the phone address needs a paired phone. Only the web app's own files and pairing work without one.

## Turn it on

1. Open **Settings**, then **Phones**.
2. Under "The way in", choose **Cloudflare Tunnel** or **Tailscale**, and follow its setup text.
3. Fill in the **Phone address** that your phone opens. For a tunnel, also fill in the Access **Team domain** and **AUD tag**.
4. Click **Turn on**.

The phone address opens at once, with no restart. The settings are saved in `config.json`.

## Pair a phone

1. On the computer, click **Pair a phone**, and choose what the phone may do (below). Settings shows a QR code and the same code as text, such as `K7QM-2XPD-9WTR`. The code works once and ends after 5 minutes.
2. On the phone, scan the QR code. It opens the pairing page on the phone address, with the code filled in. Or open the phone address, tap **Pair again** under "This phone isn't paired with Handful.", and type the code in **Pairing code**, in any case, with or without dashes. Give the phone a name, and tap **Pair**.
3. On the computer, a request "\<name> wants to pair as \<scope>." shows at the top of the window, and first in **Needs you**. Click **Allow** within 2 minutes. The phone is paired.

After 5 wrong codes in 10 minutes, pairing pauses for 10 minutes.

## What a phone may do

You choose one of three scopes when you pair:

| Scope | What the phone can do |
|---|---|
| **View** | Read chats, the Work tab, **Needs you**, questions and artifacts. |
| **Chat** | Also send messages, react, stop a turn, answer questions, and act on tasks: answer, approve, ask for changes, dismiss. Also **Wait until midnight** on a team whose budget is used up, and **Dismiss** on the note about turns that were on no task. |
| **Chat and approve once** | Also **Allow once** and **Deny** on approval cards. This is the default. |

A phone shows only the controls that its scope may use.

What a phone never does:

- choose "Always allow \<tool>” on a card;
- create a setup from Builder;
- change settings, bots, teams, routines, memory, skills, connectors, or phone access itself. Those stay on the computer.

A decision from a phone shows on the approval card with the phone's name, for example "Approved once from Pixel 8."

## Risks to know

- A phone that can chat can message your bots. A bot can then use any tool that you already always allowed for it, with no approval card. Before you pair a phone for **Chat**, check each bot's **Always allowed tools**.
- With a Cloudflare Tunnel, Cloudflare sees your chats and approvals.
- Anyone who holds your unlocked, paired phone can do what its scope allows.

## The phone layout

On a phone, the sidebar is a drawer and the panel fills the screen. A team's panel shows only **Work**, and a bot's panel only **Artifacts**. The command palette has the chats, **Waiting for you**, "Settings: Appearance", **Toggle sidebar**, the themes, and, when a chat is open, **Toggle panel** and **Search in this chat**. It has no **New bot**, **New team** or **New room**, and no other Settings sections. The **Appearance** settings work on each phone on its own.

## Revoke a phone

- The **Phones** list in **Settings** shows each paired phone, with **Revoke**, and **Revoke all**.
- A revoked phone's next request fails, and its open streams end within 15 seconds.
- **Turn off phone access** closes the phone address and its streams at once. Paired phones stay paired for the next time you turn it on.

## The settings file

You can also write the settings in `~/.handful/config.json` by hand. Then they apply after a restart.

```json
{
  "phone": {
    "enabled": true,
    "hosts": ["phone.example.com"],
    "proxy": "cloudflare",
    "cloudflare": {"teamDomain": "you.cloudflareaccess.com", "aud": "<the application's AUD tag>"}
  }
}
```

| Key | Default | Meaning |
|---|---|---|
| `phone.enabled` | `false` | Open the phone address. |
| `phone.addr` | `127.0.0.1:7778` | The phone address. It must be loopback. |
| `phone.hosts` | none | The names that your phone opens, 1 to 10. Not `localhost`, and not a quick tunnel (`trycloudflare.com`), which has no login in front. |
| `phone.proxy` | `cloudflare` | `cloudflare` for a Cloudflare Tunnel with a Cloudflare Access login in front, or `tailscale` for `tailscale serve`. |
| `phone.cloudflare.teamDomain` | none | The team domain of the Access login. Needed for `cloudflare`. |
| `phone.cloudflare.aud` | none | The AUD tag of the Access application, from its Overview page. Needed for `cloudflare`. |

If a phone key is invalid, or phone access is on and a key that it needs is missing, Handful does not start, and the error names the key.
