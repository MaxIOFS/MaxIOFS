package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// TestGenerate2FASecret tests the generation of 2FA secrets
func TestGenerate2FASecret(t *testing.T) {
	tests := []struct {
		name     string
		username string
		issuer   string
		wantErr  bool
	}{
		{
			name:     "Valid generation",
			username: "testuser",
			issuer:   "MaxIOFS",
			wantErr:  false,
		},
		{
			name:     "Valid with special characters in username",
			username: "test.user@example.com",
			issuer:   "MaxIOFS",
			wantErr:  false,
		},
		{
			name:     "Empty username",
			username: "",
			issuer:   "MaxIOFS",
			wantErr:  true, // Library requires AccountName
		},
		{
			name:     "Empty issuer",
			username: "testuser",
			issuer:   "",
			wantErr:  true, // Library requires Issuer
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup, err := Generate2FASecret(tt.username, tt.issuer)
			if (err != nil) != tt.wantErr {
				t.Errorf("Generate2FASecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify secret is not empty
				if setup.Secret == "" {
					t.Error("Secret is empty")
				}

				// Verify secret is base32 encoded (32 characters typically)
				if len(setup.Secret) < 16 {
					t.Errorf("Secret too short: %d characters", len(setup.Secret))
				}

				// Verify QR code is generated
				if len(setup.QRCode) == 0 {
					t.Error("QR code is empty")
				}

				// Verify URL format
				if !strings.HasPrefix(setup.URL, "otpauth://totp/") {
					t.Errorf("Invalid URL format: %s", setup.URL)
				}

				// Verify URL contains username
				if tt.username != "" && !strings.Contains(setup.URL, tt.username) {
					t.Errorf("URL doesn't contain username: %s", setup.URL)
				}

				// Verify URL contains issuer
				if tt.issuer != "" && !strings.Contains(setup.URL, tt.issuer) {
					t.Errorf("URL doesn't contain issuer: %s", setup.URL)
				}
			}
		})
	}
}

// TestVerifyTOTPCode tests TOTP code verification
func TestVerifyTOTPCode(t *testing.T) {
	// Generate a test secret
	setup, err := Generate2FASecret("testuser", "MaxIOFS")
	if err != nil {
		t.Fatalf("Failed to generate secret: %v", err)
	}

	// Generate a valid code for current time
	validCode, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatalf("Failed to generate valid code: %v", err)
	}

	tests := []struct {
		name   string
		secret string
		code   string
		want   bool
	}{
		{
			name:   "Valid code",
			secret: setup.Secret,
			code:   validCode,
			want:   true,
		},
		{
			name:   "Invalid code - wrong digits",
			secret: setup.Secret,
			code:   "000000",
			want:   false,
		},
		{
			name:   "Invalid code - empty",
			secret: setup.Secret,
			code:   "",
			want:   false,
		},
		{
			name:   "Invalid code - too short",
			secret: setup.Secret,
			code:   "123",
			want:   false,
		},
		{
			name:   "Invalid code - too long",
			secret: setup.Secret,
			code:   "1234567",
			want:   false,
		},
		{
			name:   "Invalid secret - empty",
			secret: "",
			code:   validCode,
			want:   false,
		},
		{
			name:   "Invalid secret - malformed",
			secret: "invalid-secret",
			code:   validCode,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifyTOTPCode(tt.secret, tt.code)
			if got != tt.want {
				t.Errorf("VerifyTOTPCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestVerifyTOTPCode_TimeSkew tests that codes within ±30 seconds are accepted
func TestVerifyTOTPCode_TimeSkew(t *testing.T) {
	setup, err := Generate2FASecret("testuser", "MaxIOFS")
	if err != nil {
		t.Fatalf("Failed to generate secret: %v", err)
	}

	// Test code from 30 seconds ago (should be valid with skew=1)
	pastTime := time.Now().Add(-30 * time.Second)
	pastCode, err := totp.GenerateCode(setup.Secret, pastTime)
	if err != nil {
		t.Fatalf("Failed to generate past code: %v", err)
	}

	if !VerifyTOTPCode(setup.Secret, pastCode) {
		t.Error("Code from 30 seconds ago should be valid (within skew)")
	}

	// Test code from 30 seconds in the future (should be valid with skew=1)
	futureTime := time.Now().Add(30 * time.Second)
	futureCode, err := totp.GenerateCode(setup.Secret, futureTime)
	if err != nil {
		t.Fatalf("Failed to generate future code: %v", err)
	}

	if !VerifyTOTPCode(setup.Secret, futureCode) {
		t.Error("Code from 30 seconds in the future should be valid (within skew)")
	}

	// Test code from 90 seconds ago (should be invalid, outside skew)
	oldTime := time.Now().Add(-90 * time.Second)
	oldCode, err := totp.GenerateCode(setup.Secret, oldTime)
	if err != nil {
		t.Fatalf("Failed to generate old code: %v", err)
	}

	if VerifyTOTPCode(setup.Secret, oldCode) {
		t.Error("Code from 90 seconds ago should be invalid (outside skew)")
	}
}

// TestGenerateBackupCodes tests backup code generation
func TestGenerateBackupCodes(t *testing.T) {
	codes, err := GenerateBackupCodes()
	if err != nil {
		t.Fatalf("GenerateBackupCodes() error = %v", err)
	}

	// Should generate exactly 10 codes
	if len(codes) != 10 {
		t.Errorf("Expected 10 backup codes, got %d", len(codes))
	}

	// All codes should be unique
	seen := make(map[string]bool)
	for _, code := range codes {
		if seen[code] {
			t.Errorf("Duplicate backup code generated: %s", code)
		}
		seen[code] = true

		// Each code should match format XXXX-XXXX
		if !IsBackupCode(code) {
			t.Errorf("Invalid backup code format: %s", code)
		}

		// Check length (should be 9: 4 + hyphen + 4)
		if len(code) != 9 {
			t.Errorf("Invalid code length: %s (len=%d)", code, len(code))
		}

		// Check hyphen position
		if code[4] != '-' {
			t.Errorf("Hyphen not in correct position: %s", code)
		}

		// Check all characters are uppercase alphanumeric
		for i, ch := range code {
			if i == 4 {
				continue // Skip hyphen
			}
			if !((ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')) {
				t.Errorf("Invalid character in code %s at position %d: %c", code, i, ch)
			}
		}
	}
}

// TestHashBackupCode tests backup code hashing
func TestHashBackupCode(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{
			name:    "Valid code with hyphen",
			code:    "ABCD-1234",
			wantErr: false,
		},
		{
			name:    "Valid code without hyphen",
			code:    "ABCD1234",
			wantErr: false,
		},
		{
			name:    "Empty code",
			code:    "",
			wantErr: false, // bcrypt will hash empty string
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashBackupCode(tt.code)
			if (err != nil) != tt.wantErr {
				t.Errorf("HashBackupCode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Hash should not be empty
				if hash == "" {
					t.Error("Hash is empty")
				}

				// Hash should start with bcrypt prefix
				if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") {
					t.Errorf("Hash doesn't have bcrypt prefix: %s", hash)
				}

				// Hash should be different from original code
				codeNoHyphen := strings.ReplaceAll(tt.code, "-", "")
				if hash == codeNoHyphen {
					t.Error("Hash should be different from original code")
				}
			}
		})
	}
}

// TestVerifyBackupCode tests backup code verification
func TestVerifyBackupCode(t *testing.T) {
	// Generate a test code and hash it
	testCode := "ABCD-1234"
	hash, err := HashBackupCode(testCode)
	if err != nil {
		t.Fatalf("Failed to hash test code: %v", err)
	}

	tests := []struct {
		name string
		code string
		hash string
		want bool
	}{
		{
			name: "Valid code with hyphen",
			code: "ABCD-1234",
			hash: hash,
			want: true,
		},
		{
			name: "Valid code without hyphen",
			code: "ABCD1234",
			hash: hash,
			want: true,
		},
		{
			name: "Invalid code - wrong code",
			code: "WRONG-CODE",
			hash: hash,
			want: false,
		},
		{
			name: "Invalid code - empty",
			code: "",
			hash: hash,
			want: false,
		},
		{
			name: "Invalid hash - empty",
			code: testCode,
			hash: "",
			want: false,
		},
		{
			name: "Invalid hash - malformed",
			code: testCode,
			hash: "not-a-valid-hash",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifyBackupCode(tt.code, tt.hash)
			if got != tt.want {
				t.Errorf("VerifyBackupCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestVerifyBackupCode_MultipleHashes ensures same code produces different hashes
func TestVerifyBackupCode_MultipleHashes(t *testing.T) {
	code := "TEST-CODE"

	// Generate multiple hashes for the same code
	hash1, err := HashBackupCode(code)
	if err != nil {
		t.Fatalf("Failed to hash code: %v", err)
	}

	hash2, err := HashBackupCode(code)
	if err != nil {
		t.Fatalf("Failed to hash code: %v", err)
	}

	// Hashes should be different (bcrypt uses salt)
	if hash1 == hash2 {
		t.Error("Multiple hashes of same code should be different")
	}

	// Both hashes should verify correctly
	if !VerifyBackupCode(code, hash1) {
		t.Error("First hash should verify")
	}

	if !VerifyBackupCode(code, hash2) {
		t.Error("Second hash should verify")
	}
}

// TestIsBackupCode tests backup code format validation
func TestIsBackupCode(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{
			name: "Valid format - uppercase alphanumeric",
			code: "ABCD-1234",
			want: true,
		},
		{
			name: "Valid format - all letters",
			code: "ABCD-EFGH",
			want: true,
		},
		{
			name: "Valid format - all numbers",
			code: "1234-5678",
			want: true,
		},
		{
			name: "Invalid - lowercase letters",
			code: "abcd-1234",
			want: false,
		},
		{
			name: "Invalid - too short",
			code: "ABC-123",
			want: false,
		},
		{
			name: "Invalid - too long",
			code: "ABCDE-12345",
			want: false,
		},
		{
			name: "Invalid - no hyphen",
			code: "ABCD1234",
			want: false,
		},
		{
			name: "Invalid - hyphen in wrong position",
			code: "ABC-D1234",
			want: false,
		},
		{
			name: "Invalid - multiple hyphens",
			code: "AB-CD-1234",
			want: false,
		},
		{
			name: "Invalid - empty string",
			code: "",
			want: false,
		},
		{
			name: "Invalid - special characters",
			code: "AB@D-1234",
			want: false,
		},
		{
			name: "Invalid - spaces",
			code: "ABCD -1234",
			want: false,
		},
		{
			name: "Invalid - first part too short",
			code: "ABC-12345",
			want: false,
		},
		{
			name: "Invalid - second part too short",
			code: "ABCDE-123",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBackupCode(tt.code)
			if got != tt.want {
				t.Errorf("IsBackupCode(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

// TestGenerateBackupCodes_AllValid ensures all generated codes pass validation
func TestGenerateBackupCodes_AllValid(t *testing.T) {
	for i := 0; i < 5; i++ { // Run multiple times to ensure consistency
		codes, err := GenerateBackupCodes()
		if err != nil {
			t.Fatalf("Iteration %d: GenerateBackupCodes() error = %v", i, err)
		}

		for j, code := range codes {
			if !IsBackupCode(code) {
				t.Errorf("Iteration %d, code %d: Invalid format: %s", i, j, code)
			}
		}
	}
}

// TestBackupCodeHashVerifyRoundTrip tests complete hash/verify cycle
func TestBackupCodeHashVerifyRoundTrip(t *testing.T) {
	// Generate backup codes
	codes, err := GenerateBackupCodes()
	if err != nil {
		t.Fatalf("Failed to generate codes: %v", err)
	}

	// Hash and verify each code
	for _, code := range codes {
		// Hash the code
		hash, err := HashBackupCode(code)
		if err != nil {
			t.Errorf("Failed to hash code %s: %v", code, err)
			continue
		}

		// Verify with original code
		if !VerifyBackupCode(code, hash) {
			t.Errorf("Failed to verify code %s with its own hash", code)
		}

		// Verify with code without hyphen
		codeNoHyphen := strings.ReplaceAll(code, "-", "")
		if !VerifyBackupCode(codeNoHyphen, hash) {
			t.Errorf("Failed to verify code %s without hyphen", code)
		}

		// Should fail with wrong code
		if VerifyBackupCode("WRONG-CODE", hash) {
			t.Errorf("Should not verify with wrong code")
		}
	}
}
