package bucket

import (
	"fmt"
	"regexp"
	"strings"
)

// S3 bucket naming rules
const (
	MinBucketNameLength = 3
	MaxBucketNameLength = 63
)

var (
	// Valid bucket name regex (S3 compatible)
	validBucketNameRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`)

	// Invalid patterns
	invalidConsecutiveDashes = regexp.MustCompile(`--`)
	ipAddressPattern         = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
)

// ValidateBucketName validates bucket name according to S3 rules
func ValidateBucketName(name string) error {
	if name == "" {
		return ErrInvalidBucketName
	}

	// Length validation
	if len(name) < MinBucketNameLength || len(name) > MaxBucketNameLength {
		return fmt.Errorf("%w: name must be between %d and %d characters",
			ErrInvalidBucketName, MinBucketNameLength, MaxBucketNameLength)
	}

	// Must start and end with alphanumeric
	if !validBucketNameRegex.MatchString(name) {
		return fmt.Errorf("%w: name must start and end with alphanumeric characters and contain only lowercase letters, numbers, and hyphens",
			ErrInvalidBucketName)
	}

	// Cannot contain consecutive dashes
	if invalidConsecutiveDashes.MatchString(name) {
		return fmt.Errorf("%w: name cannot contain consecutive dashes", ErrInvalidBucketName)
	}

	// Cannot be formatted as IP address
	if ipAddressPattern.MatchString(name) {
		return fmt.Errorf("%w: name cannot be formatted as IP address", ErrInvalidBucketName)
	}

	// Cannot start with "xn--" (reserved for internationalized domains)
	if strings.HasPrefix(name, "xn--") {
		return fmt.Errorf("%w: name cannot start with 'xn--'", ErrInvalidBucketName)
	}

	// Cannot end with "-s3alias" (reserved by AWS)
	if strings.HasSuffix(name, "-s3alias") {
		return fmt.Errorf("%w: name cannot end with '-s3alias'", ErrInvalidBucketName)
	}

	return nil
}
