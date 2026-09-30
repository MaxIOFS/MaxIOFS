package share

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every share created or deleted is reported once stored. A failed deletion is
// not, nor are expired shares dropped, which every node drops on its own.
func TestShareChangesAreReported(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()
	store, err := NewSQLiteStore(db, nil)
	require.NoError(t, err)
	manager := NewManager(store)
	var seen []string
	manager.SetChangeObserver(func(_ context.Context, table, id string, deleted bool) {
		seen = append(seen, fmt.Sprintf("%s/%s/%v", table, id, deleted))
	})
	ctx := context.Background()

	kept, err := manager.CreateShare(ctx, "b", "kept", "", "ak", "sk", "u", nil)
	require.NoError(t, err)
	gone, err := manager.CreateShare(ctx, "b", "gone", "", "ak", "sk", "u", nil)
	require.NoError(t, err)
	require.NoError(t, manager.DeleteShare(ctx, gone.ID))
	require.Error(t, manager.DeleteShare(ctx, gone.ID))
	_, err = db.Exec(`UPDATE shares SET expires_at = 1 WHERE id = ?`, kept.ID)
	require.NoError(t, err)
	require.NoError(t, manager.DeleteExpiredShares(ctx))

	assert.Equal(t, []string{
		SharesTable + "/" + kept.ID + "/false",
		SharesTable + "/" + gone.ID + "/false",
		SharesTable + "/" + gone.ID + "/true",
	}, seen)
}

// A share created for an object whose share expired but was not yet dropped
// takes the place of the expired one, under the new share's id.
func TestAShareReplacesAnExpiredOneUnderItsOwnID(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()
	store, err := NewSQLiteStore(db, nil)
	require.NoError(t, err)
	manager := NewManager(store)
	var seen []string
	manager.SetChangeObserver(func(_ context.Context, _, id string, _ bool) { seen = append(seen, id) })
	ctx := context.Background()

	expired, err := manager.CreateShare(ctx, "b", "k", "", "ak", "sk", "u", nil)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE shares SET expires_at = 1 WHERE id = ?`, expired.ID)
	require.NoError(t, err)
	fresh, err := manager.CreateShare(ctx, "b", "k", "", "ak2", "sk2", "u", nil)
	require.NoError(t, err)

	got, err := manager.GetShare(ctx, fresh.ID)
	require.NoError(t, err)
	assert.Equal(t, fresh.ShareToken, got.ShareToken)
	assert.Equal(t, "sk2", got.SecretKey)
	_, err = manager.GetShare(ctx, expired.ID)
	assert.ErrorIs(t, err, ErrShareNotFound)
	assert.Equal(t, []string{expired.ID, fresh.ID}, seen)
	require.NoError(t, manager.DeleteShare(ctx, fresh.ID))
}
