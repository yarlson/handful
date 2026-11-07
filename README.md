# YarGPT

YarGPT runs personal AI agents, called bots, on your own computer. Each bot has its own chat, memory, routines and working folder, and keeps them from one conversation to the next. Bots can also work together in group chats.

Each time a bot works, YarGPT runs the [Claude Code](https://code.claude.com) CLI on your machine with your Claude account. YarGPT adds the chats, the memory, the schedules and the approvals around it. There is no cloud service: the server, the database and the bots all run locally.

![A bot's chat in YarGPT: the tools it used, its reply, and its memory in the side panel](assets/screenshot.png)

## Requirements

- macOS or Linux.
- Claude Code CLI 2.1.319 or later, signed in, with `claude` on your `PATH`.
- [mise](https://mise.jdx.dev). It installs the Go and Node.js versions that build YarGPT.

## Install and start

```sh
git clone https://github.com/yarlson/yargpt.git
cd yargpt
mise install
make build
./bin/yargpt
```

Open <http://127.0.0.1:7777>. YarGPT keeps all its data in `~/.yargpt`.

## Use YarGPT

### Create a bot

1. Click **+** next to **Bots**.
2. Give the bot a name and instructions, for example "Research assistant. Keep answers short."
3. The bot starts and introduces itself in its chat.

Each bot works in its own folder, `~/.yargpt/bots/<bot id>`. The bot id is the last part of the chat address, `/chat/<bot id>`.

### Talk to a bot

- Write a message and send it. While the bot works, the chat shows each tool that it uses. Then the bot answers.
- To send images or videos, paste them into the message box, drop them on the chat, or click the paperclip. Bots can send you images and videos too.
  - A message can carry up to 4 files, 100 MB in total.
  - Images: PNG, JPEG, GIF or WebP, up to 10 MB each.
  - Videos: MP4, WebM or QuickTime, up to 50 MB each.
- A bot can start a long command in the background and keep talking to you. The chat lists the running tasks above the message box. **Stop** ends them.
- Type `/` at the start of a message for commands:
  - `/clear` starts a fresh conversation context. The chat stays visible.
  - `/compact` shortens the context and shows the token counts before and after.
  - `/<skill> <request>` runs a skill now (see [Skills](#skills)).

### Approve what a bot does

A bot reads files and searches the web without asking. Before it runs a command, changes a file, or calls a connector tool, it asks you with an approval card:

- **Allow once** lets this one call run.
- **Always allow &lt;tool&gt;** lets this bot use the tool without asking. To remove this permission, open the bot panel, then **Settings**, then **Always allowed tools**.
- **Deny** stops the call.

A card that you do not answer expires after 10 minutes, or when the bot stops working.

In the sidebar, a shield marks a bot that waits for an approval. A question bubble marks a bot that asked you something.

### Memory

Bots remember facts across conversations. To see and edit them, open the bot panel with the button at the top right, then **Memory**.

- When you add a fact with **Always in mind**, the bot sees it on every turn. Without it, the fact goes to the bot's journal. The bot searches the journal when it needs to.
- **Shared with all bots** gives the fact to every bot, not only this one.
- Bots also save facts on their own. They can also save short-term notes, which expire after 7 days.
- Each fact shows who wrote it. A bot treats a standing rule as yours only when the fact is from you.

### Routines

Ask a bot for anything that repeats, for example "every weekday at 8:10, brief me on my calendar". The bot saves a routine: a prompt and a schedule in your local time. Two runs must be at least 5 minutes apart.

The **Routines** tab in the bot panel shows each routine and its next run. There you can pause it, run it now or delete it. When you change a routine, the bot gets a notice.

### Skills

A skill is a saved procedure that every bot can use. When you finish a process that you will repeat, a bot can save it as a skill.

Skills are `SKILL.md` files in `~/.yargpt/skills`. The **Skills** tab lists them. To run a skill now, type `/<skill> <request>` in a bot's chat.

### Rooms

A room is a group chat with you and several bots.

To create one, click **+** next to **Rooms**, then give the room a name and choose its members.

- Your message wakes the members that it @mentions. A message without mentions wakes all members. `@here` mentions every member. Type `@` to complete a name.
- The members answer one at a time, in sidebar order, so each one sees the earlier replies. A member that is busy elsewhere answers when it is free.
- A bot's message in a room wakes only the bots that it @mentions.

So bots can hand work to each other. In this room, four bots ship a change, and each one @mentions the next:

![A room in dark mode: Implementer, Reviewer, Fixer and Merger ship a change by mentioning each other](assets/screenshot-dev-dark.png)

### Bots that message each other

A bot can send a message to another bot. An urgent message wakes the other bot at once. Any other message waits until that bot's next turn. Both chats show the message.

After 6 bot-to-bot messages in a row, the next message wakes no one. This stops bots from keeping each other busy forever.

### Global instructions

Put instructions for every bot in `~/.yargpt/AGENTS.md`, for example:

```markdown
- Never add AI attribution to commits or pull requests.
- Ask me before any merge.
```

- A bot's own instructions win over this file.
- A change reaches a bot when it next starts a Claude Code process.
- Every bot can read the file, so keep tokens out of it.
- A file over 32 KiB stops every bot's turns until you shorten it.

### Connectors

A connector gives bots the tools of an external MCP server, such as Slack or a wiki. List the servers in `~/.yargpt/connectors.json`:

```json
{
  "servers": {
    "wiki": {
      "url": "https://wiki.example/mcp",
      "bots": ["<bot id>"]
    },
    "search": {
      "url": "https://search.example/mcp",
      "headers": {"Authorization": "Bearer <token>"},
      "bots": ["<bot id>"]
    }
  }
}
```

1. Make the file private: `chmod 600 ~/.yargpt/connectors.json`. The file can hold tokens, so YarGPT does not read it otherwise.
2. Sign in to each server that has no `headers`: click **Connectors** at the bottom of the sidebar, then **Connect**, and sign in on the page that opens. YarGPT uses OAuth 2.1, with dynamic client registration and PKCE.

Rules:

- A server with `headers` gets them on each call. For a server that needs no sign-in, set `"headers": {}`.
- A server name uses only `a-z`, `0-9` and `-`. The name `yargpt` is reserved. A bot calls the server's tools as `mcp__<name>__<tool>`.
- The `url` must use https. Plain http is allowed only for `localhost`, `127.0.0.1` and `::1`.
- `bots` lists the bots that load the server. It does not hide the tokens from other bots (see [Security](#security)).
- Connector tools go through approval cards. **Always allow** applies to one tool, not to the whole server.
- YarGPT reads the file each time a bot starts a Claude Code process, so a change needs no restart. An invalid file fails the bot's turn with an error that names the problem.
- YarGPT keeps sign-in tokens in `~/.yargpt/oauth/<name>.json`, with mode 0600, and refreshes them when they are about to expire. **Disconnect** deletes them.
- When a server is not signed in, the bot works without it, and the chat says that the server needs sign-in.
- If you change `-addr`, connect again: the sign-in address contains the port.

A server without dynamic client registration needs a client that you register yourself:

```json
"oauth": {"clientId": "<id>", "clientSecret": "<secret, if any>", "scopes": ["<scope>"], "callbackPort": 3118}
```

All `oauth` fields are optional. With `callbackPort`, the sign-in returns to `http://localhost:<callbackPort>/callback`, as in Claude Code's `.mcp.json`.

For example, Slack's MCP server works with Slack's public client for Claude Code ([Slack docs](https://docs.slack.dev/ai/slack-mcp-server/connect-to-claude)). A Slack workspace admin must approve it first:

```json
"slack": {
  "url": "https://mcp.slack.com/mcp",
  "oauth": {"clientId": "1601185624273.8899143856786", "callbackPort": 3118},
  "bots": ["<bot id>"]
}
```

## Configuration

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `127.0.0.1:7777` | Listen address. YarGPT refuses an address that is not loopback. |
| `-data` | `~/.yargpt` | Data folder |
| `-claude` | `claude` | Claude Code CLI to run |
| `-model` | Claude Code's default | Model alias for every bot, for example `sonnet` |
| `-max-parallel` | `3` | Turns that can run at the same time, across all bots |
| `-login-env` | off | Take the environment, such as `PATH`, from your login shell. The macOS app uses it, because launchd starts the server with a minimal `PATH`. |
| `-log` | none | Append the log and all other error output to this file. `~/` is your home folder. Without it, they go to stderr. |

To set these without flags, put `claude`, `model` or `maxParallel` in `~/.yargpt/config.json`, for example `{"model": "sonnet", "maxParallel": 4}`.

- A flag wins over the file.
- YarGPT reads the file only when it starts, so a change needs a restart.
- If the file is invalid, YarGPT does not start, and the error names the file.

The data folder holds:

| Path | Content |
|---|---|
| `yargpt.db` | Bots, chats, memory, routines (SQLite) |
| `bots/<bot id>/` | Each bot's working folder |
| `skills/` | Skills that all bots share |
| `media/` | Images and videos of messages |
| `AGENTS.md`, `connectors.json`, `config.json` | Your optional settings |
| `oauth/` | Connector sign-in tokens |
| `run/` | MCP configs of running bots, with their per-turn tokens |

## Good to know

- Your own Claude Code settings, hooks, `CLAUDE.md` files and MCP servers do not reach the bots. Use `AGENTS.md` and `connectors.json` instead.
- When a turn reaches your Claude account's usage limit, YarGPT starts no new turns until the limit resets. A banner shows when work resumes. Turns that are already running continue.
- After a restart, turns that were running are marked interrupted, open approval cards expire, and background tasks are gone.
- The UI follows the light or dark mode of your system.

## Security

YarGPT has no login. It is safe only on a computer that one person uses, and only because of these limits:

- It listens on loopback only.
- It rejects requests whose `Host` is not loopback, to block DNS rebinding.
- It rejects requests from another web origin, to block cross-site request forgery.
- It rejects API POST requests whose body is not JSON.

Bots run as your user. An approved command can do anything that you can do, so read each approval card before you allow it.

Bots are not isolated from each other. Reading files needs no approval and has no path limit, so any bot can read `AGENTS.md`, `connectors.json`, the tokens in `oauth/`, and the per-turn tokens of other bots' running processes in `run/`.

## Not built yet

Routines that start on events (only schedules work now), per-bot Claude Code plugins, a vault for secrets, voice, and bots that run on other machines.

## Development

### Build and work on the UI

`make build` builds the UI into `web/dist` and embeds it in `bin/yargpt`. To work on the UI, start `./bin/yargpt`, then run `cd web && npm run dev`. Vite sends `/api` requests to port 7777.

`mise.toml` pins Go, Node.js and every check tool, and the Makefile runs each tool through `mise exec`. Coding agents get their working rules from `AGENTS.md`.

### Checks

```sh
make test    # Go tests, the TypeScript type check and the UI unit tests
make check   # every check of CI's check job
make fmt     # format Go and UI code
```

The agent tests replace Claude Code with a fake CLI script and run a real MCP client against the tool endpoint.

`make check` runs these targets:

| Target | Fails when |
|---|---|
| `fmt-check` | Go code is not formatted with gofumpt and goimports |
| `lint` | golangci-lint, configured in `.golangci.yml`, reports an issue |
| `generate-check` | The sqlc code in `internal/db` differs from what `sqlc generate` writes |
| `mod-check` | `go.mod` or `go.sum` is not tidy, or a module does not match `go.sum` |
| `race` | A Go test fails, or the race detector finds a data race |
| `vuln` | govulncheck finds a known vulnerability in code that YarGPT calls |
| `web-check` | The UI is not formatted with oxfmt, oxlint reports an error, the type check or a vitest test fails, or the build fails. oxlint warnings do not fail it. |
| `workflow-check` | actionlint finds a problem in a GitHub Actions workflow |

Slower analyses have their own targets. They write their reports to `target/`.

| Target | What it does |
|---|---|
| `mutation-diff` | Mutation-tests, with gremlins, the Go lines changed since `BASE` (default `origin/main`). It fails when a mutant on a changed line survives the tests, or when no test runs that line. Git must track new files, for example with `git add -N`. CI runs it on each pull request. |
| `mutation` | Mutation-tests all Go code, in about 8 minutes on 10 cores. It fails below 81 % efficacy or 85 % mutant coverage. CI does not run it. `cmd/yargpt` is not mutation-tested: gremlins 0.6.0 cannot map its coverage. |
| `crap` | Prints the CRAP score of each hand-written Go function: its complexity combined with its test coverage. Higher means riskier to change. |
| `web-mutation` | Mutation-tests `web/src/lib` with Stryker and prints the score. |

### How it works

```
browser ──REST + SSE──▶ YarGPT server (Go) ──▶ SQLite (yargpt.db)
                           │  ▲
            spawns per turn│  │MCP over HTTP (bearer token per turn)
                           ▼  │
                      claude -p … (cwd = the bot's workspace)
```

- **Turns.** A message, a routine or a bot's first start becomes a turn. A bot runs one turn at a time, and its turns share one Claude Code session, which each turn resumes. The prompt goes to the `claude` process on stdin as stream-json. Each send carries a `clientNonce`, so a retried send does not start a second turn.
- **Delivery.** The model's plain text is private. The user sees what the bot sends with the `send_to_user` tool. When a turn ends without a message, the chat shows the model's final text, marked as a fallback.
- **Tools.** Each `claude` process connects to YarGPT's MCP endpoint, `/mcp`, with a token that is valid only for its turn. The tools are `send_to_user`, `send_to_agent`, `read_room`, `recall_memory`, `update_memory`, `update_routine`, `update_skill`, `update_profile` and `approve`. Tool calls that need approval reach `approve` through `--permission-prompt-tool`.
- **Isolation.** Workers run with `--setting-sources project`, `--strict-mcp-config`, `CLAUDE_CODE_DISABLE_BUNDLED_SKILLS=1` and `CLAUDE_CODE_DISABLE_CLAUDE_MDS=1`. The built-in tools are limited to Task, Bash, Monitor, TaskStop, Read, Edit, Write, Glob, Grep, WebSearch and Skill. YarGPT writes each process's MCP config to a 0600 file in `<data>/run`, so no token shows in the process list, and removes the file when the process exits.
- **Transcript.** Each chat is an ordered log of `user`, `bot`, `event`, `activity` and `approval` entries. Each change gets a new `rev`. Clients catch up with `GET /api/bots/{id}/entries?after=<rev>` and the `GET /api/events` stream.
- **Schema.** The migrations in `internal/db/migrations` run in order at start, each in one transaction with its `PRAGMA user_version` update.

### Layout

| Path | Content |
|---|---|
| `cmd/yargpt` | Server entry point and flags |
| `internal/db` | Schema, sqlc queries and generated code (`modernc.org/sqlite`, pure Go) |
| `internal/core` | Domain state: bots, transcript, turns, memory, routines, skills, approvals, event hub |
| `internal/agent` | Claude Code runner, stream-json parsing, system prompt and MCP tools |
| `internal/httpapi` | REST API, event stream, origin guard and UI hosting |
| `web` | React 19, TypeScript 7, Tailwind v4 and shadcn/ui app, embedded through `web/embed.go` |

## License

[MIT](LICENSE)
