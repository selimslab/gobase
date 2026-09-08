//go:build hardening

// Package worker is an outbound adapter for background work: the same shape
// as httpx, on the other side of the process.
//
// When you need it: anything that must happen off the request path — sending
// mail, reconciling state, expiring rows. A handler that does slow work
// inline is a handler that times out.
//
// What it costs: a second lifecycle to shut down cleanly, and every job must
// be idempotent, because at-least-once is the only delivery you get from a
// process that can be killed mid-job.
//
// This is a periodic in-process worker. It is the right answer for
// housekeeping and the wrong answer for work that must survive a restart —
// that needs a queue, and a queue is a different adapter.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/selimslab/gobase/internal/platform/obs"
)

// Job is one unit of background work. It must be idempotent and must respect
// the context: a job that ignores cancellation blocks shutdown.
type Job interface {
	// Name identifies the job in logs and metrics.
	Name() string
	// Run does the work once.
	Run(ctx context.Context) error
}

// JobFunc adapts a function to the Job interface.
type JobFunc struct {
	JobName string
	Fn      func(context.Context) error
}

// Name implements Job.
func (j JobFunc) Name() string { return j.JobName }

// Run implements Job.
func (j JobFunc) Run(ctx context.Context) error { return j.Fn(ctx) }

// Schedule pairs a job with how often it runs.
type Schedule struct {
	Job      Job
	Interval time.Duration
	// Timeout bounds one run. A run that overruns is cancelled, not stacked
	// on top of the next one.
	Timeout time.Duration
}

// Worker runs scheduled jobs until its context is cancelled.
type Worker struct {
	logger    *slog.Logger
	schedules []Schedule
}

// New builds a worker over the given schedules.
func New(logger *slog.Logger, schedules ...Schedule) (*Worker, error) {
	for _, s := range schedules {
		if s.Job == nil {
			return nil, errors.New("worker: job must not be nil")
		}

		if s.Interval <= 0 {
			return nil, fmt.Errorf("worker: %s: interval must be positive", s.Job.Name())
		}

		if s.Timeout <= 0 || s.Timeout > s.Interval {
			return nil, fmt.Errorf("worker: %s: timeout must be positive and no longer than the interval", s.Job.Name())
		}
	}

	return &Worker{logger: logger, schedules: schedules}, nil
}

// Run starts every job and blocks until ctx is cancelled and all of them have
// returned. A job that panics is logged and restarted on its next tick; one
// bad job must not take the process down.
func (w *Worker) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	for _, s := range w.schedules {
		wg.Add(1)

		go func() {
			defer wg.Done()

			w.loop(ctx, s)
		}()
	}

	wg.Wait()
	w.logger.InfoContext(ctx, "worker stopped")

	return nil
}

func (w *Worker) loop(ctx context.Context, s Schedule) {
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()

	logger := w.logger.With(slog.String("job", s.Job.Name()))

	for {
		select {
		case <-ctx.Done():
			logger.InfoContext(ctx, "job stopping")

			return
		case <-ticker.C:
			w.runOnce(ctx, s, logger)
		}
	}
}

// runOnce executes one run under its own deadline, recovering from a panic so
// the ticker survives it.
func (w *Worker) runOnce(ctx context.Context, s Schedule, logger *slog.Logger) {
	runCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()

	defer func() {
		if rec := recover(); rec != nil {
			logger.ErrorContext(runCtx, "job panicked", slog.Any("panic", rec))
		}
	}()

	start := time.Now()

	if err := s.Job.Run(runCtx); err != nil {
		// A cancelled run during shutdown is expected, not an incident.
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return
		}

		logger.LogAttrs(runCtx, slog.LevelError, "job failed",
			append(obs.LogAttrs(runCtx),
				slog.String("error", err.Error()),
				slog.Duration("duration", time.Since(start)),
			)...,
		)

		return
	}

	logger.LogAttrs(runCtx, slog.LevelInfo, "job completed",
		append(obs.LogAttrs(runCtx), slog.Duration("duration", time.Since(start)))...,
	)
}
