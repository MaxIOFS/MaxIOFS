package replication

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/db/migrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Re-encryption rewrites the destination keys the old secret encrypted,
// leaves the ones the new secret encrypted and the ones stored before
// encryption, and names the ones neither decrypts. A check names them and
// writes nothing. The manager decrypts with the secret it holds at each read.
func TestReencryptRewritesTheDestinationKeys(t *testing.T) {
	dir, err := os.MkdirTemp("", "maxiofs-replication-reencrypt-*")
	require.NoError(t, err)
	db, err := sql.Open("sqlite", filepath.Join(dir, "maxiofs.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
	require.NoError(t, migrations.NewMigrationManager(db, nil).Migrate())
	ctx := context.Background()
	key := "old"
	m, err := NewManager(db, ReplicationConfig{Enable: true, WorkerCount: 1, QueueSize: 10, BatchSize: 1,
		RetryInterval: time.Minute, MaxRetries: 1, CleanupInterval: time.Hour, RetentionDays: 1,
		CredentialEncryptionKey: func() string { return key }}, &MockObjectAdapter{}, &MockObjectManager{}, &MockBucketLister{})
	require.NoError(t, err)

	seal := func(plain, key string) string {
		t.Helper()
		v, err := encryptCredential(plain, key)
		require.NoError(t, err)
		return v
	}
	insert := func(id, secret string) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO replication_rules (id, tenant_id, source_bucket, destination_endpoint, destination_bucket,
			destination_access_key, destination_secret_key) VALUES (?, 't', 'b', 'https://dest', 'd', 'AK', ?)`, id, secret)
		require.NoError(t, err)
	}
	insert("mine", seal("mine-secret-key", "old"))
	insert("theirs", seal("theirs-secret-key", "new"))
	insert("lost", seal("lost-secret-key", "other"))
	insert("legacy", "legacy-plain-key")
	stored := func(id string) string {
		t.Helper()
		var v string
		require.NoError(t, db.QueryRow(`SELECT destination_secret_key FROM replication_rules WHERE id = ?`, id).Scan(&v))
		return v
	}
	run := func(from, to string) []string {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		names, err := ReencryptCredentials(ctx, tx, from, to)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		return names
	}

	before := stored("mine")
	assert.ElementsMatch(t, []string{"replication rule theirs (b to https://dest/d)", "replication rule lost (b to https://dest/d)"},
		run("old", "old"))
	assert.Equal(t, before, stored("mine"), "a check writes nothing")

	theirs, lost := stored("theirs"), stored("lost")
	assert.Equal(t, []string{"replication rule lost (b to https://dest/d)"}, run("old", "new"))
	key = "new"
	rule, err := m.GetRule(ctx, "mine")
	require.NoError(t, err)
	assert.Equal(t, "mine-secret-key", rule.DestinationSecretKey)
	assert.Equal(t, theirs, stored("theirs"))
	assert.Equal(t, "legacy-plain-key", stored("legacy"))
	assert.Equal(t, lost, stored("lost"), "what no secret decrypts is left as it is")

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck
	_, err = ReencryptCredentials(ctx, tx, "old", "")
	assert.Error(t, err)
}
