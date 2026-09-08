package security_test

import (
	"testing"

	"github.com/selimslab/gobase/internal/platform/security"
)

func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		remoteAddr     string
		fwd            []string
		trustedProxies int
		want           string
	}{
		{
			name:       "no proxies trusted ignores forwarded header",
			remoteAddr: "10.0.0.5:54321",
			// The classic spoof: a client sets X-Forwarded-For itself.
			fwd:            []string{"1.2.3.4"},
			trustedProxies: 0,
			want:           "10.0.0.5",
		},
		{
			name:           "no forwarded header falls back to peer",
			remoteAddr:     "10.0.0.5:54321",
			fwd:            nil,
			trustedProxies: 1,
			want:           "10.0.0.5",
		},
		{
			name:           "one proxy takes the last chain entry",
			remoteAddr:     "10.0.0.5:54321",
			fwd:            []string{"203.0.113.9"},
			trustedProxies: 1,
			want:           "203.0.113.9",
		},
		{
			name:       "one proxy ignores a client-prepended hop",
			remoteAddr: "10.0.0.5:54321",
			// The client forged the first entry; the proxy appended the real one.
			fwd:            []string{"1.2.3.4, 203.0.113.9"},
			trustedProxies: 1,
			want:           "203.0.113.9",
		},
		{
			name:           "two proxies step one hop further left",
			remoteAddr:     "10.0.0.5:54321",
			fwd:            []string{"1.2.3.4, 203.0.113.9, 198.51.100.7"},
			trustedProxies: 2,
			want:           "203.0.113.9",
		},
		{
			name:           "more trusted proxies than entries clamps to the first",
			remoteAddr:     "10.0.0.5:54321",
			fwd:            []string{"203.0.113.9"},
			trustedProxies: 5,
			want:           "203.0.113.9",
		},
		{
			name:           "multiple header values are one chain",
			remoteAddr:     "10.0.0.5:54321",
			fwd:            []string{"1.2.3.4", "203.0.113.9"},
			trustedProxies: 1,
			want:           "203.0.113.9",
		},
		{
			name:           "garbage entries are skipped",
			remoteAddr:     "10.0.0.5:54321",
			fwd:            []string{"not-an-ip, 203.0.113.9, also bad"},
			trustedProxies: 1,
			want:           "203.0.113.9",
		},
		{
			name:           "entry with a port loses the port",
			remoteAddr:     "10.0.0.5:54321",
			fwd:            []string{"203.0.113.9:1234"},
			trustedProxies: 1,
			want:           "203.0.113.9",
		},
		{
			name:           "ipv6 peer without brackets",
			remoteAddr:     "[2001:db8::1]:443",
			fwd:            nil,
			trustedProxies: 0,
			want:           "2001:db8::1",
		},
		{
			name:           "ipv4-mapped ipv6 is unmapped",
			remoteAddr:     "10.0.0.5:54321",
			fwd:            []string{"::ffff:203.0.113.9"},
			trustedProxies: 1,
			want:           "203.0.113.9",
		},
		{
			name:           "all entries invalid falls back to peer",
			remoteAddr:     "10.0.0.5:54321",
			fwd:            []string{"nonsense, garbage"},
			trustedProxies: 1,
			want:           "10.0.0.5",
		},
		{
			name:           "peer without a port is returned as is",
			remoteAddr:     "10.0.0.5",
			fwd:            nil,
			trustedProxies: 0,
			want:           "10.0.0.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := security.ClientIP(tt.remoteAddr, tt.fwd, tt.trustedProxies)
			if got != tt.want {
				t.Errorf("ClientIP(%q, %v, %d) = %q, want %q",
					tt.remoteAddr, tt.fwd, tt.trustedProxies, got, tt.want)
			}
		})
	}
}
