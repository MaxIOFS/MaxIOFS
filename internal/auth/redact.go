package auth

import "strings"

// secretHeaders are the request headers whose values are secrets: the SSE-C
// keys and the STS session token. Logs give them redacted.
var secretHeaders = map[string]bool{
	"x-amz-server-side-encryption-customer-key":             true,
	"x-amz-copy-source-server-side-encryption-customer-key": true,
	"x-amz-security-token":                                  true,
}

// LoggedHeaderValue is value as a log gives the header name: "[redacted]"
// for a secret.
func LoggedHeaderValue(name, value string) string {
	if secretHeaders[strings.ToLower(name)] {
		return "[redacted]"
	}
	return value
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
