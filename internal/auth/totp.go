package auth

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
)

// TOTPSetup contains the setup information for 2FA
type TOTPSetup struct {
	Secret string `json:"secret"`
	QRCode []byte `json:"qr_code"`
	URL    string `json:"url"`
}

// Generate2FASecret generates a new TOTP secret for a user
// Returns the secret, QR code image, and otpauth URL
func Generate2FASecret(username, issuer string) (*TOTPSetup, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: username,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate TOTP key: %w", err)
	}

	// Generate QR code image (256x256 pixels, medium error correction)
	qrCode, err := qrcode.Encode(key.String(), qrcode.Medium, 256)
	if err != nil {
		return nil, fmt.Errorf("failed to generate QR code: %w", err)
	}

	return &TOTPSetup{
		Secret: key.Secret(),
		QRCode: qrCode,
		URL:    key.URL(),
	}, nil
}

// VerifyTOTPCode verifies a TOTP code against a secret
// Allows for time skew of ±1 period (30 seconds before/after)
func VerifyTOTPCode(secret, code string) bool {
	// Validate with current time and ±1 period for clock skew
	valid, err := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1, // Allow ±30 seconds
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})

	return err == nil && valid
}

// GenerateBackupCodes generates 10 random backup codes
// Each code is 8 characters (uppercase alphanumeric)
// Format: XXXX-XXXX for readability
func GenerateBackupCodes() ([]string, error) {
	codes := make([]string, 10)
	for i := 0; i < 10; i++ {
		code, err := generateRandomCode()
		if err != nil {
			return nil, fmt.Errorf("failed to generate backup code: %w", err)
		}
		codes[i] = code
	}
	return codes, nil
}

// generateRandomCode generates a single random backup code
// Format: XXXX-XXXX (8 characters with hyphen)
func generateRandomCode() (string, error) {
	b := make([]byte, 5) // 5 bytes = 8 base32 characters
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	// Encode to base32 and take first 8 characters
	code := base32.StdEncoding.EncodeToString(b)[:8]
	code = strings.ToUpper(code)

	// Format as XXXX-XXXX for readability
	return code[:4] + "-" + code[4:], nil
}

// HashBackupCode hashes a backup code using bcrypt
func HashBackupCode(code string) (string, error) {
	// Remove hyphen before hashing
	code = strings.ReplaceAll(code, "-", "")

	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash backup code: %w", err)
	}
	return string(hash), nil
}

// VerifyBackupCode verifies a backup code against its hash
func VerifyBackupCode(code, hash string) bool {
	// Remove hyphen before comparing
	code = strings.ReplaceAll(code, "-", "")

	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(code))
	return err == nil
}

// IsBackupCode checks if a code looks like a backup code (format: XXXX-XXXX)
func IsBackupCode(code string) bool {
	// Backup codes are 9 characters: XXXX-XXXX
	if len(code) != 9 {
		return false
	}

	// Check format
	parts := strings.Split(code, "-")
	if len(parts) != 2 {
		return false
	}

	if len(parts[0]) != 4 || len(parts[1]) != 4 {
		return false
	}

	// Check if all characters are alphanumeric
	for _, part := range parts {
		for _, ch := range part {
			if !((ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')) {
				return false
			}
		}
	}

	return true
}
