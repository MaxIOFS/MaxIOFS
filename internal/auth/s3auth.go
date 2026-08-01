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
//
// Unmapped requests fall back to the most restrictive action. Callers that use
// the action to RESTRICT access (STS session policies) must use
// S3ActionForRequest instead, which reports "" and lets them fail closed.
func (s *S3AuthHelper) GetS3Action(r *http.Request) string {
	if action := S3ActionForRequest(r); action != "" {
		return action
	}
	return ActionGetObject
}

// GetResourceARN generates an ARN for the requested resource
func (s *S3AuthHelper) GetResourceARN(r *http.Request) string {
	return ResourceARNForRequest(r)
}

// S3ActionForRequest maps an S3 request to its IAM action name, or "" when the
// request does not correspond to an action this mapping knows.
//
// The empty return is the point: a policy evaluator that restricts access must
// be able to tell "this is a GetObject" from "I do not know what this is", and
// deny the second. Requests arrive here already rewritten to path style
// (virtualHostedStyleMiddleware runs before the S3 router).
func S3ActionForRequest(r *http.Request) string {
	method := r.Method
	path := r.URL.Path
	query := r.URL.Query()

	// Service level: GET / lists the caller's buckets.
	if path == "" || path == "/" {
		if method == http.MethodGet {
			return ActionListAllMyBuckets
		}
		return ""
	}

	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	// Bucket level operations
	if len(pathParts) == 1 && pathParts[0] != "" {
		switch method {
		case http.MethodGet:
			switch {
			case query.Has("versioning"):
				return ActionGetBucketVersioning
			case query.Has("policy"):
				return ActionGetBucketPolicy
			case query.Has("lifecycle"):
				return ActionGetBucketLifecycle
			case query.Has("cors"):
				return ActionGetBucketCORS
			case query.Has("acl"):
				return ActionGetBucketAcl
			case query.Has("tagging"):
				return ActionGetBucketTagging
			case query.Has("location"):
				return ActionGetBucketLocation
			case query.Has("versions"):
				return ActionListBucketVersions
			case query.Has("uploads"):
				return ActionListBucketMultipartUploads
			}
			return ActionListBucket
		case http.MethodHead:
			// HEAD bucket is an existence check, authorized as ListBucket
			// (same as AWS).
			return ActionListBucket
		case http.MethodPut:
			switch {
			case query.Has("versioning"):
				return ActionPutBucketVersioning
			case query.Has("policy"):
				return ActionPutBucketPolicy
			case query.Has("lifecycle"):
				return ActionPutBucketLifecycle
			case query.Has("cors"):
				return ActionPutBucketCORS
			case query.Has("acl"):
				return ActionPutBucketAcl
			case query.Has("tagging"):
				return ActionPutBucketTagging
			}
			return ActionCreateBucket
		case http.MethodDelete:
			switch {
			case query.Has("policy"):
				return ActionDeleteBucketPolicy
			case query.Has("lifecycle"):
				return ActionDeleteBucketLifecycle
			case query.Has("cors"):
				return ActionDeleteBucketCORS
			case query.Has("tagging"):
				return ActionDeleteBucketTagging
			}
			return ActionDeleteBucket
		case http.MethodPost:
			// POST /bucket?delete is the multi-object delete; a bare POST to a
			// bucket is a browser form upload.
			if query.Has("delete") {
				return ActionDeleteObject
			}
			return ActionPutObject
		}
		return ""
	}

	// Object level operations
	if len(pathParts) >= 2 {
		switch method {
		case http.MethodGet:
			switch {
			case query.Has("acl"):
				return ActionGetObjectAcl
			case query.Has("tagging"):
				return ActionGetObjectTagging
			case query.Has("retention"):
				return ActionGetObjectRetention
			case query.Has("legal-hold"):
				return ActionGetObjectLegalHold
			case query.Has("uploadId"):
				return ActionListMultipartUploadParts
			}
			return ActionGetObject
		case http.MethodHead:
			return ActionGetObject
		case http.MethodPut:
			switch {
			case query.Has("acl"):
				return ActionPutObjectAcl
			case query.Has("tagging"):
				return ActionPutObjectTagging
			case query.Has("retention"):
				return ActionPutObjectRetention
			case query.Has("legal-hold"):
				return ActionPutObjectLegalHold
			}
			// Covers PutObject, UploadPart and UploadPartCopy — all writes of
			// object content, all authorized as PutObject.
			return ActionPutObject
		case http.MethodDelete:
			switch {
			case query.Has("tagging"):
				return ActionDeleteObjectTagging
			case query.Has("uploadId"):
				return ActionAbortMultipartUpload
			}
			return ActionDeleteObject
		case http.MethodPost:
			if query.Has("restore") {
				return ActionRestoreObject
			}
			// CreateMultipartUpload (?uploads) and CompleteMultipartUpload
			// (?uploadId) both write object content.
			return ActionPutObject
		}
	}

	return ""
}

// ResourceARNForRequest generates the S3 ARN of the resource a request targets.
func ResourceARNForRequest(r *http.Request) string {
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

