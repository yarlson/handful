# Configuration

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `127.0.0.1:7777` | Listen address. Handful refuses an address that is not loopback. |
| `-data` | `~/.handful` | Data folder. Without it, the first start moves an older data folder here. |
| `-claude` | `claude` | Claude Code CLI to run |
| `-model` | Claude Code's default | Model for every bot without its own: an alias (`fable`, `opus`, `sonnet`, `haiku`) or a full model ID |
| `-effort` | the model's own | Reasoning effort for every bot without its own: `low`, `medium`, `high`, `xhigh` or `max` |
| `-max-parallel` | `3` | Turns that can run at the same time, across all bots |
| `-login-env` | off | Take the environment, such as `PATH`, from your login shell. The macOS app uses it, because launchd starts the server with a minimal `PATH`. |
| `-tray` | off | Show an icon in the macOS menu bar with two items: **Check for updates…**, which opens the app and checks for a new version, and **Exit server**, which stops the server. The macOS app uses it. |
| `-log` | none | Append the log and all other error output to this file. `~/` is your home folder. Without it, they go to stderr. |
| `-move-back` | off | Go back to the data folder as it was before the move: say what would be lost, then stop. |
| `-yes` | off | With `-move-back`, go back. |

Moving from YarGPT? See [Moving from YarGPT](moving-from-yargpt.md).

To set these without flags, put `claude`, `model`, `effort` or `maxParallel` in `~/.handful/config.json`, for example `{"model": "sonnet", "effort": "high", "maxParallel": 4}`.

- A flag wins over the file.
- Handful reads the file only when it starts, so a change needs a restart.
- If the file is invalid, Handful does not start, and the error names the file.

The data folder holds:

| Path | Content |
|---|---|
| The SQLite database | Bots, chats, memory, routines, artifacts |
| `bots/<bot id>/` | Each bot's working folder |
| `teams/<team id>/` | Each team's shared folder |
| `skills/` | Skills that all bots share |
| `media/` | Images and videos of messages |
| `AGENTS.md`, `connectors.json`, `config.json` | Your optional settings |
| `ui-key` | Handful's key, which every change through the API needs (mode 0600) |
| `oauth/` | Connector sign-in tokens |
| `run/` | MCP configs of running bots, with their per-turn tokens |

## Model and effort

Each bot can have its own model and reasoning effort: set them in the bot's **Settings**, in the **New bot** dialog, or ask Builder. A team can give its members a default model and effort in its **Settings**, under **Member defaults**. Model and effort are resolved separately, in this order:

1. the bot's own value;
2. in a team's chat, the team's default;
3. the global value: the `-model` or `-effort` flag, then `config.json`;
4. otherwise Handful passes nothing, and Claude Code uses its own default.

- **Where you see them.** The chat header shows a bot's model and effort, and a team's defaults. A team's or room's **Members** tab shows each member's values and where they come from. Hover or focus a bot's name on a post to see the model and effort that wrote it. Effort is shown as *requested*, because Claude Code doesn't report the level it applied; a model that doesn't support a level runs at the nearest lower one.
- **When a change applies.** From the bot's next turn. Switching models makes that turn slower and costlier, because the prompt cache starts over.
- **Problems.** When Claude Code can't run a model, the chat says so, names where the value comes from and links to Settings, and the turn isn't retried. When Claude Code runs another model than the one asked for, the header chip turns amber and the chat notes it once. A bot can't change its own model or effort.
- **Environment.** When Handful sets an effort, it removes `CLAUDE_CODE_EFFORT_LEVEL` from the bot's run, since that variable would outrank it; with no effort in Handful, the variable applies. `ANTHROPIC_MODEL` stays, because `--model` outranks it. Handful always removes `CLAUDE_CODE_SUBAGENT_MODEL`, `CLAUDE_CODE_SUBAGENT_EFFORT` and `CLAUDE_CODE_SUBAGENT_MODEL_FORCE`, so a bot's helper subagents use the bot's model and effort.

## Handful's key

Every change through Handful's API (any request under `/api/` other than GET) needs Handful's key; without it, the request gets 401 with `{"error": "This needs Handful's key."}`. Reads need no key, and neither does a paired phone, which has its own session.

- **Where it is:** the file `ui-key` in the data folder, so `~/.handful/ui-key`, or `<dir>/ui-key` with `-data <dir>`. Handful makes it at its first start, readable by you only (mode 0600), and keeps it across restarts. A file that others can read is narrowed to 0600 at the next start.
- **Handful.app** reads the file and gives its window the key, so you never see it.
- **A browser on the computer** shows a page that asks for the key once: run `cat ~/.handful/ui-key` in a terminal (the page shows the path in use), and paste the key. The browser keeps it in a cookie.
- **A script** sends it as the `X-Handful-Key` header, for example `curl -H "X-Handful-Key: $(cat ~/.handful/ui-key)" …`.
- **To rotate it,** delete the file and restart Handful. Every browser then asks for the new key.

The key stops only code that doesn't know about it, such as a plain request that a prompt slipped into a bot's command. Bots run as your user, so a bot can read the key file, and so can any script that an approved command runs, without showing on any card. An approved command can also change Handful's database directly. Bots are not isolated from each other or from Handful's files.

## Global instructions

Put instructions for every bot in `~/.handful/AGENTS.md`, for example:

```markdown
- Never add AI attribution to commits or pull requests.
- Ask me before any merge.
```

- A bot's own instructions win over this file.
- A change reaches a bot when it next starts a Claude Code process.
- Every bot can read the file, so keep tokens out of it.
- A file over 32 KiB stops every bot's turns until you shorten it.

## Phone access

Phone access lets you use Handful from your phone. It is **off by default**, and with it off Handful opens no other port.

Turn it on in **Settings › Phones**: pick the way in (a Cloudflare Tunnel or Tailscale), follow its setup text, fill in the phone address (and, for a tunnel, the Access team domain and AUD tag), and choose **Turn on**. The phone address opens at once, without a restart, and the settings are saved in `config.json`. **Turn off phone access** on the same page closes it again.

To pair a phone:

1. On the computer, choose **Pair a phone**, and what the phone may do. Settings shows a QR code and the same code as text, such as `K7QM-2XPD-9WTR`. The code works once and ends after 5 minutes.
2. On the phone, open the phone address, scan the QR code or type the code (in any case, with or without dashes), and name the phone.
3. On the computer, choose **Allow** in the request that shows at the top of every screen, within 2 minutes. The phone is paired.

After 5 wrong codes in 10 minutes, pairing pauses for 10 minutes. The Phones list on the same page shows each paired phone, with **Revoke**, and **Revoke all**.

The settings can also be written in `config.json` by hand; then they apply after a restart:

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
| `phone.enabled` | `false` | Open the phone address |
| `phone.addr` | `127.0.0.1:7778` | The phone address. It must be loopback. |
| `phone.hosts` | none | The names your phone opens, 1 to 10. Not `localhost`, and not a quick tunnel (`trycloudflare.com`), which has no login in front. |
| `phone.proxy` | `cloudflare` | `cloudflare` for a Cloudflare Tunnel with a Cloudflare Access login in front, or `tailscale` for `tailscale serve` |
| `phone.cloudflare.teamDomain` | none | The team domain of the Access login. Needed for `cloudflare`. |
| `phone.cloudflare.aud` | none | The AUD tag of the Access application, from its Overview page. Needed for `cloudflare`. |

If a phone key is invalid, or phone access is on and a key it needs is missing, Handful does not start, and the error names the key.

- **A second listener.** A proxy on this computer forwards your phone's requests to the phone address. Handful still never listens on your network itself.
- **Every request needs a paired phone.** Only the web app's own files and pairing work without one. A phone is paired for one of three scopes: **View** (read only), **Chat** (also send messages and act on tasks), or **Chat and approve once** (also Allow once and Deny on approval cards). A phone shows only the controls its scope may use.
- **A phone never chooses Always allow**, and never allows a setup that creates bots or teams. Settings, bots, teams, routines, memory, skills, connectors and phone access itself stay on the computer.
- **A phone paired for Chat can message your bots.** A bot can then use any tool you have already always-allowed for it, with no approval card.
- **With a Cloudflare Tunnel, Cloudflare sees the traffic**: your chats, what bots print and your approvals. Handful also checks the Cloudflare Access login on every request, so the public address answers nothing without it.
- **A decision from a phone** shows on the approval card with the phone's name, such as "Approved once from Pixel 8."
- **Revoking a phone** makes its next request fail and ends its open streams within 15 seconds. **Turning phone access off** closes the phone address and its streams at once; paired phones stay paired.
- **The desktop app** hears of a waiting pair request through the page's `handful:pair-request` event, whose detail is 1 while a request waits and 0 after.
