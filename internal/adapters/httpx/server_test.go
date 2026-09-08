package httpx_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/selimslab/gobase/internal/adapters/httpx"
	"github.com/selimslab/gobase/internal/platform/config"
	"github.com/selimslab/gobase/internal/platform/security"
)

func testHTTPConfig() config.HTTP {
	return config.HTTP{
		Addr:              "127.0.0.1:0",
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       10 * time.Second,
		HandlerTimeout:    2 * time.Second,
		ShutdownTimeout:   3 * time.Second,
	}
}

func TestServerServesAndShutsDownGracefully(t *testing.T) {
	t.Parallel()

	ready := httpx.NewReadiness()

	srv := httpx.NewServer(testHTTPConfig(), httpx.Deps{
		Logger:  discardLogger(),
		Policy:  security.NewPolicy(config.EnvDevelopment, testSecurityConfig()),
		Ready:   ready,
		Version: "test",
		Service: "gobase-test",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	waitFor(t, ready.Ready, "server to become ready")

	resp, err := http.Get("http://" + srv.Addr() + "/healthz") //nolint:noctx // a plain probe is the point
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// A signal cancels the context; the server must drain and return nil.
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() error = %v, want a clean shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return within the shutdown timeout")
	}

	// Readiness flips off first, so a load balancer drains the instance
	// before connections are closed.
	if ready.Ready() {
		t.Error("readiness must be false after shutdown")
	}
}

func TestServerReportsListenFailure(t *testing.T) {
	t.Parallel()

	cfg := testHTTPConfig()
	// Port 1 needs privileges this test does not have.
	cfg.Addr = "127.0.0.1:1"

	srv := httpx.NewServer(cfg, httpx.Deps{
		Logger:  discardLogger(),
		Policy:  security.NewPolicy(config.EnvDevelopment, testSecurityConfig()),
		Ready:   httpx.NewReadiness(),
		Version: "test",
		Service: "gobase-test",
	})

	err := srv.Run(context.Background())
	if err == nil {
		t.Fatal("Run() = nil, want a listen error")
	}

	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("Run() error = %v, want it to name the address it failed to bind", err)
	}
}

func TestServerAppliesHandlerTimeout(t *testing.T) {
	t.Parallel()

	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusOK)
	})

	cfg := testHTTPConfig()
	cfg.HandlerTimeout = 200 * time.Millisecond

	ready := httpx.NewReadiness()

	srv := httpx.NewServer(cfg, httpx.Deps{
		Logger:   discardLogger(),
		Policy:   security.NewPolicy(config.EnvDevelopment, testSecurityConfig()),
		Ready:    ready,
		Version:  "test",
		Service:  "gobase-test",
		HandlerT: slow,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.Run(ctx) }()

	waitFor(t, ready.Ready, "server to become ready")

	start := time.Now()

	resp, err := http.Get("http://" + srv.Addr() + "/slow") //nolint:noctx // exercising the server's own deadline
	if err != nil {
		t.Fatalf("GET /slow: %v", err)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	// Without TimeoutHandler the client would wait out WriteTimeout and get
	// no response at all.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 from the handler timeout", resp.StatusCode)
	}

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("request took %s, want the handler timeout to fire first", elapsed)
	}
}

// waitFor polls cond until it holds or the test gives up.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}
