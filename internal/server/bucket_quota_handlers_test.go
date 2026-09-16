package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

func TestBucketQuotaAuthorization(t *testing.T) {
	s := getSharedServer()
	tenantOwnedBucket(t, "quota-owner", "quota-owned")
	cleanupTestData(t, "", "quota-global")
	require.NoError(t, s.bucketManager.CreateBucket(t.Context(), "", "quota-global", ""))
	cases := []struct {
		name, tenant, bucket, query, allow, deny string
		roles                                    []string
		read, write                              bool
	}{
		{"global-own", "", "quota-global", "", "*", "", []string{"admin"}, true, true},
		{"global-tenant", "", "quota-owned", "?tenantId=quota-owner", "*", "", []string{"admin"}, true, false},
		{"global-tenant-inferred", "", "quota-owned", "", "*", "", []string{"admin"}, true, false},
		{"tenant-admin", "quota-owner", "quota-owned", "", "*", "", []string{"tenant-admin"}, true, true},
		{"other-tenant", "other", "quota-owned", "?tenantId=quota-owner", "*", "", nil, false, false},
		{"scoped", "quota-owner", "quota-owned", "", "maxiofs:*BucketQuota", "", nil, true, true},
		{"read-only", "quota-owner", "quota-owned", "", auth.ActionGetBucketQuota, "", nil, true, false},
		{"explicit-deny", "quota-owner", "quota-owned", "", "*", auth.ActionPutBucketQuota, nil, true, false},
		{"unrelated-permission", "quota-owner", "quota-owned", "", auth.ActionPutBucketTagging, "", nil, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []string{"GET", "PUT", "DELETE"} {
				t.Run(method, func(t *testing.T) {
					owner := "quota-owner"
					if tc.bucket == "quota-global" {
						owner = ""
					}
					require.NoError(t, s.bucketManager.SetQuota(t.Context(), owner, tc.bucket, &metadata.BucketQuota{MaxSizeBytes: 99}))
					user := &auth.User{ID: "quota-reader", TenantID: tc.tenant, Roles: tc.roles}
					doc := fmt.Sprintf("{\"Statement\":[{\"Effect\":\"Allow\",\"Action\":%q,\"Resource\":%q}]}", tc.allow, consoleBucketARN(tc.bucket))
					if tc.allow == "*" && tc.tenant == "" {
						doc = "{\"Statement\":[{\"Effect\":\"Allow\",\"Action\":\"*\",\"Resource\":\"*\"}]}"
					}
					set := &auth.PolicySet{UserID: user.ID, TenantID: tc.tenant, Documents: []string{doc}}
					if tc.deny != "" {
						set.Documents = append(set.Documents, fmt.Sprintf("{\"Statement\":[{\"Effect\":\"Deny\",\"Action\":%q,\"Resource\":\"*\"}]}", tc.deny))
					}
					r := httptest.NewRequest(method, "/api/v1/buckets/"+tc.bucket+"/quota"+tc.query, strings.NewReader("{\"maxSizeBytes\":12345}"))
					r = mux.SetURLVars(r, map[string]string{"bucket": tc.bucket})
					r = r.WithContext(auth.WithPolicySet(context.WithValue(r.Context(), "user", user), set))
					w := httptest.NewRecorder()
					switch method {
					case "GET":
						s.handleGetBucketQuota(w, r)
					case "PUT":
						s.handlePutBucketQuota(w, r)
					case "DELETE":
						s.handleDeleteBucketQuota(w, r)
					}
					allowed := tc.write
					if method == "GET" {
						allowed = tc.read
					}
					if allowed {
						require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					} else {
						if tc.name == "other-tenant" {
							require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
						} else {
							require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
						}
					}
					info, err := s.bucketManager.GetBucketInfo(t.Context(), owner, tc.bucket)
					require.NoError(t, err)
					if allowed && method == "DELETE" {
						require.Nil(t, info.Quota)
					} else {
						require.NotNil(t, info.Quota)
						want := int64(99)
						if allowed && method == "PUT" {
							want = 12345
						}
						require.Equal(t, want, info.Quota.MaxSizeBytes)
					}
				})
			}
		})
	}
}

func TestBucketQuotaTenantAdminPolicy(t *testing.T) {
	s := getSharedServer()
	tenantOwnedBucket(t, "quota-admin-tenant", "quota-admin-bucket")
	user := &auth.User{ID: "quota-real-admin", Username: "quota-real-admin", TenantID: "quota-admin-tenant", Roles: []string{"tenant-admin"}, Status: auth.UserStatusActive}
	require.NoError(t, s.authManager.CreateUser(t.Context(), user))
	t.Cleanup(func() { _ = s.authManager.DeleteUser(context.Background(), user.ID) })
	r := httptest.NewRequest("PUT", "/api/v1/buckets/quota-admin-bucket/quota", strings.NewReader("{\"maxSizeBytes\":777}"))
	r = mux.SetURLVars(r, map[string]string{"bucket": "quota-admin-bucket"})
	r = r.WithContext(context.WithValue(r.Context(), "user", user))
	w := httptest.NewRecorder()
	s.handlePutBucketQuota(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
