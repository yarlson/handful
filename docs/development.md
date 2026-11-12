# Development

How to build, check and release Handful, and how it works inside.

## Build and work on the UI

`make build` builds the UI into `ui/dist` and embeds it in `bin/handful`. To work on the UI, start `./bin/handful`, then run `cd ui && npm run dev`. Vite sends `/api` requests to port 7777.

`desktop/` holds the macOS app: the Electron window (`main.js`, `app.js`, and `context-menu.js`, which builds the right-click menu and has its own tests), the updater (`updater.js`, `preload.js`, and `updater-state.js` and `updater-io.js`, which have no Electron code and their own tests), the launch agent that runs the server (`build/dev.yarlson.handful.server.plist`), and `manual-move.js`, which stops the old app's server before Handful registers its own, and has its own tests. `make app` builds it for this Mac, and `make app-universal` builds the zip for Apple silicon and Intel Macs. Its server also listens on port 7777, so choose **Exit server** before you start `./bin/handful`. `make icons` regenerates the menu-bar icon from its SVG source and the site's apple-touch-icon; it needs `rsvg-convert` (Homebrew `librsvg`). The app icon, `desktop/build/icon.icns`, is a committed brand asset that no target rebuilds.

`mise.toml` pins Go, Node.js and every check tool, and the Makefile runs them through `mise exec`. `make app-universal` and `make icons` also use macOS tools that mise does not install: `lipo` and `sips`, and `rsvg-convert` from Homebrew `librsvg`. Coding agents get their working rules from `AGENTS.md`.

## Website

`web/` holds the handful.dev website, an Astro site. To work on it, run `cd web && npm ci && npm run dev`, then open <http://localhost:4321>. `npm run build` runs `astro check` and writes the site to `web/dist`.

## Releases

Push a tag such as `v0.2.0` to release; the tag must be a version, such as `v0.2.0` or `v0.2.0-rc.1`. `.github/workflows/release.yml` then runs `make app-universal VERSION=0.2.0` on a macOS runner, which signs the app with a Developer ID and the hardened runtime and has Apple notarize it, and uploads the zip as `Handful-universal-mac.zip` to the release of the tag. When no such release exists, it creates one; a tag with a hyphen, such as `v0.2.0-rc.1`, makes a pre-release. When you publish a release on GitHub, which also pushes its tag, the release keeps your title and notes. handful.dev links `releases/latest/download/Handful-universal-mac.zip`, so it needs no change for a new release. The release also gets `handful-mac.yml` and the zip's blockmap, which the app's updater reads, and the bridge files for the apps from before the rename. [docs/releasing.md](releasing.md) explains the release, the bridge files, staged rollouts, how to stop a bad release, and why the signing certificate must not change.

## Checks

```sh
make test    # Go tests, the TypeScript type check and the UI unit tests
make check   # every check of CI
make fmt     # format Go and UI code
```

The agent tests replace Claude Code with a fake CLI script and run a real MCP client against the tool endpoint.

`make check` runs these targets. CI runs them as six jobs at the same time: `go-check` (the targets from `fmt-check` to `vuln` that check Go code), `race`, `ui-check`, `web-check`, `desktop-check` and `repo-check` (`workflow-check` and `name-check`). The `check` job fails when one of them fails.

CI keeps Go's build and test cache and the npm cache from the last run on main. Go reports a test result from the cache only when the test binary and every repository file and environment variable that the test read are unchanged, so a change that touches no Go code tests nothing again. The weekly run tests everything again.


| Target | Fails when |
|---|---|
| `fmt-check` | Go code is not formatted with gofumpt and goimports |
| `lint` | golangci-lint, configured in `.golangci.yml`, reports an issue |
| `generate-check` | The sqlc code in `internal/db` differs from what `sqlc generate` writes |
| `mod-check` | `go.mod` or `go.sum` is not tidy, or a module does not match `go.sum` |
| `race` | A Go test fails, or the race detector finds a data race |
| `vuln` | govulncheck finds a known vulnerability in code that Handful calls |
| `ui-check` | The UI is not formatted with oxfmt, oxlint reports an error, the type check or a vitest test fails, or the build fails. oxlint warnings do not fail it. |
| `web-check` | `astro check` finds an error in the website, or its build fails |
| `desktop-check` | A unit test of the macOS app, its updater or `desktop/scripts/release-feed.js` fails |
| `workflow-check` | actionlint finds a problem in a GitHub Actions workflow |
| `name-check` | The old name is left in a file outside `scripts/name-allow.txt` |

Slower analyses have their own targets. They write their reports to `target/`.

| Target | What it does |
|---|---|
| `mutation-diff` | Mutation-tests, with gremlins, the Go lines changed since `BASE` (default `origin/main`). It fails when a mutant on a changed line survives the tests, or when no test runs that line. Git must track new files, for example with `git add -N`. CI runs it on each pull request that changes such a line. |
| `ui-mutation-diff` | The same for the lines of `ui/src/lib` changed since `BASE`, with Stryker. CI runs it on each pull request that changes such a line. A pull request that changes only the website, the docs or other files runs neither. |
| `mutation` | Mutation-tests all Go code, in about 8 minutes on 10 cores. It fails below 81 % efficacy or 85 % mutant coverage. CI does not run it. `cmd/handful` is not mutation-tested: gremlins 0.6.0 cannot map its coverage. |
| `crap` | Prints the CRAP score of each hand-written Go function: its complexity combined with its test coverage. Higher means riskier to change. |
| `ui-mutation` | Mutation-tests `ui/src/lib` with Stryker and prints the score. |

## How it works

```
browser ──REST + SSE──▶ Handful server (Go) ──▶ SQLite (yargpt.db)
                           │  ▲
            spawns per turn│  │MCP over HTTP (bearer token per turn)
                           ▼  │
                      claude -p … (cwd = the bot's workspace)
```

- **Turns.** A message, a routine or a bot's first start becomes a turn. A bot runs one turn at a time, and its turns share one Claude Code session, which each turn resumes. The prompt goes to the `claude` process on stdin as stream-json. Each send carries a `clientNonce`, so a retried send does not start a second turn.
- **Delivery.** The model's plain text is private. The user sees what the bot sends with the `send_to_user` tool. When a turn ends without a message, the chat shows the model's final text, marked as a fallback.
- **Tools.** Each `claude` process connects to Handful's MCP endpoint, `/mcp`, with a token that is valid only for its turn. The tools are `send_to_user`, `send_to_agent`, `read_room`, `recall_memory`, `update_memory`, `update_routine`, `update_skill`, `find_artifacts`, `read_artifact`, `update_artifact`, `update_profile`, `update_board`, `create_setup` (Builder only) and `approve`. Tool calls that need approval reach `approve` through `--permission-prompt-tool`.
- **Isolation.** Workers run with `--setting-sources project`, `--strict-mcp-config`, `--settings` that turn off auto memory and commit and PR attribution, `CLAUDE_CODE_DISABLE_BUNDLED_SKILLS=1` and `CLAUDE_CODE_DISABLE_CLAUDE_MDS=1`. The built-in tools are limited to Task, Bash, Monitor, TaskStop, Read, Edit, Write, Glob, Grep, WebSearch and Skill. Handful writes each process's MCP config to a 0600 file in `<data>/run`, so no token shows in the process list, and removes the file when the process exits.
- **Transcript.** Each chat is an ordered log of `user`, `bot`, `event`, `activity` and `approval` entries. Each change gets a new `rev`. Clients catch up with `GET /api/bots/{id}/entries?after=<rev>` and the `GET /api/events` stream. A change can reach the stream before one that took the rev just below it, so the UI catches up from the highest rev with no gap below it, not from the highest rev it has seen.
- **Schema.** The migrations in `internal/db/migrations` run in order at start, each in one transaction with its `PRAGMA user_version` update.

## Layout

| Path | Content |
|---|---|
| `cmd/handful` | Server entry point and flags |
| `internal/db` | Schema, sqlc queries and generated code (`modernc.org/sqlite`, pure Go) |
| `internal/core` | Domain state: bots, teams, transcript, turns, memory, routines, skills, artifacts, approvals, event hub |
| `internal/agent` | Claude Code runner, stream-json parsing, system prompt and MCP tools |
| `internal/httpapi` | REST API, event stream, origin guard and UI hosting |
| `internal/shellenv` | The environment of the user's login shell, for a server that launchd starts |
| `ui` | React 19, TypeScript 7, Tailwind v4 and shadcn/ui app, embedded through `ui/embed.go` |
| `desktop` | The macOS app: Electron window, launch agent and icons |
| `web` | The handful.dev website: Astro 4 and Tailwind 3 |
