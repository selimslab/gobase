package httpx

import (
	"net/http"
	"sync/atomic"
)

// healthResponse is the body of a probe response.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// Readiness reports whether the service may receive traffic. It starts not
// ready, becomes ready once the server is listening, and returns to not ready
// at the first shutdown signal so the load balancer drains the instance before
// connections are closed.
type Readiness struct {
	ready atomic.Bool
}

// NewReadiness returns a probe that is not ready yet.
func NewReadiness() *Readiness { return &Readiness{} }

// SetReady marks the service ready or draining.
func (r *Readiness) SetReady(ready bool) { r.ready.Store(ready) }

// Ready reports the current state.
func (r *Readiness) Ready() bool { return r.ready.Load() }

// handleLiveness answers /healthz: the process is up and can serve. It must
// not check dependencies — a failing liveness probe gets the pod killed.
func handleLiveness(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, healthResponse{Status: "ok", Version: version})
	}
}

// handleReadiness answers /readyz: the process is willing to take traffic.
// Add dependency checks here when you add dependencies.
func handleReadiness(ready *Readiness) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ready.Ready() {
			writeProblem(w, r, http.StatusServiceUnavailable, "service is not ready")

			return
		}

		writeJSON(w, r, http.StatusOK, healthResponse{Status: "ready"})
	}
}
