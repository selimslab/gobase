//go:build hardening

// Package hardening holds the options this template deliberately leaves out
// of the default build. Each file states when you need it and what it costs.
//
// Nothing here is compiled into the server binary: the `hardening` build tag
// keeps it out. CI still compiles and tests it, so an option cannot rot.
//
// To adopt one, move the file into the package where it belongs, drop the
// build tag, and wire it in cmd/server.
package hardening

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// # Rate limiting
//
// When you need it: any endpoint reachable from the internet that costs more
// to serve than to request — login, search, anything that writes.
//
// What it costs: one map per process that grows with the number of distinct
// clients, plus a sweeper goroutine. It is per-instance, so N replicas allow
// N times the configured rate. If you need a global limit, you need shared
// state (Redis) and the latency that comes with it.
//
// What it does not do: stop a distributed flood. That belongs at the edge —
// a CDN or a load balancer — not in the process.

// RateLimiter allows a burst of requests per client and then refills at a
// steady rate. Clients are keyed by whatever KeyFunc returns.
type RateLimiter struct {
	// Rate is the sustained requests per second allowed per client.
	Rate rate.Limit
	// Burst is how many requests may arrive at once.
	Burst int
	// TTL is how long an idle client is remembered.
	TTL time.Duration
	// KeyFunc identifies the client. Use security.ClientIP, never a header
	// the client controls.
	KeyFunc func(*http.Request) string

	mu      sync.Mutex
	clients map[string]*client
	stop    chan struct{}
	once    sync.Once
}

type client struct {
	limiter *rate.Limiter
	seen    time.Time
}

// Start begins evicting idle clients. Call it once; call Stop to release the
// goroutine. Without this, the map is an unbounded memory leak keyed by
// anything an attacker can vary.
func (l *RateLimiter) Start() {
	l.once.Do(func() {
		l.clients = make(map[string]*client)
		l.stop = make(chan struct{})

		go l.sweep()
	})
}

// Stop ends the sweeper.
func (l *RateLimiter) Stop() {
	if l.stop != nil {
		close(l.stop)
	}
}

func (l *RateLimiter) sweep() {
	ticker := time.NewTicker(l.TTL)
	defer ticker.Stop()

	for {
		select {
		case <-l.stop:
			return
		case now := <-ticker.C:
			l.mu.Lock()

			for key, c := range l.clients {
				if now.Sub(c.seen) > l.TTL {
					delete(l.clients, key)
				}
			}

			l.mu.Unlock()
		}
	}
}

// Allow reports whether this key may make a request now.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	c, ok := l.clients[key]
	if !ok {
		c = &client{limiter: rate.NewLimiter(l.Rate, l.Burst)}
		l.clients[key] = c
	}

	c.seen = time.Now()

	return c.limiter.Allow()
}

// Middleware rejects a client over its limit with 429 and a Retry-After
// header, so a well-behaved client knows when to come back.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	l.Start()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := l.KeyFunc(r)

		if !l.Allow(key) {
			retryAfter := max(int(time.Second/time.Duration(l.Rate)), 1)

			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Too Many Requests","status":429,` +
				`"detail":"rate limit exceeded"}` + "\n"))

			return
		}

		next.ServeHTTP(w, r)
	})
}
