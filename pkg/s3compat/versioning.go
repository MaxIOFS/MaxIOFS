package s3compat

import (
	"encoding/xml"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metadata"
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

func (h *Handler) ListBucketVersions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: ListBucketVersions")

	if !h.requireBucketS3Action(w, r, bucketName, auth.ActionListBucketVersions) || !h.requireBucketExists(w, r, bucketName) {
		return
	}

	bucketPath := h.getBucketPath(r, bucketName)

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

	page, err := h.metadataStore.ListObjectVersionsPage(r.Context(), bucketPath, prefix, delimiter, keyMarker, versionIDMarker, maxKeys)
	if err != nil {
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	var allVersions []VersionEntry
	var allDeleteMarkers []DeleteMarker
	var commonPrefixes []CommonPrefix

	owner := Owner{ID: "maxiofs", DisplayName: "MaxIOFS"}
	for _, ver := range page.Versions {
		versionID := ver.VersionID
		if versionID == "" {
			versionID = metadata.NullVersionID
		}
		if ver.Size == 0 && ver.ETag == "" {
			allDeleteMarkers = append(allDeleteMarkers, DeleteMarker{
				Key:          encodeStr(ver.Key),
				VersionId:    versionID,
				IsLatest:     ver.IsLatest,
				LastModified: ver.LastModified,
				Owner:        owner,
			})
			continue
		}
		allVersions = append(allVersions, VersionEntry{
			Key:          encodeStr(ver.Key),
			VersionId:    versionID,
			IsLatest:     ver.IsLatest,
			LastModified: ver.LastModified,
			ETag:         QuoteETag(ver.ETag),
			Size:         ver.Size,
			Owner:        owner,
			StorageClass: storageClassOrStandard(ver.StorageClass),
		})
	}
	for _, common := range page.CommonPrefixes {
		commonPrefixes = append(commonPrefixes, CommonPrefix{Prefix: encodeStr(common)})
	}

	// The markers of a page that is not the last name where the next one starts.
	nextKeyMarker, nextVersionIDMarker := "", ""
	if page.IsTruncated {
		nextKeyMarker, nextVersionIDMarker = page.NextKeyMarker, page.NextVersionIDMarker
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
		IsTruncated:         page.IsTruncated,
		Versions:            allVersions,
		DeleteMarkers:       allDeleteMarkers,
		CommonPrefixes:      commonPrefixes,
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}
