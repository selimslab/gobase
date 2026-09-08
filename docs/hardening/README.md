# Hardening options

Everything in this directory is deliberately **not** in the default build.

The split follows one rule: **core is what is expensive or unsafe to add
later; an option is what is mechanical to add later.** Server timeouts and a
deny-by-default CORS policy are core, because retrofitting them means auditing
every handler you already wrote. CodeQL is an option, because adopting it is
copying one file.

## How this works

Go files here carry a `//go:build hardening` tag. They compile and test in CI
(`make hardening`) but never enter the server binary — so an option cannot rot
into something that no longer builds, and you pay nothing for the ones you do
not use.

To adopt one: move the file where it belongs, delete the build tag, wire it in
`cmd/server/main.go`. YAML files are copied to `.github/workflows/`.

Verify nothing leaked into the binary:

```sh
go list -deps ./cmd/server | grep golang.org/x/time   # empty: the rate limiter is not shipped
```

## What is here

| File | Adopt it when | It costs you |
|---|---|---|
| `ratelimit.go` | An endpoint costs more to serve than to request | A per-process map; N replicas allow N×the rate |
| `cors_allowlist.go` | A browser front-end on another origin calls this API | Every listed origin can read authenticated responses |
| `worker/` | Work must happen off the request path | A second lifecycle to drain; jobs must be idempotent |
| `codeql.yml` | The repository is public, or PR review needs a second pair of eyes | Minutes per run; paid on private repos |
| `trivy.yml` | You ship a container | Minutes per run, plus a DB download |
| `release.yml` | Someone other than you consumes your builds | Longer releases — and consumers must actually verify |

## What is not here, and will not be

**Authentication and authorization.** Not an oversight — a decision. Auth
belongs to your identity provider and your threat model, and a template that
guesses at either produces something worse than nothing: a login form that
looks finished. See `SECURITY.md`.

**A database.** `internal/domain/ports.go` declares `WidgetRepo` and
`internal/adapters/memstore` implements it. A Postgres adapter is another
implementation of the same interface and one changed line in `cmd/server`.
That is the whole point of declaring the port in the domain.

**A compose file.** One process with no dependencies does not need
orchestration to run locally. `make run` is enough, and a compose file that
exists only to run one container is a file that drifts.
