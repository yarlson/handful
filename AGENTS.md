# Instructions for coding agents

- Run `mise install` once. The Makefile runs every tool through `mise exec`, so it uses the versions that `mise.toml` pins. To add or update a tool, change `mise.toml`.
- The section "Tests and checks" in README.md lists the make targets.
- Run `make check` before you finish a change. CI runs the same target.

## Go changes

- For a change to production Go code, run `make mutation-diff`. It mutation-tests the lines changed since `origin/main`. A mutant that lives shows a change to the code that no test notices, and a mutant that no test runs shows an untested line: add a test that kills it. Git must track a new file first, for example with `git add -N <file>`.
- Do not lower the floors in the `mutation` target or add `exclude-files` to `.gremlins.yaml`. gremlins cannot skip a single mutant: if a mutant on a changed line is equivalent to the original code, explain it in the pull request. Do not set `test-cpu` in `.gremlins.yaml`: gremlins 0.6.0 then counts every mutant as killed.
- For a change to production Go code, run `make crap`. Each function that you add or change must score below 15. A function that you did not change can score higher; leave it unless the task is about it.
- Run `make mutation` when you change `.gremlins.yaml`, the mutation targets or existing Go tests. CI runs only `make mutation-diff`, which tests changed production lines, so nothing else notices a weaker test. It takes about 8 minutes on a 10-core machine.
- Keep the tests in `internal/agent` parallel: each mutant runs the whole package, so their wall time multiplies the mutation run. A test must not depend on the environment or on a fixed port to run in parallel.

## Web changes

- For a change in `web/src/lib`, run `make web-mutation` and kill the surviving mutants in the code that you changed.
- oxlint warnings in `web/` are known follow-ups. Do not add new ones.
