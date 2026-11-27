package bucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A default retention is GOVERNANCE or COMPLIANCE for a positive number of
// days or of years, not both, as AWS takes it.
func TestADefaultRetentionIsAModeForAPeriod(t *testing.T) {
	for _, tc := range []struct {
		mode        string
		days, years int
		valid       bool
	}{
		{"GOVERNANCE", 1, 0, true},
		{"COMPLIANCE", 0, 1, true},
		{"COMPLIANCE", 36500, 0, true},
		{"governance", 1, 0, false},
		{"FOO", 1, 0, false},
		{"", 1, 0, false},
		{"GOVERNANCE", 0, 0, false},
		{"GOVERNANCE", 1, 1, false},
		{"GOVERNANCE", -1, 0, false},
		{"COMPLIANCE", 0, -1, false},
	} {
		err := ValidateDefaultRetention(tc.mode, tc.days, tc.years)
		if tc.valid {
			assert.NoError(t, err, "%+v", tc)
		} else {
			assert.ErrorIs(t, err, ErrInvalidDefaultRetention, "%+v", tc)
		}
	}
}
