package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A presigned URL is a credential for whoever holds it: its signature and
// session token never reach a log. The rest of the query does, as sent.
func TestLoggedQueryRedactsWhatMakesAPresignedURLACredential(t *testing.T) {
	for raw, want := range map[string]string{
		"X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIA%2F20261116&X-Amz-Expires=3600&X-Amz-Security-Token=FwoGZ&X-Amz-Signature=abc123":
		"X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIA%2F20261116&X-Amz-Expires=3600&X-Amz-Security-Token=[redacted]&X-Amz-Signature=[redacted]",
		"AWSAccessKeyId=AKIA&Expires=1&Signature=v2sig%3D": "AWSAccessKeyId=AKIA&Expires=1&Signature=[redacted]",
		"x-amz-signature=lower":                            "x-amz-signature=[redacted]",
		"X%2DAmz%2DSignature=encoded":                      "X-Amz-Signature=[redacted]",
		"prefix=a&list-type=2":                             "prefix=a&list-type=2",
		"uploads":                                          "uploads",
		"":                                                 "",
	} {
		got := LoggedQuery(raw)
		assert.Equal(t, want, got, raw)
		assert.NotContains(t, got, "abc123")
		assert.NotContains(t, got, "FwoGZ")
	}
}

// An Authorization header keeps in a log who signed and what, never the
// signature, which repeats the request for whoever reads it. A cookie, which
// carries the console's session, is redacted whole.
func TestLoggedHeaderValueKeepsTheSignerButNotTheSignature(t *testing.T) {
	v4 := "AWS4-HMAC-SHA256 Credential=AKIAEXAMPLE/20261116/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=deadbeef"
	assert.Equal(t, "AWS4-HMAC-SHA256 Credential=AKIAEXAMPLE/20261116/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=[redacted]",
		LoggedHeaderValue("Authorization", v4))
	assert.Equal(t, "Credential=AKIAEXAMPLE/x, Signature=[redacted]", LoggedAuthorization("Credential=AKIAEXAMPLE/x, Signature=deadbeef"))
	assert.Equal(t, "AWS AKIAEXAMPLE:[redacted]", LoggedHeaderValue("authorization", "AWS AKIAEXAMPLE:c2lnbmF0dXJl"))
	assert.Equal(t, "[redacted]", LoggedHeaderValue("Authorization", "Bearer eyJhbGciOi.session"))
	assert.Equal(t, "[redacted]", LoggedHeaderValue("Cookie", "auth_token=eyJhbGciOi.session"))
	assert.Equal(t, "[redacted]", LoggedHeaderValue("X-Amz-Security-Token", "FwoGZ"))
	assert.Equal(t, "Mozilla/5.0", LoggedHeaderValue("User-Agent", "Mozilla/5.0"))
}
