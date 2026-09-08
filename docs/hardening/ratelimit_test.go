//go:build hardening

package hardening_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/selimslab/gobase/docs/hardening"
)

func testLimiter(r rate.Limit, burst int) *hardening.RateLimiter {
	return &hardening.RateLimiter{
		Rate:    r,
		Burst:   burst,
		TTL:     time.Minute,
		KeyFunc: func(req *http.Request) string { return req.RemoteAddr },
	}
}

func TestRateLimiterAllowsBurstThenRejects(t *testing.T) {
	t.Parallel()

	limiter := testLimiter(1, 2)
	limiter.Start()

	t.Cleanup(limiter.Stop)

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := limiter.Middleware(ok)

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "10.0.0.1:1234"

	// The burst is spent first.
	for i := range 2 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i+1, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}

	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 must tell the client when to retry")
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want a problem document", ct)
	}
}

func TestRateLimiterIsPerClient(t *testing.T) {
	t.Parallel()

	limiter := testLimiter(1, 1)
	limiter.Start()

	t.Cleanup(limiter.Stop)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	first := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	first.RemoteAddr = "10.0.0.1:1111"

	second := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	second.RemoteAddr = "10.0.0.2:2222"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, first)

	if rec.Code != http.StatusOK {
		t.Fatalf("first client: status = %d, want 200", rec.Code)
	}

	// One client exhausting its budget must not affect another.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, second)

	if rec.Code != http.StatusOK {
		t.Errorf("second client: status = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, first)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("first client again: status = %d, want 429", rec.Code)
	}
}

func TestAllowIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	limiter := testLimiter(1000, 1000)
	limiter.Start()

	t.Cleanup(limiter.Stop)

	done := make(chan struct{})

	for i := range 20 {
		go func() {
			defer func() { done <- struct{}{} }()

			for range 50 {
				limiter.Allow(string(rune('a' + i%5)))
			}
		}()
	}

	for range 20 {
		<-done
	}
}
