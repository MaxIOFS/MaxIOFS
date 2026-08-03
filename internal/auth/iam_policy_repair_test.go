package auth

// A managed policy whose default version row is missing.
//
// It served an empty document, which the console showed as a policy granting
// nothing — and saving from that screen would have written the emptiness back.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicy_OrphanedDefaultVersionIsRepaired(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMPolicy(ctx, "Orphaned", "/", "", readOnlyBucketDocument)
	require.NoError(t, err)

	// Point the policy at a version that does not exist, which is what a failed
	// version delete or an interrupted write leaves behind.
	_, err = am.store.db.Exec(
		`UPDATE iam_policies SET default_version_id = 'v99' WHERE name = 'Orphaned'`)
	require.NoError(t, err)

	policy, err := am.GetIAMPolicy(ctx, "Orphaned")
	require.NoError(t, err)
	assert.Equal(t, readOnlyBucketDocument, policy.Document,
		"the surviving version is served instead of an empty document")
	assert.Equal(t, "v1", policy.DefaultVersionID, "and the policy is pointed back at it")

	// Listing shows the same thing, not a blank.
	list, err := am.ListIAMPolicies(ctx)
	require.NoError(t, err)
	for _, p := range list {
		if p.Name == "Orphaned" {
			assert.NotEmpty(t, p.Document, "a listing must not show a policy as granting nothing")
		}
	}
}

func TestPolicy_NoVersionsAtAllIsAnError(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.CreateIAMPolicy(ctx, "Empty", "/", "", readOnlyBucketDocument)
	require.NoError(t, err)

	_, err = am.store.db.Exec(`DELETE FROM iam_policy_versions WHERE policy_name = 'Empty'`)
	require.NoError(t, err)

	_, err = am.GetIAMPolicy(ctx, "Empty")
	assert.Error(t, err, "a policy with nothing to serve is reported, not served empty")
}
