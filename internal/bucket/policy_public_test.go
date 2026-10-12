package bucket

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A policy is public as AWS S3 defines it: an Allow naming everyone that no
// source address range of /8 or narrower (IPv4), /32 or narrower (IPv6) limits.
func TestIsPublicPolicy(t *testing.T) {
	for name, tc := range map[string]struct {
		statement string
		public    bool
	}{
		"everyone":              {`{"Effect":"Allow","Principal":"*"}`, true},
		"AWS everyone":          {`{"Effect":"Allow","Principal":{"AWS":["arn:aws:iam:::user/a","*"]}}`, true},
		"named user":            {`{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam:::user/a"}}`, false},
		"deny everyone":         {`{"Effect":"Deny","Principal":"*"}`, false},
		"IPv4 /8":               {`{"Effect":"Allow","Principal":"*","Condition":{"IpAddress":{"aws:SourceIp":"10.0.0.0/8"}}}`, false},
		"single address":        {`{"Effect":"Allow","Principal":"*","Condition":{"IpAddress":{"aws:SourceIp":"203.0.113.5"}}}`, false},
		"IPv4 /1":               {`{"Effect":"Allow","Principal":"*","Condition":{"IpAddress":{"aws:SourceIp":"0.0.0.0/1"}}}`, true},
		"one wide range of two": {`{"Effect":"Allow","Principal":"*","Condition":{"IpAddress":{"aws:SourceIp":["10.0.0.0/8","0.0.0.0/0"]}}}`, true},
		"IPv6 /32":              {`{"Effect":"Allow","Principal":"*","Condition":{"IpAddress":{"aws:SourceIp":"2001:db8::/32"}}}`, false},
		"IPv6 /16":              {`{"Effect":"Allow","Principal":"*","Condition":{"IpAddress":{"aws:SourceIp":"2001::/16"}}}`, true},
		"NotIpAddress":          {`{"Effect":"Allow","Principal":"*","Condition":{"NotIpAddress":{"aws:SourceIp":"10.0.0.0/8"}}}`, true},
		"TLS only":              {`{"Effect":"Allow","Principal":"*","Condition":{"Bool":{"aws:SecureTransport":"true"}}}`, true},
		"invalid range":         {`{"Effect":"Allow","Principal":"*","Condition":{"IpAddress":{"aws:SourceIp":"not-an-address"}}}`, true},
	} {
		var st Statement
		require.NoError(t, json.Unmarshal([]byte(tc.statement), &st), name)
		assert.Equal(t, tc.public, IsPublicPolicy(&Policy{Version: "2012-10-17", Statement: []Statement{st}}), name)
	}
}
