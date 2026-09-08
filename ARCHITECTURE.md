# Architecture

Why this template is shaped the way it is. For how to use it, see
[README.md](README.md).

## Core vs. option

One rule decides what ships by default:

**Core is what is expensive or unsafe to add later. An option is what is
mechanical to add later.**

| Core | Option |
|---|---|
| Server timeouts, body caps, secure headers | CodeQL, Trivy |
| Strict linting (35 linters) | SBOM + signed releases |
| gitleaks | Rate limiting |
| Config redaction (`LogValue()`) | CORS allowlist (deny-by-default stays core) |
| Graceful shutdown, request IDs, RFC 7807 | Postgres, auth |

Retrofitting a body cap means auditing every handler you already wrote.
Adopting CodeQL means copying one file. That asymmetry is the whole test.

## Three layers, pointing inward

```
adapters ──→ domain ──→ platform
adapters ─────────────→ platform
```

**`internal/platform/`** — machinery with no business meaning: `config`,
`obs`, `security`, `version`. Identical in any service you write. No
`net/http`.

**`internal/domain/`** — entities, rules, and the **ports** they declare.
Pure; imports platform only. This is the layer you replace when you clone.

**`internal/adapters/`** — everything touching the outside. Inbound `httpx`,
outbound `memstore`.

`domain` may not import `adapters`. `platform` may not import either.

### Ports are declared inward

`domain/ports.go` declares `WidgetRepo`. `adapters/memstore` implements it.
`cmd/server` binds them. Infrastructure conforms to the business, not the
reverse — so swapping in Postgres is one new adapter and one changed line in
`main.go`, with the domain untouched.

Without that inversion, "layers" is just three directories.

### Enforced twice

- **`depguard`** (`.golangci.yml`) fails the lint on a forbidden import.
  Layers are directories, so a new package inherits its layer's rule.
- **`scripts/check-layers.sh`** (`make layers`) checks the same rules against
  the real import graph.

The script tests **direct** imports, deliberately. `go list -deps` reports
`net/http` under `platform/` because the OTLP **gRPC** exporters pull it in —
gRPC speaks HTTP/2. That is a transitive dependency of a third-party library,
not our code reaching for a transport, and the two must not be conflated.

## Options ship as compilable files

`docs/hardening/` carries a `//go:build hardening` tag: compiled and tested in
CI (`make hardening`), absent from the default binary. An option cannot rot
into something that no longer builds, and you pay nothing for the ones you
skip.

Verify: `go list -deps ./cmd/server | grep golang.org/x/time` is empty.

## Decisions worth stating

**Middleware order is load-bearing.**

```
request-id → recover → otelhttp → context-logger → access-log → secure-headers → body-limit
```

`request-id` is outermost so that a panic's 500 carries the same ID as the log
line recording it. `access-log` sits inside the tracing span (to capture
`trace_id`) and outside the handler (to see the final status).

**`/healthz` and `/readyz` differ on purpose.** `/readyz` goes false the
instant shutdown begins, draining traffic before connections close.
`/healthz` stays true — a failing liveness probe kills the pod mid-drain.

**`X-Forwarded-For` is ignored by default.** `SECURITY_TRUSTED_PROXIES=0`
means the connection peer wins. Each trusted proxy vouches for exactly one
chain entry, so the leftmost believable address sits `trustedProxies` places
from the end. Setting the value too high lets a client forge its own IP.

**HSTS requires TLS.** Sending it over plain HTTP pins a scheme the client
cannot reach.

**One error shape.** Every failure — 404, 405, 413, 415, 422, 500 — is an RFC
7807 document with `request_id` and `trace_id`. Panics return an opaque 500;
the stack goes to the log only.

**Tracing is off until configured.** An empty OTLP endpoint leaves the
providers no-op: instrumented code runs, nothing is dialed. W3C `traceparent`
is honored either way, so an incoming trace context is never dropped.

**`Validate()` reports every problem at once**, joined with `errors.Join`, not
just the first.

## Out of scope by design

**Auth.** It depends on an identity provider and a threat model a template
cannot guess. A plausible-looking guess is worse than an obvious gap — it
invites trust. See [SECURITY.md](SECURITY.md).

**A database.** The port is declared; the adapter is yours.

**A compose file.** One process with no dependencies needs no orchestration to
run locally, and a compose file that exists to run one container is a file
that drifts.
