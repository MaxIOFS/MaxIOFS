package s3compat

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
)

func TestIAMReadPolicyDoesNotAuthorizeWriteOrDelete(t *testing.T) {
	user := &auth.User{ID: "user-1", TenantID: "tenant-1"}
	policy := `{
		"Version":"2012-10-17",
		"Statement":[
			{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::photos"]},
			{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::photos/*"]}
		]
	}`
	set := &auth.PolicySet{UserID: user.ID, Documents: []string{policy}}
	ctx := auth.WithPolicySet(context.Background(), set)
	handler := &Handler{authManager: &mockAuthManager{}}

	req := httptest.NewRequest("PUT", "/photos/private/object.txt", nil).WithContext(ctx)
	if handler.validateBucketWritePermission(req, user, true, "tenant-1", "photos", "private/object.txt") {
		t.Fatal("read-only IAM policy authorized PutObject")
	}

	if handler.checkDeleteObjectPermission(ctx, user, true, "tenant-1", "photos", "tenant-1/photos", "private/object.txt", "") {
		t.Fatal("read-only IAM policy authorized DeleteObject")
	}

	if !handler.userCanPerformS3Action(ctx, user, auth.ActionGetObject, objectARN("photos", "private/object.txt")) {
		t.Fatal("read-only IAM policy should still authorize GetObject")
	}

	headReq := httptest.NewRequest("HEAD", "/photos/private/object.txt", nil).WithContext(ctx)
	if !handler.validateHeadBucketReadPermission(httptest.NewRecorder(), headReq, user, true, "tenant-1", "photos", "private/object.txt") {
		t.Fatal("read-only IAM policy should authorize HeadObject")
	}

	headVersionReq := httptest.NewRequest("HEAD", "/photos/private/object.txt?versionId=v1", nil).WithContext(ctx)
	if handler.validateHeadBucketReadPermission(httptest.NewRecorder(), headVersionReq, user, true, "tenant-1", "photos", "private/object.txt") {
		t.Fatal("GetObject permission authorized HeadObject with versionId")
	}
}

func TestIAMVersionActionsAreResourceScoped(t *testing.T) {
	user := &auth.User{ID: "user-1", TenantID: "tenant-1"}
	policy := `{
		"Version":"2012-10-17",
		"Statement":[
			{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::photos/*"]}
		]
	}`
	set := &auth.PolicySet{UserID: user.ID, Documents: []string{policy}}
	ctx := auth.WithPolicySet(context.Background(), set)
	handler := &Handler{authManager: &mockAuthManager{}}

	if handler.userCanPerformS3Action(ctx, user, auth.ActionGetObjectVersion, objectARN("photos", "private/object.txt")) {
		t.Fatal("GetObject permission authorized GetObjectVersion")
	}

	if handler.userCanPerformS3Action(ctx, user, auth.ActionDeleteObjectVersion, objectARN("photos", "private/object.txt")) {
		t.Fatal("GetObject permission authorized DeleteObjectVersion")
	}
}
