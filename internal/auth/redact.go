package auth

import (
	"net/url"
	"strings"
)

// secretHeaders are the request headers whose values are secrets: the SSE-C
// keys, the STS session token, and the cookies, which carry the console's
// session token. Logs give them redacted.
var secretHeaders = map[string]bool{
	"x-amz-server-side-encryption-customer-key":             true,
	"x-amz-copy-source-server-side-encryption-customer-key": true,
	"x-amz-security-token":                                  true,
	"cookie":                                                true,
}

// secretQueryParams are the query parameters whose values are secrets: those
// that make a presigned URL a credential for whoever holds it.
var secretQueryParams = map[string]bool{
	"x-amz-signature":      true,
	"x-amz-security-token": true,
	"signature":            true,
}

// LoggedHeaderValue is value as a log gives the header name: "[redacted]"
// for a secret, and an Authorization without its signature.
func LoggedHeaderValue(name, value string) string {
	switch lower := strings.ToLower(name); {
	case secretHeaders[lower]:
		return "[redacted]"
	case lower == "authorization":
		return LoggedAuthorization(value)
	}
	return value
}

// LoggedAuthorization is an Authorization header as a log gives it: who signed
// and what, without the signature, which repeats the request for whoever reads
// it. Anything else, a bearer token, is redacted whole.
func LoggedAuthorization(value string) string {
	if i := strings.Index(value, "Signature="); i >= 0 {
		return value[:i] + "Signature=[redacted]"
	}
	if strings.HasPrefix(value, "AWS ") {
		if i := strings.LastIndex(value, ":"); i >= 0 {
			return value[:i] + ":[redacted]"
		}
	}
	if value == "" || strings.HasPrefix(value, "AWS4-HMAC-SHA256") {
		return value
	}
	return "[redacted]"
}

// LoggedQuery is a raw query string as a log gives it: the value of a
// parameter that is a secret redacted, everything else as sent.
func LoggedQuery(raw string) string {
	if raw == "" {
		return raw
	}
	params := strings.Split(raw, "&")
	for i, param := range params {
		name, _, _ := strings.Cut(param, "=")
		if unescaped, err := url.QueryUnescape(name); err == nil {
			name = unescaped
		}
		if secretQueryParams[strings.ToLower(name)] {
			params[i] = name + "=[redacted]"
		}
	}
	return strings.Join(params, "&")
}

// loggedCanonicalHeaders are the lines of canonical headers as a log gives
// them, secrets redacted.
func loggedCanonicalHeaders(canonical string) []string {
	lines := strings.Split(strings.TrimSuffix(canonical, "\n"), "\n")
	for i, line := range lines {
		if name, value, ok := strings.Cut(line, ":"); ok {
			lines[i] = name + ":" + LoggedHeaderValue(name, value)
		}
	}
	return lines
}
