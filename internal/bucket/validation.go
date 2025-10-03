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

// ValidatePolicy validates bucket policy JSON structure
func ValidatePolicy(policy *Policy) error {
	if policy == nil {
		return nil
	}

	// Version must be specified
	if policy.Version == "" {
		return fmt.Errorf("policy version is required")
	}

	// Must have at least one statement
	if len(policy.Statement) == 0 {
		return fmt.Errorf("policy must have at least one statement")
	}

	for i, stmt := range policy.Statement {
		if err := validateStatement(stmt, i); err != nil {
			return err
		}
	}

	return nil
}

// validateStatement validates a single policy statement
func validateStatement(stmt Statement, index int) error {
	// Effect must be Allow or Deny
	if stmt.Effect != "Allow" && stmt.Effect != "Deny" {
		return fmt.Errorf("statement %d: effect must be 'Allow' or 'Deny'", index)
	}

	// Must have at least one action
	if len(stmt.Action) == 0 {
		return fmt.Errorf("statement %d: must specify at least one action", index)
	}

	// Must have at least one resource
	if len(stmt.Resource) == 0 {
		return fmt.Errorf("statement %d: must specify at least one resource", index)
	}

	return nil
}

// ValidateVersioningConfig validates versioning configuration
func ValidateVersioningConfig(config *VersioningConfig) error {
	if config == nil {
		return nil
	}

	if config.Status != "Enabled" && config.Status != "Suspended" {
		return fmt.Errorf("versioning status must be 'Enabled' or 'Suspended'")
	}

	return nil
}

// ValidateObjectLockConfig validates object lock configuration
func ValidateObjectLockConfig(config *ObjectLockConfig) error {
	if config == nil {
		return nil
	}

	if !config.ObjectLockEnabled {
		return fmt.Errorf("object lock enabled must be 'Enabled'")
	}

	if config.Rule != nil && config.Rule.DefaultRetention != nil {
		return validateDefaultRetention(config.Rule.DefaultRetention)
	}

	return nil
}

// validateDefaultRetention validates default retention configuration
func validateDefaultRetention(retention *DefaultRetention) error {
	if retention.Mode != "GOVERNANCE" && retention.Mode != "COMPLIANCE" {
		return fmt.Errorf("retention mode must be 'GOVERNANCE' or 'COMPLIANCE'")
	}

	// Must specify either Days or Years, but not both
	if retention.Days != nil && retention.Years != nil {
		return fmt.Errorf("cannot specify both days and years for retention")
	}

	if retention.Days == nil && retention.Years == nil {
		return fmt.Errorf("must specify either days or years for retention")
	}

	if retention.Days != nil && *retention.Days <= 0 {
		return fmt.Errorf("retention days must be positive")
	}

	if retention.Years != nil && *retention.Years <= 0 {
		return fmt.Errorf("retention years must be positive")
	}

	return nil
}

// ValidateLifecycleConfig validates lifecycle configuration
func ValidateLifecycleConfig(config *LifecycleConfig) error {
	if config == nil {
		return nil
	}

	if len(config.Rules) == 0 {
		return fmt.Errorf("lifecycle configuration must have at least one rule")
	}

	for i, rule := range config.Rules {
		if err := validateLifecycleRule(rule, i); err != nil {
			return err
		}
	}

	return nil
}

// validateLifecycleRule validates a single lifecycle rule
func validateLifecycleRule(rule LifecycleRule, index int) error {
	// ID is required
	if rule.ID == "" {
		return fmt.Errorf("rule %d: ID is required", index)
	}

	// Status must be Enabled or Disabled
	if rule.Status != "Enabled" && rule.Status != "Disabled" {
		return fmt.Errorf("rule %d: status must be 'Enabled' or 'Disabled'", index)
	}

	// Must have at least one action
	if rule.Expiration == nil && rule.Transition == nil && rule.AbortIncompleteMultipartUpload == nil {
		return fmt.Errorf("rule %d: must specify at least one action", index)
	}

	return nil
}

// ValidateCORSConfig validates CORS configuration
func ValidateCORSConfig(config *CORSConfig) error {
	if config == nil {
		return nil
	}

	if len(config.CORSRules) == 0 {
		return fmt.Errorf("CORS configuration must have at least one rule")
	}

	for i, rule := range config.CORSRules {
		if err := validateCORSRule(rule, i); err != nil {
			return err
		}
	}

	return nil
}

// validateCORSRule validates a single CORS rule
func validateCORSRule(rule CORSRule, index int) error {
	// Must have at least one allowed method
	if len(rule.AllowedMethods) == 0 {
		return fmt.Errorf("CORS rule %d: must specify at least one allowed method", index)
	}

	// Must have at least one allowed origin
	if len(rule.AllowedOrigins) == 0 {
		return fmt.Errorf("CORS rule %d: must specify at least one allowed origin", index)
	}

	// Validate methods
	validMethods := map[string]bool{
		"GET": true, "PUT": true, "POST": true, "DELETE": true, "HEAD": true,
	}

	for _, method := range rule.AllowedMethods {
		if !validMethods[method] {
			return fmt.Errorf("CORS rule %d: invalid method '%s'", index, method)
		}
	}

	return nil
}
