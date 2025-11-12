package s3compat

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
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
	MaxUploads         int               `xml:"MaxUploads"`
	IsTruncated        bool              `xml:"IsTruncated"`
	Uploads            []MultipartUpload `xml:"Upload,omitempty"`
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

	bucketPath := h.getBucketPath(r, bucketName)

	// Parse query parameters
	keyMarker := r.URL.Query().Get("key-marker")
	uploadIdMarker := r.URL.Query().Get("upload-id-marker")
	maxUploads := 1000

	if maxUploadsStr := r.URL.Query().Get("max-uploads"); maxUploadsStr != "" {
		if parsed, err := strconv.Atoi(maxUploadsStr); err == nil && parsed > 0 {
			maxUploads = parsed
		}
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

	// Filter and paginate results
	var filteredUploads []MultipartUpload
	for _, upload := range uploads {
		// Apply marker filtering
		if keyMarker != "" && upload.Key < keyMarker {
			continue
		}
		if uploadIdMarker != "" && upload.Key == keyMarker && upload.UploadID <= uploadIdMarker {
			continue
		}

		filteredUploads = append(filteredUploads, MultipartUpload{
			Key:      upload.Key,
			UploadId: upload.UploadID,
			Initiator: Initiator{
				ID:          "maxiofs",
				DisplayName: "MaxIOFS",
			},
			Owner: Owner{
				ID:          "maxiofs",
				DisplayName: "MaxIOFS",
			},
			StorageClass: "STANDARD",
			Initiated:    upload.Initiated,
		})

		if len(filteredUploads) >= maxUploads {
			break
		}
	}

	// Determine if truncated
	isTruncated := len(uploads) > len(filteredUploads)
	nextKeyMarker := ""
	nextUploadIdMarker := ""
	if isTruncated && len(filteredUploads) > 0 {
		lastUpload := filteredUploads[len(filteredUploads)-1]
		nextKeyMarker = lastUpload.Key
		nextUploadIdMarker = lastUpload.UploadId
	}

	result := ListMultipartUploadsResult{
		Bucket:             bucketName,
		KeyMarker:          keyMarker,
		UploadIdMarker:     uploadIdMarker,
		NextKeyMarker:      nextKeyMarker,
		NextUploadIdMarker: nextUploadIdMarker,
		MaxUploads:         maxUploads,
		IsTruncated:        isTruncated,
		Uploads:            filteredUploads,
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

	// Handle AWS chunked encoding
	// IMPORTANT: Some clients (like warp/MinIO-Go) send AWS chunked format
	// WITHOUT the Content-Encoding header. We need to detect it.
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

	// Parse query parameters
	partNumberMarker := 0
	if markerStr := r.URL.Query().Get("part-number-marker"); markerStr != "" {
		if parsed, err := strconv.Atoi(markerStr); err == nil {
			partNumberMarker = parsed
		}
	}

	maxParts := 1000
	if maxPartsStr := r.URL.Query().Get("max-parts"); maxPartsStr != "" {
		if parsed, err := strconv.Atoi(maxPartsStr); err == nil && parsed > 0 {
			maxParts = parsed
		}
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

	// Sort parts by part number
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	// Filter and paginate results
	var filteredParts []Part
	for _, part := range parts {
		if part.PartNumber <= partNumberMarker {
			continue
		}

		filteredParts = append(filteredParts, Part{
			PartNumber:   part.PartNumber,
			LastModified: part.LastModified,
			ETag:         part.ETag,
			Size:         part.Size,
		})

		if len(filteredParts) >= maxParts {
			break
		}
	}

	// Determine if truncated
	isTruncated := len(parts) > len(filteredParts)+partNumberMarker
	nextPartNumberMarker := 0
	if isTruncated && len(filteredParts) > 0 {
		nextPartNumberMarker = filteredParts[len(filteredParts)-1].PartNumber
	}

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
		StorageClass:         "STANDARD",
		PartNumberMarker:     partNumberMarker,
		NextPartNumberMarker: nextPartNumberMarker,
		MaxParts:             maxParts,
		IsTruncated:          isTruncated,
		Parts:                filteredParts,
	}

	h.writeXMLResponse(w, http.StatusOK, result)
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

	// Parse the complete multipart upload request
	var completeRequest CompleteMultipartUploadRequest
	if err := xml.NewDecoder(r.Body).Decode(&completeRequest); err != nil {
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

	// Note: For multipart uploads, quota validation happens in objectManager.CompleteMultipartUpload
	// which has access to the actual final object size

	// Complete the multipart upload
	obj, err := h.objectManager.CompleteMultipartUpload(r.Context(), uploadID, parts)
	if err != nil {
		if err == object.ErrUploadNotFound {
			h.writeError(w, "NoSuchUpload", "The specified multipart upload does not exist", uploadID, r)
			return
		}
		if err == object.ErrInvalidPart {
			h.writeError(w, "InvalidPart", "One or more of the specified parts could not be found", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Note: Bucket metrics and tenant storage are updated by objectManager.CompleteMultipartUpload()
	// No need to increment here to avoid double-counting on overwrites

	result := CompleteMultipartUploadResult{
		Location: "/" + bucketName + "/" + objectKey,
		Bucket:   bucketName,
		Key:      objectKey,
		ETag:     obj.ETag,
	}

	// Return version ID if versioning is enabled
	if obj.VersionID != "" {
		w.Header().Set("x-amz-version-id", obj.VersionID)
	}

	h.writeXMLResponse(w, http.StatusOK, result)
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
	// Parse source bucket and key from copy source
	// Format can be: "/source-bucket/source-key" OR "source-bucket/source-key"
	// AWS CLI may send with or without leading slash
	if len(copySource) > 0 && copySource[0] == '/' {
		copySource = copySource[1:] // Remove leading slash if present
	}

	slashIdx := strings.Index(copySource, "/")
	if slashIdx == -1 {
		h.writeError(w, "InvalidArgument", "Invalid copy source format", uploadID, r)
		return
	}

	sourceBucket := copySource[:slashIdx]
	sourceKey := copySource[slashIdx+1:]

	if sourceBucket == "" || sourceKey == "" {
		h.writeError(w, "InvalidArgument", "Invalid copy source: empty bucket or key", uploadID, r)
		return
	}

	logrus.WithFields(logrus.Fields{
		"uploadId":     uploadID,
		"partNumber":   partNumber,
		"sourceBucket": sourceBucket,
		"sourceKey":    sourceKey,
		"sourceRange":  copySourceRange,
	}).Info("UploadPartCopy: Processing copy part")

	sourceBucketPath := h.getBucketPath(r, sourceBucket)
	// Get source object
	sourceObj, reader, err := h.objectManager.GetObject(r.Context(), sourceBucketPath, sourceKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified source key does not exist", sourceKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), sourceKey, r)
		return
	}
	defer reader.Close()

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
