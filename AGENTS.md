# Instructions for coding agents

- Run `mise install` once. The Makefile runs every tool that `mise.toml` pins through `mise exec`, so it uses those versions. To add or update a tool, change `mise.toml`. The macOS app targets also use `lipo`, `sips`, `iconutil` and `rsvg-convert`, which mise does not install.
- The section "Tests and checks" in README.md lists the make targets.
- Run `make check` before you finish a change. CI runs the same target.
- Never stop a process by a pattern: no `pkill -f`, `pkill <pattern>` or `killall`. A pattern can match other processes, such as the claude processes of YarGPT's bots, and stop them too. Stop a process by its PID (save `$!` when you start it) or by its port (`lsof -ti tcp:PORT`, then `kill` that PID).

## Go changes

- For a change to production Go code, run `make mutation-diff`. It mutation-tests the lines changed since `origin/main`. A mutant that lives shows a change to the code that no test notices, and a mutant that no test runs shows an untested line: add a test that kills it. Git must track a new file first, for example with `git add -N <file>`.
- Do not lower the floors in the `mutation` target or add `exclude-files` to `.gremlins.yaml`. gremlins cannot skip a single mutant: if a mutant on a changed line is equivalent to the original code, explain it in the pull request. Do not set `test-cpu` in `.gremlins.yaml`: gremlins 0.6.0 then counts every mutant as killed.
- For a change to production Go code, run `make crap`. Each function that you add or change must score below 15. A function that you did not change can score higher; leave it unless the task is about it.
- Run `make mutation` when you change `.gremlins.yaml`, the mutation targets or existing Go tests. CI runs only `make mutation-diff`, which tests changed production lines, so nothing else notices a weaker test. It takes about 8 minutes on a 10-core machine.
- Keep the tests in `internal/agent` parallel: each mutant runs the whole package, so their wall time multiplies the mutation run. A test must not depend on the environment or on a fixed port to run in parallel.

## UI changes

- For a change in `ui/src/lib`, run `make ui-mutation` and kill the surviving mutants in the code that you changed.
- oxlint warnings in `ui/` are known follow-ups. Do not add new ones.

## macOS app changes

- For a change in `desktop/` or in `cmd/yargpt/tray_darwin.go`, run `make app`, quit YarGPT with ⌘Q, and open the built app, which replaces the running server. Check the window, a link, a right-click in the message box, **Settings…**, **Open Settings File**, the menu-bar icon and **Exit server**. `make check` covers only the files without Electron code: `desktop/app.js`, `desktop/bridge.js` (the bridge to Handful: its feed check, steps and copy), `desktop/context-menu.js`, `desktop/menu.js`, `desktop/server-log.js`, `desktop/settings-bridge.js`, `desktop/updater-state.js`, `desktop/updater-io.js` and `desktop/scripts/release-feed.js`.
- For a change to `make app-universal` or `.github/workflows/release.yml`, run `make app-universal VERSION=0.0.0-test` and unzip the result with `ditto -x -k`: `codesign --verify --deep --strict` must pass, and `lipo -archs` must list x86_64 and arm64 for `Contents/MacOS/YarGPT` and `Contents/Resources/yargpt`. Without `CSC_LINK` the build is signed ad-hoc; only a release tag tests the Developer ID signature and notarization, so test such a change with a pre-release tag such as `v0.2.0-rc.1`.
