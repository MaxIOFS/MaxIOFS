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
	XMLName             xml.Name       `xml:"ListVersionsResult"`
	Name                string         `xml:"Name"`
	Prefix              string         `xml:"Prefix,omitempty"`
	KeyMarker           string         `xml:"KeyMarker,omitempty"`
	VersionIdMarker     string         `xml:"VersionIdMarker,omitempty"`
	NextKeyMarker       string         `xml:"NextKeyMarker,omitempty"`
	NextVersionIdMarker string         `xml:"NextVersionIdMarker,omitempty"`
	MaxKeys             int            `xml:"MaxKeys"`
	Delimiter           string         `xml:"Delimiter,omitempty"`
	EncodingType        string         `xml:"EncodingType,omitempty"`
	IsTruncated         bool           `xml:"IsTruncated"`
	Versions            []VersionEntry `xml:"Version,omitempty"`
	DeleteMarkers       []DeleteMarker `xml:"DeleteMarker,omitempty"`
	CommonPrefixes      []CommonPrefix `xml:"CommonPrefixes,omitempty"`
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

	// Parse encoding-type — only "url" is valid per the S3 spec.
	encodingType := r.URL.Query().Get("encoding-type")
	if encodingType != "" && encodingType != "url" {
		h.writeError(w, "InvalidArgument", "Invalid Encoding Method specified in Request", bucketName, r)
		return
	}
	encodeStr := func(s string) string {
		if encodingType == "url" {
			return s3URLEncode(s)
		}
		return s
	}

	// Get all versions directly from metadata (don't rely on ListObjects which excludes deleted objects)
	allObjectVersions, err := h.metadataStore.ListAllObjectVersions(r.Context(), bucketPath, prefix, maxKeys*10)
	if err != nil {
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	// Collect all versions and delete markers
	var allVersions []VersionEntry
	var allDeleteMarkers []DeleteMarker

	for _, ver := range allObjectVersions {
		// Skip if before key marker
		if keyMarker != "" && ver.Key < keyMarker {
			continue
		}

		// Skip if we're after keyMarker but this specific version is before versionIDMarker
		if keyMarker == ver.Key && versionIDMarker != "" && ver.VersionID < versionIDMarker {
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
				Key:          encodeStr(ver.Key),
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
				Key:          encodeStr(ver.Key),
				VersionId:    versionID,
				IsLatest:     ver.IsLatest,
				LastModified: ver.LastModified,
				ETag:         ver.ETag,
				Size:         ver.Size,
				Owner: Owner{
					ID:          "maxiofs",
					DisplayName: "MaxIOFS",
				},
				StorageClass: storageClassOrStandard(ver.StorageClass),
			})
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
		Prefix:              encodeStr(prefix),
		KeyMarker:           encodeStr(keyMarker),
		VersionIdMarker:     versionIDMarker,
		NextKeyMarker:       encodeStr(nextKeyMarker),
		NextVersionIdMarker: nextVersionIDMarker,
		MaxKeys:             maxKeys,
		Delimiter:           encodeStr(delimiter),
		EncodingType:        encodingType,
		IsTruncated:         isTruncated,
		Versions:            allVersions,
		DeleteMarkers:       allDeleteMarkers,
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}
