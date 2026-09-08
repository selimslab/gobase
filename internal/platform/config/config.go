// Package config loads and validates service configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment names the deployment tier a process runs in.
type Environment string

// Recognized deployment tiers.
const (
	EnvDevelopment Environment = "development"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
)

// IsProduction reports whether e is the production tier.
func (e Environment) IsProduction() bool { return e == EnvProduction }

// Config is the whole configuration of the service. Consumers take the
// sub-config they need, never the whole struct.
type Config struct {
	Env           Environment
	ServiceName   string
	HTTP          HTTP
	Observability Observability
	Security      Security
}

// HTTP configures the inbound HTTP adapter.
type HTTP struct {
	Addr              string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	HandlerTimeout    time.Duration
	ShutdownTimeout   time.Duration
}

// Observability configures logging, tracing and metrics.
type Observability struct {
	LogLevel     string
	OTLPEndpoint string
	OTLPInsecure bool
	SampleRatio  float64
}

// Security configures the request-facing protections.
type Security struct {
	MaxBodyBytes   int64
	TrustedProxies int
	EnableHSTS     bool
	HSTSMaxAge     time.Duration
}

// Load reads the configuration from the environment and validates it.
func Load() (Config, error) {
	cfg := Config{
		Env:         Environment(envString("APP_ENV", string(EnvDevelopment))),
		ServiceName: envString("SERVICE_NAME", "gobase"),
		HTTP: HTTP{
			Addr:              envString("HTTP_ADDR", ":8080"),
			ReadTimeout:       envDuration("HTTP_READ_TIMEOUT", 10*time.Second),
			ReadHeaderTimeout: envDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			WriteTimeout:      envDuration("HTTP_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:       envDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			HandlerTimeout:    envDuration("HTTP_HANDLER_TIMEOUT", 10*time.Second),
			ShutdownTimeout:   envDuration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		Observability: Observability{
			LogLevel:     envString("LOG_LEVEL", "info"),
			OTLPEndpoint: envString("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
			OTLPInsecure: envBool("OTEL_EXPORTER_OTLP_INSECURE", true),
			SampleRatio:  envFloat("OTEL_TRACES_SAMPLER_RATIO", 1.0),
		},
		Security: Security{
			MaxBodyBytes:   int64(envInt("SECURITY_MAX_BODY_BYTES", 1<<20)),
			TrustedProxies: envInt("SECURITY_TRUSTED_PROXIES", 0),
			EnableHSTS:     envBool("SECURITY_ENABLE_HSTS", false),
			HSTSMaxAge:     envDuration("SECURITY_HSTS_MAX_AGE", 365*24*time.Hour),
		},
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate reports every problem in the configuration at once.
func (c Config) Validate() error {
	return errors.Join(
		c.validateService(),
		c.HTTP.validate(),
		c.Observability.validate(),
		c.Security.validate(),
	)
}

func (c Config) validateService() error {
	var errs []error

	switch c.Env {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("APP_ENV: unknown environment %q", c.Env))
	}

	if strings.TrimSpace(c.ServiceName) == "" {
		errs = append(errs, errors.New("SERVICE_NAME: must not be empty"))
	}

	return errors.Join(errs...)
}

func (h HTTP) validate() error {
	var errs []error

	if strings.TrimSpace(h.Addr) == "" {
		errs = append(errs, errors.New("HTTP_ADDR: must not be empty"))
	}

	// gosec G112: a zero ReadHeaderTimeout leaves the server open to Slowloris.
	if h.ReadHeaderTimeout <= 0 {
		errs = append(errs, errors.New("HTTP_READ_HEADER_TIMEOUT: must be positive"))
	}

	for name, d := range map[string]time.Duration{
		"HTTP_READ_TIMEOUT":     h.ReadTimeout,
		"HTTP_WRITE_TIMEOUT":    h.WriteTimeout,
		"HTTP_IDLE_TIMEOUT":     h.IdleTimeout,
		"HTTP_HANDLER_TIMEOUT":  h.HandlerTimeout,
		"HTTP_SHUTDOWN_TIMEOUT": h.ShutdownTimeout,
	} {
		if d <= 0 {
			errs = append(errs, fmt.Errorf("%s: must be positive", name))
		}
	}

	// The handler deadline must fire before the write deadline kills the
	// connection, otherwise the client never sees the 503.
	if h.HandlerTimeout > 0 && h.WriteTimeout > 0 && h.HandlerTimeout >= h.WriteTimeout {
		errs = append(errs, errors.New("HTTP_HANDLER_TIMEOUT: must be less than HTTP_WRITE_TIMEOUT"))
	}

	return errors.Join(errs...)
}

func (o Observability) validate() error {
	var errs []error

	switch strings.ToLower(o.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("LOG_LEVEL: unknown level %q", o.LogLevel))
	}

	if o.SampleRatio < 0 || o.SampleRatio > 1 {
		errs = append(errs, errors.New("OTEL_TRACES_SAMPLER_RATIO: must be within [0,1]"))
	}

	return errors.Join(errs...)
}

func (s Security) validate() error {
	var errs []error

	if s.MaxBodyBytes <= 0 {
		errs = append(errs, errors.New("SECURITY_MAX_BODY_BYTES: must be positive"))
	}

	if s.TrustedProxies < 0 {
		errs = append(errs, errors.New("SECURITY_TRUSTED_PROXIES: must not be negative"))
	}

	if s.EnableHSTS && s.HSTSMaxAge <= 0 {
		errs = append(errs, errors.New("SECURITY_HSTS_MAX_AGE: must be positive when HSTS is enabled"))
	}

	return errors.Join(errs...)
}

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}

	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}

	return d
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}

	return n
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}

	return b
}

func envFloat(key string, def float64) float64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}

	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}

	return f
}
