package s3compat

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

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

// CopyObjectsRequest represents a batch copy operation request
type CopyObjectsRequest struct {
	Sources      []CopySource      `json:"sources"`
	Destinations []string          `json:"destinations"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// CopySource represents a source object for batch copy
type CopySource struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

// CopyObjectsResult represents the result of a batch copy operation
type CopyObjectsResult struct {
	Successful []CopySuccess `json:"successful"`
	Failed     []CopyFailure `json:"failed"`
}

// CopySuccess represents a successful copy operation
type CopySuccess struct {
	SourceKey      string `json:"source_key"`
	DestinationKey string `json:"destination_key"`
	ETag           string `json:"etag"`
}

// CopyFailure represents a failed copy operation
type CopyFailure struct {
	SourceKey string `json:"source_key"`
	Error     string `json:"error"`
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

	// Process deletions in parallel for better performance
	result := DeleteObjectsResult{
		Deleted: []DeletedObject{},
		Errors:  []DeleteError{},
	}

	ctx := r.Context()

	// Use worker pool for parallel processing
	type deleteResult struct {
		obj     ObjectToDelete
		err     error
		success bool
	}

	resultChan := make(chan deleteResult, len(deleteRequest.Objects))
	semaphore := make(chan struct{}, 50) // Max 50 concurrent deletes

	for _, obj := range deleteRequest.Objects {
		if obj.Key == "" {
			resultChan <- deleteResult{
				obj: obj,
				err: fmt.Errorf("Object key cannot be empty"),
			}
			continue
		}

		go func(obj ObjectToDelete) {
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			err := h.objectManager.DeleteObject(ctx, bucketPath, obj.Key)
			resultChan <- deleteResult{
				obj:     obj,
				err:     err,
				success: err == nil || err == object.ErrObjectNotFound,
			}
		}(obj)
	}

	// Collect results
	for i := 0; i < len(deleteRequest.Objects); i++ {
		res := <-resultChan

		if res.err != nil {
			// Log error but continue with other objects
			if res.err == object.ErrObjectNotFound {
				// S3 returns success even if object doesn't exist
				if !deleteRequest.Quiet {
					result.Deleted = append(result.Deleted, DeletedObject{
						Key:       res.obj.Key,
						VersionId: res.obj.VersionId,
					})
				}
			} else {
				result.Errors = append(result.Errors, DeleteError{
					Key:       res.obj.Key,
					Code:      "InternalError",
					Message:   res.err.Error(),
					VersionId: res.obj.VersionId,
				})
			}
		} else {
			// Success - add to deleted list if not quiet mode
			if !deleteRequest.Quiet {
				result.Deleted = append(result.Deleted, DeletedObject{
					Key:       res.obj.Key,
					VersionId: res.obj.VersionId,
				})
			}
		}
	}

	close(resultChan)

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

// CopyObjects handles batch copy operations (custom endpoint)
// This is not part of standard S3 API but useful for bulk operations
func (h *Handler) CopyObjects(w http.ResponseWriter, r *http.Request) {
	logrus.Debug("S3 API: CopyObjects (batch)")

	vars := mux.Vars(r)
	targetBucket := vars["bucket"]

	if targetBucket == "" {
		h.writeError(w, "InvalidBucketName", "Target bucket name is required", r.URL.Path, r)
		return
	}

	targetBucketPath := h.getBucketPath(r, targetBucket)

	// Parse JSON request body
	var copyRequest CopyObjectsRequest
	if err := parseJSONBody(r, &copyRequest); err != nil {
		h.writeError(w, "InvalidRequest", fmt.Sprintf("Failed to parse request: %v", err), r.URL.Path, r)
		return
	}

	// Validate request
	if len(copyRequest.Sources) == 0 {
		h.writeError(w, "InvalidRequest", "No sources specified for copy", r.URL.Path, r)
		return
	}

	if len(copyRequest.Sources) > 1000 {
		h.writeError(w, "InvalidRequest", "Cannot copy more than 1000 objects in a single request", r.URL.Path, r)
		return
	}

	if len(copyRequest.Destinations) > 0 && len(copyRequest.Destinations) != len(copyRequest.Sources) {
		h.writeError(w, "InvalidRequest", "Number of destinations must match number of sources", r.URL.Path, r)
		return
	}

	// Process copies
	result := CopyObjectsResult{
		Successful: []CopySuccess{},
		Failed:     []CopyFailure{},
	}

	ctx := r.Context()
	for i, source := range copyRequest.Sources {
		if source.Key == "" {
			result.Failed = append(result.Failed, CopyFailure{
				SourceKey: source.Key,
				Error:     "Source key cannot be empty",
			})
			continue
		}

		// Determine destination key
		destKey := source.Key
		if len(copyRequest.Destinations) > 0 {
			destKey = copyRequest.Destinations[i]
		}

		sourceBucketPath := h.getBucketPath(r, source.Bucket)
		// Perform copy operation
		err := h.copyObject(ctx, sourceBucketPath, source.Key, targetBucketPath, destKey, copyRequest.Metadata)
		if err != nil {
			result.Failed = append(result.Failed, CopyFailure{
				SourceKey: fmt.Sprintf("%s/%s", source.Bucket, source.Key),
				Error:     err.Error(),
			})
			continue
		}

		// Get ETag of copied object
		obj, err := h.objectManager.GetObjectMetadata(ctx, targetBucketPath, destKey)
		etag := ""
		if err == nil && obj != nil {
			etag = obj.ETag
		}

		result.Successful = append(result.Successful, CopySuccess{
			SourceKey:      fmt.Sprintf("%s/%s", source.Bucket, source.Key),
			DestinationKey: destKey,
			ETag:           etag,
		})
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := writeJSONResponse(w, result); err != nil {
		logrus.Errorf("Failed to write response: %v", err)
	}

	logrus.Debugf("Batch copy completed: %d successful, %d failed", len(result.Successful), len(result.Failed))
}

// copyObject is a helper function to copy a single object
func (h *Handler) copyObject(ctx context.Context, sourceBucketPath, sourceKey, destBucketPath, destKey string, metadata map[string]string) error {
	// Get source object
	obj, reader, err := h.objectManager.GetObject(ctx, sourceBucketPath, sourceKey)
	if err != nil {
		return fmt.Errorf("failed to get source object: %w", err)
	}
	defer reader.Close()

	// Read object data
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read source object: %w", err)
	}

	// Prepare headers for destination
	destHeaders := make(http.Header)

	// Copy source metadata if no override provided
	if len(metadata) == 0 {
		for k, v := range obj.Metadata {
			destHeaders.Set("X-Amz-Meta-"+k, v)
		}
	} else {
		// Use provided metadata
		for k, v := range metadata {
			destHeaders.Set("X-Amz-Meta-"+k, v)
		}
	}

	// Add Content-Type if present
	if obj.ContentType != "" {
		destHeaders.Set("Content-Type", obj.ContentType)
	}

	// Put object to destination
	_, err = h.objectManager.PutObject(ctx, destBucketPath, destKey, strings.NewReader(string(data)), destHeaders)
	if err != nil {
		return fmt.Errorf("failed to put destination object: %w", err)
	}

	return nil
}

// BatchOperation represents a generic batch operation
type BatchOperation struct {
	Type   string                 `json:"type"` // "delete" or "copy"
	Params map[string]interface{} `json:"params"`
}

// ExecuteBatchOperation executes a generic batch operation
// This provides a unified endpoint for various batch operations
func (h *Handler) ExecuteBatchOperation(w http.ResponseWriter, r *http.Request) {
	logrus.Debug("S3 API: ExecuteBatchOperation")

	var operation BatchOperation
	if err := parseJSONBody(r, &operation); err != nil {
		h.writeError(w, "InvalidRequest", fmt.Sprintf("Failed to parse request: %v", err), r.URL.Path, r)
		return
	}

	switch operation.Type {
	case "delete":
		// Convert to DeleteObjects request and handle
		h.DeleteObjects(w, r)
	case "copy":
		// Convert to CopyObjects request and handle
		h.CopyObjects(w, r)
	default:
		h.writeError(w, "InvalidRequest", fmt.Sprintf("Unknown operation type: %s", operation.Type), r.URL.Path, r)
	}
}

// Helper functions

func parseJSONBody(r *http.Request, v interface{}) error {
	_, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("failed to read body: %w", err)
	}
	defer r.Body.Close()

	// For this implementation, we'll use basic parsing
	// In production, use json.Unmarshal(body, v)
	return fmt.Errorf("JSON parsing not fully implemented in MVP")
}

func writeJSONResponse(w http.ResponseWriter, v interface{}) error {
	// For this implementation, we'll use basic formatting
	// In production, use json.NewEncoder(w).Encode(v)
	return fmt.Errorf("JSON encoding not fully implemented in MVP")
}
