package httputil

import (
	"net"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		// IPv4 Private
		{"10.0.0.1", true},
		{"172.16.0.2", true},
		{"192.168.1.1", true},
		// IPv4 Local/Loopback
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"169.254.169.254", true}, // Link-local / Cloud Metadata
		{"0.0.0.0", true},
		// IPv4 Public
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"157.240.22.35", false},
		
		// IPv6 Local/Loopback
		{"::1", true},
		{"::", true},
		{"fe80::1", true}, // Link-local
		{"fc00::1", true}, // Unique Local / Private RFC 4193
		// IPv6 Public
		{"2001:4860:4860::8888", false},
		
		// IPv4-mapped IPv6
		{"::ffff:127.0.0.1", true},
		{"::ffff:8.8.8.8", false},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Errorf("failed to parse IP: %s", tt.ip)
			continue
		}
		result := isPrivateIP(ip)
		if result != tt.expected {
			t.Errorf("isPrivateIP(%s) = %v; expected %v", tt.ip, result, tt.expected)
		}
	}
}
