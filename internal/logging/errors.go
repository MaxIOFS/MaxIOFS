package logging

import "errors"

var (
	// ErrSettingsManagerNotSet is returned when settings manager is not configured
	ErrSettingsManagerNotSet = errors.New("settings manager not set")

	// ErrInvalidOutputType is returned when an invalid output type is specified
	ErrInvalidOutputType = errors.New("invalid output type")

	// ErrSyslogHostNotConfigured is returned when syslog host is not configured
	ErrSyslogHostNotConfigured = errors.New("syslog host not configured")

	// ErrHTTPURLNotConfigured is returned when HTTP URL is not configured
	ErrHTTPURLNotConfigured = errors.New("HTTP URL not configured")
)
