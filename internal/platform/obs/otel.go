package obs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/selimslab/gobase/internal/platform/config"
)

// ShutdownFunc flushes and releases the telemetry pipeline. Always defer it:
// spans buffered at exit are lost otherwise.
type ShutdownFunc func(context.Context) error

// noopShutdown is what you get when telemetry is disabled.
func noopShutdown(context.Context) error { return nil }

// InitOTel installs the global tracer and meter providers and the W3C
// propagators, and returns the function that shuts them down.
//
// An empty OTLP endpoint disables export entirely: the global providers stay
// no-op, instrumented code still runs, and nothing is dialed. That is the
// default, so `go run ./cmd/server` needs no collector.
func InitOTel(ctx context.Context, cfg config.Observability, service, version, env string) (ShutdownFunc, error) {
	// Propagators are free and must work even with export disabled, so an
	// incoming traceparent is honored and passed on regardless.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if cfg.OTLPEndpoint == "" {
		return noopShutdown, nil
	}

	res, err := newResource(ctx, service, version, env)
	if err != nil {
		return noopShutdown, fmt.Errorf("otel resource: %w", err)
	}

	var shutdowns []func(context.Context) error

	// shutdownAll runs every registered shutdown, even if an earlier one fails.
	shutdownAll := func(ctx context.Context) error {
		errs := make([]error, 0, len(shutdowns))
		for _, fn := range shutdowns {
			errs = append(errs, fn(ctx))
		}

		return errors.Join(errs...)
	}

	tp, err := newTracerProvider(ctx, cfg, res)
	if err != nil {
		return noopShutdown, err
	}

	shutdowns = append(shutdowns, tp.Shutdown)
	otel.SetTracerProvider(tp)

	mp, err := newMeterProvider(ctx, cfg, res)
	if err != nil {
		// The tracer provider is already live; tear it down before returning.
		return noopShutdown, errors.Join(err, shutdownAll(ctx))
	}

	shutdowns = append(shutdowns, mp.Shutdown)
	otel.SetMeterProvider(mp)

	return shutdownAll, nil
}

func newResource(ctx context.Context, service, version, env string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName(service),
			semconv.ServiceVersion(version),
			attribute.String("deployment.environment", env),
		),
	)
}

func newTracerProvider(ctx context.Context, cfg config.Observability, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.OTLPInsecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exp, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otlp trace exporter: %w", err)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exp),
		// ParentBased keeps a distributed trace whole: a sampled parent is
		// always followed, and only a root span consults the ratio.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	), nil
}

func newMeterProvider(ctx context.Context, cfg config.Observability, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.OTLPInsecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}

	exp, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otlp metric exporter: %w", err)
	}

	return sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(30*time.Second))),
	), nil
}
