package httpx

import (
	"log/slog"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/selimslab/gobase/internal/domain"
	"github.com/selimslab/gobase/internal/platform/security"
)

// Deps are everything the HTTP adapter needs from the layers beneath it.
// They arrive already constructed: the adapter builds no dependency of its own.
type Deps struct {
	Logger   *slog.Logger
	Policy   security.Policy
	Examples *domain.ExampleService
	Ready    *Readiness
	Version  string
	Service  string
	HandlerT http.Handler // optional override, for tests
}

// NewRouter builds the route table and wraps it in the middleware stack.
//
// Order matters and runs outermost first:
//
//	request-id → recover → otelhttp → context-logger → access-log → secure-headers → body-limit
//
// request-id is outermost so every response, including a panic's 500, carries
// the ID that the log line for it also carries — that pairing is the whole
// point of the header. recover sits directly inside it, catching a panic from
// anything below. access-log sits inside the tracing span so it can record the
// trace ID, and outside the handler so it sees the final status.
func NewRouter(deps Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleLiveness(deps.Version))
	mux.HandleFunc("GET /readyz", handleReadiness(deps.Ready))

	if deps.Examples != nil {
		h := exampleHandler{svc: deps.Examples}
		mux.HandleFunc("POST /examples", h.create)
		mux.HandleFunc("GET /examples", h.list)
		mux.HandleFunc("GET /examples/{id}", h.get)
		mux.HandleFunc("POST /examples/{id}/restock", h.restock)
		mux.HandleFunc("DELETE /examples/{id}", h.remove)
	}

	// A catch-all "/" would swallow ServeMux's own 405 handling, so the
	// fallback is applied outside the mux instead: notFoundJSON asks the mux
	// what it would have done and only rewrites the plain-text 404/405 bodies
	// into problem documents.
	return chain(problemNotFound(mux),
		requestID,
		recoverPanic(deps.Logger),
		otelMiddleware(deps.Service),
		contextLogger(deps.Logger),
		accessLog(deps.Logger, deps.Policy),
		secureHeaders(deps.Policy),
		limitBody(deps.Policy.MaxBodyBytes),
	)
}

// otelMiddleware starts a server span per request, named by the matched route
// pattern rather than the raw path so high-cardinality IDs stay out of the
// span name.
func otelMiddleware(service string) middleware {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "http.server",
			otelhttp.WithServerName(service),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if pattern := r.Pattern; pattern != "" {
					return pattern
				}

				return r.Method + " " + r.URL.Path
			}),
			// The probes run every few seconds and say nothing useful; a
			// trace per probe is pure cost.
			otelhttp.WithFilter(func(r *http.Request) bool {
				return r.URL.Path != "/healthz" && r.URL.Path != "/readyz"
			}),
		)
	}
}

// problemNotFound converts net/http's plain-text 404 and 405 into the same
// problem document every other error uses, so a client parses one shape.
//
// It asks the mux whether a route matches before calling it, which is cheaper
// and more honest than buffering the response and rewriting it afterwards.
func problemNotFound(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r)

			return
		}

		// No pattern matched. The mux still distinguishes "this path exists
		// under another method" from "nothing here at all".
		if allowed := allowedMethods(mux, r); len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			writeProblem(w, r, http.StatusMethodNotAllowed, "the method is not allowed for this resource")

			return
		}

		writeProblem(w, r, http.StatusNotFound, "the requested resource does not exist")
	})
}

// allowedMethods reports which methods the mux would accept for this path.
func allowedMethods(mux *http.ServeMux, r *http.Request) []string {
	methods := []string{
		http.MethodGet, http.MethodHead, http.MethodPost,
		http.MethodPut, http.MethodPatch, http.MethodDelete,
	}

	allowed := make([]string, 0, len(methods))

	for _, method := range methods {
		if method == r.Method {
			continue
		}

		probe := r.Clone(r.Context())
		probe.Method = method

		if _, pattern := mux.Handler(probe); pattern != "" {
			allowed = append(allowed, method)
		}
	}

	return allowed
}
