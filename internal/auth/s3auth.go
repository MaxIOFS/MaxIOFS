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

	pathParts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)

	// Bucket level operations
	if len(pathParts) == 1 || pathParts[1] == "" {
		for _, op := range bucketSubresourceActions {
			if !query.Has(op.query) || query.Get(op.query) != op.value {
				continue
			}
			var action string
			switch method {
			case http.MethodGet:
				action = op.get
			case http.MethodPut:
				action = op.put
			case http.MethodDelete:
				action = op.delete
			}
			if action != "" {
				return action
			}
		}
		switch method {
		case http.MethodGet:
			return ActionListBucket
		case http.MethodHead:
			// HEAD bucket is an existence check, authorized as ListBucket
			// (same as AWS).
			return ActionListBucket
		case http.MethodPut:
			return ActionCreateBucket
		case http.MethodDelete:
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
		if method == http.MethodGet && query.Has("uploads") {
			return ActionListBucketMultipartUploads
		}
		if query.Has("uploadId") {
			switch method {
			case http.MethodGet:
				return ActionListMultipartUploadParts
			case http.MethodDelete:
				return ActionAbortMultipartUpload
			case http.MethodPost:
				return ActionPutObject
			case http.MethodPut:
				if query.Has("partNumber") {
					return ActionPutObject
				}
			}
		}
		switch method {
		case http.MethodGet:
			switch {
			case query.Has("retention"):
				return ActionGetObjectRetention
			case query.Has("legal-hold"):
				return ActionGetObjectLegalHold
			case query.Has("acl"):
				return ActionGetObjectAcl
			case query.Has("tagging"):
				return ActionGetObjectTagging
			case query.Has("versionId"):
				return ActionGetObjectVersion
			}
			return ActionGetObject
		case http.MethodHead:
			if query.Has("versionId") {
				return ActionGetObjectVersion
			}
			return ActionGetObject
		case http.MethodPut:
			switch {
			case query.Has("retention"):
				return ActionPutObjectRetention
			case query.Has("legal-hold"):
				return ActionPutObjectLegalHold
			case query.Has("acl"):
				return ActionPutObjectAcl
			case query.Has("tagging"):
				return ActionPutObjectTagging
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
			case query.Has("versionId"):
				return ActionDeleteObjectVersion
			}
			return ActionDeleteObject
		case http.MethodPost:
			if query.Has("uploads") {
				return ActionPutObject
			}
			if query.Has("restore") {
				return ActionRestoreObject
			}
			if query.Has("select") {
				return ActionGetObject
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
	if len(pathParts) == 1 || (len(pathParts) == 2 && pathParts[1] == "") {
		// Bucket resource
		return "arn:aws:s3:::" + pathParts[0]
	}

	// Object resource
	bucket := pathParts[0]
	object := strings.Join(pathParts[1:], "/")
	return "arn:aws:s3:::" + bucket + "/" + object
}

func isS3BatchDelete(r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/")
	return r.Method == http.MethodPost && r.URL.Query().Has("delete") &&
		!strings.Contains(strings.TrimSuffix(path, "/"), "/")
}

// Order matches the bucket routes, including requests with multiple subresources.
var bucketSubresourceActions = []struct{ query, value, get, put, delete string }{
	{"location", "", ActionGetBucketLocation, "", ""},
	{"versioning", "", ActionGetBucketVersioning, ActionPutBucketVersioning, ""},
	{"policy", "", ActionGetBucketPolicy, ActionPutBucketPolicy, ActionDeleteBucketPolicy},
	{"object-lock", "", ActionGetBucketObjectLockConfiguration, ActionPutBucketObjectLockConfiguration, ""},
	{"lifecycle", "", ActionGetBucketLifecycle, ActionPutBucketLifecycle, ActionDeleteBucketLifecycle},
	{"cors", "", ActionGetBucketCORS, ActionPutBucketCORS, ActionDeleteBucketCORS},
	{"tagging", "", ActionGetBucketTagging, ActionPutBucketTagging, ActionDeleteBucketTagging},
	{"acl", "", ActionGetBucketAcl, ActionPutBucketAcl, ""},
	{"versions", "", ActionListBucketVersions, "", ""},
	{"list-type", "2", ActionListBucket, "", ""},
	{"notification", "", ActionGetBucketNotification, ActionPutBucketNotification, ActionPutBucketNotification},
	{"website", "", ActionGetBucketWebsite, ActionPutBucketWebsite, ActionDeleteBucketWebsite},
	{"accelerate", "", ActionGetAccelerate, ActionPutAccelerate, ""},
	{"requestPayment", "", ActionGetRequestPayment, ActionPutRequestPayment, ""},
	{"encryption", "", ActionGetBucketEncryption, ActionPutBucketEncryption, ActionPutBucketEncryption},
	{"replication", "", ActionGetBucketReplication, ActionPutBucketReplication, ActionPutBucketReplication},
	{"logging", "", ActionGetBucketLogging, ActionPutBucketLogging, ""},
	{"publicAccessBlock", "", ActionGetBucketPublicAccess, ActionPutBucketPublicAccess, ActionPutBucketPublicAccess},
	{"ownershipControls", "", ActionGetBucketOwnership, ActionPutBucketOwnership, ActionPutBucketOwnership},
	{"inventory", "", ActionGetBucketInventory, ActionPutBucketInventory, ActionPutBucketInventory},
	{"uploads", "", ActionListBucketMultipartUploads, "", ""},
}
