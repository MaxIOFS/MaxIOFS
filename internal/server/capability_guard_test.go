package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metadata"
)

func TestGlobalAdminConsoleS3AccessIsReadOnlyAcrossTenants(t *testing.T) {
	s := &Server{}
	user := &auth.User{ID: "global-admin", Roles: []string{auth.RoleAdmin}}
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", user))

	if !s.userCanPerformConsoleS3Action(req, user, "", auth.ActionPutObject, "arn:aws:s3:::global/cat.jpg") {
		t.Fatal("global admin should have full access to global buckets from the console")
	}
	if !s.userCanPerformConsoleS3Action(req, user, "", auth.ActionDeleteBucket, "arn:aws:s3:::global") {
		t.Fatal("global admin should be allowed to delete global buckets from the console")
	}
	if !s.userCanPerformConsoleS3Action(req, user, "tenant-a", auth.ActionGetObject, "arn:aws:s3:::photos/cat.jpg") {
		t.Fatal("global admin should be allowed to read tenant objects from the console")
	}
	if !s.userCanPerformConsoleS3Action(req, user, "tenant-a", auth.ActionGetBucketReplication, "arn:aws:s3:::photos") {
		t.Fatal("global admin should be allowed to read tenant bucket configuration from the console")
	}
	if s.userCanPerformConsoleS3Action(req, user, "tenant-a", auth.ActionPutObject, "arn:aws:s3:::photos/cat.jpg") {
		t.Fatal("global admin must not upload tenant objects from the console")
	}
	if s.userCanPerformConsoleS3Action(req, user, "tenant-a", auth.ActionDeleteBucket, "arn:aws:s3:::photos") {
		t.Fatal("global admin must not delete tenant buckets from the console")
	}
	if s.userCanPerformConsoleS3Action(req, user, "tenant-a", auth.ActionPutBucketPolicy, "arn:aws:s3:::photos") {
		t.Fatal("global admin must not modify tenant bucket policy from the console")
	}
}

func TestResolveConsoleBucketTenantIDPrefersExplicitTenantForGlobalAdmin(t *testing.T) {
	s := &Server{metadataStore: tenantResolvingStore{
		byName: &metadata.BucketMetadata{Name: "shared-name", TenantID: ""},
	}}
	globalAdmin := &auth.User{ID: "global-admin", Roles: []string{auth.RoleAdmin}}
	req := httptest.NewRequest("DELETE", "/api/v1/buckets/shared-name?tenantId=tenant-a", nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", globalAdmin))

	if got := s.resolveConsoleBucketTenantID(req, "shared-name", globalAdmin); got != "tenant-a" {
		t.Fatalf("expected explicit tenant-a to win over same-name global bucket, got %q", got)
	}
}

type tenantResolvingStore struct {
	metadata.Store
	byName *metadata.BucketMetadata
	global *metadata.BucketMetadata
}

func (s tenantResolvingStore) GetBucket(ctx context.Context, tenantID, name string) (*metadata.BucketMetadata, error) {
	if tenantID == "" && s.global != nil && s.global.Name == name {
		return s.global, nil
	}
	return nil, metadata.ErrBucketNotFound
}

func (s tenantResolvingStore) GetBucketByName(ctx context.Context, name string) (*metadata.BucketMetadata, error) {
	if s.byName != nil && s.byName.Name == name {
		return s.byName, nil
	}
	return nil, metadata.ErrBucketNotFound
}
