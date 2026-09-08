package httpx_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/selimslab/gobase/internal/adapters/httpx"
	"github.com/selimslab/gobase/internal/platform/config"
)

// The access log must join to the trace. With a real tracer installed, the
// log line carries the same trace_id the span does.
func TestAccessLogCarriesTraceID(t *testing.T) {
	prev := otel.GetTracerProvider()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(tp)

	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(t.Context())
	})

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	router := newTestRouter(t, config.EnvDevelopment, func(d *httpx.Deps) {
		d.Logger = logger
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/widgets", http.NoBody))

	var line map[string]any
	dec := json.NewDecoder(bytes.NewReader(logs.Bytes()))

	for dec.More() {
		var l map[string]any
		if err := dec.Decode(&l); err != nil {
			t.Fatalf("decode log: %v", err)
		}

		if l["msg"] == "request" {
			line = l
		}
	}

	if line == nil {
		t.Fatalf("no access log line: %s", logs.String())
	}

	traceID, _ := line["trace_id"].(string)
	if len(traceID) != 32 {
		t.Errorf("trace_id = %q, want a 32-character trace id: %s", traceID, logs.String())
	}

	if _, ok := line["request_id"].(string); !ok {
		t.Errorf("request_id missing from the access log: %s", logs.String())
	}
}
