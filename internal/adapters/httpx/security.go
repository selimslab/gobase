package httpx

import (
	"net/http"

	"github.com/selimslab/gobase/internal/platform/security"
)

// secureHeaders applies the platform policy to every response. It is an
// adapter and nothing more: which headers to send is decided in
// platform/security, this only knows how to put them on the wire.
func secureHeaders(policy security.Policy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()

			for _, header := range policy.Headers {
				h.Set(header.Name, header.Value)
			}

			// HSTS over plain HTTP is at best ignored and at worst pins a
			// scheme the client cannot reach, so it needs a TLS connection.
			if policy.UseHSTS(isTLS(r)) {
				hsts := policy.HSTSHeader()
				h.Set(hsts.Name, hsts.Value)
			}

			applyCORS(w, r, policy)

			next.ServeHTTP(w, r)
		})
	}
}

// isTLS reports whether the request reached us over a secure connection,
// directly or through a proxy we trust to set X-Forwarded-Proto.
func isTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	return r.Header.Get("X-Forwarded-Proto") == "https"
}

// applyCORS emits CORS headers only when the policy allows the origin. The
// default policy allows nothing, so by default this writes no headers at all.
func applyCORS(w http.ResponseWriter, r *http.Request, policy security.Policy) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}

	decision := policy.Origin(origin)
	if !decision.Allow {
		return
	}

	h := w.Header()
	h.Set("Access-Control-Allow-Origin", decision.AllowOrigin)
	// The response varies by Origin, so a shared cache must not serve one
	// origin's response to another.
	h.Add("Vary", "Origin")

	if decision.AllowCredentials {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
}

// limitBody caps how much of a request body a handler can read. The limit is
// enforced by the reader, so a handler that streams cannot bypass it, and the
// oversized request is answered with a 413 problem.
func limitBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}

			next.ServeHTTP(w, r)
		})
	}
}
