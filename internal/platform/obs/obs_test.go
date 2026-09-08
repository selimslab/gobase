package obs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/selimslab/gobase/internal/platform/config"
	"github.com/selimslab/gobase/internal/platform/obs"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := map[string]slog.Level{
		"debug":    slog.LevelDebug,
		"DEBUG":    slog.LevelDebug,
		"  info  ": slog.LevelInfo, //nolint:gocritic // leading and trailing space is the case under test
		"warn":     slog.LevelWarn,
		"warning":  slog.LevelWarn,
		"error":    slog.LevelError,
		"":         slog.LevelInfo,
		"nonsense": slog.LevelInfo,
	}

	for name, want := range tests {
		if got := obs.ParseLevel(name); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestNewLoggerEmitsJSONWithServiceIdentity(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := obs.NewLogger(&buf, config.Observability{LogLevel: "info"}, "billing", "v1.2.3")
	logger.Info("hello")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line is not JSON: %v (%s)", err, buf.String())
	}

	if line["service"] != "billing" {
		t.Errorf("service = %v, want billing", line["service"])
	}

	if line["version"] != "v1.2.3" {
		t.Errorf("version = %v, want v1.2.3", line["version"])
	}
}

func TestNewLoggerHonoursLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := obs.NewLogger(&buf, config.Observability{LogLevel: "warn"}, "billing", "v1")
	logger.Info("dropped")
	logger.Warn("kept")

	out := buf.String()
	if strings.Contains(out, "dropped") {
		t.Errorf("info line was emitted at warn level: %s", out)
	}

	if !strings.Contains(out, "kept") {
		t.Errorf("warn line was dropped: %s", out)
	}
}

func TestRequestIDRoundTripsThroughContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	if got := obs.RequestID(ctx); got != "" {
		t.Errorf("RequestID on a bare context = %q, want empty", got)
	}

	ctx = obs.WithRequestID(ctx, "abc123")
	if got := obs.RequestID(ctx); got != "abc123" {
		t.Errorf("RequestID = %q, want abc123", got)
	}
}

func TestLoggerFromFallsBackToDefault(t *testing.T) {
	t.Parallel()

	if obs.LoggerFrom(context.Background()) == nil {
		t.Fatal("LoggerFrom must never return nil")
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	ctx := obs.WithLogger(context.Background(), logger)
	obs.LoggerFrom(ctx).Info("scoped")

	if !strings.Contains(buf.String(), "scoped") {
		t.Errorf("the context logger was not used: %s", buf.String())
	}
}

func TestTraceIDEmptyWithoutSpan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	if got := obs.TraceID(ctx); got != "" {
		t.Errorf("TraceID = %q, want empty without a span", got)
	}

	if got := obs.SpanID(ctx); got != "" {
		t.Errorf("SpanID = %q, want empty without a span", got)
	}
}

func TestLogAttrsCarriesCorrelationIDs(t *testing.T) {
	t.Parallel()

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("TraceIDFromHex: %v", err)
	}

	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("SpanIDFromHex: %v", err)
	}

	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))
	ctx = obs.WithRequestID(ctx, "req-1")

	got := make(map[string]string)
	for _, a := range obs.LogAttrs(ctx) {
		got[a.Key] = a.Value.String()
	}

	want := map[string]string{
		"request_id": "req-1",
		"trace_id":   "4bf92f3577b34da6a3ce929d0e0e4736",
		"span_id":    "00f067aa0ba902b7",
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestInitOTelWithoutEndpointIsANoop(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	shutdown, err := obs.InitOTel(ctx, config.Observability{SampleRatio: 1}, "billing", "v1", "test")
	if err != nil {
		t.Fatalf("InitOTel() error = %v", err)
	}

	if shutdown == nil {
		t.Fatal("InitOTel must always return a shutdown function")
	}

	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}
