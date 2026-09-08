# Security

## Reporting a vulnerability

Report privately through GitHub's **Security → Report a vulnerability** tab,
or by email to the maintainer. Please do not open a public issue for an
unfixed vulnerability.

Include what you did, what happened, and what you expected. A proof of concept
helps. Expect an acknowledgement within a few days.

## What this template protects against

Set up by default, on every response and every request:

| Protection | Where | What it stops |
|---|---|---|
| All four server timeouts, `ReadHeaderTimeout` non-zero | `httpx/server.go` | Slowloris and connection exhaustion |
| `http.TimeoutHandler` | `httpx/server.go` | One stuck handler holding a connection open with no reply |
| 1 MiB body cap (`http.MaxBytesReader`) | `httpx/security.go` | Memory exhaustion from a large POST |
| `nosniff`, `DENY`, `no-referrer`, restrictive CSP and Permissions-Policy | `platform/security/policy.go` | MIME sniffing, clickjacking, referrer leakage, device access |
| HSTS only over TLS | `platform/security/policy.go` | Pinning a scheme the client cannot reach |
| CORS denied by default | `platform/security/policy.go` | Any origin reading authenticated responses |
| `X-Forwarded-For` ignored unless proxies are configured | `platform/security/ip.go` | A client forging its own IP in logs and limits |
| Panic → opaque 500, stack to the log only | `httpx/middleware.go` | Internal detail leaking to a caller |
| Request IDs sanitized and length-capped | `httpx/middleware.go` | Header injection and log flooding |
| Unknown JSON fields rejected | `httpx/decode.go` | Silent acceptance of a payload you did not model |
| `LogValue()` redaction | `platform/config/redact.go` | Credentials in log sinks |
| Distroless non-root image, no shell | `Dockerfile` | Post-exploitation: there is nothing to exec |
| Pinned base digests and action SHAs | `Dockerfile`, `ci.yml` | A moved tag becoming a supply-chain compromise |
| `gitleaks`, `govulncheck`, `gosec` | CI and hooks | Committed secrets and known-vulnerable dependencies |

## What it does not protect against, by design

**There is no authentication or authorization.** Every route is public. This
is deliberate: auth depends on an identity provider and a threat model that a
template cannot guess, and a plausible-looking guess is worse than an obvious
gap — it invites you to trust it.

Before this service faces anything real, decide:

- Who calls it? A person with a session, or a service with a credential?
- Where is identity verified — at the edge, or in this process?
- What does each caller get to do, and where is that decided?

Then add the middleware. `httpx/middleware.go` shows the shape.

**Also out of scope:** rate limiting (see `docs/hardening/ratelimit.go`),
TLS termination (do it at the load balancer or a sidecar), secret management
(use your platform's — this template only makes sure secrets do not reach the
logs), and audit logging.

## Running it safely

The container is built to run with every privilege removed:

```sh
docker run --read-only --cap-drop=ALL --security-opt=no-new-privileges \
  --user 65532:65532 gobase:latest
```

In Kubernetes, the equivalent `securityContext`:

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

Probe `/healthz` for liveness and `/readyz` for readiness. They are different
on purpose: `/readyz` goes false the moment shutdown begins, so traffic drains
before connections close, while `/healthz` stays true — a failing liveness
probe gets the pod killed mid-drain.

**Set `SECURITY_TRUSTED_PROXIES` to the number of proxies you actually run.**
The default of `0` ignores `X-Forwarded-For` entirely, which is correct when
nothing sits in front of the service. Setting it too high lets a client forge
its own address by prepending entries to the header.
