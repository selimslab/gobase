//go:build hardening

package hardening

import (
	"net/http"
	"slices"
	"strings"

	"github.com/selimslab/gobase/internal/platform/security"
)

// # CORS allowlist
//
// When you need it: a browser front-end on a different origin calls this API.
// Server-to-server callers and same-origin front-ends need nothing.
//
// What it costs: every origin you add is a site that can read authenticated
// responses on a user's behalf. The cost is not CPU, it is trust.
//
// The default policy in platform/security denies everything and emits no CORS
// headers at all. Replace it in cmd/server:
//
//	policy := security.NewPolicy(cfg.Env, cfg.Security)
//	policy.AllowOrigin = hardening.AllowOrigins([]string{"https://app.example.com"}, false)
//
// Rules worth keeping:
//   - Exact origins only. A suffix match on "example.com" also matches
//     "evil-example.com" and "example.com.evil.test".
//   - Never reflect an arbitrary Origin back. That is the same as no policy.
//   - Never combine "*" with credentials. Browsers reject it, and wanting it
//     means the design is wrong.

// AllowOrigins returns an OriginPolicy that permits exactly the listed
// origins, compared in full and case-insensitively on the scheme and host.
func AllowOrigins(allowed []string, withCredentials bool) security.OriginPolicy {
	normalized := make([]string, 0, len(allowed))
	for _, o := range allowed {
		normalized = append(normalized, strings.ToLower(strings.TrimRight(o, "/")))
	}

	return func(origin string) security.OriginDecision {
		candidate := strings.ToLower(strings.TrimRight(origin, "/"))

		// "null" is what a sandboxed iframe or a file:// page sends. It is
		// not an origin you can trust, and it is never an allowlist entry
		// worth having.
		if candidate == "" || candidate == "null" {
			return security.OriginDecision{}
		}

		if !slices.Contains(normalized, candidate) {
			return security.OriginDecision{}
		}

		return security.OriginDecision{
			Allow:            true,
			AllowOrigin:      origin,
			AllowCredentials: withCredentials,
		}
	}
}

// PreflightHandler answers OPTIONS requests. The default template has no
// preflight handler because it allows no origins; add this only alongside an
// allowlist.
func PreflightHandler(policy security.Policy, methods, headers []string, maxAgeSeconds string) func(http.Handler) http.Handler {
	allowMethods := strings.Join(methods, ", ")
	allowHeaders := strings.Join(headers, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodOptions || r.Header.Get("Access-Control-Request-Method") == "" {
				next.ServeHTTP(w, r)

				return
			}

			decision := policy.Origin(r.Header.Get("Origin"))
			if !decision.Allow {
				// Say nothing. The browser blocks the real request.
				w.WriteHeader(http.StatusNoContent)

				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", decision.AllowOrigin)
			h.Set("Access-Control-Allow-Methods", allowMethods)
			h.Set("Access-Control-Allow-Headers", allowHeaders)
			h.Set("Access-Control-Max-Age", maxAgeSeconds)
			h.Add("Vary", "Origin")
			h.Add("Vary", "Access-Control-Request-Method")

			if decision.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}

			w.WriteHeader(http.StatusNoContent)
		})
	}
}
