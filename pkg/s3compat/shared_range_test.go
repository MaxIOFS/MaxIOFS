package s3compat

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The shared address space (100.64.0.0/10) is inside: overlay networks use it,
// and some clouds serve their metadata from it.
func TestTheSharedAddressSpaceIsBlocked(t *testing.T) {
	assert.True(t, notifIsBlockedIP(net.ParseIP("100.100.100.200")))
	assert.True(t, notifIsBlockedIP(net.ParseIP("100.64.0.1")))
	assert.False(t, notifIsBlockedIP(net.ParseIP("100.128.0.1")), "the address after the range")
	assert.False(t, notifIsBlockedIP(net.ParseIP("100.63.255.255")), "the address before the range")
}
