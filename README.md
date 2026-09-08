# gobase

A Go HTTP service template with the quality gates already wired: strict
linting, layering enforced by the linter, security headers, graceful shutdown,
and a distroless image.

Clone it, replace `internal/domain`, ship.

```sh
make tools && make hooks && make ci
make run     # → http://localhost:8080/healthz
```

## Clone checklist

1. **Rename the module.** Every import path depends on it, and the
   `.golangci.yml` depguard rules and `goimports.local-prefixes` name it too —
   this catches all of them.

   ```sh
   go mod edit -module github.com/you/yourservice
   grep -rl github.com/selimslab/gobase --include='*.go' --include='*.yml' . \
     | xargs sed -i '' 's|github.com/selimslab/gobase|github.com/you/yourservice|g'
   ```

2. **Install the tools and hooks.** `make tools && make hooks`
3. **Verify.** `make ci` — lint, vet, layering, tests, vulnerability scan.
4. **Replace the domain.** Delete `internal/domain/example.go`,
   `internal/adapters/memstore/`, and `internal/adapters/httpx/example.go`.
   They demonstrate the port pattern; nothing depends on them.
5. **Decide on auth.** There is none, deliberately — it needs an identity
   provider and a threat model a template cannot guess. `httpx/middleware.go`
   shows the shape.

## Layout

```
cmd/server/          main() + run(ctx): the composition root
internal/
  platform/          machinery with no business meaning — config, network,
                     obs, security, version. Identical in any service.
  domain/            entities, rules, and the ports they declare. Pure.
                     This is the layer you replace.
  adapters/          everything touching the outside
    httpx/           inbound HTTP
    memstore/        outbound storage, implementing a domain port
scripts/             check-layers.sh
```

Dependencies point inward, always:

```
adapters ──→ domain ──→ platform
adapters ─────────────→ platform
```

`make layers` checks this against the real import graph and `depguard` fails
the lint on a forbidden import. Both run in CI and on every push, so the rule
cannot drift.

## Configuration

Every variable has a working default, so the service runs with nothing set.
`.env.example` lists and annotates them all. Invalid configuration fails at
startup with **every** problem reported at once, not the first.

Two are easy to get wrong:

- **`SECURITY_TRUSTED_PROXIES`** defaults to `0`, which ignores
  `X-Forwarded-For` entirely — correct unless you run behind a proxy you
  control. Too high lets a client forge its own IP.
- **`HTTP_HANDLER_TIMEOUT`** must be less than `HTTP_WRITE_TIMEOUT`, or the
  write deadline kills the connection before the client sees the 503.

## Endpoints

`GET /healthz` is liveness (no dependency checks — a failure kills the pod) and
`GET /readyz` is readiness (false during drain). The `/examples` routes are a
sample; delete them with the rest of the example domain.

Every error — 404, 405, 413, 415, 422, 500 — is an RFC 7807
`application/problem+json` document carrying `request_id` and `trace_id`. One
shape to parse.

## Make targets

`make help` lists them all.

## Running in production

```sh
docker run --read-only --cap-drop=ALL --security-opt=no-new-privileges \
  --user 65532:65532 -p 8080:8080 gobase:latest
```

The image is distroless and non-root, with no shell and no package manager.
The Kubernetes equivalent:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: [ALL]
  seccompProfile:
    type: RuntimeDefault
```

Probe `/healthz` for liveness and `/readyz` for readiness — they differ on
purpose: `/readyz` goes false the moment shutdown begins so traffic drains
before connections close, while `/healthz` stays true, because a failing
liveness probe gets the pod killed mid-drain.

## License

MIT. See `LICENSE`.
