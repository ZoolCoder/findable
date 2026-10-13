## What

<!-- One topic, and why. Link the issue: Closes #123 -->

## Checklist

- [ ] `make test` and `make lint` pass, `gofmt -l .` prints nothing
- [ ] Targets `develop`; only `release/*` and `hotfix/*` target `main`
- [ ] Conventional commit messages, rebased on `develop`
- [ ] No new non-stdlib dependency, or the pull request explains why one is unavoidable
- [ ] `Plan` still writes nothing: every write is inside an action's `Do`
- [ ] Config a provider cannot honour is rejected in `config.Validate`, not ignored
- [ ] Writes stay inside `write_dir`: only the robots.txt AI crawler block and llms.txt
- [ ] New command or JSON shape: a test, `commands.adoc`, and `adapters/cloud/AGENTS.md` updated
- [ ] New or changed skill: eval case under `evals/` when it changes what the agent asks, edits or publishes
- [ ] No real path, host or account name in tests or fixtures
