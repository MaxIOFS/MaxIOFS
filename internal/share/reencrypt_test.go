package share

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/db/migrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func migratedDB(t *testing.T) *sql.DB {
	t.Helper()
	dir, err := os.MkdirTemp("", "maxiofs-share-reencrypt-*")
	require.NoError(t, err)
	db, err := sql.Open("sqlite", filepath.Join(dir, "maxiofs.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
	require.NoError(t, migrations.NewMigrationManager(db, nil).Migrate())
	return db
}

// Re-encryption rewrites the share keys the old secret encrypted, leaves the
// ones the new secret encrypted and the ones stored before encryption, and
// names the ones neither decrypts. A check names them and writes nothing. The
// store decrypts with the secret it holds at each read.
func TestReencryptRewritesTheShareKeys(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	key := "old"
	store, err := NewSQLiteStore(db, func() string { return key })
	require.NoError(t, err)
	mine, err := NewManager(store).CreateShare(ctx, "b", "mine", "", "AK", "mine-secret", "u", nil)
	require.NoError(t, err)
	insert := func(object, secret string) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO shares (id, bucket_name, object_key, tenant_id, access_key_id, secret_key, share_token, created_at, created_by)
			VALUES (?, 'b', ?, '', 'AK', ?, ?, 0, 'u')`, object, object, secret, object)
		require.NoError(t, err)
	}
	seal := func(plain, key string) string {
		t.Helper()
		v, err := encryptShareCredential(plain, key)
		require.NoError(t, err)
		return v
	}
	insert("theirs", seal("theirs-secret", "new"))
	insert("lost", seal("lost-secret", "other"))
	insert("legacy", "legacy-plain")
	stored := func(object string) string {
		t.Helper()
		var v string
		require.NoError(t, db.QueryRow(`SELECT secret_key FROM shares WHERE object_key = ?`, object).Scan(&v))
		return v
	}
	run := func(from, to string) []string {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		names, err := Reencrypt(ctx, tx, from, to)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		return names
	}

	before := stored("mine")
	assert.True(t, strings.HasPrefix(before, shareEncryptionPrefix), "the store encrypts with the secret it holds")
	assert.ElementsMatch(t, []string{"share link of b/theirs", "share link of b/lost"}, run("old", "old"))
	assert.Equal(t, before, stored("mine"), "a check writes nothing")

	theirs, lost := stored("theirs"), stored("lost")
	assert.Equal(t, []string{"share link of b/lost"}, run("old", "new"))
	key = "new"
	got, err := store.GetShare(ctx, mine.ID)
	require.NoError(t, err)
	assert.Equal(t, "mine-secret", got.SecretKey)
	assert.Equal(t, theirs, stored("theirs"))
	assert.Equal(t, "legacy-plain", stored("legacy"))
	assert.Equal(t, lost, stored("lost"), "what no secret decrypts is left as it is")

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck
	_, err = Reencrypt(ctx, tx, "", "new")
	assert.Error(t, err)
}
