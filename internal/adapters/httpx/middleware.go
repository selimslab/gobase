package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/selimslab/gobase/internal/platform/network"
	"github.com/selimslab/gobase/internal/platform/obs"
	"github.com/selimslab/gobase/internal/platform/security"
)

// headerRequestID carries the correlation ID in and out of the service.
const headerRequestID = "X-Request-Id"

// maxRequestIDLen bounds an inbound request ID. It is echoed in responses and
// written to logs, so an unbounded client-supplied value is a log-flooding
// vector.
const maxRequestIDLen = 128

// middleware wraps a handler with one concern.
type middleware func(http.Handler) http.Handler

// chain applies middlewares so the first listed is the outermost, which is
// the order they run in per request.
func chain(h http.Handler, mws ...middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}

	return h
}

// recoverPanic turns a panic into an opaque 500. The stack goes to the log
// with the request ID; the client gets nothing but that ID.
func recoverPanic(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The deferred closure logs with r.Context(), the request's own
			// context; contextcheck cannot follow a context across a defer.
			defer func() { //nolint:contextcheck // r.Context() is used inside
				rec := recover()
				if rec == nil {
					return
				}

				// A panic after the client disconnected is net/http's own
				// signal, not a bug in the handler; re-panic so the server
				// handles it.
				if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel is compared by identity
					panic(rec)
				}

				ctx := r.Context()
				logger.LogAttrs(ctx, slog.LevelError, "panic recovered",
					append(obs.LogAttrs(ctx),
						slog.String("panic", fmt.Sprint(rec)),
						slog.String("stack", string(debug.Stack())),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
					)...,
				)

				writeProblem(w, r, http.StatusInternalServerError, "an internal error occurred")
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// requestID adopts a sane inbound X-Request-Id or generates one, puts it in
// the context, and echoes it on the response so a client can quote it.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeRequestID(r.Header.Get(headerRequestID))
		if id == "" {
			id = newRequestID()
		}

		w.Header().Set(headerRequestID, id)

		next.ServeHTTP(w, r.WithContext(obs.WithRequestID(r.Context(), id)))
	})
}

// sanitizeRequestID keeps a client-supplied ID only when it is short and
// printable ASCII, so it cannot forge log fields or split headers.
func sanitizeRequestID(id string) string {
	if id == "" || len(id) > maxRequestIDLen {
		return ""
	}

	for i := range len(id) {
		if c := id[i]; c < 0x20 || c > 0x7e {
			return ""
		}
	}

	return id
}

// newRequestID returns a random 128-bit hex identifier.
func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])

	return hex.EncodeToString(b[:])
}

// contextLogger attaches a logger carrying the request ID and trace IDs, so
// handlers log correlated lines with obs.LoggerFrom(ctx).
func contextLogger(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			attrs := obs.LogAttrs(ctx)
			args := make([]any, 0, len(attrs))

			for _, a := range attrs {
				args = append(args, a)
			}

			next.ServeHTTP(w, r.WithContext(obs.WithLogger(ctx, logger.With(args...))))
		})
	}
}

// accessLog records one line per completed request.
func accessLog(logger *slog.Logger, policy security.Policy) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			ctx := r.Context()
			logger.LogAttrs(ctx, levelForStatus(rec.status), "request",
				append(obs.LogAttrs(ctx),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", rec.status),
					slog.Int64("bytes", rec.written),
					slog.Duration("duration", time.Since(start)),
					slog.String("client_ip", clientIP(r, policy)),
					slog.String("user_agent", r.UserAgent()),
				)...,
			)
		})
	}
}

// levelForStatus logs server errors loudly and everything else at info, so a
// 404 does not page anyone.
func levelForStatus(status int) slog.Level {
	if status >= http.StatusInternalServerError {
		return slog.LevelError
	}

	return slog.LevelInfo
}

// clientIP resolves the caller's address under the trusted-proxy policy.
func clientIP(r *http.Request, policy security.Policy) string {
	return network.ClientIP(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), policy.TrustedProxies)
}

// statusRecorder remembers what the handler wrote, so the access log can
// report it without buffering the body.
type statusRecorder struct {
	http.ResponseWriter

	status  int
	written int64
	wrote   bool
}

// WriteHeader records the first status written.
func (s *statusRecorder) WriteHeader(status int) {
	if s.wrote {
		return
	}

	s.wrote = true
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

// Write records the byte count, defaulting the status to 200 as net/http does.
func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.WriteHeader(http.StatusOK)
	}

	n, err := s.ResponseWriter.Write(b)
	s.written += int64(n)

	if err != nil {
		return n, fmt.Errorf("write response: %w", err)
	}

	return n, nil
}

// Unwrap exposes the wrapped writer so http.ResponseController can reach the
// optional interfaces (Flusher, Hijacker) this type does not implement.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
