package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func storedLastUsed(t *testing.T, store *SQLiteStore, accessKeyID string) int64 {
	t.Helper()
	var lastUsed int64
	require.NoError(t, store.db.QueryRow(
		`SELECT last_used FROM access_keys WHERE access_key_id = ?`, accessKeyID).Scan(&lastUsed))
	return lastUsed
}

func TestUpdateAccessKeyLastUsed_SkipsASecondAlreadyOnDisk(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	require.NoError(t, store.CreateUser(&User{
		ID: "lu-user", Username: "lu-user", Status: UserStatusActive,
		Roles: []string{"user"}, CreatedAt: time.Now().Unix(),
	}))
	const keyID = "AKIALASTUSEDTEST"
	require.NoError(t, store.CreateAccessKey(&AccessKey{
		AccessKeyID: keyID, SecretAccessKey: "secret", UserID: "lu-user",
		Status: AccessKeyStatusActive, CreatedAt: time.Now().Unix(),
	}))

	require.NoError(t, store.UpdateAccessKeyLastUsed(keyID, 1000))
	require.Equal(t, int64(1000), storedLastUsed(t, store, keyID))

	// A sentinel the next call would overwrite if it reached the database.
	_, err := store.db.Exec(`UPDATE access_keys SET last_used = -1 WHERE access_key_id = ?`, keyID)
	require.NoError(t, err)

	require.NoError(t, store.UpdateAccessKeyLastUsed(keyID, 1000))
	assert.Equal(t, int64(-1), storedLastUsed(t, store, keyID),
		"the same second was written again")

	require.NoError(t, store.UpdateAccessKeyLastUsed(keyID, 1001))
	assert.Equal(t, int64(1001), storedLastUsed(t, store, keyID),
		"a new second must reach the database")
}

func TestUpdateAccessKeyLastUsed_NeverMovesBackwards(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	require.NoError(t, store.CreateUser(&User{
		ID: "lu-user-2", Username: "lu-user-2", Status: UserStatusActive,
		Roles: []string{"user"}, CreatedAt: time.Now().Unix(),
	}))
	const keyID = "AKIALASTUSEDBACK"
	require.NoError(t, store.CreateAccessKey(&AccessKey{
		AccessKeyID: keyID, SecretAccessKey: "secret", UserID: "lu-user-2",
		Status: AccessKeyStatusActive, CreatedAt: time.Now().Unix(),
	}))

	require.NoError(t, store.UpdateAccessKeyLastUsed(keyID, 2000))
	require.NoError(t, store.UpdateAccessKeyLastUsed(keyID, 1999))
	assert.Equal(t, int64(2000), storedLastUsed(t, store, keyID))
}
