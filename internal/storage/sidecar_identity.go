package storage

import "strconv"

// SidecarIdentity reports the size and ETag an object was stored with, as a
// client sees them: the plaintext pair for an encrypted object, and the
// multipart ETag when the object was assembled from parts.
//
// The multipart ETag is the MD5 of the part digests — it cannot be recomputed
// from the assembled bytes, so it is only correct for objects whose sidecar
// carries it.
func SidecarIdentity(sidecar map[string]string) (int64, string) {
	var size int64
	var etag string
	if sidecar["encrypted"] == "true" {
		size, _ = strconv.ParseInt(sidecar["original-size"], 10, 64)
		etag = sidecar["original-etag"]
	} else {
		size, _ = strconv.ParseInt(sidecar["size"], 10, 64)
		etag = sidecar["etag"]
	}
	if multipartETag := sidecar["multipart-etag"]; multipartETag != "" {
		etag = multipartETag
	}
	return size, etag
}
