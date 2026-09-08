//go:build hardening

package worker_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/selimslab/gobase/docs/hardening/worker"
)

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestNewRejectsBadSchedules(t *testing.T) {
	t.Parallel()

	job := worker.JobFunc{JobName: "noop", Fn: func(context.Context) error { return nil }}

	tests := []struct {
		name     string
		schedule worker.Schedule
	}{
		{name: "nil job", schedule: worker.Schedule{Interval: time.Second, Timeout: time.Second}},
		{name: "zero interval", schedule: worker.Schedule{Job: job, Timeout: time.Second}},
		{name: "zero timeout", schedule: worker.Schedule{Job: job, Interval: time.Second}},
		// A timeout longer than the interval means runs pile up.
		{name: "timeout exceeds interval", schedule: worker.Schedule{Job: job, Interval: time.Second, Timeout: 2 * time.Second}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := worker.New(discardLogger(), tt.schedule); err == nil {
				t.Error("New() = nil error, want a rejection")
			}
		})
	}
}

func TestWorkerRunsJobAndStopsOnCancel(t *testing.T) {
	t.Parallel()

	var runs atomic.Int64

	w, err := worker.New(discardLogger(), worker.Schedule{
		Job: worker.JobFunc{
			JobName: "counter",
			Fn: func(context.Context) error {
				runs.Add(1)

				return nil
			},
		},
		Interval: 10 * time.Millisecond,
		Timeout:  10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for runs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if runs.Load() < 2 {
		t.Fatalf("job ran %d times, want at least 2", runs.Load())
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
}

func TestPanickingJobDoesNotKillTheWorker(t *testing.T) {
	t.Parallel()

	var runs atomic.Int64

	w, err := worker.New(discardLogger(), worker.Schedule{
		Job: worker.JobFunc{
			JobName: "panicker",
			Fn: func(context.Context) error {
				runs.Add(1)

				panic("boom")
			},
		},
		Interval: 10 * time.Millisecond,
		Timeout:  10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// The ticker must survive the first panic and fire again.
	deadline := time.Now().Add(2 * time.Second)
	for runs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if runs.Load() < 2 {
		t.Errorf("job ran %d times; a panic must not stop the ticker", runs.Load())
	}

	cancel()
	<-done
}

func TestJobFailureIsLoggedNotFatal(t *testing.T) {
	t.Parallel()

	var runs atomic.Int64

	w, err := worker.New(discardLogger(), worker.Schedule{
		Job: worker.JobFunc{
			JobName: "failer",
			Fn: func(context.Context) error {
				runs.Add(1)

				return errors.New("expected failure")
			},
		},
		Interval: 10 * time.Millisecond,
		Timeout:  10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for runs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if runs.Load() < 2 {
		t.Errorf("job ran %d times; a returned error must not stop the ticker", runs.Load())
	}

	cancel()
	<-done
}
