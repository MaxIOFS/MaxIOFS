package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
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
