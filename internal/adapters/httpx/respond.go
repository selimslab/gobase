// Package httpx is the inbound HTTP adapter. It is the only package that
// knows about net/http; the layers beneath it stay transport-agnostic.
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/selimslab/gobase/internal/domain"
	"github.com/selimslab/gobase/internal/platform/obs"
)

// contentTypeJSON is the media type for successful responses.
const contentTypeJSON = "application/json; charset=utf-8"

// contentTypeProblem is the RFC 7807 media type for error responses.
const contentTypeProblem = "application/problem+json"

// Problem is an RFC 7807 problem document. It is the only error shape this
// service emits, so clients parse one format for every failure.
type Problem struct {
	// Type is a URI identifying the problem kind.
	Type string `json:"type"`
	// Title is a short, stable summary.
	Title string `json:"title"`
	// Status repeats the HTTP status code.
	Status int `json:"status"`
	// Detail explains this occurrence. It must never leak internals.
	Detail string `json:"detail,omitempty"`
	// Instance is the request path that produced the problem.
	Instance string `json:"instance,omitempty"`
	// RequestID correlates the response with the server logs.
	RequestID string `json:"request_id,omitempty"`
	// TraceID correlates the response with the trace.
	TraceID string `json:"trace_id,omitempty"`
}

// writeJSON encodes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)

	if v == nil || r.Method == http.MethodHead {
		return
	}

	// The status line is already sent, so a failure here can only be logged.
	if err := json.NewEncoder(w).Encode(v); err != nil {
		obs.LoggerFrom(r.Context()).ErrorContext(r.Context(), "encoding response failed",
			slogErr(err),
		)
	}
}

// writeProblem emits an RFC 7807 document. detail is client-facing: pass only
// text you would be happy to see in an attacker's terminal.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	ctx := r.Context()

	p := Problem{
		Type:      "about:blank",
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		Instance:  r.URL.Path,
		RequestID: obs.RequestID(ctx),
		TraceID:   obs.TraceID(ctx),
	}

	w.Header().Set("Content-Type", contentTypeProblem)
	w.WriteHeader(status)

	if r.Method == http.MethodHead {
		return
	}

	if err := json.NewEncoder(w).Encode(p); err != nil {
		obs.LoggerFrom(ctx).ErrorContext(ctx, "encoding problem failed", slogErr(err))
	}
}

// problemForError maps a domain error to its transport status. Only the
// mapped kinds carry their message outward; anything else is reported as an
// opaque 500 and the real error goes to the log.
func problemForError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "the requested resource does not exist")
	case errors.Is(err, domain.ErrAlreadyExists):
		writeProblem(w, r, http.StatusConflict, "the resource already exists")
	case errors.Is(err, domain.ErrInvalid):
		writeProblem(w, r, http.StatusUnprocessableEntity, err.Error())
	default:
		ctx := r.Context()
		obs.LoggerFrom(ctx).ErrorContext(ctx, "unhandled error", slogErr(err))
		writeProblem(w, r, http.StatusInternalServerError, "an internal error occurred")
	}
}
