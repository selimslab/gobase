// Package network holds transport-independent address handling: resolving the
// caller's IP from a connection peer and a forwarding chain. Nothing here
// imports net/http, so it is testable without a server.
package network

import (
	"net"
	"net/netip"
	"strings"
)

// ClientIP resolves the caller's address from the connection address and the
// X-Forwarded-For chain.
//
// remoteAddr is the peer address of the TCP connection ("host:port" or a bare
// host). fwd holds every X-Forwarded-For header value, in the order received.
// trustedProxies is how many reverse proxies sit in front of this service.
//
// The chain is attacker-controlled up to the point where trusted infra
// appended to it, so only the last trustedProxies entries mean anything: with
// zero trusted proxies the header is ignored entirely and the connection peer
// wins. This is the difference between a header a client can forge and an
// address it cannot.
func ClientIP(remoteAddr string, fwd []string, trustedProxies int) string {
	peer := hostOnly(remoteAddr)

	if trustedProxies <= 0 {
		return peer
	}

	chain := forwardedChain(fwd)
	if len(chain) == 0 {
		return peer
	}

	// The rightmost entry was appended by the proxy we are talking to, the
	// one before it by the proxy before that, and so on. Each trusted proxy
	// vouches for one entry, so the leftmost address we can believe sits
	// trustedProxies places from the end. Anything further left was supplied
	// by the client and is worthless.
	idx := max(len(chain)-trustedProxies, 0)

	if ip := normalize(chain[idx]); ip != "" {
		return ip
	}

	return peer
}

// forwardedChain flattens every X-Forwarded-For value into one ordered list of
// valid addresses.
func forwardedChain(fwd []string) []string {
	out := make([]string, 0, len(fwd))

	for _, header := range fwd {
		for part := range strings.SplitSeq(header, ",") {
			if ip := normalize(part); ip != "" {
				out = append(out, ip)
			}
		}
	}

	return out
}

// normalize validates one address and returns it without a port or IPv6
// brackets. It returns "" when the value is not an IP address at all.
func normalize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	s = strings.Trim(s, `"`)
	s = hostOnly(s)

	addr, err := netip.ParseAddr(s)
	if err != nil {
		return ""
	}

	return addr.Unmap().String()
}

// hostOnly strips a trailing port and IPv6 brackets when present.
func hostOnly(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}

	return strings.Trim(s, "[]")
}
