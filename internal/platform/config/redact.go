package config

import "log/slog"

// redacted replaces any value that must never reach a log sink.
const redacted = "REDACTED"

// Secret wraps a string that must never be logged in full. Use it for every
// credential you add when cloning this template: a Secret renders as
// "REDACTED" through slog, fmt, and any encoder that honors Stringer.
type Secret string

var (
	_ slog.LogValuer = Secret("")
	_ slog.LogValuer = Config{}
)

// String implements fmt.Stringer so accidental interpolation stays safe.
func (s Secret) String() string {
	if s == "" {
		return ""
	}

	return redacted
}

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.String()) }

// Reveal returns the underlying secret. Call it only where the value is used,
// never where it is logged.
func (s Secret) Reveal() string { return string(s) }

// LogValue implements slog.LogValuer so a whole Config can be logged safely.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", string(c.Env)),
		slog.String("service_name", c.ServiceName),
		slog.Any("http", c.HTTP),
		slog.Any("observability", c.Observability),
		slog.Any("security", c.Security),
	)
}

// LogValue implements slog.LogValuer.
func (h HTTP) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("addr", h.Addr),
		slog.Duration("read_timeout", h.ReadTimeout),
		slog.Duration("read_header_timeout", h.ReadHeaderTimeout),
		slog.Duration("write_timeout", h.WriteTimeout),
		slog.Duration("idle_timeout", h.IdleTimeout),
		slog.Duration("handler_timeout", h.HandlerTimeout),
		slog.Duration("shutdown_timeout", h.ShutdownTimeout),
	)
}

// LogValue implements slog.LogValuer. The OTLP endpoint may embed credentials
// in its userinfo, so it is reported only as present or absent.
func (o Observability) LogValue() slog.Value {
	endpoint := ""
	if o.OTLPEndpoint != "" {
		endpoint = redacted
	}

	return slog.GroupValue(
		slog.String("log_level", o.LogLevel),
		slog.String("otlp_endpoint", endpoint),
		slog.Bool("otlp_insecure", o.OTLPInsecure),
		slog.Float64("sample_ratio", o.SampleRatio),
	)
}

// LogValue implements slog.LogValuer.
func (s Security) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("max_body_bytes", s.MaxBodyBytes),
		slog.Int("trusted_proxies", s.TrustedProxies),
		slog.Bool("enable_hsts", s.EnableHSTS),
		slog.Duration("hsts_max_age", s.HSTSMaxAge),
	)
}
