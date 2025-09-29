package bucket

import (
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
)

func TestValidateBucketName(t *testing.T) {
	tests := []struct {
		name        string
		bucketName  string
		expectError bool
	}{
		// Valid names
		{"valid-simple", "mybucket", false},
		{"valid-with-dash", "my-bucket", false},
		{"valid-with-numbers", "bucket123", false},
		{"valid-mixed", "my-data-bucket-2023", false},
		{"valid-minimum-length", "abc", false},
		{"valid-maximum-length", "a" + strings.Repeat("b", 61) + "z", false},

		// Invalid names
		{"empty", "", true},
		{"too-short", "ab", true},
		{"too-long", "a" + string(make([]byte, 62)) + "z", true},
		{"uppercase", "MyBucket", true},
		{"underscore", "my_bucket", true},
		{"starts-with-dash", "-bucket", true},
		{"ends-with-dash", "bucket-", true},
		{"consecutive-dashes", "bucket--name", true},
		{"ip-address", "192.168.1.1", true},
		{"xn-prefix", "xn--bucket", true},
		{"s3alias-suffix", "bucket-s3alias", true},
		{"special-chars", "bucket@name", true},
		{"dots", "bucket.name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bucket.ValidateBucketName(tt.bucketName)
			if tt.expectError {
				assert.Error(t, err, "Expected error for bucket name: %s", tt.bucketName)
			} else {
				assert.NoError(t, err, "Expected no error for bucket name: %s", tt.bucketName)
			}
		})
	}
}

func TestValidatePolicy(t *testing.T) {
	tests := []struct {
		name        string
		policy      *bucket.Policy
		expectError bool
	}{
		{
			name:        "nil-policy",
			policy:      nil,
			expectError: false,
		},
		{
			name: "valid-policy",
			policy: &bucket.Policy{
				Version: "2012-10-17",
				Statement: []bucket.Statement{
					{
						Effect:   "Allow",
						Action:   []string{"s3:GetObject"},
						Resource: []string{"arn:aws:s3:::mybucket/*"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "missing-version",
			policy: &bucket.Policy{
				Statement: []bucket.Statement{
					{
						Effect:   "Allow",
						Action:   []string{"s3:GetObject"},
						Resource: []string{"arn:aws:s3:::mybucket/*"},
					},
				},
			},
			expectError: true,
		},
		{
			name: "empty-statements",
			policy: &bucket.Policy{
				Version:   "2012-10-17",
				Statement: []bucket.Statement{},
			},
			expectError: true,
		},
		{
			name: "invalid-effect",
			policy: &bucket.Policy{
				Version: "2012-10-17",
				Statement: []bucket.Statement{
					{
						Effect:   "Maybe",
						Action:   []string{"s3:GetObject"},
						Resource: []string{"arn:aws:s3:::mybucket/*"},
					},
				},
			},
			expectError: true,
		},
		{
			name: "missing-action",
			policy: &bucket.Policy{
				Version: "2012-10-17",
				Statement: []bucket.Statement{
					{
						Effect:   "Allow",
						Action:   []string{},
						Resource: []string{"arn:aws:s3:::mybucket/*"},
					},
				},
			},
			expectError: true,
		},
		{
			name: "missing-resource",
			policy: &bucket.Policy{
				Version: "2012-10-17",
				Statement: []bucket.Statement{
					{
						Effect:   "Allow",
						Action:   []string{"s3:GetObject"},
						Resource: []string{},
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bucket.ValidatePolicy(tt.policy)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateVersioningConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *bucket.VersioningConfig
		expectError bool
	}{
		{
			name:        "nil-config",
			config:      nil,
			expectError: false,
		},
		{
			name: "enabled",
			config: &bucket.VersioningConfig{
				Status: "Enabled",
			},
			expectError: false,
		},
		{
			name: "suspended",
			config: &bucket.VersioningConfig{
				Status: "Suspended",
			},
			expectError: false,
		},
		{
			name: "invalid-status",
			config: &bucket.VersioningConfig{
				Status: "Maybe",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bucket.ValidateVersioningConfig(tt.config)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateObjectLockConfig(t *testing.T) {
	days := 30
	years := 1

	tests := []struct {
		name        string
		config      *bucket.ObjectLockConfig
		expectError bool
	}{
		{
			name:        "nil-config",
			config:      nil,
			expectError: false,
		},
		{
			name: "enabled-no-rule",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: "Enabled",
			},
			expectError: false,
		},
		{
			name: "enabled-with-governance-days",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: "Enabled",
				Rule: &bucket.ObjectLockRule{
					DefaultRetention: &bucket.DefaultRetention{
						Mode: "GOVERNANCE",
						Days: &days,
					},
				},
			},
			expectError: false,
		},
		{
			name: "enabled-with-compliance-years",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: "Enabled",
				Rule: &bucket.ObjectLockRule{
					DefaultRetention: &bucket.DefaultRetention{
						Mode:  "COMPLIANCE",
						Years: &years,
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid-mode",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: "Enabled",
				Rule: &bucket.ObjectLockRule{
					DefaultRetention: &bucket.DefaultRetention{
						Mode: "INVALID",
						Days: &days,
					},
				},
			},
			expectError: true,
		},
		{
			name: "both-days-and-years",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: "Enabled",
				Rule: &bucket.ObjectLockRule{
					DefaultRetention: &bucket.DefaultRetention{
						Mode:  "GOVERNANCE",
						Days:  &days,
						Years: &years,
					},
				},
			},
			expectError: true,
		},
		{
			name: "neither-days-nor-years",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: "Enabled",
				Rule: &bucket.ObjectLockRule{
					DefaultRetention: &bucket.DefaultRetention{
						Mode: "GOVERNANCE",
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bucket.ValidateObjectLockConfig(tt.config)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCORSConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *bucket.CORSConfig
		expectError bool
	}{
		{
			name:        "nil-config",
			config:      nil,
			expectError: false,
		},
		{
			name: "valid-cors",
			config: &bucket.CORSConfig{
				CORSRules: []bucket.CORSRule{
					{
						AllowedMethods: []string{"GET", "POST"},
						AllowedOrigins: []string{"*"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "empty-rules",
			config: &bucket.CORSConfig{
				CORSRules: []bucket.CORSRule{},
			},
			expectError: true,
		},
		{
			name: "missing-methods",
			config: &bucket.CORSConfig{
				CORSRules: []bucket.CORSRule{
					{
						AllowedMethods: []string{},
						AllowedOrigins: []string{"*"},
					},
				},
			},
			expectError: true,
		},
		{
			name: "missing-origins",
			config: &bucket.CORSConfig{
				CORSRules: []bucket.CORSRule{
					{
						AllowedMethods: []string{"GET"},
						AllowedOrigins: []string{},
					},
				},
			},
			expectError: true,
		},
		{
			name: "invalid-method",
			config: &bucket.CORSConfig{
				CORSRules: []bucket.CORSRule{
					{
						AllowedMethods: []string{"INVALID"},
						AllowedOrigins: []string{"*"},
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bucket.ValidateCORSConfig(tt.config)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}