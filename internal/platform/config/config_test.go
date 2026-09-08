package config_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/selimslab/gobase/internal/platform/config"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Env != config.EnvDevelopment {
		t.Errorf("Env = %q, want %q", cfg.Env, config.EnvDevelopment)
	}

	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("HTTP.Addr = %q, want :8080", cfg.HTTP.Addr)
	}

	if cfg.HTTP.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout must default to a positive value (gosec G112)")
	}

	if cfg.Security.MaxBodyBytes != 1<<20 {
		t.Errorf("MaxBodyBytes = %d, want %d", cfg.Security.MaxBodyBytes, 1<<20)
	}

	if cfg.Security.TrustedProxies != 0 {
		t.Error("TrustedProxies must default to 0 so X-Forwarded-For is untrusted")
	}

	if cfg.Observability.OTLPEndpoint != "" {
		t.Error("OTLP export must be off by default")
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("SERVICE_NAME", "widgets")
	t.Setenv("HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("HTTP_READ_TIMEOUT", "3s")
	t.Setenv("SECURITY_MAX_BODY_BYTES", "2048")
	t.Setenv("SECURITY_TRUSTED_PROXIES", "1")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "0.25")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Env.IsProduction() {
		t.Errorf("Env = %q, want production", cfg.Env)
	}

	if cfg.ServiceName != "widgets" {
		t.Errorf("ServiceName = %q", cfg.ServiceName)
	}

	if cfg.HTTP.Addr != "127.0.0.1:9000" {
		t.Errorf("HTTP.Addr = %q", cfg.HTTP.Addr)
	}

	if cfg.HTTP.ReadTimeout != 3*time.Second {
		t.Errorf("ReadTimeout = %s", cfg.HTTP.ReadTimeout)
	}

	if cfg.Security.MaxBodyBytes != 2048 {
		t.Errorf("MaxBodyBytes = %d", cfg.Security.MaxBodyBytes)
	}

	if cfg.Security.TrustedProxies != 1 {
		t.Errorf("TrustedProxies = %d", cfg.Security.TrustedProxies)
	}

	if cfg.Observability.SampleRatio != 0.25 {
		t.Errorf("SampleRatio = %v", cfg.Observability.SampleRatio)
	}
}

func TestLoadUnparsableValueFallsBackToDefault(t *testing.T) {
	t.Setenv("HTTP_READ_TIMEOUT", "not-a-duration")
	t.Setenv("SECURITY_MAX_BODY_BYTES", "huge")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTP.ReadTimeout != 10*time.Second {
		t.Errorf("ReadTimeout = %s, want the 10s default", cfg.HTTP.ReadTimeout)
	}

	if cfg.Security.MaxBodyBytes != 1<<20 {
		t.Errorf("MaxBodyBytes = %d, want the 1MiB default", cfg.Security.MaxBodyBytes)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	base := func() config.Config {
		return config.Config{
			Env:         config.EnvProduction,
			ServiceName: "gobase",
			HTTP: config.HTTP{
				Addr:              ":8080",
				ReadTimeout:       10 * time.Second,
				ReadHeaderTimeout: 5 * time.Second,
				WriteTimeout:      15 * time.Second,
				IdleTimeout:       60 * time.Second,
				HandlerTimeout:    10 * time.Second,
				ShutdownTimeout:   15 * time.Second,
			},
			Observability: config.Observability{LogLevel: "info", SampleRatio: 1},
			Security:      config.Security{MaxBodyBytes: 1 << 20},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr string
	}{
		{name: "valid", mutate: func(*config.Config) {}},
		{
			name:    "unknown environment",
			mutate:  func(c *config.Config) { c.Env = "staging-2" },
			wantErr: "APP_ENV",
		},
		{
			name:    "empty service name",
			mutate:  func(c *config.Config) { c.ServiceName = "  " },
			wantErr: "SERVICE_NAME",
		},
		{
			name:    "zero read header timeout",
			mutate:  func(c *config.Config) { c.HTTP.ReadHeaderTimeout = 0 },
			wantErr: "HTTP_READ_HEADER_TIMEOUT",
		},
		{
			name:    "handler timeout not below write timeout",
			mutate:  func(c *config.Config) { c.HTTP.HandlerTimeout = 20 * time.Second },
			wantErr: "HTTP_HANDLER_TIMEOUT",
		},
		{
			name:    "unknown log level",
			mutate:  func(c *config.Config) { c.Observability.LogLevel = "trace" },
			wantErr: "LOG_LEVEL",
		},
		{
			name:    "sample ratio out of range",
			mutate:  func(c *config.Config) { c.Observability.SampleRatio = 1.5 },
			wantErr: "OTEL_TRACES_SAMPLER_RATIO",
		},
		{
			name:    "non-positive body cap",
			mutate:  func(c *config.Config) { c.Security.MaxBodyBytes = 0 },
			wantErr: "SECURITY_MAX_BODY_BYTES",
		},
		{
			name:    "negative trusted proxies",
			mutate:  func(c *config.Config) { c.Security.TrustedProxies = -1 },
			wantErr: "SECURITY_TRUSTED_PROXIES",
		},
		{
			name: "hsts enabled without max age",
			mutate: func(c *config.Config) {
				c.Security.EnableHSTS = true
				c.Security.HSTSMaxAge = 0
			},
			wantErr: "SECURITY_HSTS_MAX_AGE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := base()
			tt.mutate(&cfg)

			err := cfg.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("Validate() = nil, want an error mentioning %s", tt.wantErr)
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want it to mention %s", err, tt.wantErr)
			}
		})
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Env: "nope", ServiceName: ""}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want errors")
	}

	for _, want := range []string{"APP_ENV", "SERVICE_NAME", "HTTP_ADDR", "LOG_LEVEL", "SECURITY_MAX_BODY_BYTES"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("joined error is missing %s: %v", want, err)
		}
	}
}

func TestLogValueRedactsSecrets(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	cfg := config.Config{
		Env:         config.EnvProduction,
		ServiceName: "gobase",
		Observability: config.Observability{
			LogLevel:     "info",
			OTLPEndpoint: "https://user:sup3rs3cret@otel.example.com:4317",
		},
	}

	logger.Info("config", slog.Any("config", cfg))

	out := buf.String()
	if strings.Contains(out, "sup3rs3cret") {
		t.Errorf("credentials leaked into the log: %s", out)
	}

	if !strings.Contains(out, "REDACTED") {
		t.Errorf("endpoint was not redacted: %s", out)
	}

	if !strings.Contains(out, "gobase") {
		t.Errorf("non-secret fields must still be logged: %s", out)
	}
}

func TestSecretNeverRendersItsValue(t *testing.T) {
	t.Parallel()

	s := config.Secret("hunter2")

	if got := s.String(); got != "REDACTED" {
		t.Errorf("String() = %q, want REDACTED", got)
	}

	if got := s.Reveal(); got != "hunter2" {
		t.Errorf("Reveal() = %q, want the real value", got)
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("secret", slog.Any("token", s))

	if strings.Contains(buf.String(), "hunter2") {
		t.Errorf("secret leaked through slog: %s", buf.String())
	}

	if got := config.Secret("").String(); got != "" {
		t.Errorf("empty Secret rendered as %q, want empty", got)
	}
}
