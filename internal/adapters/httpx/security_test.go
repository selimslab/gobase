package httpx_test

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/selimslab/gobase/internal/adapters/httpx"
	"github.com/selimslab/gobase/internal/platform/config"
	"github.com/selimslab/gobase/internal/platform/network"
	"github.com/selimslab/gobase/internal/platform/security"
)

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvProduction, nil)

	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
		"Permissions-Policy":      "camera=(), microphone=(), geolocation=()",
	}

	// Success and failure paths alike: headers are set before the handler runs.
	for _, path := range []string{"/healthz", "/nope"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))

		for name, value := range want {
			if got := rec.Header().Get(name); got != value {
				t.Errorf("%s: header %s = %q, want %q", path, name, got, value)
			}
		}
	}
}

func TestHSTSAbsentOverPlainHTTP(t *testing.T) {
	t.Parallel()

	// Production is the strictest setting, and even there HSTS must not be
	// sent over a plain connection.
	router := newTestRouter(t, config.EnvProduction, nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS sent over plain HTTP: %q", got)
	}
}

func TestHSTSPresentOverTLS(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvProduction, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	req.TLS = &tls.ConnectionState{}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	got := rec.Header().Get("Strict-Transport-Security")
	if !strings.HasPrefix(got, "max-age=") {
		t.Errorf("HSTS over TLS = %q, want a max-age directive", got)
	}

	if !strings.Contains(got, "includeSubDomains") {
		t.Errorf("HSTS = %q, want includeSubDomains", got)
	}
}

func TestHSTSAbsentInDevelopmentOverTLS(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	req.TLS = &tls.ConnectionState{}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS sent in development: %q", got)
	}
}

func TestNoCORSHeadersByDefault(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvProduction, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	req.Header.Set("Origin", "https://evil.test")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	for _, h := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Credentials",
		"Access-Control-Allow-Methods",
	} {
		if got := rec.Header().Get(h); got != "" {
			t.Errorf("default policy emitted %s: %q", h, got)
		}
	}
}

func TestCORSHeadersWhenPolicyAllows(t *testing.T) {
	t.Parallel()

	const allowed = "https://app.example.com"

	policy := security.NewPolicy(config.EnvProduction, testSecurityConfig())
	policy.AllowOrigin = func(origin string) security.OriginDecision {
		if origin != allowed {
			return security.OriginDecision{}
		}

		return security.OriginDecision{Allow: true, AllowOrigin: origin, AllowCredentials: true}
	}

	router := newTestRouter(t, config.EnvProduction, func(d *httpx.Deps) {
		d.Policy = policy
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	req.Header.Set("Origin", allowed)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != allowed {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, allowed)
	}

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want true", got)
	}

	// Without Vary, a shared cache can hand one origin's response to another.
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("Vary = %q, want it to include Origin", got)
	}

	// A different origin still gets nothing.
	req = httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	req.Header.Set("Origin", "https://evil.test")

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unlisted origin got %q", got)
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	t.Parallel()

	policy := security.NewPolicy(config.EnvDevelopment, testSecurityConfig())
	policy.MaxBodyBytes = 64

	router := newTestRouter(t, config.EnvDevelopment, func(d *httpx.Deps) {
		d.Policy = policy
	})

	body := `{"name":"` + strings.Repeat("x", 512) + `","quantity":1}`

	req := httptest.NewRequest(http.MethodPost, "/examples", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want a problem document", ct)
	}
}

func TestBodyWithinLimitIsAccepted(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	req := httptest.NewRequest(http.MethodPost, "/examples", strings.NewReader(`{"name":"bolt","quantity":2}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/examples/") {
		t.Errorf("Location = %q, want a example URL", loc)
	}
}

func TestForwardedForIgnoredWithoutTrustedProxy(t *testing.T) {
	t.Parallel()

	// The default policy trusts zero proxies, so a client cannot choose the
	// IP that lands in the access log or in any rate limiter built on it.
	policy := security.NewPolicy(config.EnvProduction, testSecurityConfig())

	if policy.TrustedProxies != 0 {
		t.Fatalf("TrustedProxies = %d, want 0 by default", policy.TrustedProxies)
	}

	got := network.ClientIP("10.0.0.9:1234", []string{"1.2.3.4"}, policy.TrustedProxies)
	if got != "10.0.0.9" {
		t.Errorf("ClientIP = %q, want the connection peer", got)
	}
}

func TestUnknownJSONFieldIsRejected(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	req := httptest.NewRequest(http.MethodPost, "/examples",
		strings.NewReader(`{"name":"bolt","quantity":1,"is_admin":true}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unknown field", rec.Code)
	}
}

func TestWrongContentTypeIsRejected(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	req := httptest.NewRequest(http.MethodPost, "/examples", strings.NewReader(`{"name":"bolt"}`))
	req.Header.Set("Content-Type", "text/plain")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", rec.Code)
	}
}

func TestDomainValidationBecomes422(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	req := httptest.NewRequest(http.MethodPost, "/examples", strings.NewReader(`{"name":"","quantity":-5}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
	}

	var p struct {
		Detail string `json:"detail"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}

	if p.Detail == "" {
		t.Error("a validation failure must say what was wrong")
	}
}

func TestMissingExampleBecomes404Problem(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/examples/nope", http.NoBody))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	// The internal error text names the ID; the client response must not
	// echo storage internals back.
	if strings.Contains(rec.Body.String(), "example \"nope\"") {
		t.Errorf("internal error text leaked: %s", rec.Body.String())
	}
}

func TestExampleLifecycle(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	create := httptest.NewRequest(http.MethodPost, "/examples", strings.NewReader(`{"name":"bolt","quantity":1}`))
	create.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, create)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}

	var created struct {
		ID       string `json:"id"`
		Quantity int    `json:"quantity"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created example: %v", err)
	}

	restock := httptest.NewRequest(http.MethodPost, "/examples/"+created.ID+"/restock",
		strings.NewReader(`{"quantity":4}`))
	restock.Header.Set("Content-Type", "application/json")

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, restock)

	if rec.Code != http.StatusOK {
		t.Fatalf("restock status = %d: %s", rec.Code, rec.Body.String())
	}

	var restocked struct {
		Quantity int `json:"quantity"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &restocked); err != nil {
		t.Fatalf("decode restocked example: %v", err)
	}

	if restocked.Quantity != 5 {
		t.Errorf("quantity = %d, want 5", restocked.Quantity)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/examples/"+created.ID, http.NoBody))

	if rec.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", rec.Code)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/examples/"+created.ID, http.NoBody))

	if rec.Code != http.StatusNotFound {
		t.Errorf("get after delete status = %d, want 404", rec.Code)
	}
}
