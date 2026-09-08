// Package security holds transport-independent security decisions: which
// response headers to set, how large a request body may be, and whether an
// origin is allowed. Nothing here imports net/http, so it is testable without
// a server.
package security

import (
	"fmt"
	"time"

	"github.com/selimslab/gobase/internal/platform/config"
)

// Header is one response header to set on every response.
type Header struct {
	Name  string
	Value string
}

// OriginDecision is the CORS answer for one request origin.
type OriginDecision struct {
	// Allow reports whether the origin may read the response.
	Allow bool
	// AllowOrigin is echoed as Access-Control-Allow-Origin when Allow is true.
	AllowOrigin string
	// AllowCredentials permits cookies and Authorization on the request.
	AllowCredentials bool
}

// deny is the default CORS answer: emit no headers at all.
var deny = OriginDecision{}

// OriginPolicy decides whether a cross-origin request is allowed. The default
// policy denies everything. To allow an origin, assign Policy.AllowOrigin a
// func that matches exact origins — never a suffix match, because a suffix
// test on "example.com" also matches "evil-example.com" and
// "example.com.evil.test". Every origin you allow is a site that can read
// authenticated responses on a user's behalf.
type OriginPolicy func(origin string) OriginDecision

// DenyAllOrigins is the default OriginPolicy. It never allows an origin, so
// the HTTP adapter emits no CORS headers whatsoever.
func DenyAllOrigins(string) OriginDecision { return deny }

// Policy is the resolved set of security decisions for a service.
type Policy struct {
	// Headers are set on every response before the handler runs.
	Headers []Header
	// MaxBodyBytes caps the request body the adapter will read.
	MaxBodyBytes int64
	// TrustedProxies is the number of reverse proxies in front of this
	// service. Zero means X-Forwarded-For is not trusted at all.
	TrustedProxies int
	// AllowOrigin decides CORS.
	AllowOrigin OriginPolicy

	hsts       string
	enableHSTS bool
}

// NewPolicy builds the Policy for an environment. HSTS is emitted only when
// the connection is already secure, which UseHSTS decides per request.
func NewPolicy(env config.Environment, cfg config.Security) Policy {
	return Policy{
		Headers:        baseHeaders(),
		MaxBodyBytes:   cfg.MaxBodyBytes,
		TrustedProxies: cfg.TrustedProxies,
		AllowOrigin:    DenyAllOrigins,
		hsts:           hstsValue(cfg.HSTSMaxAge),
		enableHSTS:     cfg.EnableHSTS || env.IsProduction(),
	}
}

// baseHeaders returns the headers that are safe for an API that serves JSON
// and nothing else: no scripts, no framing, no referrer, no device access.
func baseHeaders() []Header {
	return []Header{
		{Name: "X-Content-Type-Options", Value: "nosniff"},
		{Name: "X-Frame-Options", Value: "DENY"},
		{Name: "Referrer-Policy", Value: "no-referrer"},
		{Name: "Content-Security-Policy", Value: "default-src 'none'; frame-ancestors 'none'"},
		{Name: "Permissions-Policy", Value: "camera=(), microphone=(), geolocation=()"},
		{Name: "Cross-Origin-Opener-Policy", Value: "same-origin"},
		{Name: "Cross-Origin-Resource-Policy", Value: "same-origin"},
	}
}

func hstsValue(maxAge time.Duration) string {
	return fmt.Sprintf("max-age=%d; includeSubDomains", int64(maxAge.Seconds()))
}

// UseHSTS reports whether Strict-Transport-Security may be sent. Sending it
// over plain HTTP is meaningless and pins a scheme the client cannot reach,
// so it requires both an enabled policy and a TLS connection.
func (p Policy) UseHSTS(overTLS bool) bool { return p.enableHSTS && overTLS }

// HSTSHeader returns the Strict-Transport-Security header to send when
// UseHSTS allows it.
func (p Policy) HSTSHeader() Header {
	return Header{Name: "Strict-Transport-Security", Value: p.hsts}
}

// Origin applies the CORS policy, defaulting to deny when none is set.
func (p Policy) Origin(origin string) OriginDecision {
	if origin == "" || p.AllowOrigin == nil {
		return deny
	}

	return p.AllowOrigin(origin)
}
