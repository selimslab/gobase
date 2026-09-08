// Command server is the service entry point.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/selimslab/gobase/internal/adapters/httpx"
	"github.com/selimslab/gobase/internal/adapters/memstore"
	"github.com/selimslab/gobase/internal/domain"
	"github.com/selimslab/gobase/internal/platform/config"
	"github.com/selimslab/gobase/internal/platform/obs"
	"github.com/selimslab/gobase/internal/platform/security"
	"github.com/selimslab/gobase/internal/platform/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// run is the composition root: the only place all three layers meet, and the
// only place a port is bound to an implementation. Swapping memstore for a
// real store is one line here and no change anywhere else.
func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	v, commit, buildTime := version.Info()

	logger := obs.NewLogger(os.Stdout, cfg.Observability, cfg.ServiceName, v)
	slog.SetDefault(logger)
	logger.InfoContext(ctx, "starting",
		slog.String("commit", commit),
		slog.String("build_time", buildTime),
		slog.Any("config", cfg),
	)

	shutdownOTel, err := obs.InitOTel(ctx, cfg.Observability, cfg.ServiceName, v, string(cfg.Env))
	if err != nil {
		return fmt.Errorf("init otel: %w", err)
	}

	policy := security.NewPolicy(cfg.Env, cfg.Security)

	repo := memstore.NewWidgetRepo()

	widgets, err := domain.NewWidgetService(repo, memstore.NewIDGenerator(), nil)
	if err != nil {
		return fmt.Errorf("init widget service: %w", err)
	}

	srv := httpx.NewServer(cfg.HTTP, httpx.Deps{
		Logger:  logger,
		Policy:  policy,
		Widgets: widgets,
		Ready:   httpx.NewReadiness(),
		Version: v,
		Service: cfg.ServiceName,
	})

	runErr := srv.Run(ctx)

	// Flush telemetry after the server drains, so the last spans are exported.
	// This context is fresh: ctx is already canceled by the signal.
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	return errors.Join(runErr, shutdownOTel(flushCtx))
}
