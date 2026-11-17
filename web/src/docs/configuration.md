# Configuration

After this page, you can set the defaults for every bot, find each file that Handful keeps, and use Handful's key from a script.

Handful has three places for settings:

- The Settings page, for most of them. Click **Settings** at the bottom of the sidebar, or press ⌘, in the Mac app.
- `~/.handful/config.json`, the file that the Settings page reads and writes. Editing it by hand still works.
- Command-line flags, when you start the server yourself. A flag wins over the file.

## The Settings page

| Section | What it holds |
|---|---|
| **General** | **Default model**, **Default effort**, **Turns at once**, and under **Advanced**, **Claude Code program**. |
| **Appearance** | Theme, chat width and text size, for this browser only. See [The window](/docs/window/#appearance). |
| **Updates** | How the Mac app updates itself. See [The Mac app](/docs/mac-app/#updates). |
| **Connectors** | **Manage connectors…**. See [Connectors](/docs/connectors/). |
| **Phones** | Phone access and the paired phones. See [Phone access](/docs/phone/). |
| **Runtime** | The values that flags set when the server starts: the address, the data folder, the log file, the login-shell environment and the menu-bar icon. Each one shows where it comes from: the default, a flag or the Mac app. Read only. |
| **Settings file** | Where `config.json` is, with **Copy path**. |

A dot next to a label means that the value differs from its default. The values in **General** apply after a restart. Then a banner says what waits, with **Restart server…** (see [The Mac app](/docs/mac-app/#restart-the-server)). When you run the server yourself, stop it and start it again.

A value that a flag sets shows "Set by -effort", for example, and you cannot change it on the page. Remove the flag to use the page's value.

## Turns at once

**Turns at once** is how many bot turns can run at the same time, across all bots. The default is 3. The Settings page takes 1 to 20; a higher value only in `config.json`.

- More turns at once use more of your Claude usage at once.
- A turn that does not fit waits for a free one.
- The board turns of all teams together use at most one less than this value, and at least 1. So when the value is above 1, your other bots still answer while teams work.

## Model and effort

Each bot can have its own model and reasoning effort. Set them in the bot's **Settings**, in the **New bot** dialog, or ask Builder. A team can give its members defaults, in the team's **Settings** under **Member defaults**.

Handful finds the model and the effort separately, in this order:

1. the bot's own value;
2. in a team's chat, the team's default;
3. **Default model** or **Default effort** in Settings, or the `-model` or `-effort` flag;
4. the environment, `ANTHROPIC_MODEL` or `CLAUDE_CODE_EFFORT_LEVEL`;
5. otherwise Claude Code's own default.

The models are the aliases `fable`, `opus`, `sonnet` and `haiku`, or a full model ID. An alias always means the newest model of its family. The efforts are `low`, `medium`, `high`, `xhigh` and `max`.

- The chat header shows a bot's model, and a team's defaults. A team's or room's **Members** tab shows each member's values and where they come from. Point at a bot's name on a message to see the model and effort that wrote it, for example "Written with Opus · effort high".
- The effort that you see is the one that Handful asked for. Claude Code does not report the level that it used. A model that does not support a level runs at the nearest lower one.
- A change applies from the bot's next turn. A change of model makes that turn slower and costlier. The bot's **Settings** says so under the fields.
- When Claude Code cannot run a model, the chat says so, names where the value comes from and links to Settings. The turn is not retried.
- A bot cannot change its own model or effort.
- When Handful sets an effort, it removes `CLAUDE_CODE_EFFORT_LEVEL` from the bot's run, because that variable would win. Handful always removes the `CLAUDE_CODE_SUBAGENT_*` variables, so a bot's helper subagents use the bot's model and effort.

## Global instructions

Put instructions for every bot in `~/.handful/AGENTS.md` or `~/.handful/CLAUDE.md`, for example:

```markdown
- Never add AI attribution to commits or pull requests.
- Ask me before any merge.
```

- A bot's own instructions win over this file.
- Every bot can read the file, so keep tokens out of it.

Claude Code loads the file itself, because every bot works in a folder inside `~/.handful`. Claude Code loads these instruction files for a bot:

1. `CLAUDE.md`, `CLAUDE.local.md`, `AGENTS.md` and the rules in `.claude/rules/` of each folder from `~/.handful` down to the bot's working folder. In a bot's own chat and in a room, that folder is `~/.handful/bots/<bot id>/workspace`. In a team's chat, it is `~/.handful/teams/<team id>/workspace`, so a team's `AGENTS.md` there applies only in that team.
2. `CLAUDE.md` and the rules in `rules/` of the bot's own Claude Code folder, `~/.handful/bots/<bot id>/claude/`. They apply only to that bot.

- An `AGENTS.md` file loads only when no `CLAUDE.md` file is on the way from `~/.handful` to the working folder. With `~/.handful/CLAUDE.md`, a team's `AGENTS.md` does not load. To load both kinds, put `{"pluginConfigs": {"cc-plugin-agents-md@builtin": {"options": {"instructionFiles": "claude-md-and-agents-md"}}}}` in `settings.json` of the bot's Claude Code folder.
- To give one bot a file from elsewhere, import it in the bot's own `CLAUDE.md` with a line of `@` and the file's full path, for example `@/Users/me/.handful/rules/review.md`. An import in a file in `~/.handful` of a file outside the bot's working folder does not load.
- A change applies from each bot's next turn.
- The `CLAUDE.md` and `AGENTS.md` files and rules in your home folder, in `~/.claude` and in the folders above `~/.handful` do not reach bots.

## config.json

```json
{"model": "sonnet", "effort": "high", "maxParallel": 4}
```

| Key | Default | Meaning |
|---|---|---|
| `model` | Claude Code's default | **Default model** |
| `effort` | the model's own | **Default effort** |
| `maxParallel` | `3` | **Turns at once** |
| `claude` | `claude` | **Claude Code program**: the `claude` command that Handful runs for every turn. Change it only if Claude Code is not on your `PATH`. |
| `updates.autoCheck` | `true` | The Mac app checks for updates once a day. |
| `updates.drainMinutes` | `30` | How long an update or a restart waits for running turns, 5 to 240. |
| `updateCheck` | `true` | A server that you built from source checks GitHub once a day and shows a notice of a new release. It never updates itself. `HANDFUL_NO_UPDATE_CHECK=1` turns the check off too. |
| `phone` | off | See [Phone access](/docs/phone/#the-settings-file). |

- A change by hand to `model`, `effort`, `maxParallel`, `claude`, `updateCheck` or a `phone` key applies after a restart. The Settings page shows the keys that changed on disk. The `updates` keys apply at once.
- If the file is invalid, or has a key that Handful does not know, Handful does not start, and the error names the file.

## Phone access

Phone access lets you read and answer your bots from your phone. It is off by default. See [Phone access](/docs/phone/).

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `127.0.0.1:7777` | The address to listen on. Handful refuses an address that is not loopback. |
| `-data` | `~/.handful` | The data folder. |
| `-claude` | `claude` | The Claude Code program to run. |
| `-model` | Claude Code's default | The model for every bot without its own. |
| `-effort` | the model's own | The reasoning effort for every bot without its own. |
| `-max-parallel` | `3` | Turns that can run at the same time, across all bots. |
| `-login-env` | off | Take the environment, such as `PATH`, from your login shell. The Mac app uses it, because macOS starts the server with a minimal `PATH`. |
| `-tray` | off | Show the menu-bar icon, with **Check for updates…** and **Exit server**. The Mac app uses it. |
| `-log` | none | Append the log and all other error output to this file. `~/` is your home folder. Without it, they go to stderr. |

## The data folder

| Path in `~/.handful` | What it holds |
|---|---|
| The SQLite database | Bots, chats, memory, routines, teams, tasks and artifacts. |
| `bots/<bot id>/workspace/` | Each bot's working folder. |
| `bots/<bot id>/claude/` | Each bot's own Claude Code folder: its own skills in `skills/`, and the sessions and settings of Claude Code for this bot. Handful makes it at the bot's next turn. |
| `teams/<team id>/workspace/` | Each team's shared folder. |
| `skills/` | The skills that all bots share. |
| `media/` | The images and videos of messages. |
| `AGENTS.md` or `CLAUDE.md`, `connectors.json`, `config.json` | Your settings. Each one is optional. |
| `ui-key` | Handful's key (below). |
| `oauth/` | Connector sign-in tokens. |
| `run/` | The MCP configs of running bots, with their tokens for one run. |

## Handful's key

Every change through Handful's API needs Handful's key: any request under `/api/` other than GET, HEAD or OPTIONS, except the one that saves the key in the browser. Without the key, the request gets 401 with `{"error": "This needs Handful's key."}`. A read needs no key. A paired phone uses its own session instead.

- The key is the file `ui-key` in the data folder. Handful makes it at its first start, readable by you only, and keeps it across restarts.
- The Mac app gives its window the key, so you never see it.
- A browser on this computer asks for the key once. Run `cat ~/.handful/ui-key` in a terminal, paste the key, and click **Save key**. The browser keeps it in a cookie.
- A script sends it in the `X-Handful-Key` header, for example `curl -H "X-Handful-Key: $(cat ~/.handful/ui-key)" …`.
- To change the key, delete the file and restart Handful. Every browser then asks for the new key.

The key stops code that does not know about it, such as a plain request that a prompt slipped into a bot's command. It is not a wall between bots and Handful. Claude Code's file tools cannot read the key, the sign-in tokens, `connectors.json` or the database. But bots run as your user, so a command that you allow can read all of them, and can change Handful's database. See [Security and data](/security/).

A bot that you always allow Bash can change its own skills, instructions and settings, and those of other bots, without a card. Only Write and Edit always ask for these files (see [Approve what a bot does](/docs/bots/#approve-what-a-bot-does)).
