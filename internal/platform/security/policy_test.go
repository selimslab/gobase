package security_test

import (
	"testing"
	"time"

	"github.com/selimslab/gobase/internal/platform/config"
	"github.com/selimslab/gobase/internal/platform/security"
)

func testSecurityConfig() config.Security {
	return config.Security{
		MaxBodyBytes:   1 << 20,
		TrustedProxies: 0,
		EnableHSTS:     false,
		HSTSMaxAge:     365 * 24 * time.Hour,
	}
}

func TestNewPolicySetsBaselineHeaders(t *testing.T) {
	t.Parallel()

	policy := security.NewPolicy(config.EnvDevelopment, testSecurityConfig())

	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	}

	got := make(map[string]string, len(policy.Headers))
	for _, h := range policy.Headers {
		got[h.Name] = h.Value
	}

	for name, value := range want {
		if got[name] != value {
			t.Errorf("header %s = %q, want %q", name, got[name], value)
		}
	}

	if _, ok := got["Strict-Transport-Security"]; ok {
		t.Error("HSTS must never be a baseline header; it is decided per request")
	}
}

func TestUseHSTS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     config.Environment
		enable  bool
		overTLS bool
		want    bool
	}{
		{name: "dev plain http", env: config.EnvDevelopment, enable: false, overTLS: false, want: false},
		{name: "dev over tls without opt-in", env: config.EnvDevelopment, enable: false, overTLS: true, want: false},
		{name: "dev over tls with opt-in", env: config.EnvDevelopment, enable: true, overTLS: true, want: true},
		{name: "prod plain http", env: config.EnvProduction, enable: false, overTLS: false, want: false},
		{name: "prod over tls", env: config.EnvProduction, enable: false, overTLS: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := testSecurityConfig()
			cfg.EnableHSTS = tt.enable

			policy := security.NewPolicy(tt.env, cfg)
			if got := policy.UseHSTS(tt.overTLS); got != tt.want {
				t.Errorf("UseHSTS(%t) = %t, want %t", tt.overTLS, got, tt.want)
			}
		})
	}
}

func TestHSTSHeaderValue(t *testing.T) {
	t.Parallel()

	cfg := testSecurityConfig()
	cfg.EnableHSTS = true
	cfg.HSTSMaxAge = 48 * time.Hour

	h := security.NewPolicy(config.EnvProduction, cfg).HSTSHeader()

	if h.Name != "Strict-Transport-Security" {
		t.Errorf("name = %q", h.Name)
	}

	if want := "max-age=172800; includeSubDomains"; h.Value != want {
		t.Errorf("value = %q, want %q", h.Value, want)
	}
}

func TestOriginDeniedByDefault(t *testing.T) {
	t.Parallel()

	policy := security.NewPolicy(config.EnvProduction, testSecurityConfig())

	for _, origin := range []string{"", "https://evil.test", "https://example.com", "null"} {
		if policy.Origin(origin).Allow {
			t.Errorf("origin %q was allowed by the default policy", origin)
		}
	}
}

func TestOriginUsesInstalledPolicy(t *testing.T) {
	t.Parallel()

	policy := security.NewPolicy(config.EnvProduction, testSecurityConfig())
	policy.AllowOrigin = func(origin string) security.OriginDecision {
		if origin != "https://app.example.com" {
			return security.OriginDecision{}
		}

		return security.OriginDecision{Allow: true, AllowOrigin: origin, AllowCredentials: true}
	}

	if d := policy.Origin("https://app.example.com"); !d.Allow || d.AllowOrigin != "https://app.example.com" {
		t.Errorf("allowlisted origin: got %+v", d)
	}

	if d := policy.Origin("https://evil.test"); d.Allow {
		t.Errorf("unlisted origin was allowed: %+v", d)
	}
}

func TestOriginNilPolicyDenies(t *testing.T) {
	t.Parallel()

	var policy security.Policy
	if policy.Origin("https://example.com").Allow {
		t.Error("a zero Policy must deny every origin")
	}
}
