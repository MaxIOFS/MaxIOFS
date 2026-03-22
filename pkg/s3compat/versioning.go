package s3compat

import (
	"encoding/xml"
	"net/http"
	"sort"
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

	// versionItem is a unified type for sorting versions and delete markers together
	type versionItem struct {
		key          string
		versionID    string
		lastModified time.Time
		isDeleteMarker bool
		// fields for VersionEntry
		isLatest     bool
		etag         string
		size         int64
		storageClass string
	}

	var unified []versionItem

	for _, ver := range allObjectVersions {
		// Skip if before key marker
		if keyMarker != "" && ver.Key < keyMarker {
			continue
		}
		// Skip if at keyMarker but before versionIDMarker
		if keyMarker == ver.Key && versionIDMarker != "" && ver.VersionID < versionIDMarker {
			continue
		}

		versionID := ver.VersionID
		if versionID == "" {
			versionID = "null"
		}

		isDeleteMarker := ver.Size == 0 && ver.ETag == ""

		unified = append(unified, versionItem{
			key:            ver.Key,
			versionID:      versionID,
			lastModified:   ver.LastModified,
			isDeleteMarker: isDeleteMarker,
			isLatest:       ver.IsLatest,
			etag:           ver.ETag,
			size:           ver.Size,
			storageClass:   ver.StorageClass,
		})
	}

	// Sort: by Key ASC, then LastModified DESC (newest first within same key) — matches AWS S3 ordering
	sort.Slice(unified, func(i, j int) bool {
		if unified[i].key != unified[j].key {
			return unified[i].key < unified[j].key
		}
		return unified[i].lastModified.After(unified[j].lastModified)
	})

	// Truncate the unified list to maxKeys, then split into versions and delete markers
	isTruncated := len(unified) > maxKeys
	nextKeyMarker := ""
	nextVersionIDMarker := ""
	if isTruncated {
		nextKeyMarker = encodeStr(unified[maxKeys].key)
		nextVersionIDMarker = unified[maxKeys].versionID
		unified = unified[:maxKeys]
	}

	var allVersions []VersionEntry
	var allDeleteMarkers []DeleteMarker

	for _, item := range unified {
		if item.isDeleteMarker {
			allDeleteMarkers = append(allDeleteMarkers, DeleteMarker{
				Key:          encodeStr(item.key),
				VersionId:    item.versionID,
				IsLatest:     item.isLatest,
				LastModified: item.lastModified,
				Owner: Owner{
					ID:          "maxiofs",
					DisplayName: "MaxIOFS",
				},
			})
		} else {
			allVersions = append(allVersions, VersionEntry{
				Key:          encodeStr(item.key),
				VersionId:    item.versionID,
				IsLatest:     item.isLatest,
				LastModified: item.lastModified,
				ETag:         item.etag,
				Size:         item.size,
				Owner: Owner{
					ID:          "maxiofs",
					DisplayName: "MaxIOFS",
				},
				StorageClass: storageClassOrStandard(item.storageClass),
			})
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
