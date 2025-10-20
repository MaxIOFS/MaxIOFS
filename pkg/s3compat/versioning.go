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
// Since MaxIOFS doesn't support versioning yet, this returns each object as a single version
func (h *Handler) ListBucketVersions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: ListBucketVersions")

	bucketPath := h.getBucketPath(r, bucketName)

	// Parse query parameters
	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	keyMarker := r.URL.Query().Get("key-marker")
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

	// List objects (since we don't have real versioning, each object is one version)
	listResult, err := h.objectManager.ListObjects(r.Context(), bucketPath, prefix, delimiter, "", maxKeys)
	if err != nil {
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	// Convert objects to version entries
	var versions []VersionEntry
	for _, obj := range listResult.Objects {
		// Skip if before key marker
		if keyMarker != "" && obj.Key <= keyMarker {
			continue
		}

		versions = append(versions, VersionEntry{
			Key:          obj.Key,
			VersionId:    "null", // MaxIOFS doesn't support versioning yet
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

		if len(versions) >= maxKeys {
			break
		}
	}

	// Determine if truncated
	isTruncated := listResult.IsTruncated
	nextKeyMarker := ""
	if isTruncated && len(versions) > 0 {
		nextKeyMarker = versions[len(versions)-1].Key
	}

	result := ListVersionsResult{
		Name:          bucketName,
		Prefix:        prefix,
		KeyMarker:     keyMarker,
		NextKeyMarker: nextKeyMarker,
		MaxKeys:       maxKeys,
		Delimiter:     delimiter,
		IsTruncated:   isTruncated,
		Versions:      versions,
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}
