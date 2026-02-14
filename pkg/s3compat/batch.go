package s3compat

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// DeleteObjectsRequest represents the XML structure for batch delete requests
type DeleteObjectsRequest struct {
	XMLName xml.Name         `xml:"Delete"`
	Quiet   bool             `xml:"Quiet"`
	Objects []ObjectToDelete `xml:"Object"`
}

// ObjectToDelete represents an object to delete in batch operations
type ObjectToDelete struct {
	Key       string `xml:"Key"`
	VersionId string `xml:"VersionId,omitempty"`
}

// DeleteObjectsResult represents the XML response for batch delete
type DeleteObjectsResult struct {
	XMLName xml.Name        `xml:"DeleteResult"`
	Deleted []DeletedObject `xml:"Deleted,omitempty"`
	Errors  []DeleteError   `xml:"Error,omitempty"`
}

// DeletedObject represents a successfully deleted object
type DeletedObject struct {
	Key                   string `xml:"Key"`
	VersionId             string `xml:"VersionId,omitempty"`
	DeleteMarker          bool   `xml:"DeleteMarker,omitempty"`
	DeleteMarkerVersionId string `xml:"DeleteMarkerVersionId,omitempty"`
}

// DeleteError represents an error during batch delete
type DeleteError struct {
	Key       string `xml:"Key"`
	Code      string `xml:"Code"`
	Message   string `xml:"Message"`
	VersionId string `xml:"VersionId,omitempty"`
}

// DeleteObjects handles batch delete operations (S3 POST /?delete)
func (h *Handler) DeleteObjects(w http.ResponseWriter, r *http.Request) {
	logrus.Debug("S3 API: DeleteObjects (batch)")

	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	if bucketName == "" {
		h.writeError(w, "InvalidBucketName", "Bucket name is required", r.URL.Path, r)
		return
	}

	bucketPath := h.getBucketPath(r, bucketName)

	// Parse XML request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, "InvalidRequest", "Failed to read request body", r.URL.Path, r)
		return
	}
	defer r.Body.Close()

	var deleteRequest DeleteObjectsRequest
	if err := xml.Unmarshal(body, &deleteRequest); err != nil {
		h.writeError(w, "MalformedXML", fmt.Sprintf("Failed to parse XML: %v", err), r.URL.Path, r)
		return
	}

	// Validate request
	if len(deleteRequest.Objects) == 0 {
		h.writeError(w, "InvalidRequest", "No objects specified for deletion", r.URL.Path, r)
		return
	}

	if len(deleteRequest.Objects) > 1000 {
		h.writeError(w, "InvalidRequest", "Cannot delete more than 1000 objects in a single request", r.URL.Path, r)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":       bucketName,
		"object_count": len(deleteRequest.Objects),
	}).Debug("Batch delete request received")

	// Process deletions sequentially to avoid BadgerDB transaction conflicts
	// This is more reliable than parallel processing with retries, especially under high load
	result := DeleteObjectsResult{
		Deleted: []DeletedObject{},
		Errors:  []DeleteError{},
	}

	ctx := r.Context()

	// Process each object deletion sequentially
	for _, obj := range deleteRequest.Objects {
		if obj.Key == "" {
			result.Errors = append(result.Errors, DeleteError{
				Key:     obj.Key,
				Code:    "InvalidArgument",
				Message: "Object key cannot be empty",
			})
			continue
		}

		// Delete object (filesystem + BadgerDB metadata + bucket metrics)
		// Batch delete doesn't support bypass governance
		_, err := h.objectManager.DeleteObject(ctx, bucketPath, obj.Key, false)

		if err != nil {
			// Log error but continue with other objects
			if err == object.ErrObjectNotFound {
				// S3 spec: DELETE on non-existent object should return success
				if !deleteRequest.Quiet {
					result.Deleted = append(result.Deleted, DeletedObject{
						Key:       obj.Key,
						VersionId: obj.VersionId,
					})
				}
			} else {
				// Log the error for debugging
				logrus.WithError(err).WithFields(logrus.Fields{
					"bucket": bucketName,
					"key":    obj.Key,
				}).Warn("Failed to delete object in batch operation")

				result.Errors = append(result.Errors, DeleteError{
					Key:       obj.Key,
					Code:      "InternalError",
					Message:   err.Error(),
					VersionId: obj.VersionId,
				})
			}
		} else {
			// Success - add to deleted list if not quiet mode
			if !deleteRequest.Quiet {
				result.Deleted = append(result.Deleted, DeletedObject{
					Key:       obj.Key,
					VersionId: obj.VersionId,
				})
			}
		}
	}

	// Return XML response
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)

	xmlData, err := xml.MarshalIndent(result, "", "  ")
	if err != nil {
		h.writeError(w, "InternalError", "Failed to generate response", r.URL.Path, r)
		return
	}

	w.Write([]byte(xml.Header))
	w.Write(xmlData)

	logrus.Debugf("Batch delete completed: %d deleted, %d errors", len(result.Deleted), len(result.Errors))
}
