package s3compat

// Which buckets a caller sees.
//
// Listing used to be decided by the role name, by ownership, and by
// CheckBucketAccess — which reads the legacy bucket_permissions table that is
// no longer consulted when authorizing. The two answers had drifted apart: a
// bucket granted purely through IAM was invisible in the listing while every
// object inside it was perfectly accessible, and a grant revoked in IAM kept
// its bucket on screen.

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listedBuckets performs ListBuckets as the given user and returns the names.
func listedBuckets(t *testing.T, env *coverageTestEnv, user *auth.User) []string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(setUserInContext(req.Context(), user))
	w := httptest.NewRecorder()
	env.handler.ListBuckets(w, req)

	if w.Code != http.StatusOK {
		return nil
	}

	var result struct {
		Buckets struct {
			Bucket []struct {
				Name string `xml:"Name"`
			} `xml:"Bucket"`
		} `xml:"Buckets"`
	}
	require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &result))

	names := make([]string, 0, len(result.Buckets.Bucket))
	for _, b := range result.Buckets.Bucket {
		names = append(names, b.Name)
	}
	return names
}

func TestListBuckets_ShowsWhatThePoliciesGrant(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "mine", env.userID))
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "someone-elses", "another-user"))

	owner := &auth.User{ID: env.userID, TenantID: env.tenantID, Roles: []string{"user"}}

	names := listedBuckets(t, env, owner)
	assert.Contains(t, names, "mine",
		"a bucket the caller owns carries an owner policy and must be listed")
	assert.NotContains(t, names, "someone-elses",
		"sharing a tenant is not a grant; that bucket was never given to this caller")
}

// TestListBuckets_HidesNothingTheCallerCanRead is the drift the fix exists for:
// what the listing shows and what the caller can actually open must be the same
// set. A bucket whose objects are readable must not be missing from the list.
func TestListBuckets_HidesNothingTheCallerCanRead(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "granted", "another-user"))

	// Granted through IAM alone — no ownership, no legacy bucket_permissions row.
	writer, ok := env.authManager.(interface {
		PutIAMInlinePolicy(ctx context.Context, targetType, targetID, name, document string) error
	})
	require.True(t, ok)
	require.NoError(t, writer.PutIAMInlinePolicy(ctx, auth.IAMTargetUser, "iam-only-user", "granted",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow",`+
			`"Action":["`+auth.ActionListAllMyBuckets+`","`+auth.ActionListBucket+`"],`+
			`"Resource":["*","arn:aws:s3:::granted"]}]}`))

	user := &auth.User{ID: "iam-only-user", TenantID: env.tenantID, Roles: []string{"user"}}

	assert.Contains(t, listedBuckets(t, env, user), "granted",
		"a bucket reachable through IAM must appear in the listing")
}

func TestListBuckets_NeedsThePermissionToListAtAll(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()

	// s3:ListAllMyBuckets was catalogued but enforced nowhere: grantable, and
	// granting nothing.
	nobody := &auth.User{ID: "no-policies", TenantID: env.tenantID, Roles: []string{"guest"}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(setUserInContext(req.Context(), nobody))
	w := httptest.NewRecorder()
	env.handler.ListBuckets(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code,
		"listing every bucket you hold is itself a permission")
}
