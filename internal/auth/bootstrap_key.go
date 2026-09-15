package auth

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	BootstrapAccessKeyEnv = "MAXIOFS_BOOTSTRAP_ACCESS_KEY"
	BootstrapSecretKeyEnv = "MAXIOFS_BOOTSTRAP_SECRET_KEY"

	bootstrapAccessKeyMinLen = 3
	bootstrapSecretKeyMinLen = 8
	bootstrapKeyMaxLen       = 128
)

// SeedBootstrapAccessKey gives the administrator the key pair named in the
// environment, once, on a deployment that has none. A deployment that already
// has a key is left alone, so changing the variables later does nothing.
func SeedBootstrapAccessKey(ctx context.Context, am Manager) error {
	accessKeyID := strings.TrimSpace(os.Getenv(BootstrapAccessKeyEnv))
	secretKey := strings.TrimSpace(os.Getenv(BootstrapSecretKeyEnv))
	if accessKeyID == "" && secretKey == "" {
		return nil
	}
	if err := validateBootstrapPair(accessKeyID, secretKey); err != nil {
		return err
	}

	manager, ok := am.(*authManager)
	if !ok {
		return nil
	}

	existing, err := manager.store.ListAccessKeysByUser("admin")
	if err != nil {
		return fmt.Errorf("could not read the administrator's access keys: %w", err)
	}
	if len(existing) > 0 {
		logrus.WithField("env", BootstrapAccessKeyEnv).
			Debug("The administrator already has an access key; the environment pair was not applied")
		return nil
	}

	if err := manager.createAccessKeyWithSecret(ctx, "admin", accessKeyID, secretKey); err != nil {
		return fmt.Errorf("could not seed the access key from %s: %w", BootstrapAccessKeyEnv, err)
	}

	logrus.WithField("access_key", accessKeyID).
		Warn("Access key seeded from the environment. It is stored now: changing the variable later has no effect, and the secret is as readable as the environment it came from")
	return nil
}

func validateBootstrapPair(accessKeyID, secretKey string) error {
	if accessKeyID == "" || secretKey == "" {
		return fmt.Errorf("%s and %s must both be set", BootstrapAccessKeyEnv, BootstrapSecretKeyEnv)
	}
	if err := validateBootstrapValue(BootstrapAccessKeyEnv, accessKeyID, bootstrapAccessKeyMinLen); err != nil {
		return err
	}
	return validateBootstrapValue(BootstrapSecretKeyEnv, secretKey, bootstrapSecretKeyMinLen)
}

// validateBootstrapValue rejects what a client could not sign with. The
// credential scope of a SigV4 signature is slash-separated, so a key carrying a
// slash or a space produces a signature nothing can verify.
func validateBootstrapValue(name, value string, minLen int) error {
	if len(value) < minLen {
		return fmt.Errorf("%s is %d characters, the minimum is %d", name, len(value), minLen)
	}
	if len(value) > bootstrapKeyMaxLen {
		return fmt.Errorf("%s is %d characters, the maximum is %d", name, len(value), bootstrapKeyMaxLen)
	}
	if strings.ContainsAny(value, "/ \t\r\n") {
		return fmt.Errorf("%s must not contain spaces or slashes", name)
	}
	return nil
}

// createAccessKeyWithSecret stores a key pair chosen by the operator, encrypting
// the secret the way a generated one is.
func (am *authManager) createAccessKeyWithSecret(ctx context.Context, userID, accessKeyID, secretKey string) error {
	user, err := am.store.GetUserByID(userID)
	if err != nil {
		return ErrUserNotFound
	}
	if user.Status != UserStatusActive {
		return ErrAccessDenied
	}

	encrypted, err := am.encryptSecret(secretKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt secret access key: %w", err)
	}

	return am.store.CreateAccessKey(&AccessKey{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: encrypted,
		UserID:          userID,
		Status:          AccessKeyStatusActive,
		CreatedAt:       time.Now().Unix(),
	})
}
