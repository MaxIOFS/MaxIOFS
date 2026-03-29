package replication

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// validateReplicationEndpoint
// ---------------------------------------------------------------------------

func TestValidateReplicationEndpoint_Empty(t *testing.T) {
	assert.NoError(t, validateReplicationEndpoint(""))
}

func TestValidateReplicationEndpoint_ValidHTTP(t *testing.T) {
	assert.NoError(t, validateReplicationEndpoint("http://public-s3.example.com:9000"))
}

func TestValidateReplicationEndpoint_ValidHTTPS(t *testing.T) {
	assert.NoError(t, validateReplicationEndpoint("https://s3.amazonaws.com"))
}

func TestValidateReplicationEndpoint_InvalidScheme(t *testing.T) {
	err := validateReplicationEndpoint("ftp://some-host/bucket")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http or https")
}

func TestValidateReplicationEndpoint_NoScheme(t *testing.T) {
	err := validateReplicationEndpoint("some-host:9000/bucket")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http or https")
}

func TestValidateReplicationEndpoint_Loopback(t *testing.T) {
	err := validateReplicationEndpoint("http://127.0.0.1:9000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private/internal")
}

func TestValidateReplicationEndpoint_Loopback_Named(t *testing.T) {
	// localhost resolves at dial time — static check only catches literal IPs.
	// This should pass validation (blocked at dial time by the SSRF dialer).
	assert.NoError(t, validateReplicationEndpoint("http://localhost:9000"))
}

func TestValidateReplicationEndpoint_PrivateClass_A(t *testing.T) {
	err := validateReplicationEndpoint("http://10.0.0.1:9000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private/internal")
}

func TestValidateReplicationEndpoint_PrivateClass_B(t *testing.T) {
	err := validateReplicationEndpoint("http://172.16.0.1:9000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private/internal")
}

func TestValidateReplicationEndpoint_PrivateClass_C(t *testing.T) {
	err := validateReplicationEndpoint("http://192.168.1.100:9000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private/internal")
}

func TestValidateReplicationEndpoint_LinkLocal_Metadata(t *testing.T) {
	// AWS/GCP metadata endpoint
	err := validateReplicationEndpoint("http://169.254.169.254/latest/meta-data/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private/internal")
}

func TestValidateReplicationEndpoint_IPv6_Loopback(t *testing.T) {
	err := validateReplicationEndpoint("http://[::1]:9000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private/internal")
}

func TestValidateReplicationEndpoint_UnspecifiedIP(t *testing.T) {
	err := validateReplicationEndpoint("http://0.0.0.0:9000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private/internal")
}

// ---------------------------------------------------------------------------
// isBlockedIP
// ---------------------------------------------------------------------------

func TestIsBlockedIP_Loopback(t *testing.T) {
	assert.True(t, isBlockedIP(net.ParseIP("127.0.0.1")))
	assert.True(t, isBlockedIP(net.ParseIP("127.255.255.255")))
}

func TestIsBlockedIP_IPv6Loopback(t *testing.T) {
	assert.True(t, isBlockedIP(net.ParseIP("::1")))
}

func TestIsBlockedIP_PrivateClassA(t *testing.T) {
	assert.True(t, isBlockedIP(net.ParseIP("10.0.0.1")))
	assert.True(t, isBlockedIP(net.ParseIP("10.255.255.255")))
}

func TestIsBlockedIP_PrivateClassB(t *testing.T) {
	assert.True(t, isBlockedIP(net.ParseIP("172.16.0.1")))
	assert.True(t, isBlockedIP(net.ParseIP("172.31.255.255")))
}

func TestIsBlockedIP_PrivateClassC(t *testing.T) {
	assert.True(t, isBlockedIP(net.ParseIP("192.168.0.1")))
	assert.True(t, isBlockedIP(net.ParseIP("192.168.255.255")))
}

func TestIsBlockedIP_LinkLocal_Metadata(t *testing.T) {
	assert.True(t, isBlockedIP(net.ParseIP("169.254.0.1")))
	assert.True(t, isBlockedIP(net.ParseIP("169.254.169.254")))
}

func TestIsBlockedIP_IPv6_UniqueLocal(t *testing.T) {
	assert.True(t, isBlockedIP(net.ParseIP("fc00::1")))
	assert.True(t, isBlockedIP(net.ParseIP("fd00::1")))
}

func TestIsBlockedIP_IPv6_LinkLocal(t *testing.T) {
	assert.True(t, isBlockedIP(net.ParseIP("fe80::1")))
}

func TestIsBlockedIP_Unspecified(t *testing.T) {
	assert.True(t, isBlockedIP(net.ParseIP("0.0.0.0")))
}

func TestIsBlockedIP_PublicIP(t *testing.T) {
	assert.False(t, isBlockedIP(net.ParseIP("8.8.8.8")))
	assert.False(t, isBlockedIP(net.ParseIP("1.1.1.1")))
	assert.False(t, isBlockedIP(net.ParseIP("52.94.76.1"))) // AWS public
}

func TestIsBlockedIP_PublicIPv6(t *testing.T) {
	assert.False(t, isBlockedIP(net.ParseIP("2606:4700:4700::1111"))) // Cloudflare DNS
}

func TestIsBlockedIP_Nil(t *testing.T) {
	// nil IP should not panic
	assert.False(t, isBlockedIP(nil))
}
