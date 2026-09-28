package object

import "strings"

// A multipart upload keeps, in one map until it completes, the object's storage
// fields, the client's user metadata and internal state. User metadata keeps its
// header prefix and internal state uses keys no header name can have (':'), so
// none of them can stand in for another.
const (
	uploadUserMetaPrefix = "x-amz-meta-"
	uploadCannedACL      = "acl:canned"
)

var uploadStorageKeys = map[string]bool{
	"content-type": true, "content-disposition": true, "content-encoding": true,
	"cache-control": true, "content-language": true, "storage-class": true,
}

// uploadMetadata builds the map an upload keeps.
func uploadMetadata(storage, user map[string]string, cannedACL string) map[string]string {
	m := make(map[string]string, len(storage)+len(user)+1)
	for k, v := range storage {
		m[k] = v
	}
	for k, v := range user {
		m[uploadUserMetaPrefix+k] = v
	}
	if cannedACL != "" {
		m[uploadCannedACL] = cannedACL
	}
	return m
}

// uploadUserMetadata is the user metadata an upload keeps. An upload created
// before user metadata kept its prefix holds it under the bare name; its
// canned ACL was then kept as "x-amz-acl", which is not user metadata.
func uploadUserMetadata(m map[string]string) map[string]string {
	out := make(map[string]string)
	for k, v := range m {
		switch {
		case strings.HasPrefix(k, uploadUserMetaPrefix):
			out[strings.TrimPrefix(k, uploadUserMetaPrefix)] = v
		case uploadStorageKeys[k], strings.Contains(k, ":"), k == "x-amz-acl":
		default:
			out[k] = v
		}
	}
	return out
}

// uploadStorageMetadata is the storage fields an upload keeps.
func uploadStorageMetadata(m map[string]string) map[string]string {
	out := make(map[string]string, len(uploadStorageKeys))
	for k := range uploadStorageKeys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

// UploadCannedACL is the canned ACL requested when the upload was created.
func UploadCannedACL(m map[string]string) string {
	return m[uploadCannedACL]
}
