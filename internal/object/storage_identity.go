package object

import (
	"strconv"
	"strings"

	"github.com/maxiofs/maxiofs/internal/storage"
)

// storageIdentityVerdict says whether bytes without a metadata sidecar may be
// served as the recorded object.
type storageIdentityVerdict struct {
	Servable bool
	Reason   string
}

// confirmStorageIdentity decides whether sidecar-less bytes can be PROVEN to be
// the recorded object. The distinction matters: a rule that refuses only on
// evidence of a mismatch serves anything it cannot check, which is how raw
// ciphertext came to be delivered under a plaintext Content-Length.
//
// Without a sidecar the wrapped DEK is gone, so an encrypted object is
// unrecoverable by definition. The one servable case is a legacy plaintext
// object whose size and digest still match what was recorded.
func confirmStorageIdentity(recorded *Object, derived map[string]string) storageIdentityVerdict {
	if derived[storage.MetadataGeneratedKey] != "true" {
		return storageIdentityVerdict{Servable: true} // a real sidecar; not this rule's business
	}

	if recorded == nil {
		return storageIdentityVerdict{Reason: "nothing was recorded for it, so the bytes cannot be identified"}
	}

	onDiskETag := derived["etag"]
	if onDiskETag == "" {
		return storageIdentityVerdict{Reason: "the bytes on disk have no digest to compare"}
	}

	recordedETag := strings.Trim(recorded.ETag, `"`)
	if recordedETag == "" {
		return storageIdentityVerdict{Reason: "the recorded object has no digest to compare"}
	}
	if strings.Contains(recordedETag, "-") {
		// A multipart ETag is "<md5>-<n>" and never equals the digest of the
		// assembled file, so no comparison can establish identity here.
		return storageIdentityVerdict{Reason: "the recorded object is multipart and its digest cannot identify the assembled bytes"}
	}

	onDiskSize, err := strconv.ParseInt(derived["size"], 10, 64)
	if err != nil {
		return storageIdentityVerdict{Reason: "the bytes on disk have no readable size"}
	}
	if recorded.Size != onDiskSize {
		return storageIdentityVerdict{Reason: "the bytes on disk are a different size from the recorded object"}
	}
	if !strings.EqualFold(recordedETag, onDiskETag) {
		return storageIdentityVerdict{Reason: "the bytes on disk do not match the recorded digest"}
	}

	return storageIdentityVerdict{Servable: true}
}
