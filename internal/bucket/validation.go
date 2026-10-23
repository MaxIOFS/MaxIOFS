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
	// Lowercase letters, numbers, periods and hyphens, from and to a letter or
	// a number.
	validBucketNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*[a-z0-9]$`)

	ipAddressPattern = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)

	// Names S3 keeps for itself.
	reservedBucketPrefixes = []string{"xn--", "sthree-", "amzn-s3-demo-"}
	reservedBucketSuffixes = []string{"-s3alias", "--ol-s3", ".mrap", "--x-s3", "--table-s3"}
)

// ValidateBucketName validates a bucket name by the rules S3 names general
// purpose buckets by.
func ValidateBucketName(name string) error {
	if name == "" {
		return ErrInvalidBucketName
	}

	// Length validation
	if len(name) < MinBucketNameLength || len(name) > MaxBucketNameLength {
		return fmt.Errorf("%w: name must be between %d and %d characters",
			ErrInvalidBucketName, MinBucketNameLength, MaxBucketNameLength)
	}

	if !validBucketNameRegex.MatchString(name) {
		return fmt.Errorf("%w: name must start and end with a letter or a number and contain only lowercase letters, numbers, periods and hyphens",
			ErrInvalidBucketName)
	}

	if strings.Contains(name, "..") {
		return fmt.Errorf("%w: name cannot contain two adjacent periods", ErrInvalidBucketName)
	}

	// Cannot be formatted as IP address
	if ipAddressPattern.MatchString(name) {
		return fmt.Errorf("%w: name cannot be formatted as IP address", ErrInvalidBucketName)
	}

	for _, prefix := range reservedBucketPrefixes {
		if strings.HasPrefix(name, prefix) {
			return fmt.Errorf("%w: name cannot start with '%s'", ErrInvalidBucketName, prefix)
		}
	}
	for _, suffix := range reservedBucketSuffixes {
		if strings.HasSuffix(name, suffix) {
			return fmt.Errorf("%w: name cannot end with '%s'", ErrInvalidBucketName, suffix)
		}
	}

	return nil
}
