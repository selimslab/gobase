package obs

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceID reports the W3C trace ID recorded in ctx, or "" when the context
// carries no sampled span.
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}

	return sc.TraceID().String()
}

// SpanID reports the span ID recorded in ctx, or "" when there is none.
func SpanID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasSpanID() {
		return ""
	}

	return sc.SpanID().String()
}

// LogAttrs returns the correlation attributes for ctx: request ID plus trace
// and span IDs when a span is active. Attach them to every request-scoped log
// line so logs and traces join on the same keys.
func LogAttrs(ctx context.Context) []slog.Attr {
	attrs := make([]slog.Attr, 0, 3)

	if id := RequestID(ctx); id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}

	if id := TraceID(ctx); id != "" {
		attrs = append(attrs, slog.String("trace_id", id))
	}

	if id := SpanID(ctx); id != "" {
		attrs = append(attrs, slog.String("span_id", id))
	}

	return attrs
}
