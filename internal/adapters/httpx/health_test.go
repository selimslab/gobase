package httpx_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/selimslab/gobase/internal/adapters/httpx"
	"github.com/selimslab/gobase/internal/adapters/memstore"
	"github.com/selimslab/gobase/internal/domain"
	"github.com/selimslab/gobase/internal/platform/config"
	"github.com/selimslab/gobase/internal/platform/security"
)

// discardLogger keeps test output readable.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func testSecurityConfig() config.Security {
	return config.Security{
		MaxBodyBytes:   1 << 20,
		TrustedProxies: 0,
		HSTSMaxAge:     365 * 24 * time.Hour,
	}
}

// newTestRouter builds the full stack the way cmd/server does, so these tests
// exercise the real middleware chain rather than a handler in isolation.
func newTestRouter(t *testing.T, env config.Environment, mutate func(*httpx.Deps)) http.Handler {
	t.Helper()

	repo := memstore.NewExampleRepo()

	svc, err := domain.NewExampleService(repo, memstore.NewIDGenerator(), nil)
	if err != nil {
		t.Fatalf("NewExampleService() error = %v", err)
	}

	ready := httpx.NewReadiness()
	ready.SetReady(true)

	deps := httpx.Deps{
		Logger:   discardLogger(),
		Policy:   security.NewPolicy(env, testSecurityConfig()),
		Examples: svc,
		Ready:    ready,
		Version:  "test",
		Service:  "gobase-test",
	}

	if mutate != nil {
		mutate(&deps)
	}

	return httpx.NewRouter(deps)
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}

	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}

	if body.Version != "test" {
		t.Errorf("version = %q, want test", body.Version)
	}
}

func TestReadyzReportsReadiness(t *testing.T) {
	t.Parallel()

	ready := httpx.NewReadiness()

	router := newTestRouter(t, config.EnvDevelopment, func(d *httpx.Deps) {
		d.Ready = ready
	})

	// Before the server is listening, readiness must fail so the load
	// balancer keeps traffic away.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status before ready = %d, want 503", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want a problem document", ct)
	}

	ready.SetReady(true)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Errorf("status when ready = %d, want 200", rec.Code)
	}

	// Draining flips it back, which is what makes rolling deploys safe.
	ready.SetReady(false)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status while draining = %d, want 503", rec.Code)
	}
}

func TestUnknownPathIsAProblemDocument(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", http.NoBody))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
	}

	var p struct {
		Status    int    `json:"status"`
		Title     string `json:"title"`
		Instance  string `json:"instance"`
		RequestID string `json:"request_id"`
	}

	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}

	if p.Status != http.StatusNotFound {
		t.Errorf("problem status = %d, want 404", p.Status)
	}

	if p.Instance != "/nope" {
		t.Errorf("instance = %q, want /nope", p.Instance)
	}

	if p.RequestID == "" {
		t.Error("problem must carry the request id so a client can quote it")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/healthz", http.NoBody))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
