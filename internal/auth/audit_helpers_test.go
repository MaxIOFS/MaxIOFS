package auth

import (
	"net/http"
	"testing"
)

// TestExtractIPFromRequest tests IP extraction from various headers
func TestExtractIPFromRequest(t *testing.T) {
	tests := []struct {
		name         string
		xForwardedFor string
		xRealIP      string
		remoteAddr   string
		expectedIP   string
	}{
		{
			name:          "X-Forwarded-For single IP",
			xForwardedFor: "203.0.113.1",
			xRealIP:       "",
			remoteAddr:    "192.168.1.1:12345",
			expectedIP:    "203.0.113.1",
		},
		{
			name:          "X-Forwarded-For multiple IPs",
			xForwardedFor: "203.0.113.1, 192.168.1.100, 10.0.0.1",
			xRealIP:       "",
			remoteAddr:    "192.168.1.1:12345",
			expectedIP:    "203.0.113.1",
		},
		{
			name:          "X-Forwarded-For with spaces",
			xForwardedFor: "  203.0.113.5  ",
			xRealIP:       "",
			remoteAddr:    "192.168.1.1:12345",
			expectedIP:    "203.0.113.5",
		},
		{
			name:          "X-Real-IP when no X-Forwarded-For",
			xForwardedFor: "",
			xRealIP:       "198.51.100.1",
			remoteAddr:    "192.168.1.1:12345",
			expectedIP:    "198.51.100.1",
		},
		{
			name:          "X-Real-IP with spaces",
			xForwardedFor: "",
			xRealIP:       "  198.51.100.5  ",
			remoteAddr:    "192.168.1.1:12345",
			expectedIP:    "198.51.100.5",
		},
		{
			name:          "RemoteAddr fallback with port",
			xForwardedFor: "",
			xRealIP:       "",
			remoteAddr:    "192.168.1.100:54321",
			expectedIP:    "192.168.1.100",
		},
		{
			name:          "RemoteAddr fallback without port",
			xForwardedFor: "",
			xRealIP:       "",
			remoteAddr:    "192.168.1.200",
			expectedIP:    "192.168.1.200",
		},
		{
			name:          "IPv6 RemoteAddr",
			xForwardedFor: "",
			xRealIP:       "",
			remoteAddr:    "[2001:db8::1]:8080",
			expectedIP:    "[2001",
		},
		{
			name:          "X-Forwarded-For priority over X-Real-IP",
			xForwardedFor: "203.0.113.1",
			xRealIP:       "198.51.100.1",
			remoteAddr:    "192.168.1.1:12345",
			expectedIP:    "203.0.113.1",
		},
		{
			name:          "Empty RemoteAddr",
			xForwardedFor: "",
			xRealIP:       "",
			remoteAddr:    "",
			expectedIP:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				Header:     make(http.Header),
				RemoteAddr: tt.remoteAddr,
			}

			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}

			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			result := extractIPFromRequest(req)

			if result != tt.expectedIP {
				t.Errorf("Expected IP %q, got %q", tt.expectedIP, result)
			}
		})
	}
}

// TestExtractIPFromRequest_ComplexScenarios tests complex IP extraction scenarios
func TestExtractIPFromRequest_ComplexScenarios(t *testing.T) {
	t.Run("Multiple comma-separated IPs in X-Forwarded-For", func(t *testing.T) {
		req := &http.Request{
			Header: make(http.Header),
		}
		req.Header.Set("X-Forwarded-For", "1.1.1.1,2.2.2.2,3.3.3.3")

		ip := extractIPFromRequest(req)
		if ip != "1.1.1.1" {
			t.Errorf("Expected first IP from list, got %q", ip)
		}
	})

	t.Run("X-Forwarded-For with proxy chain", func(t *testing.T) {
		req := &http.Request{
			Header: make(http.Header),
		}
		// Client IP, Proxy1, Proxy2
		req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18, 150.172.238.178")

		ip := extractIPFromRequest(req)
		// Should return the first IP (original client)
		if ip != "203.0.113.195" {
			t.Errorf("Expected client IP from proxy chain, got %q", ip)
		}
	})

	t.Run("RemoteAddr with IPv6 and port", func(t *testing.T) {
		req := &http.Request{
			Header:     make(http.Header),
			RemoteAddr: "[::1]:9000",
		}

		ip := extractIPFromRequest(req)
		// Will split on ":" and take first part
		if ip != "[" {
			t.Errorf("Expected IPv6 bracket, got %q", ip)
		}
	})

	t.Run("All headers missing", func(t *testing.T) {
		req := &http.Request{
			Header:     make(http.Header),
			RemoteAddr: "10.0.0.1:8080",
		}

		ip := extractIPFromRequest(req)
		if ip != "10.0.0.1" {
			t.Errorf("Expected RemoteAddr IP, got %q", ip)
		}
	})
}

// TestExtractUserAgentFromRequest tests user agent extraction
func TestExtractUserAgentFromRequest(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		expected  string
	}{
		{
			name:      "Standard browser user agent",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
			expected:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		},
		{
			name:      "Mobile user agent",
			userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 14_6 like Mac OS X) AppleWebKit/605.1.15",
			expected:  "Mozilla/5.0 (iPhone; CPU iPhone OS 14_6 like Mac OS X) AppleWebKit/605.1.15",
		},
		{
			name:      "Bot user agent",
			userAgent: "Googlebot/2.1 (+http://www.google.com/bot.html)",
			expected:  "Googlebot/2.1 (+http://www.google.com/bot.html)",
		},
		{
			name:      "AWS SDK user agent",
			userAgent: "aws-sdk-go/1.44.0 (go1.18; linux; amd64)",
			expected:  "aws-sdk-go/1.44.0 (go1.18; linux; amd64)",
		},
		{
			name:      "S3 CLI user agent",
			userAgent: "aws-cli/2.7.0 Python/3.9.11 Linux/5.10.0",
			expected:  "aws-cli/2.7.0 Python/3.9.11 Linux/5.10.0",
		},
		{
			name:      "Empty user agent",
			userAgent: "",
			expected:  "",
		},
		{
			name:      "Custom application user agent",
			userAgent: "MyApp/1.0.0",
			expected:  "MyApp/1.0.0",
		},
		{
			name:      "User agent with special characters",
			userAgent: "Test/1.0 (compatible; +https://example.com)",
			expected:  "Test/1.0 (compatible; +https://example.com)",
		},
		{
			name:      "Very long user agent",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36 Edg/91.0.864.59",
			expected:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36 Edg/91.0.864.59",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				Header: make(http.Header),
			}

			if tt.userAgent != "" {
				req.Header.Set("User-Agent", tt.userAgent)
			}

			result := extractUserAgentFromRequest(req)

			if result != tt.expected {
				t.Errorf("Expected user agent %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestExtractUserAgentFromRequest_MissingHeader tests missing User-Agent header
func TestExtractUserAgentFromRequest_MissingHeader(t *testing.T) {
	req := &http.Request{
		Header: make(http.Header),
	}

	// Don't set User-Agent header
	result := extractUserAgentFromRequest(req)

	if result != "" {
		t.Errorf("Expected empty string for missing User-Agent, got %q", result)
	}
}

// TestExtractUserAgentFromRequest_MultipleUserAgents tests multiple User-Agent headers
func TestExtractUserAgentFromRequest_MultipleUserAgents(t *testing.T) {
	req := &http.Request{
		Header: make(http.Header),
	}

	// Add multiple User-Agent headers (unusual but possible)
	req.Header.Add("User-Agent", "FirstAgent/1.0")
	req.Header.Add("User-Agent", "SecondAgent/2.0")

	result := extractUserAgentFromRequest(req)

	// Get() returns the first value
	if result != "FirstAgent/1.0" {
		t.Errorf("Expected first User-Agent header, got %q", result)
	}
}

// TestExtractIPFromRequest_NilRequest tests handling of nil request
func TestExtractIPFromRequest_EdgeCases(t *testing.T) {
	t.Run("Request with only port in RemoteAddr", func(t *testing.T) {
		req := &http.Request{
			Header:     make(http.Header),
			RemoteAddr: ":8080",
		}

		ip := extractIPFromRequest(req)
		if ip != "" {
			t.Errorf("Expected empty IP for port-only RemoteAddr, got %q", ip)
		}
	})

	t.Run("Request with malformed RemoteAddr", func(t *testing.T) {
		req := &http.Request{
			Header:     make(http.Header),
			RemoteAddr: "not:a:valid:address:format",
		}

		ip := extractIPFromRequest(req)
		// Should return first part before colon
		if ip != "not" {
			t.Errorf("Expected 'not' for malformed address, got %q", ip)
		}
	})

	t.Run("X-Forwarded-For with empty value", func(t *testing.T) {
		req := &http.Request{
			Header:     make(http.Header),
			RemoteAddr: "10.0.0.1:8080",
		}
		req.Header.Set("X-Forwarded-For", "")

		ip := extractIPFromRequest(req)
		// Should fall back to X-Real-IP or RemoteAddr
		if ip != "10.0.0.1" {
			t.Errorf("Expected RemoteAddr fallback, got %q", ip)
		}
	})

	t.Run("X-Forwarded-For with only commas", func(t *testing.T) {
		req := &http.Request{
			Header:     make(http.Header),
			RemoteAddr: "10.0.0.1:8080",
		}
		req.Header.Set("X-Forwarded-For", ",,,")

		ip := extractIPFromRequest(req)
		// First split will be empty after trim
		if ip != "" {
			t.Errorf("Expected empty IP from comma-only header, got %q", ip)
		}
	})
}
