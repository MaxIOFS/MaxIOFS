package bucket

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEvaluatePolicy_NoPolicyDefaultDeny tests that no policy results in default deny
func TestEvaluatePolicy_NoPolicyDefaultDeny(t *testing.T) {
	ctx := context.Background()

	request := PolicyEvaluationRequest{
		Principal: "user123",
		Action:    "s3:GetObject",
		Resource:  "arn:aws:s3:::my-bucket/object.txt",
		Bucket:    "my-bucket",
	}

	// Nil policy
	decision := EvaluatePolicy(ctx, nil, request)
	assert.Equal(t, DecisionDeny, decision, "Nil policy should result in default deny")

	// Empty policy
	emptyPolicy := &Policy{
		Version:   "2012-10-17",
		Statement: []Statement{},
	}
	decision = EvaluatePolicy(ctx, emptyPolicy, request)
	assert.Equal(t, DecisionDeny, decision, "Empty policy should result in default deny")
}

// TestEvaluatePolicy_ExplicitAllow tests that explicit allow grants permission
func TestEvaluatePolicy_ExplicitAllow(t *testing.T) {
	ctx := context.Background()

	policy := &Policy{
		Version: "2012-10-17",
		Statement: []Statement{
			{
				Effect:    "Allow",
				Principal: map[string]interface{}{"AWS": "user123"},
				Action:    "s3:GetObject",
				Resource:  "arn:aws:s3:::my-bucket/*",
			},
		},
	}

	request := PolicyEvaluationRequest{
		Principal: "user123",
		Action:    "s3:GetObject",
		Resource:  "arn:aws:s3:::my-bucket/object.txt",
		Bucket:    "my-bucket",
	}

	decision := EvaluatePolicy(ctx, policy, request)
	assert.Equal(t, DecisionAllow, decision, "Matching Allow statement should allow access")
}

// TestEvaluatePolicy_ExplicitDeny tests that explicit deny overrides allows
func TestEvaluatePolicy_ExplicitDeny(t *testing.T) {
	ctx := context.Background()

	policy := &Policy{
		Version: "2012-10-17",
		Statement: []Statement{
			{
				Effect:    "Allow",
				Principal: "*",
				Action:    "s3:GetObject",
				Resource:  "arn:aws:s3:::my-bucket/*",
			},
			{
				Effect:    "Deny",
				Principal: map[string]interface{}{"AWS": "user123"},
				Action:    "s3:GetObject",
				Resource:  "arn:aws:s3:::my-bucket/secret/*",
			},
		},
	}

	// Request to secret path - should be denied
	request := PolicyEvaluationRequest{
		Principal: "user123",
		Action:    "s3:GetObject",
		Resource:  "arn:aws:s3:::my-bucket/secret/file.txt",
		Bucket:    "my-bucket",
	}

	decision := EvaluatePolicy(ctx, policy, request)
	assert.Equal(t, DecisionExplicitDeny, decision, "Explicit Deny should override Allow")

	// Request to non-secret path - should be allowed
	request.Resource = "arn:aws:s3:::my-bucket/public/file.txt"
	decision = EvaluatePolicy(ctx, policy, request)
	assert.Equal(t, DecisionAllow, decision, "Non-matching Deny should not affect Allow")
}

// TestEvaluatePolicy_WildcardPrincipal tests wildcard principal matching
func TestEvaluatePolicy_WildcardPrincipal(t *testing.T) {
	ctx := context.Background()

	policy := &Policy{
		Version: "2012-10-17",
		Statement: []Statement{
			{
				Effect:    "Allow",
				Principal: "*", // Wildcard - matches all principals
				Action:    "s3:GetObject",
				Resource:  "arn:aws:s3:::my-bucket/*",
			},
		},
	}

	tests := []struct {
		name      string
		principal string
	}{
		{"user1", "user1"},
		{"user2", "user2"},
		{"anonymous", "anonymous"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := PolicyEvaluationRequest{
				Principal: tt.principal,
				Action:    "s3:GetObject",
				Resource:  "arn:aws:s3:::my-bucket/file.txt",
				Bucket:    "my-bucket",
			}

			decision := EvaluatePolicy(ctx, policy, request)
			assert.Equal(t, DecisionAllow, decision, "Wildcard principal should match %s", tt.principal)
		})
	}
}

// TestEvaluatePolicy_WildcardActions tests wildcard action matching
func TestEvaluatePolicy_WildcardActions(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		policyAction  interface{}
		requestAction string
		shouldMatch   bool
	}{
		{
			name:          "Exact match",
			policyAction:  "s3:GetObject",
			requestAction: "s3:GetObject",
			shouldMatch:   true,
		},
		{
			name:          "s3:* matches all S3 actions",
			policyAction:  "s3:*",
			requestAction: "s3:GetObject",
			shouldMatch:   true,
		},
		{
			name:          "* matches all actions",
			policyAction:  "*",
			requestAction: "s3:GetObject",
			shouldMatch:   true,
		},
		{
			name:          "s3:Get* matches s3:GetObject",
			policyAction:  "s3:Get*",
			requestAction: "s3:GetObject",
			shouldMatch:   true,
		},
		{
			name:          "s3:Get* matches s3:GetBucket",
			policyAction:  "s3:Get*",
			requestAction: "s3:GetBucket",
			shouldMatch:   true,
		},
		{
			name:          "s3:Get* does not match s3:PutObject",
			policyAction:  "s3:Get*",
			requestAction: "s3:PutObject",
			shouldMatch:   false,
		},
		{
			name:          "Array of actions",
			policyAction:  []string{"s3:GetObject", "s3:PutObject"},
			requestAction: "s3:PutObject",
			shouldMatch:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &Policy{
				Version: "2012-10-17",
				Statement: []Statement{
					{
						Effect:    "Allow",
						Principal: "*",
						Action:    tt.policyAction,
						Resource:  "arn:aws:s3:::my-bucket/*",
					},
				},
			}

			request := PolicyEvaluationRequest{
				Principal: "user123",
				Action:    tt.requestAction,
				Resource:  "arn:aws:s3:::my-bucket/file.txt",
				Bucket:    "my-bucket",
			}

			decision := EvaluatePolicy(ctx, policy, request)
			if tt.shouldMatch {
				assert.Equal(t, DecisionAllow, decision, "Action %s should match %v", tt.requestAction, tt.policyAction)
			} else {
				assert.Equal(t, DecisionDeny, decision, "Action %s should not match %v", tt.requestAction, tt.policyAction)
			}
		})
	}
}

// TestEvaluatePolicy_WildcardResources tests wildcard resource matching
func TestEvaluatePolicy_WildcardResources(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		policyResource  interface{}
		requestResource string
		shouldMatch     bool
	}{
		{
			name:            "Exact match",
			policyResource:  "arn:aws:s3:::my-bucket/file.txt",
			requestResource: "arn:aws:s3:::my-bucket/file.txt",
			shouldMatch:     true,
		},
		{
			name:            "Wildcard * matches all",
			policyResource:  "*",
			requestResource: "arn:aws:s3:::my-bucket/file.txt",
			shouldMatch:     true,
		},
		{
			name:            "Bucket/* matches any object",
			policyResource:  "arn:aws:s3:::my-bucket/*",
			requestResource: "arn:aws:s3:::my-bucket/file.txt",
			shouldMatch:     true,
		},
		{
			name:            "Bucket/* matches nested paths",
			policyResource:  "arn:aws:s3:::my-bucket/*",
			requestResource: "arn:aws:s3:::my-bucket/folder/subfolder/file.txt",
			shouldMatch:     true,
		},
		{
			name:            "Bucket/folder/* matches within folder",
			policyResource:  "arn:aws:s3:::my-bucket/folder/*",
			requestResource: "arn:aws:s3:::my-bucket/folder/file.txt",
			shouldMatch:     true,
		},
		{
			name:            "Bucket/folder/* does not match outside folder",
			policyResource:  "arn:aws:s3:::my-bucket/folder/*",
			requestResource: "arn:aws:s3:::my-bucket/other/file.txt",
			shouldMatch:     false,
		},
		{
			name:            "Array of resources",
			policyResource:  []string{"arn:aws:s3:::my-bucket/public/*", "arn:aws:s3:::my-bucket/shared/*"},
			requestResource: "arn:aws:s3:::my-bucket/shared/file.txt",
			shouldMatch:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &Policy{
				Version: "2012-10-17",
				Statement: []Statement{
					{
						Effect:    "Allow",
						Principal: "*",
						Action:    "s3:GetObject",
						Resource:  tt.policyResource,
					},
				},
			}

			request := PolicyEvaluationRequest{
				Principal: "user123",
				Action:    "s3:GetObject",
				Resource:  tt.requestResource,
				Bucket:    "my-bucket",
			}

			decision := EvaluatePolicy(ctx, policy, request)
			if tt.shouldMatch {
				assert.Equal(t, DecisionAllow, decision, "Resource %s should match %v", tt.requestResource, tt.policyResource)
			} else {
				assert.Equal(t, DecisionDeny, decision, "Resource %s should not match %v", tt.requestResource, tt.policyResource)
			}
		})
	}
}

// TestEvaluatePolicy_ResourceARNNormalization tests ARN normalization
func TestEvaluatePolicy_ResourceARNNormalization(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		policyResource  string
		requestResource string
		shouldMatch     bool
	}{
		{
			name:            "Short format matches ARN format",
			policyResource:  "my-bucket/*",
			requestResource: "arn:aws:s3:::my-bucket/file.txt",
			shouldMatch:     true,
		},
		{
			name:            "ARN format matches short format",
			policyResource:  "arn:aws:s3:::my-bucket/*",
			requestResource: "my-bucket/file.txt",
			shouldMatch:     true,
		},
		{
			name:            "Both short format",
			policyResource:  "my-bucket/*",
			requestResource: "my-bucket/file.txt",
			shouldMatch:     true,
		},
		{
			name:            "Both ARN format",
			policyResource:  "arn:aws:s3:::my-bucket/*",
			requestResource: "arn:aws:s3:::my-bucket/file.txt",
			shouldMatch:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &Policy{
				Version: "2012-10-17",
				Statement: []Statement{
					{
						Effect:    "Allow",
						Principal: "*",
						Action:    "s3:GetObject",
						Resource:  tt.policyResource,
					},
				},
			}

			request := PolicyEvaluationRequest{
				Principal: "user123",
				Action:    "s3:GetObject",
				Resource:  tt.requestResource,
				Bucket:    "my-bucket",
			}

			decision := EvaluatePolicy(ctx, policy, request)
			if tt.shouldMatch {
				assert.Equal(t, DecisionAllow, decision, "Resource formats should normalize and match")
			} else {
				assert.Equal(t, DecisionDeny, decision, "Resources should not match")
			}
		})
	}
}

// TestEvaluatePolicy_MultipleStatements tests multiple statements evaluation
func TestEvaluatePolicy_MultipleStatements(t *testing.T) {
	ctx := context.Background()

	policy := &Policy{
		Version: "2012-10-17",
		Statement: []Statement{
			{
				Effect:    "Allow",
				Principal: "*",
				Action:    "s3:GetObject",
				Resource:  "arn:aws:s3:::my-bucket/public/*",
			},
			{
				Effect:    "Allow",
				Principal: map[string]interface{}{"AWS": "user123"},
				Action:    "s3:GetObject",
				Resource:  "arn:aws:s3:::my-bucket/private/*",
			},
			{
				Effect:    "Deny",
				Principal: "*",
				Action:    "s3:DeleteObject",
				Resource:  "arn:aws:s3:::my-bucket/*",
			},
		},
	}

	tests := []struct {
		name             string
		principal        string
		action           string
		resource         string
		expectedDecision PolicyDecision
	}{
		{
			name:             "Public GetObject allowed for all",
			principal:        "user456",
			action:           "s3:GetObject",
			resource:         "arn:aws:s3:::my-bucket/public/file.txt",
			expectedDecision: DecisionAllow,
		},
		{
			name:             "Private GetObject allowed for user123",
			principal:        "user123",
			action:           "s3:GetObject",
			resource:         "arn:aws:s3:::my-bucket/private/file.txt",
			expectedDecision: DecisionAllow,
		},
		{
			name:             "Private GetObject denied for other users",
			principal:        "user456",
			action:           "s3:GetObject",
			resource:         "arn:aws:s3:::my-bucket/private/file.txt",
			expectedDecision: DecisionDeny,
		},
		{
			name:             "DeleteObject denied for everyone",
			principal:        "user123",
			action:           "s3:DeleteObject",
			resource:         "arn:aws:s3:::my-bucket/public/file.txt",
			expectedDecision: DecisionExplicitDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := PolicyEvaluationRequest{
				Principal: tt.principal,
				Action:    tt.action,
				Resource:  tt.resource,
				Bucket:    "my-bucket",
			}

			decision := EvaluatePolicy(ctx, policy, request)
			assert.Equal(t, tt.expectedDecision, decision)
		})
	}
}

// TestEvaluatePolicy_RealWorldPublicReadPolicy tests real AWS public read policy
func TestEvaluatePolicy_RealWorldPublicReadPolicy(t *testing.T) {
	ctx := context.Background()

	// Real AWS public read policy
	policy := &Policy{
		Version: "2012-10-17",
		Statement: []Statement{
			{
				Sid:       "PublicRead",
				Effect:    "Allow",
				Principal: "*",
				Action:    "s3:GetObject",
				Resource:  "arn:aws:s3:::my-public-bucket/*",
			},
		},
	}

	// Any user should be able to read
	request := PolicyEvaluationRequest{
		Principal: "anonymous",
		Action:    "s3:GetObject",
		Resource:  "arn:aws:s3:::my-public-bucket/document.pdf",
		Bucket:    "my-public-bucket",
	}

	decision := EvaluatePolicy(ctx, policy, request)
	assert.Equal(t, DecisionAllow, decision, "Public read policy should allow any user to GetObject")

	// But not write
	request.Action = "s3:PutObject"
	decision = EvaluatePolicy(ctx, policy, request)
	assert.Equal(t, DecisionDeny, decision, "Public read policy should not allow PutObject")
}

// TestEvaluatePolicy_RealWorldCrossAccountPolicy tests cross-account access policy
func TestEvaluatePolicy_RealWorldCrossAccountPolicy(t *testing.T) {
	ctx := context.Background()

	// Cross-account access policy
	policy := &Policy{
		Version: "2012-10-17",
		Statement: []Statement{
			{
				Sid:    "AllowCrossAccountAccess",
				Effect: "Allow",
				Principal: map[string]interface{}{
					"AWS": []string{
						"arn:aws:iam::123456789012:user/alice",
						"arn:aws:iam::123456789012:user/bob",
					},
				},
				Action: []string{"s3:GetObject", "s3:PutObject"},
				Resource: []string{
					"arn:aws:s3:::shared-bucket/*",
				},
			},
		},
	}

	// Alice should have access
	request := PolicyEvaluationRequest{
		Principal: "arn:aws:iam::123456789012:user/alice",
		Action:    "s3:GetObject",
		Resource:  "arn:aws:s3:::shared-bucket/data.csv",
		Bucket:    "shared-bucket",
	}

	decision := EvaluatePolicy(ctx, policy, request)
	assert.Equal(t, DecisionAllow, decision, "Alice should have GetObject access")

	// Bob should have access
	request.Principal = "arn:aws:iam::123456789012:user/bob"
	decision = EvaluatePolicy(ctx, policy, request)
	assert.Equal(t, DecisionAllow, decision, "Bob should have GetObject access")

	// Charlie should NOT have access
	request.Principal = "arn:aws:iam::123456789012:user/charlie"
	decision = EvaluatePolicy(ctx, policy, request)
	assert.Equal(t, DecisionDeny, decision, "Charlie should not have access")
}

// TestEvaluatePolicy_ConvenienceFunctions tests IsActionAllowed and IsActionDenied
func TestEvaluatePolicy_ConvenienceFunctions(t *testing.T) {
	ctx := context.Background()

	allowPolicy := &Policy{
		Version: "2012-10-17",
		Statement: []Statement{
			{
				Effect:    "Allow",
				Principal: "*",
				Action:    "s3:GetObject",
				Resource:  "arn:aws:s3:::my-bucket/*",
			},
		},
	}

	denyPolicy := &Policy{
		Version: "2012-10-17",
		Statement: []Statement{
			{
				Effect:    "Deny",
				Principal: "*",
				Action:    "s3:DeleteObject",
				Resource:  "arn:aws:s3:::my-bucket/*",
			},
		},
	}

	request := PolicyEvaluationRequest{
		Principal: "user123",
		Action:    "s3:GetObject",
		Resource:  "arn:aws:s3:::my-bucket/file.txt",
		Bucket:    "my-bucket",
	}

	// Test IsActionAllowed
	assert.True(t, IsActionAllowed(ctx, allowPolicy, request), "IsActionAllowed should return true for Allow policy")
	assert.False(t, IsActionAllowed(ctx, denyPolicy, request), "IsActionAllowed should return false for Deny policy")

	// Test IsActionDenied
	request.Action = "s3:DeleteObject"
	assert.True(t, IsActionDenied(ctx, denyPolicy, request), "IsActionDenied should return true for Deny policy")
	request.Action = "s3:GetObject"
	assert.False(t, IsActionDenied(ctx, allowPolicy, request), "IsActionDenied should return false for Allow policy")
}

// TestEvaluatePolicy_PrincipalFormats tests different principal formats
func TestEvaluatePolicy_PrincipalFormats(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		policyPrincipal interface{}
		requestPrinc    string
		shouldMatch     bool
	}{
		{
			name:            "String wildcard",
			policyPrincipal: "*",
			requestPrinc:    "user123",
			shouldMatch:     true,
		},
		{
			name:            "String exact match",
			policyPrincipal: "user123",
			requestPrinc:    "user123",
			shouldMatch:     true,
		},
		{
			name:            "String no match",
			policyPrincipal: "user456",
			requestPrinc:    "user123",
			shouldMatch:     false,
		},
		{
			name: "AWS map single user",
			policyPrincipal: map[string]interface{}{
				"AWS": "user123",
			},
			requestPrinc: "user123",
			shouldMatch:  true,
		},
		{
			name: "AWS map wildcard",
			policyPrincipal: map[string]interface{}{
				"AWS": "*",
			},
			requestPrinc: "user123",
			shouldMatch:  true,
		},
		{
			name: "AWS map array",
			policyPrincipal: map[string]interface{}{
				"AWS": []interface{}{"user123", "user456"},
			},
			requestPrinc: "user123",
			shouldMatch:  true,
		},
		{
			name: "CanonicalUser format",
			policyPrincipal: map[string]interface{}{
				"CanonicalUser": "user123",
			},
			requestPrinc: "user123",
			shouldMatch:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &Policy{
				Version: "2012-10-17",
				Statement: []Statement{
					{
						Effect:    "Allow",
						Principal: tt.policyPrincipal,
						Action:    "s3:GetObject",
						Resource:  "arn:aws:s3:::my-bucket/*",
					},
				},
			}

			request := PolicyEvaluationRequest{
				Principal: tt.requestPrinc,
				Action:    "s3:GetObject",
				Resource:  "arn:aws:s3:::my-bucket/file.txt",
				Bucket:    "my-bucket",
			}

			decision := EvaluatePolicy(ctx, policy, request)
			if tt.shouldMatch {
				assert.Equal(t, DecisionAllow, decision, "Principal should match")
			} else {
				assert.Equal(t, DecisionDeny, decision, "Principal should not match")
			}
		})
	}
}

// TestNormalizeResourceARN tests ARN normalization function
func TestNormalizeResourceARN(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		bucket   string
		expected string
	}{
		{
			name:     "Already ARN format",
			resource: "arn:aws:s3:::my-bucket/object.txt",
			bucket:   "my-bucket",
			expected: "arn:aws:s3:::my-bucket/object.txt",
		},
		{
			name:     "Bucket only",
			resource: "my-bucket",
			bucket:   "my-bucket",
			expected: "arn:aws:s3:::my-bucket",
		},
		{
			name:     "Bucket with object",
			resource: "my-bucket/object.txt",
			bucket:   "my-bucket",
			expected: "arn:aws:s3:::my-bucket/object.txt",
		},
		{
			name:     "Bucket with wildcard",
			resource: "my-bucket/*",
			bucket:   "my-bucket",
			expected: "arn:aws:s3:::my-bucket/*",
		},
		{
			name:     "Bucket with folder",
			resource: "my-bucket/folder/object.txt",
			bucket:   "my-bucket",
			expected: "arn:aws:s3:::my-bucket/folder/object.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeResourceARN(tt.resource, tt.bucket)
			assert.Equal(t, tt.expected, result)
		})
	}
}
