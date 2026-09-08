# gobase

A Go HTTP service template with the quality gates already wired: strict
linting, layering enforced by the linter, security headers, graceful
shutdown, and a distroless image.

Clone it, replace `internal/domain`, ship.

```sh
make tools && make hooks && make ci
make run     # → http://localhost:8080/healthz
```

## Clone checklist

1. **Rename the module.** Every import path depends on it.

   ```sh
   go mod edit -module github.com/you/yourservice
   grep -rl github.com/selimslab/gobase --include='*.go' --include='*.yml' . \
     | xargs sed -i '' 's|github.com/selimslab/gobase|github.com/you/yourservice|g'
   ```

   The `.golangci.yml` depguard rules and `goimports.local-prefixes` name the
   module too — that command catches them.

2. **Install the tools and hooks.** `make tools && make hooks`
3. **Verify.** `make ci` — lint, vet, layering, tests, vulnerability scan.
4. **Replace the domain.** Delete `internal/domain/widget.go`,
   `internal/adapters/memstore/`, and `internal/adapters/httpx/widget.go`.
   They are an example of the port pattern, not a dependency.
5. **Read `SECURITY.md`.** There is no authentication. That is deliberate,
   and it is your decision to make.
6. **Turn on branch protection** (below).

## Layout

```
cmd/server/          main() + run(ctx): the composition root
internal/
  platform/          machinery with no business meaning — config, obs,
                     security, version. Identical in any service. No net/http.
  domain/            entities, rules, and the ports they declare. Pure.
                     This is the layer you replace.
  adapters/          everything touching the outside
    httpx/           inbound HTTP
    memstore/        outbound storage, implementing a domain port
docs/hardening/      options that compile in CI but ship in no binary
scripts/             check-layers.sh
```

## The three layers

Dependencies point inward, always:

```
adapters ──→ domain ──→ platform
adapters ─────────────→ platform
```

`domain` may not import `adapters`. `platform` may not import either, and
neither `platform` nor `domain` may import `net/http`.

Why it is shaped this way: [ARCHITECTURE.md](ARCHITECTURE.md).

This is enforced two ways, so it cannot drift:

- **`depguard`** in `.golangci.yml` fails the lint on a forbidden import.
- **`scripts/check-layers.sh`** (`make layers`) checks the same rules against
  the real import graph, in CI and on every push.

**Ports are declared in `domain` and implemented in `adapters`.**
`domain/ports.go` declares `WidgetRepo`; `adapters/memstore` implements it;
`cmd/server` binds them. Swapping in Postgres is one new adapter and one
changed line in `main.go` — the domain does not move.

That inversion is the reason for the layout. Without it, "layers" is just
three directories.

## Configuration

Everything has a working default; the service runs with no environment set.
See `.env.example`.

| Variable | Default | Notes |
|---|---|---|
| `APP_ENV` | `development` | `production` turns on HSTS over TLS |
| `SERVICE_NAME` | `gobase` | Tags every log line and span |
| `HTTP_ADDR` | `:8080` | |
| `HTTP_READ_TIMEOUT` | `10s` | |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Must be non-zero — Slowloris |
| `HTTP_WRITE_TIMEOUT` | `15s` | |
| `HTTP_IDLE_TIMEOUT` | `60s` | |
| `HTTP_HANDLER_TIMEOUT` | `10s` | Must be **less** than write timeout |
| `HTTP_SHUTDOWN_TIMEOUT` | `15s` | Grace period for in-flight requests |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | *(empty)* | Empty disables export entirely |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | |
| `OTEL_TRACES_SAMPLER_RATIO` | `1.0` | Parent-based; only roots consult it |
| `SECURITY_MAX_BODY_BYTES` | `1048576` | 1 MiB |
| `SECURITY_TRUSTED_PROXIES` | `0` | **0 ignores `X-Forwarded-For`.** Set to the number of proxies you actually run |
| `SECURITY_ENABLE_HSTS` | `false` | Automatic in production; only sent over TLS |
| `SECURITY_HSTS_MAX_AGE` | `8760h` | |

Invalid configuration fails at startup with **every** problem listed at once,
not the first one.

## Endpoints

| Method | Path | |
|---|---|---|
| GET | `/healthz` | Liveness. No dependency checks — a failure kills the pod |
| GET | `/readyz` | Readiness. False during drain |
| POST | `/widgets` | Example. Delete it |
| GET | `/widgets` | |
| GET | `/widgets/{id}` | |
| POST | `/widgets/{id}/restock` | |
| DELETE | `/widgets/{id}` | |

Every error — 404, 405, 413, 415, 422, 500 — is an RFC 7807
`application/problem+json` document carrying `request_id` and `trace_id`.
One shape to parse.

## Make targets

| | |
|---|---|
| `make tools` | Install the pinned dev tools |
| `make lint` / `lint-fix` | 35 linters |
| `make test` | `-race -shuffle=on`, with coverage |
| `make layers` | Verify the layering rules |
| `make sec` | `go mod verify`, govulncheck, gitleaks |
| `make build` / `run` | Version stamped via `-ldflags` |
| `make docker-build` / `docker-run` | Distroless image, hardened flags |
| `make hardening` | Compile and test the options |
| `make ci` | Everything above |

## Observability

Logs are JSON on stdout. Every request line carries `request_id`, and
`trace_id`/`span_id` when tracing is on — logs and traces join on the same
keys.

Tracing is off until you set `OTEL_EXPORTER_OTLP_ENDPOINT`. With it empty the
providers are no-op: instrumented code still runs, nothing is dialed, and no
collector is needed to run locally. W3C `traceparent` is honored either way,
so an incoming trace context is never dropped.

## Running in production

```sh
docker run --read-only --cap-drop=ALL --security-opt=no-new-privileges \
  --user 65532:65532 -p 8080:8080 gobase:latest
```

The image is distroless and non-root, with no shell and no package manager.
Kubernetes `securityContext` equivalents are in `SECURITY.md`.

## Branch protection

CI enforces nothing unless the repository requires it. On `main`:

- Require pull request reviews (at least 1)
- Require status checks: **Lint**, **Test**, **Security**, **Docker**
- Require branches to be up to date before merging
- Require conversation resolution
- Do not allow bypassing the above, including for administrators
- Restrict force pushes and deletions

The CI workflow already sets `permissions: contents: read` at the top level,
pins every action to a commit SHA, and uses `persist-credentials: false`, so a
compromised action cannot push, tag, or open a pull request.

## What is not here

No auth, no database, no compose file. See
[docs/hardening/README.md](docs/hardening/README.md) for what is available as a
drop-in option, [SECURITY.md](SECURITY.md) for why auth is not one of them, and
[ARCHITECTURE.md](ARCHITECTURE.md) for the reasoning behind the split.

## License

MIT. See `LICENSE`.
