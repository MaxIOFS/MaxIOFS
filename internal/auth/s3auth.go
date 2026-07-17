package auth

import (
	"net/http"
	"strings"
)

// S3AuthHelper provides S3-specific authentication helpers
type S3AuthHelper struct {
	manager *authManager
}

// NewS3AuthHelper creates a new S3 auth helper
func NewS3AuthHelper(manager Manager) *S3AuthHelper {
	if am, ok := manager.(*authManager); ok {
		return &S3AuthHelper{manager: am}
	}
	return nil
}


// GetS3Action extracts S3 action from HTTP request.
// Not wired into request authorization today (that is done by roles, the
// capability system, bucket policies and ACLs) — kept as the action-mapping
// primitive for the future IAM policy engine.
func (s *S3AuthHelper) GetS3Action(r *http.Request) string {
	method := r.Method
	path := r.URL.Path

	// Root level operations
	if path == "/" || path == "" {
		switch method {
		case "GET":
			return ActionListAllMyBuckets
		}
	}

	// Bucket level operations
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	if len(pathParts) == 1 && pathParts[0] != "" {
		// Bucket operations
		switch method {
		case "GET":
			if r.URL.Query().Has("versioning") {
				return ActionGetBucketVersioning
			}
			if r.URL.Query().Has("policy") {
				return ActionGetBucketPolicy
			}
			if r.URL.Query().Has("lifecycle") {
				return ActionGetBucketLifecycle
			}
			if r.URL.Query().Has("cors") {
				return ActionGetBucketCORS
			}
			return ActionListBucket
		case "PUT":
			if r.URL.Query().Has("versioning") {
				return ActionPutBucketVersioning
			}
			if r.URL.Query().Has("policy") {
				return ActionPutBucketPolicy
			}
			if r.URL.Query().Has("lifecycle") {
				return ActionPutBucketLifecycle
			}
			if r.URL.Query().Has("cors") {
				return ActionPutBucketCORS
			}
			return ActionCreateBucket
		case "DELETE":
			if r.URL.Query().Has("policy") {
				return ActionDeleteBucketPolicy
			}
			if r.URL.Query().Has("lifecycle") {
				return ActionDeleteBucketLifecycle
			}
			if r.URL.Query().Has("cors") {
				return ActionDeleteBucketCORS
			}
			return ActionDeleteBucket
		}
	}

	// Object level operations
	if len(pathParts) >= 2 {
		switch method {
		case "GET":
			if r.URL.Query().Has("acl") {
				return ActionGetObjectAcl
			}
			if r.URL.Query().Has("tagging") {
				return ActionGetObjectTagging
			}
			if r.URL.Query().Has("retention") {
				return ActionGetObjectRetention
			}
			if r.URL.Query().Has("legal-hold") {
				return ActionGetObjectLegalHold
			}
			if r.URL.Query().Has("uploadId") {
				return ActionListMultipartUploadParts
			}
			return ActionGetObject
		case "PUT":
			if r.URL.Query().Has("acl") {
				return ActionPutObjectAcl
			}
			if r.URL.Query().Has("tagging") {
				return ActionPutObjectTagging
			}
			if r.URL.Query().Has("retention") {
				return ActionPutObjectRetention
			}
			if r.URL.Query().Has("legal-hold") {
				return ActionPutObjectLegalHold
			}
			return ActionPutObject
		case "DELETE":
			if r.URL.Query().Has("tagging") {
				return ActionDeleteObjectTagging
			}
			if r.URL.Query().Has("uploadId") {
				return ActionAbortMultipartUpload
			}
			return ActionDeleteObject
		}
	}

	// Default to most restrictive action
	return ActionGetObject
}

// GetResourceARN generates an ARN for the requested resource
func (s *S3AuthHelper) GetResourceARN(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		return "arn:aws:s3:::*"
	}

	pathParts := strings.Split(path, "/")
	if len(pathParts) == 1 {
		// Bucket resource
		return "arn:aws:s3:::" + pathParts[0]
	}

	// Object resource
	bucket := pathParts[0]
	object := strings.Join(pathParts[1:], "/")
	return "arn:aws:s3:::" + bucket + "/" + object
}

