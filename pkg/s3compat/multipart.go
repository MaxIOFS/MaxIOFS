package s3compat

import (
	"encoding/xml"
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

	// Handle AWS chunked encoding (same as PutObject)
	contentEncoding := r.Header.Get("Content-Encoding")
	decodedContentLength := r.Header.Get("X-Amz-Decoded-Content-Length")

	var bodyReader io.Reader = r.Body

	if strings.Contains(contentEncoding, "aws-chunked") {
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

	result := CompleteMultipartUploadResult{
		Location: "/" + bucketName + "/" + objectKey,
		Bucket:   bucketName,
		Key:      objectKey,
		ETag:     obj.ETag,
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
