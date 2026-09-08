//go:build hardening

package hardening_test

import (
	"testing"

	"github.com/selimslab/gobase/docs/hardening"
)

func TestAllowOrigins(t *testing.T) {
	t.Parallel()

	policy := hardening.AllowOrigins([]string{"https://app.example.com", "https://admin.example.com"}, true)

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{name: "listed origin", origin: "https://app.example.com", want: true},
		{name: "second listed origin", origin: "https://admin.example.com", want: true},
		{name: "case insensitive host", origin: "https://APP.example.com", want: true},
		{name: "trailing slash tolerated", origin: "https://app.example.com/", want: true},
		{name: "unlisted origin", origin: "https://evil.test", want: false},
		// The classic suffix-match bug: these must not pass.
		{name: "suffix impostor", origin: "https://evil-example.com", want: false},
		{name: "subdomain impostor", origin: "https://app.example.com.evil.test", want: false},
		{name: "wrong scheme", origin: "http://app.example.com", want: false},
		{name: "null origin", origin: "null", want: false},
		{name: "empty origin", origin: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := policy(tt.origin)
			if got.Allow != tt.want {
				t.Errorf("AllowOrigins()(%q).Allow = %t, want %t", tt.origin, got.Allow, tt.want)
			}

			if got.Allow && got.AllowOrigin != tt.origin {
				t.Errorf("AllowOrigin = %q, want the request's origin %q", got.AllowOrigin, tt.origin)
			}
		})
	}
}

func TestAllowOriginsWithoutCredentials(t *testing.T) {
	t.Parallel()

	policy := hardening.AllowOrigins([]string{"https://app.example.com"}, false)

	if d := policy("https://app.example.com"); d.AllowCredentials {
		t.Error("credentials must stay off unless explicitly requested")
	}
}
