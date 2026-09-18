package membership

import (
	"testing"
)

func TestParseWindowsProxyServer(t *testing.T) {
	tests := []struct {
		scheme   string
		input    string
		expected string
	}{
		{"https", "127.0.0.1:7897", "http://127.0.0.1:7897"},
		{"https", "http=127.0.0.1:8080;https=127.0.0.1:7897", "http://127.0.0.1:7897"},
		{"http", "http=127.0.0.1:8080;https=127.0.0.1:7897", "http://127.0.0.1:8080"},
		{"https", "http=127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"https", "socks5://127.0.0.1:1080", "socks5://127.0.0.1:1080"},
		{"https", "", ""},
	}

	for _, tt := range tests {
		u, err := parseWindowsProxyServer(tt.scheme, tt.input)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.input, err)
		}
		actual := ""
		if u != nil {
			actual = u.String()
		}
		if actual != tt.expected {
			t.Errorf("parseWindowsProxyServer(%q, %q) = %q, want %q", tt.scheme, tt.input, actual, tt.expected)
		}
	}
}

func TestIsProxyOverridden(t *testing.T) {
	tests := []struct {
		hostname string
		override string
		expected bool
	}{
		{"localhost", "<local>", true},
		{"internal-host", "<local>", true},
		{"doropay.top", "<local>", false},
		{"doropay.top", "<local>;*.top", true},
		{"doropay.top", "<local>;doropay.top", true},
		{"doropay.top", "<local>;api.doropay.top", false},
		{"127.0.0.1", "<local>", true},
	}

	for _, tt := range tests {
		got := isProxyOverridden(tt.hostname, tt.override)
		if got != tt.expected {
			t.Errorf("isProxyOverridden(%q, %q) = %v, want %v", tt.hostname, tt.override, got, tt.expected)
		}
	}
}
