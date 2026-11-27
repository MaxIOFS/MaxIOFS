package bucket

import (
	"errors"
	"fmt"
)

// ErrInvalidDefaultRetention is a default retention AWS would refuse.
var ErrInvalidDefaultRetention = errors.New("invalid default retention")

// ValidateDefaultRetention accepts a bucket's default retention as AWS does:
// GOVERNANCE or COMPLIANCE, for a positive number of days or of years, not
// both. The retention enforced on objects knows no other mode.
func ValidateDefaultRetention(mode string, days, years int) error {
	if mode != "GOVERNANCE" && mode != "COMPLIANCE" {
		return fmt.Errorf("%w: the mode must be GOVERNANCE or COMPLIANCE", ErrInvalidDefaultRetention)
	}
	if (days != 0) == (years != 0) {
		return fmt.Errorf("%w: give a number of days or of years, not both", ErrInvalidDefaultRetention)
	}
	if days < 0 || years < 0 {
		return fmt.Errorf("%w: the period must be a positive number", ErrInvalidDefaultRetention)
	}
	return nil
}
