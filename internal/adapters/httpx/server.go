package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/selimslab/gobase/internal/platform/config"
)

// Server owns the listener and its lifecycle.
type Server struct {
	http     *http.Server
	logger   *slog.Logger
	ready    *Readiness
	shutdown time.Duration
}

// NewServer builds the HTTP server with every timeout set. The handler is
// wrapped in http.TimeoutHandler, so a stuck handler returns 503 instead of
// holding the connection until WriteTimeout kills it without a response.
func NewServer(cfg config.HTTP, deps Deps) *Server {
	handler := deps.HandlerT
	if handler == nil {
		handler = NewRouter(deps)
	}

	return &Server{
		http: &http.Server{
			Addr:    cfg.Addr,
			Handler: http.TimeoutHandler(handler, cfg.HandlerTimeout, ""),
			// ReadHeaderTimeout must be non-zero: without it a client can
			// hold a connection open by dribbling headers (Slowloris).
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			MaxHeaderBytes:    1 << 20,
			ErrorLog:          slog.NewLogLogger(deps.Logger.Handler(), slog.LevelWarn),
			BaseContext: func(net.Listener) context.Context {
				return context.Background()
			},
		},
		logger:   deps.Logger,
		ready:    deps.Ready,
		shutdown: cfg.ShutdownTimeout,
	}
}

// Addr reports the address the server is configured to listen on.
func (s *Server) Addr() string { return s.http.Addr }

// Run serves until ctx is canceled, then drains.
//
// The sequence on shutdown is deliberate: flip readiness to false first, so
// the load balancer stops sending new requests, then give in-flight requests
// ShutdownTimeout to finish, then force-close whatever is left. Closing first
// would fail requests that were already accepted.
func (s *Server) Run(ctx context.Context) error {
	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.http.Addr, err)
	}

	// Report the resolved address, so a ":0" port in tests is visible.
	s.http.Addr = ln.Addr().String()

	errCh := make(chan error, 1)

	go func() {
		s.logger.InfoContext(ctx, "http server listening", slog.String("addr", s.http.Addr))

		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve: %w", err)

			return
		}

		errCh <- nil
	}()

	s.ready.SetReady(true)

	select {
	case err := <-errCh:
		s.ready.SetReady(false)

		return err
	case <-ctx.Done():
		return s.drain(context.WithoutCancel(ctx), errCh)
	}
}

// drain stops accepting, waits out the in-flight requests, and closes. It
// takes an uncancellable copy of the request context: the context that
// triggered the shutdown is already done, and reusing it would abort the
// grace period immediately.
func (s *Server) drain(ctx context.Context, errCh <-chan error) error {
	s.ready.SetReady(false)
	s.logger.InfoContext(ctx, "shutting down", slog.Duration("timeout", s.shutdown))

	shutdownCtx, cancel := context.WithTimeout(ctx, s.shutdown)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		// Requests outlived the grace period. Cut them off rather than hang.
		if closeErr := s.http.Close(); closeErr != nil {
			return errors.Join(fmt.Errorf("graceful shutdown: %w", err), fmt.Errorf("force close: %w", closeErr))
		}

		return fmt.Errorf("graceful shutdown: %w", err)
	}

	if err := <-errCh; err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "shutdown complete")

	return nil
}
