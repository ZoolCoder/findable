# Contributing

## Running everything

```console
make test    # go test ./...
make race    # the same, with the race detector
make lint    # gofmt check, go vet, golangci-lint (with the import boundaries)
make cover   # coverage across the whole suite
make build   # bin/findable
make evals   # the plugin eval suite in evals/, needs the claude CLI
```

No test touches a network. A pull request from a fork must be testable
without being handed access to anything, so CI runs with no credentials.
A provider that talks to an API is tested against an `httptest.Server` that
answers in that API's real shapes.

## What a good change looks like

**Say what breaks, not what differs.** `content differs, 12 to 40 bytes` sends
someone to the right place; `mismatch` does not. Plan and error text is most of
this tool's value.

**Never print a value that could be a secret.** Not in a plan, not in `-json`,
not in a saved plan, not in an error. Say where the value comes from instead.

**Prefer reporting to guessing.** If the tool cannot do something, that is a
`MANUAL` action saying what a person must do, printed every run. A tool that
silently skips what it cannot do is how a half-finished state passes for a
finished one.

**Test the failure, not the success.** The interesting tests are a provider
that accepts every write and changes nothing, a saved plan applied after the
world moved, a symlink that points out of the target. Successes tend to hold on
their own.

## Adding a provider

See [Adding a provider](docs/modules/ROOT/pages/extending.adoc): a package
under `internal/provider/`, a name in `config.Providers`, an entry in
`engine.Default`, and a depguard line in `.golangci.yml`.

## Docs

The manual is Antora:

```console
make docs && open build/site/index.html
```

It builds on the [zc-supplemental-ui](https://github.com/zoolcoder/zc-supplemental-ui)
theme release named in `package.json`; `npm run docs:local` builds against a
checkout of the theme next to this repository instead.

It must build with no warnings. `${VAR}` in prose is an attribute reference —
wrap it in `+…+` or it renders as nothing.

## Branches

The project follows git flow:

| Branch | From | Merges into | Holds |
|---|---|---|---|
| `main` | | | released code only; every commit is a tag `vX.Y.Z` |
| `develop` | `main` | | the next release; open pull requests here |
| `feature/<topic>` | `develop` | `develop` | one change; `fix/`, `docs/`, `chore/` work the same |
| `release/<X.Y.Z>` | `develop` | `main`, then `develop` | version bump and changelog only |
| `hotfix/<X.Y.Z>` | `main` | `main`, then `develop` | an urgent fix to a release |

`main` and `develop` take changes only through pull requests with green CI, and
neither can be force-pushed or deleted.

Rebase a topic branch on `develop` rather than merging `develop` into it.
Topic branches are rebased into `develop`; release and hotfix branches are
merged into `main` with a merge commit, so each release is one visible merge.

## Commits

Conventional commits: `type(scope): description`, imperative, lowercase, no
trailing period, under 60 characters. Types: `feat`, `fix`, `refactor`, `docs`,
`test`, `chore`, `build`, `ci`, `perf`, `style`.

One change per commit, with a body saying what was wrong and why the fix is
the right one. If a test found the bug, say so — that is the useful part for
whoever reads it next.
