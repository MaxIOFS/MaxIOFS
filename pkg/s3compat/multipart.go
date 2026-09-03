package s3compat

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bandwidth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// Multipart Upload XML structures
type InitiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadId string   `xml:"UploadId"`
}

type ListMultipartUploadsResult struct {
	XMLName            xml.Name          `xml:"ListMultipartUploadsResult"`
	Bucket             string            `xml:"Bucket"`
	KeyMarker          string            `xml:"KeyMarker,omitempty"`
	UploadIdMarker     string            `xml:"UploadIdMarker,omitempty"`
	NextKeyMarker      string            `xml:"NextKeyMarker,omitempty"`
	NextUploadIdMarker string            `xml:"NextUploadIdMarker,omitempty"`
	Prefix             string            `xml:"Prefix"`
	Delimiter          string            `xml:"Delimiter,omitempty"`
	MaxUploads         int               `xml:"MaxUploads"`
	IsTruncated        bool              `xml:"IsTruncated"`
	Uploads            []MultipartUpload `xml:"Upload,omitempty"`
	CommonPrefixes     []CommonPrefix    `xml:"CommonPrefixes,omitempty"`
}

type MultipartUpload struct {
	Key          string    `xml:"Key"`
	UploadId     string    `xml:"UploadId"`
	Initiator    Initiator `xml:"Initiator"`
	Owner        Owner     `xml:"Owner"`
	StorageClass string    `xml:"StorageClass"`
	Initiated    time.Time `xml:"Initiated"`
}

type Initiator struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

type ListPartsResult struct {
	XMLName              xml.Name  `xml:"ListPartsResult"`
	Bucket               string    `xml:"Bucket"`
	Key                  string    `xml:"Key"`
	UploadId             string    `xml:"UploadId"`
	Initiator            Initiator `xml:"Initiator"`
	Owner                Owner     `xml:"Owner"`
	StorageClass         string    `xml:"StorageClass"`
	PartNumberMarker     int       `xml:"PartNumberMarker"`
	NextPartNumberMarker int       `xml:"NextPartNumberMarker,omitempty"`
	MaxParts             int       `xml:"MaxParts"`
	IsTruncated          bool      `xml:"IsTruncated"`
	Parts                []Part    `xml:"Part,omitempty"`
}

type Part struct {
	PartNumber   int       `xml:"PartNumber"`
	LastModified time.Time `xml:"LastModified"`
	ETag         string    `xml:"ETag"`
	Size         int64     `xml:"Size"`
}

type CompleteMultipartUploadRequest struct {
	XMLName xml.Name       `xml:"CompleteMultipartUpload"`
	Parts   []CompletePart `xml:"Part"`
}

type CompletePart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

type CompleteMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

// CreateMultipartUpload initiates a multipart upload
func (h *Handler) CreateMultipartUpload(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: CreateMultipartUpload")

	if !h.requireObjectS3Action(w, r, bucketName, objectKey, auth.ActionPutObject) {
		return
	}

	bucketPath := h.getBucketPath(r, bucketName)
	// Create multipart upload
	upload, err := h.objectManager.CreateMultipartUpload(r.Context(), bucketPath, objectKey, r.Header)
	if err != nil {
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	result := InitiateMultipartUploadResult{
		Bucket:   bucketName,
		Key:      objectKey,
		UploadId: upload.UploadID,
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}

// ListMultipartUploads lists in-progress multipart uploads
func (h *Handler) ListMultipartUploads(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: ListMultipartUploads")

	if !h.requireBucketS3Action(w, r, bucketName, auth.ActionListBucketMultipartUploads) {
		return
	}

	bucketPath := h.getBucketPath(r, bucketName)

	// Parse query parameters
	keyMarker := r.URL.Query().Get("key-marker")
	uploadIdMarker := r.URL.Query().Get("upload-id-marker")
	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	maxUploads := 1000

	if maxUploadsStr := r.URL.Query().Get("max-uploads"); maxUploadsStr != "" {
		parsed, err := strconv.Atoi(maxUploadsStr)
		if err != nil || parsed <= 0 {
			h.writeError(w, "InvalidArgument", "Argument max-uploads must be a positive integer", bucketName, r)
			return
		}
		if parsed > 1000 {
			parsed = 1000 // S3 spec cap
		}
		maxUploads = parsed
	}

	// List multipart uploads
	uploads, err := h.objectManager.ListMultipartUploads(r.Context(), bucketPath)
	if err != nil {
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	// Without the prefix filter a cleanup tool asking for its own namespace is
	// handed every in-progress upload in the bucket, including other workloads'.
	if prefix != "" {
		kept := uploads[:0]
		for _, u := range uploads {
			if strings.HasPrefix(u.Key, prefix) {
				kept = append(kept, u)
			}
		}
		uploads = kept
	}

	filteredUploads, commonPrefixes, isTruncated, nextKeyMarker, nextUploadIdMarker :=
		paginateMultipartUploads(uploads, prefix, delimiter, keyMarker, uploadIdMarker, maxUploads)

	result := ListMultipartUploadsResult{
		Bucket:             bucketName,
		Prefix:             prefix,
		Delimiter:          delimiter,
		KeyMarker:          keyMarker,
		UploadIdMarker:     uploadIdMarker,
		NextKeyMarker:      nextKeyMarker,
		NextUploadIdMarker: nextUploadIdMarker,
		MaxUploads:         maxUploads,
		IsTruncated:        isTruncated,
		Uploads:            filteredUploads,
		CommonPrefixes:     commonPrefixes,
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}

// UploadPart uploads a part for a multipart upload
func (h *Handler) UploadPart(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	uploadID := r.URL.Query().Get("uploadId")
	partNumberStr := r.URL.Query().Get("partNumber")

	if uploadID == "" {
		h.writeError(w, "InvalidArgument", "Upload ID is required", objectKey, r)
		return
	}

	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil || partNumber < 1 || partNumber > 10000 {
		h.writeError(w, "InvalidArgument", "Part number must be between 1 and 10000", objectKey, r)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":     bucketName,
		"object":     objectKey,
		"uploadId":   uploadID,
		"partNumber": partNumber,
	}).Debug("S3 API: UploadPart")

	if !h.requireObjectS3Action(w, r, bucketName, objectKey, auth.ActionPutObject) {
		return
	}

	// The permission above was checked against the URL's key; every operation
	// below acts on uploadID alone. This ties the two together.
	if !h.requireUploadTarget(w, r, uploadID, h.getBucketPath(r, bucketName), objectKey) {
		return
	}

	// IMPORTANT: Detect UploadPartCopy operation
	// AWS CLI uses this for copying large files (>5MB) between buckets
	copySource := r.Header.Get("x-amz-copy-source")
	copySourceRange := r.Header.Get("x-amz-copy-source-range")

	if copySource != "" {
		logrus.WithFields(logrus.Fields{
			"bucket":      bucketName,
			"object":      objectKey,
			"uploadId":    uploadID,
			"partNumber":  partNumber,
			"copySource":  copySource,
			"sourceRange": copySourceRange,
		}).Info("S3 API: UploadPartCopy detected")

		h.UploadPartCopy(w, r, uploadID, partNumber, copySource, copySourceRange)
		return
	}

	contentEncoding := r.Header.Get("Content-Encoding")
	decodedContentLength := r.Header.Get("X-Amz-Decoded-Content-Length")

	var bodyReader io.Reader = r.Body

	// Detect AWS chunked by header OR by decoded-content-length presence
	isAwsChunked := strings.Contains(contentEncoding, "aws-chunked") || decodedContentLength != ""

	if isAwsChunked {
		logrus.WithFields(logrus.Fields{
			"bucket":     bucketName,
			"object":     objectKey,
			"uploadId":   uploadID,
			"partNumber": partNumber,
			"decodedLen": decodedContentLength,
		}).Info("AWS chunked encoding detected in UploadPart")

		bodyReader = NewAwsChunkedReader(r.Body)

		// Update Content-Length from X-Amz-Decoded-Content-Length
		if decodedContentLength != "" {
			if size, err := strconv.ParseInt(decodedContentLength, 10, 64); err == nil {
				r.ContentLength = size
				r.Header.Set("Content-Length", decodedContentLength)
			}
		}
		r.Header.Del("Content-Encoding")
	}

	// Throttle the part upload to the owning tenant's aggregate bandwidth budget
	// (no-op when unlimited); shares the same tenant limiter as other transfers.
	bodyReader = bandwidth.ThrottleReader(r.Context(), bodyReader, h.tenantBandwidthLimiter(r.Context(), r, bucketName))

	// Upload the part
	part, err := h.objectManager.UploadPart(r.Context(), uploadID, partNumber, bodyReader)
	if err != nil {
		if err == object.ErrUploadNotFound {
			h.writeError(w, "NoSuchUpload", "The specified multipart upload does not exist", uploadID, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Return ETag in response header
	w.Header().Set("ETag", part.ETag)
	w.WriteHeader(http.StatusOK)
}

// ListParts lists parts that have been uploaded for a multipart upload
func (h *Handler) ListParts(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	uploadID := r.URL.Query().Get("uploadId")
	if uploadID == "" {
		h.writeError(w, "InvalidArgument", "Upload ID is required", objectKey, r)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":   bucketName,
		"object":   objectKey,
		"uploadId": uploadID,
	}).Debug("S3 API: ListParts")

	if !h.requireObjectS3Action(w, r, bucketName, objectKey, auth.ActionListMultipartUploadParts) {
		return
	}

	// The permission above was checked against the URL's key; every operation
	// below acts on uploadID alone. This ties the two together.
	if !h.requireUploadTarget(w, r, uploadID, h.getBucketPath(r, bucketName), objectKey) {
		return
	}

	// Parse query parameters
	partNumberMarker := 0
	if markerStr := r.URL.Query().Get("part-number-marker"); markerStr != "" {
		parsed, err := strconv.Atoi(markerStr)
		if err != nil || parsed < 0 {
			h.writeError(w, "InvalidArgument", "Argument part-number-marker must be a non-negative integer", uploadID, r)
			return
		}
		partNumberMarker = parsed
	}

	maxParts := 1000
	if maxPartsStr := r.URL.Query().Get("max-parts"); maxPartsStr != "" {
		parsed, err := strconv.Atoi(maxPartsStr)
		if err != nil || parsed <= 0 {
			h.writeError(w, "InvalidArgument", "Argument max-parts must be a positive integer", uploadID, r)
			return
		}
		if parsed > 1000 {
			parsed = 1000 // S3 spec cap
		}
		maxParts = parsed
	}

	// List parts
	parts, err := h.objectManager.ListParts(r.Context(), uploadID)
	if err != nil {
		if err == object.ErrUploadNotFound {
			h.writeError(w, "NoSuchUpload", "The specified multipart upload does not exist", uploadID, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), uploadID, r)
		return
	}

	// Resolve StorageClass from the upload metadata; fall back to STANDARD.
	uploadStorageClass := "STANDARD"
	if h.metadataStore != nil {
		if uploadMeta, metaErr := h.metadataStore.GetMultipartUpload(r.Context(), uploadID); metaErr == nil && uploadMeta != nil {
			uploadStorageClass = storageClassOrStandard(uploadMeta.StorageClass)
		}
	}

	filteredParts, isTruncated, nextPartNumberMarker := paginateMultipartParts(parts, partNumberMarker, maxParts)

	result := ListPartsResult{
		Bucket:   bucketName,
		Key:      objectKey,
		UploadId: uploadID,
		Initiator: Initiator{
			ID:          "maxiofs",
			DisplayName: "MaxIOFS",
		},
		Owner: Owner{
			ID:          "maxiofs",
			DisplayName: "MaxIOFS",
		},
		StorageClass:         uploadStorageClass,
		PartNumberMarker:     partNumberMarker,
		NextPartNumberMarker: nextPartNumberMarker,
		MaxParts:             maxParts,
		IsTruncated:          isTruncated,
		Parts:                filteredParts,
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}

type multipartListItem struct {
	key          string
	uploadID     string
	upload       *object.MultipartUpload
	commonPrefix string
}

func paginateMultipartUploads(uploads []object.MultipartUpload, prefix, delimiter, keyMarker, uploadIDMarker string, maxUploads int) ([]MultipartUpload, []CommonPrefix, bool, string, string) {
	items := buildMultipartListItems(uploads, prefix, delimiter)
	sort.Slice(items, func(i, j int) bool {
		if items[i].key == items[j].key {
			return items[i].uploadID < items[j].uploadID
		}
		return items[i].key < items[j].key
	})

	filteredUploads := make([]MultipartUpload, 0, maxUploads)
	commonPrefixes := make([]CommonPrefix, 0)
	isTruncated := false
	var lastItem *multipartListItem

	for i := range items {
		item := &items[i]
		if !multipartListItemAfterMarker(item, keyMarker, uploadIDMarker) {
			continue
		}

		if len(filteredUploads)+len(commonPrefixes) >= maxUploads {
			isTruncated = true
			break
		}

		if item.commonPrefix != "" {
			commonPrefixes = append(commonPrefixes, CommonPrefix{Prefix: item.commonPrefix})
		} else if item.upload != nil {
			filteredUploads = append(filteredUploads, MultipartUpload{
				Key:      item.upload.Key,
				UploadId: item.upload.UploadID,
				Initiator: Initiator{
					ID:          "maxiofs",
					DisplayName: "MaxIOFS",
				},
				Owner: Owner{
					ID:          "maxiofs",
					DisplayName: "MaxIOFS",
				},
				StorageClass: storageClassOrStandard(item.upload.StorageClass),
				Initiated:    item.upload.Initiated,
			})
		}
		lastItem = item
	}

	nextKeyMarker := ""
	nextUploadIDMarker := ""
	if isTruncated && lastItem != nil {
		nextKeyMarker = lastItem.key
		nextUploadIDMarker = lastItem.uploadID
	}

	return filteredUploads, commonPrefixes, isTruncated, nextKeyMarker, nextUploadIDMarker
}

func buildMultipartListItems(uploads []object.MultipartUpload, prefix, delimiter string) []multipartListItem {
	sort.Slice(uploads, func(i, j int) bool {
		if uploads[i].Key == uploads[j].Key {
			return uploads[i].UploadID < uploads[j].UploadID
		}
		return uploads[i].Key < uploads[j].Key
	})

	items := make([]multipartListItem, 0, len(uploads))
	seenCommonPrefixes := make(map[string]struct{})

	for i := range uploads {
		upload := &uploads[i]
		if delimiter != "" {
			remaining := strings.TrimPrefix(upload.Key, prefix)
			if idx := strings.Index(remaining, delimiter); idx >= 0 {
				commonPrefix := prefix + remaining[:idx+len(delimiter)]
				if _, ok := seenCommonPrefixes[commonPrefix]; ok {
					continue
				}
				seenCommonPrefixes[commonPrefix] = struct{}{}
				items = append(items, multipartListItem{
					key:          commonPrefix,
					commonPrefix: commonPrefix,
				})
				continue
			}
		}

		items = append(items, multipartListItem{
			key:      upload.Key,
			uploadID: upload.UploadID,
			upload:   upload,
		})
	}
	return items
}

func multipartListItemAfterMarker(item *multipartListItem, keyMarker, uploadIDMarker string) bool {
	if keyMarker == "" {
		return true
	}
	if item.key < keyMarker {
		return false
	}
	if item.key > keyMarker {
		return true
	}
	if item.commonPrefix != "" {
		return false
	}
	return uploadIDMarker != "" && item.uploadID > uploadIDMarker
}

func paginateMultipartParts(parts []object.Part, partNumberMarker, maxParts int) ([]Part, bool, int) {
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	filteredParts := make([]Part, 0, maxParts)
	isTruncated := false

	for _, part := range parts {
		if part.PartNumber <= partNumberMarker {
			continue
		}
		if len(filteredParts) >= maxParts {
			isTruncated = true
			break
		}

		filteredParts = append(filteredParts, Part{
			PartNumber:   part.PartNumber,
			LastModified: part.LastModified,
			ETag:         part.ETag,
			Size:         part.Size,
		})
	}

	nextPartNumberMarker := 0
	if isTruncated && len(filteredParts) > 0 {
		nextPartNumberMarker = filteredParts[len(filteredParts)-1].PartNumber
	}

	return filteredParts, isTruncated, nextPartNumberMarker
}

// CompleteMultipartUpload completes a multipart upload
func (h *Handler) CompleteMultipartUpload(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	uploadID := r.URL.Query().Get("uploadId")
	if uploadID == "" {
		h.writeError(w, "InvalidArgument", "Upload ID is required", objectKey, r)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":   bucketName,
		"object":   objectKey,
		"uploadId": uploadID,
	}).Debug("S3 API: CompleteMultipartUpload")

	if !h.requireObjectS3Action(w, r, bucketName, objectKey, auth.ActionPutObject) {
		return
	}

	// Nothing has been written yet, so this is an ordinary 404. The in-body
	// error further down exists only because the keep-alive path has already
	// committed a 200 by the time it can fail.
	if !h.uploadMatchesTarget(r, uploadID, h.getBucketPath(r, bucketName), objectKey) {
		h.writeError(w, "NoSuchUpload", "The specified multipart upload does not exist", objectKey, r)
		return
	}

	// Parse the complete multipart upload request
	var completeRequest CompleteMultipartUploadRequest
	if err := decodeS3ControlXML(w, r, &completeRequest); err != nil {
		h.writeError(w, "MalformedXML", "The XML is not well-formed", objectKey, r)
		return
	}
	defer r.Body.Close()

	// Validate parts
	if len(completeRequest.Parts) == 0 {
		h.writeError(w, "InvalidRequest", "You must specify at least one part", objectKey, r)
		return
	}

	// Convert to internal Part structure
	parts := make([]object.Part, len(completeRequest.Parts))
	for i, part := range completeRequest.Parts {
		parts[i] = object.Part{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
		}
	}

	bucketPath := h.getBucketPath(r, bucketName)
	var storedCannedACL string
	if h.metadataStore != nil {
		if uploadMeta, metaErr := h.metadataStore.GetMultipartUpload(r.Context(), uploadID); metaErr == nil && uploadMeta != nil {
			if uploadMeta.Metadata != nil {
				storedCannedACL = uploadMeta.Metadata["x-amz-acl"]
			}
		}
	}

	// Run the heavy processing in the background.
	// Request values are preserved, but cancellation is tied to the server
	// lifecycle instead of the client connection. If the client times out and
	// reconnects, the file must already be assembled; if the server shuts down,
	// shutdown waits for this worker before closing the stores.
	type completionResult struct {
		obj *object.Object
		err error
	}
	resultCh := make(chan completionResult, 1)
	bgCtx := h.backgroundJobContext(r.Context())
	if !h.runBackground("S3 complete multipart upload", func() {
		obj, err := h.objectManager.CompleteMultipartUpload(bgCtx, uploadID, parts)
		resultCh <- completionResult{obj, err}
	}) {
		h.writeError(w, "ServiceUnavailable", "server is shutting down", objectKey, r)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var res completionResult
	for {
		select {
		case res = <-resultCh:
			goto done
		case <-ticker.C:
			w.Write([]byte(" ")) //nolint:errcheck
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-r.Context().Done():
			// Client disconnected; nothing to do — background goroutine will finish
			// and the deduplication layer will handle any retried request.
			return
		}
	}

done:
	if res.err != nil {
		// We already committed 200 OK, so embed the error in the body.
		// AWS S3 uses this same pattern; compliant clients parse the body.
		code := "InternalError"
		if res.err == object.ErrInvalidUploadID || res.err == object.ErrUploadNotFound {
			code = "NoSuchUpload"
		} else if res.err == object.ErrInvalidPart {
			code = "InvalidPart"
		} else if res.err == object.ErrInvalidPartOrder {
			code = "InvalidPartOrder"
		} else if errors.Is(res.err, cluster.ErrClusterDegraded) {
			code = "ServiceUnavailable"
		} else if strings.Contains(res.err.Error(), "storage quota exceeded") || strings.Contains(res.err.Error(), "quota exceeded") {
			code = "QuotaExceeded"
		}
		logrus.WithFields(logrus.Fields{
			"bucket":   bucketName,
			"object":   objectKey,
			"uploadId": uploadID,
			"code":     code,
		}).WithError(res.err).Error("CompleteMultipartUpload failed after 200 OK was sent")
		w.Write([]byte(xml.Header)) //nolint:errcheck
		errResp := struct {
			XMLName  xml.Name `xml:"Error"`
			Code     string   `xml:"Code"`
			Message  string   `xml:"Message"`
			Resource string   `xml:"Resource"`
		}{
			Code:     code,
			Message:  res.err.Error(),
			Resource: objectKey,
		}
		xml.NewEncoder(w).Encode(errResp) //nolint:errcheck
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	// Return version ID if versioning is enabled
	if res.obj.VersionID != "" {
		// Headers already sent; version ID can only be delivered in a trailer,
		// which most S3 clients don't support. Log it and move on.
		logrus.WithField("versionID", res.obj.VersionID).Debug("CompleteMultipartUpload: version ID set after early 200, cannot set header")
	}

	// Apply x-amz-acl originally set during CreateMultipartUpload (if any).
	if storedCannedACL != "" {
		h.applyObjectCannedACLHeader(bgCtx, bucketPath, objectKey, storedCannedACL)
	}

	result := CompleteMultipartUploadResult{
		Location: h.buildLocationURL(r, bucketName, objectKey),
		Bucket:   bucketName,
		Key:      objectKey,
		ETag:     res.obj.ETag,
	}
	if err := xml.NewEncoder(w).Encode(result); err != nil {
		logrus.WithError(err).Error("Failed to encode CompleteMultipartUpload response")
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Fire s3:ObjectCreated:CompleteMultipartUpload notification asynchronously.
	tenantID := h.resolveBucketTenantID(r, bucketName)
	h.fireNotifications(bgCtx, bucketName, tenantID, objectKey, "s3:ObjectCreated:CompleteMultipartUpload", res.obj.ETag, res.obj.Size)
}

// AbortMultipartUpload aborts a multipart upload
func (h *Handler) AbortMultipartUpload(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	uploadID := r.URL.Query().Get("uploadId")
	if uploadID == "" {
		h.writeError(w, "InvalidArgument", "Upload ID is required", objectKey, r)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":   bucketName,
		"object":   objectKey,
		"uploadId": uploadID,
	}).Debug("S3 API: AbortMultipartUpload")

	if !h.requireObjectS3Action(w, r, bucketName, objectKey, auth.ActionAbortMultipartUpload) {
		return
	}

	// The permission above was checked against the URL's key; every operation
	// below acts on uploadID alone. This ties the two together.
	if !h.requireUploadTarget(w, r, uploadID, h.getBucketPath(r, bucketName), objectKey) {
		return
	}

	// Abort the multipart upload
	if err := h.objectManager.AbortMultipartUpload(r.Context(), uploadID); err != nil {
		if err == object.ErrUploadNotFound {
			h.writeError(w, "NoSuchUpload", "The specified multipart upload does not exist", uploadID, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), uploadID, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UploadPartCopy implements S3 UploadPartCopy for copying parts of large files
// This is used by AWS CLI when copying files > 5MB between buckets
func (h *Handler) UploadPartCopy(w http.ResponseWriter, r *http.Request, uploadID string, partNumber int, copySource, copySourceRange string) {
	vars := mux.Vars(r)
	destBucket := vars["bucket"]
	destKey := vars["object"]

	sourceBucket, sourceKey, copySourceVersionID, err := parseCopySourceHeader(copySource)
	if err != nil {
		h.writeError(w, "InvalidArgument", "Invalid copy source format", uploadID, r)
		return
	}

	logrus.WithFields(logrus.Fields{
		"uploadId":     uploadID,
		"partNumber":   partNumber,
		"sourceBucket": sourceBucket,
		"sourceKey":    sourceKey,
		"sourceRange":  copySourceRange,
	}).Info("UploadPartCopy: Processing copy part")

	user, userExists := auth.GetUserFromContext(r.Context())
	sourceBucketPath := h.getBucketPath(r, sourceBucket)
	destTenantID := h.resolveBucketTenantID(r, destBucket)

	sourceReadAction := auth.ActionGetObject
	if copySourceVersionID != "" {
		sourceReadAction = auth.ActionGetObjectVersion
	}
	if !h.requireObjectS3ActionOnVersion(w, r, sourceBucket, sourceKey,
		sourceReadAction, copySourceVersionID) {
		return
	}
	if !h.validateBucketWritePermission(r, user, userExists, destTenantID, destBucket, destKey) {
		h.writeError(w, "AccessDenied", "Access Denied", destKey, r)
		return
	}
	if h.metadataStore != nil {
		if _, uploadErr := h.metadataStore.GetMultipartUpload(r.Context(), uploadID); uploadErr != nil {
			if uploadErr == metadata.ErrUploadNotFound {
				h.writeError(w, "NoSuchUpload", "The specified multipart upload does not exist", uploadID, r)
				return
			}
			h.writeError(w, "InternalError", uploadErr.Error(), uploadID, r)
			return
		}
	}

	// Get source object
	var sourceObj *object.Object
	var reader io.ReadCloser
	if copySourceVersionID != "" {
		sourceObj, reader, err = h.objectManager.GetObject(r.Context(), sourceBucketPath, sourceKey, copySourceVersionID)
	} else {
		sourceObj, reader, err = h.objectManager.GetObject(r.Context(), sourceBucketPath, sourceKey)
	}
	if err != nil {
		if err == object.ErrObjectNotFound {
			if copySourceVersionID != "" {
				h.writeError(w, "NoSuchVersion", "The specified version does not exist", sourceKey, r)
				return
			}
			h.writeError(w, "NoSuchKey", "The specified source key does not exist", sourceKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), sourceKey, r)
		return
	}
	defer reader.Close()

	if !h.validateCopySourceConditionals(w, r, sourceObj, sourceKey) {
		return
	}

	// Parse range if specified
	var partReader io.Reader
	if copySourceRange != "" {
		// Format: "bytes=start-end"
		rangeStart, rangeEnd, err := parseCopySourceRange(copySourceRange, sourceObj.Size)
		if err != nil {
			h.writeError(w, "InvalidArgument", fmt.Sprintf("Invalid copy source range: %v", err), uploadID, r)
			return
		}

		rangeSize := rangeEnd - rangeStart + 1

		logrus.WithFields(logrus.Fields{
			"uploadId":   uploadID,
			"partNumber": partNumber,
			"rangeStart": rangeStart,
			"rangeEnd":   rangeEnd,
			"rangeSize":  rangeSize,
		}).Info("UploadPartCopy: Using range")

		// Skip to start position if needed
		if rangeStart > 0 {
			if _, err := io.CopyN(io.Discard, reader, rangeStart); err != nil {
				h.writeError(w, "InternalError", fmt.Sprintf("Failed to seek to range start: %v", err), uploadID, r)
				return
			}
		}

		// Use LimitReader to read only the range size - streaming without loading into memory
		partReader = io.LimitReader(reader, rangeSize)
	} else {
		// No range specified, use entire reader - streaming without loading into memory
		logrus.WithFields(logrus.Fields{
			"uploadId":   uploadID,
			"partNumber": partNumber,
			"sourceSize": sourceObj.Size,
		}).Debug("UploadPartCopy: Streaming entire object")

		partReader = reader
	}

	logrus.WithFields(logrus.Fields{
		"uploadId":   uploadID,
		"partNumber": partNumber,
	}).Debug("UploadPartCopy: Uploading part with streaming")

	// Upload the part with streaming reader - no memory loading
	part, err := h.objectManager.UploadPart(r.Context(), uploadID, partNumber, partReader)
	if err != nil {
		if err == object.ErrUploadNotFound {
			h.writeError(w, "NoSuchUpload", "The specified multipart upload does not exist", uploadID, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), uploadID, r)
		return
	}

	// Return copy part result
	type CopyPartResult struct {
		XMLName      xml.Name  `xml:"CopyPartResult"`
		LastModified time.Time `xml:"LastModified"`
		ETag         string    `xml:"ETag"`
	}

	result := CopyPartResult{
		LastModified: time.Now(),
		ETag:         part.ETag,
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}

// parseCopySourceRange parses S3 copy-source-range header
// Format: "bytes=start-end"
func parseCopySourceRange(rangeHeader string, objectSize int64) (int64, int64, error) {
	// Remove "bytes=" prefix
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, fmt.Errorf("invalid range format, must start with 'bytes='")
	}
	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")

	// Split on dash
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format, expected 'start-end'")
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range start: %w", err)
	}

	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range end: %w", err)
	}

	if start < 0 || end < start || end >= objectSize {
		return 0, 0, fmt.Errorf("range out of bounds (start=%d, end=%d, size=%d)", start, end, objectSize)
	}

	return start, end, nil
}
