package logging

import "errors"

var (
	// ErrSettingsManagerNotSet is returned when settings manager is not configured
	ErrSettingsManagerNotSet = errors.New("settings manager not set")
)
