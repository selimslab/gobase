package httpx_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/selimslab/gobase/internal/adapters/httpx"
	"github.com/selimslab/gobase/internal/platform/config"
	"github.com/selimslab/gobase/internal/platform/obs"
)

func TestRequestIDIsGeneratedWhenAbsent(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))

	id := rec.Header().Get(httpx.HeaderRequestID)
	if id == "" {
		t.Fatal("no request id on the response")
	}

	if len(id) != 32 {
		t.Errorf("generated id = %q, want 32 hex characters", id)
	}

	// Two requests must not share an id.
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))

	if rec2.Header().Get(httpx.HeaderRequestID) == id {
		t.Error("two requests were given the same id")
	}
}

func TestRequestIDPassesThrough(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	req.Header.Set(httpx.HeaderRequestID, "client-supplied-id")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get(httpx.HeaderRequestID); got != "client-supplied-id" {
		t.Errorf("request id = %q, want the client's value echoed back", got)
	}
}

func TestRequestIDRejectsHostileValues(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, config.EnvDevelopment, nil)

	tests := []struct {
		name string
		id   string
	}{
		{name: "too long", id: strings.Repeat("a", 129)},
		{name: "newline injection", id: "abc\nX-Admin: true"},
		{name: "control characters", id: "abc\x00def"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
			// Set the header directly: http.Header.Set would not stop us, and
			// this is exactly what a hostile client sends.
			req.Header[http.CanonicalHeaderKey(httpx.HeaderRequestID)] = []string{tt.id}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			got := rec.Header().Get(httpx.HeaderRequestID)
			if got == tt.id {
				t.Errorf("hostile request id %q was echoed back", tt.id)
			}

			if len(got) != 32 {
				t.Errorf("request id = %q, want a freshly generated one", got)
			}
		})
	}
}

func TestPanicBecomesOpaque500(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom: db password is hunter2")
	})

	handler := httpx.ChainForTest(panicking,
		httpx.RequestIDForTest,
		httpx.RecoverPanicForTest(logger),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", http.NoBody))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, "hunter2") || strings.Contains(body, "db password") {
		t.Errorf("panic details leaked to the client: %s", body)
	}

	if strings.Contains(body, "goroutine") {
		t.Errorf("stack trace leaked to the client: %s", body)
	}

	var p struct {
		Status    int    `json:"status"`
		Detail    string `json:"detail"`
		RequestID string `json:"request_id"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("response is not a problem document: %v", err)
	}

	if p.RequestID == "" {
		t.Error("the 500 must carry a request id to correlate with the log")
	}

	// The operator, unlike the client, gets everything.
	logged := logs.String()
	if !strings.Contains(logged, "panic recovered") {
		t.Errorf("panic was not logged: %s", logged)
	}

	if !strings.Contains(logged, "hunter2") {
		t.Error("the panic value must reach the log")
	}

	if !strings.Contains(logged, "stack") {
		t.Error("the stack trace must reach the log")
	}
}

func TestRecoverReraisesAbortHandler(t *testing.T) {
	t.Parallel()

	aborting := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})

	handler := httpx.RecoverPanicForTest(discardLogger())(aborting)

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler { //nolint:errorlint // identity comparison is the point
			t.Errorf("recovered %v, want ErrAbortHandler to propagate to net/http", rec)
		}
	}()

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", http.NoBody))
}

func TestContextCarriesRequestIDToHandlers(t *testing.T) {
	t.Parallel()

	var seen string

	inspect := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = obs.RequestID(r.Context())
	})

	handler := httpx.ChainForTest(inspect, httpx.RequestIDForTest)

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set(httpx.HeaderRequestID, "trace-me")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "trace-me" {
		t.Errorf("handler saw request id %q, want trace-me", seen)
	}
}

func TestAccessLogRecordsRequest(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	router := newTestRouter(t, config.EnvDevelopment, func(d *httpx.Deps) {
		d.Logger = logger
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))

	line := logs.String()

	for _, want := range []string{`"msg":"request"`, `"method":"GET"`, `"path":"/healthz"`, `"status":200`, `"request_id"`, `"client_ip"`} {
		if !strings.Contains(line, want) {
			t.Errorf("access log is missing %s: %s", want, line)
		}
	}
}

func TestAccessLogLevelsByStatus(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	router := newTestRouter(t, config.EnvDevelopment, func(d *httpx.Deps) {
		d.Logger = logger
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", http.NoBody))

	// A 404 is the client's problem, not an incident.
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("a 404 must not log at ERROR: %s", logs.String())
	}
}
