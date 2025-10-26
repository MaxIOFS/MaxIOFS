package s3compat

import (
	"encoding/xml"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

// ListVersionsResult is the XML response for ListObjectVersions
type ListVersionsResult struct {
	XMLName             xml.Name        `xml:"ListVersionsResult"`
	Name                string          `xml:"Name"`
	Prefix              string          `xml:"Prefix,omitempty"`
	KeyMarker           string          `xml:"KeyMarker,omitempty"`
	VersionIdMarker     string          `xml:"VersionIdMarker,omitempty"`
	NextKeyMarker       string          `xml:"NextKeyMarker,omitempty"`
	NextVersionIdMarker string          `xml:"NextVersionIdMarker,omitempty"`
	MaxKeys             int             `xml:"MaxKeys"`
	Delimiter           string          `xml:"Delimiter,omitempty"`
	IsTruncated         bool            `xml:"IsTruncated"`
	Versions            []VersionEntry  `xml:"Version,omitempty"`
	DeleteMarkers       []DeleteMarker  `xml:"DeleteMarker,omitempty"`
	CommonPrefixes      []CommonPrefix  `xml:"CommonPrefixes,omitempty"`
}

// VersionEntry represents a single object version
type VersionEntry struct {
	Key          string    `xml:"Key"`
	VersionId    string    `xml:"VersionId"`
	IsLatest     bool      `xml:"IsLatest"`
	LastModified time.Time `xml:"LastModified"`
	ETag         string    `xml:"ETag"`
	Size         int64     `xml:"Size"`
	Owner        Owner     `xml:"Owner"`
	StorageClass string    `xml:"StorageClass"`
}

// DeleteMarker represents a delete marker in versioning
type DeleteMarker struct {
	Key          string    `xml:"Key"`
	VersionId    string    `xml:"VersionId"`
	IsLatest     bool      `xml:"IsLatest"`
	LastModified time.Time `xml:"LastModified"`
	Owner        Owner     `xml:"Owner"`
}

// ListBucketVersions lists all versions of objects in a bucket
func (h *Handler) ListBucketVersions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: ListBucketVersions")

	bucketPath := h.getBucketPath(r, bucketName)

	// Parse query parameters
	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	keyMarker := r.URL.Query().Get("key-marker")
	versionIDMarker := r.URL.Query().Get("version-id-marker")
	maxKeys := 1000

	if maxKeysStr := r.URL.Query().Get("max-keys"); maxKeysStr != "" {
		if parsed, err := strconv.Atoi(maxKeysStr); err == nil && parsed > 0 {
			if parsed > 1000 {
				maxKeys = 1000
			} else {
				maxKeys = parsed
			}
		}
	}

	// List objects to get all keys
	listResult, err := h.objectManager.ListObjects(r.Context(), bucketPath, prefix, delimiter, "", maxKeys*10) // Get more keys since each key may have multiple versions
	if err != nil {
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	// Collect all versions and delete markers
	var allVersions []VersionEntry
	var allDeleteMarkers []DeleteMarker
	processedKeys := make(map[string]bool)

	for _, obj := range listResult.Objects {
		// Skip if before key marker
		if keyMarker != "" && obj.Key < keyMarker {
			continue
		}

		// Avoid processing same key multiple times
		if processedKeys[obj.Key] {
			continue
		}
		processedKeys[obj.Key] = true

		// Get all versions for this key
		objectVersions, err := h.objectManager.GetObjectVersions(r.Context(), bucketPath, obj.Key)
		if err != nil {
			logrus.WithError(err).WithField("key", obj.Key).Debug("Failed to get object versions, using current version only")
			// Fall back to single version
			versionID := obj.VersionID
			if versionID == "" {
				versionID = "null"
			}

			allVersions = append(allVersions, VersionEntry{
				Key:          obj.Key,
				VersionId:    versionID,
				IsLatest:     true,
				LastModified: obj.LastModified,
				ETag:         obj.ETag,
				Size:         obj.Size,
				Owner: Owner{
					ID:          "maxiofs",
					DisplayName: "MaxIOFS",
				},
				StorageClass: "STANDARD",
			})
			continue
		}

		// Add all versions to the list
		for _, ver := range objectVersions {
			// Skip if we're after keyMarker but this specific version is before versionIDMarker
			if keyMarker == obj.Key && versionIDMarker != "" && ver.VersionID < versionIDMarker {
				continue
			}

			versionID := ver.VersionID
			if versionID == "" {
				versionID = "null"
			}

			// Check if this is a delete marker (Size==0 and ETag=="")
			isDeleteMarker := ver.Size == 0 && ver.ETag == ""

			if isDeleteMarker {
				// Add as DeleteMarker
				allDeleteMarkers = append(allDeleteMarkers, DeleteMarker{
					Key:          ver.Key,
					VersionId:    versionID,
					IsLatest:     ver.IsLatest,
					LastModified: ver.LastModified,
					Owner: Owner{
						ID:          "maxiofs",
						DisplayName: "MaxIOFS",
					},
				})
			} else {
				// Add as regular version
				allVersions = append(allVersions, VersionEntry{
					Key:          ver.Key,
					VersionId:    versionID,
					IsLatest:     ver.IsLatest,
					LastModified: ver.LastModified,
					ETag:         ver.ETag,
					Size:         ver.Size,
					Owner: Owner{
						ID:          "maxiofs",
						DisplayName: "MaxIOFS",
					},
					StorageClass: "STANDARD",
				})
			}
		}
	}

	// Truncate to maxKeys (combined versions + delete markers)
	totalItems := len(allVersions) + len(allDeleteMarkers)
	isTruncated := totalItems > maxKeys
	if isTruncated {
		// Simple truncation - in real S3 we'd need to interleave and sort properly
		remaining := maxKeys
		if len(allVersions) > remaining {
			allVersions = allVersions[:remaining]
			allDeleteMarkers = nil
		} else {
			remaining -= len(allVersions)
			if len(allDeleteMarkers) > remaining {
				allDeleteMarkers = allDeleteMarkers[:remaining]
			}
		}
	}

	// Determine next markers
	nextKeyMarker := ""
	nextVersionIDMarker := ""
	if isTruncated {
		if len(allVersions) > 0 {
			lastVersion := allVersions[len(allVersions)-1]
			nextKeyMarker = lastVersion.Key
			nextVersionIDMarker = lastVersion.VersionId
		} else if len(allDeleteMarkers) > 0 {
			lastMarker := allDeleteMarkers[len(allDeleteMarkers)-1]
			nextKeyMarker = lastMarker.Key
			nextVersionIDMarker = lastMarker.VersionId
		}
	}

	result := ListVersionsResult{
		Name:                bucketName,
		Prefix:              prefix,
		KeyMarker:           keyMarker,
		VersionIdMarker:     versionIDMarker,
		NextKeyMarker:       nextKeyMarker,
		NextVersionIdMarker: nextVersionIDMarker,
		MaxKeys:             maxKeys,
		Delimiter:           delimiter,
		IsTruncated:         isTruncated,
		Versions:            allVersions,
		DeleteMarkers:       allDeleteMarkers,
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}
