package s3compat

import (
	"encoding/xml"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// Object Lock XML structures
type ObjectLockConfiguration struct {
	XMLName           xml.Name `xml:"ObjectLockConfiguration"`
	ObjectLockEnabled string   `xml:"ObjectLockEnabled"`
	Rule              *ObjectLockRule `xml:"Rule,omitempty"`
}

type ObjectLockRule struct {
	DefaultRetention *DefaultRetention `xml:"DefaultRetention"`
}

type DefaultRetention struct {
	Mode  string `xml:"Mode"`
	Days  int    `xml:"Days,omitempty"`
	Years int    `xml:"Years,omitempty"`
}

type ObjectRetention struct {
	XMLName         xml.Name  `xml:"Retention"`
	Mode            string    `xml:"Mode"`
	RetainUntilDate time.Time `xml:"RetainUntilDate"`
}

type ObjectLegalHold struct {
	XMLName xml.Name `xml:"LegalHold"`
	Status  string   `xml:"Status"`
}

// GetObjectRetention retrieves the object retention configuration
func (h *Handler) GetObjectRetention(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: GetObjectRetention")

	bucketPath := h.getBucketPath(r, bucketName)
	retention, err := h.objectManager.GetObjectRetention(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		if err == object.ErrNoRetentionConfiguration {
			h.writeError(w, "NoSuchObjectLockConfiguration", "The specified object does not have a retention configuration", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	if retention == nil {
		h.writeError(w, "NoSuchObjectLockConfiguration", "The specified object does not have a retention configuration", objectKey, r)
		return
	}

	xmlRetention := ObjectRetention{
		Mode:            retention.Mode,
		RetainUntilDate: retention.RetainUntilDate,
	}

	h.writeXMLResponse(w, http.StatusOK, xmlRetention)
}

// PutObjectRetention sets the object retention configuration
func (h *Handler) PutObjectRetention(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: PutObjectRetention")

	// Parse the XML retention configuration
	var xmlRetention ObjectRetention
	if err := xml.NewDecoder(r.Body).Decode(&xmlRetention); err != nil {
		h.writeError(w, "MalformedXML", "The XML is not well-formed", objectKey, r)
		return
	}
	defer r.Body.Close()

	// Validate retention mode
	if xmlRetention.Mode != "GOVERNANCE" && xmlRetention.Mode != "COMPLIANCE" {
		h.writeError(w, "InvalidArgument", "Invalid retention mode. Must be GOVERNANCE or COMPLIANCE", objectKey, r)
		return
	}

	// Validate retain until date is in the future
	if xmlRetention.RetainUntilDate.Before(time.Now()) {
		h.writeError(w, "InvalidArgument", "Retain until date must be in the future", objectKey, r)
		return
	}

	// Convert to internal structure
	retention := &object.RetentionConfig{
		Mode:            xmlRetention.Mode,
		RetainUntilDate: xmlRetention.RetainUntilDate,
	}

	bucketPath := h.getBucketPath(r, bucketName)
	// Set the retention
	if err := h.objectManager.SetObjectRetention(r.Context(), bucketPath, objectKey, retention); err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		if err == object.ErrRetentionLocked {
			h.writeError(w, "AccessDenied", "The retention period is locked and cannot be modified", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetObjectLegalHold retrieves the object legal hold status
func (h *Handler) GetObjectLegalHold(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: GetObjectLegalHold")

	bucketPath := h.getBucketPath(r, bucketName)
	legalHold, err := h.objectManager.GetObjectLegalHold(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	status := "OFF"
	if legalHold != nil && legalHold.Status == "ON" {
		status = "ON"
	}

	xmlLegalHold := ObjectLegalHold{
		Status: status,
	}

	h.writeXMLResponse(w, http.StatusOK, xmlLegalHold)
}

// PutObjectLegalHold sets the object legal hold status
func (h *Handler) PutObjectLegalHold(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: PutObjectLegalHold")

	// Parse the XML legal hold configuration
	var xmlLegalHold ObjectLegalHold
	if err := xml.NewDecoder(r.Body).Decode(&xmlLegalHold); err != nil {
		h.writeError(w, "MalformedXML", "The XML is not well-formed", objectKey, r)
		return
	}
	defer r.Body.Close()

	// Validate status
	if xmlLegalHold.Status != "ON" && xmlLegalHold.Status != "OFF" {
		h.writeError(w, "InvalidArgument", "Invalid legal hold status. Must be ON or OFF", objectKey, r)
		return
	}

	// Convert to internal structure
	legalHold := &object.LegalHoldConfig{
		Status: xmlLegalHold.Status,
	}

	bucketPath := h.getBucketPath(r, bucketName)
	// Set the legal hold
	if err := h.objectManager.SetObjectLegalHold(r.Context(), bucketPath, objectKey, legalHold); err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Object Tagging XML structures
type Tagging struct {
	XMLName xml.Name `xml:"Tagging"`
	TagSet  TagSet   `xml:"TagSet"`
}

type TagSet struct {
	Tags []Tag `xml:"Tag"`
}

type Tag struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

// GetObjectTagging retrieves the object tags
func (h *Handler) GetObjectTagging(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: GetObjectTagging")

	bucketPath := h.getBucketPath(r, bucketName)
	obj, err := h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Convert tags to XML structure
	xmlTagging := Tagging{
		TagSet: TagSet{
			Tags: make([]Tag, 0),
		},
	}

	if obj.Tags != nil {
		for _, tag := range obj.Tags.Tags {
			xmlTagging.TagSet.Tags = append(xmlTagging.TagSet.Tags, Tag{
				Key:   tag.Key,
				Value: tag.Value,
			})
		}
	}

	h.writeXMLResponse(w, http.StatusOK, xmlTagging)
}

// PutObjectTagging sets the object tags
func (h *Handler) PutObjectTagging(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: PutObjectTagging")

	// Parse the XML tagging configuration
	var xmlTagging Tagging
	if err := xml.NewDecoder(r.Body).Decode(&xmlTagging); err != nil {
		h.writeError(w, "MalformedXML", "The XML is not well-formed", objectKey, r)
		return
	}
	defer r.Body.Close()

	// Convert to internal tags structure
	tags := &object.TagSet{
		Tags: make([]object.Tag, len(xmlTagging.TagSet.Tags)),
	}
	for i, tag := range xmlTagging.TagSet.Tags {
		if tag.Key == "" {
			h.writeError(w, "InvalidTag", "Tag key cannot be empty", objectKey, r)
			return
		}
		tags.Tags[i] = object.Tag{
			Key:   tag.Key,
			Value: tag.Value,
		}
	}

	bucketPath := h.getBucketPath(r, bucketName)
	// Get current object metadata
	obj, err := h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Update tags
	obj.Tags = tags

	// Update object metadata with new tags
	if err := h.objectManager.UpdateObjectMetadata(r.Context(), bucketPath, objectKey, obj.Metadata); err != nil {
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteObjectTagging removes all tags from the object
func (h *Handler) DeleteObjectTagging(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: DeleteObjectTagging")

	bucketPath := h.getBucketPath(r, bucketName)
	// Get current object metadata
	obj, err := h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Clear tags
	obj.Tags = &object.TagSet{Tags: make([]object.Tag, 0)}

	// Update object metadata
	if err := h.objectManager.UpdateObjectMetadata(r.Context(), bucketPath, objectKey, obj.Metadata); err != nil {
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Object ACL XML structures
type AccessControlPolicy struct {
	XMLName           xml.Name          `xml:"AccessControlPolicy"`
	Owner             Owner             `xml:"Owner"`
	AccessControlList AccessControlList `xml:"AccessControlList"`
}

type AccessControlList struct {
	Grants []Grant `xml:"Grant"`
}

type Grant struct {
	Grantee    Grantee `xml:"Grantee"`
	Permission string  `xml:"Permission"`
}

type Grantee struct {
	Type         string `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	ID           string `xml:"ID,omitempty"`
	DisplayName  string `xml:"DisplayName,omitempty"`
	EmailAddress string `xml:"EmailAddress,omitempty"`
	URI          string `xml:"URI,omitempty"`
}

// GetObjectACL retrieves the object ACL
func (h *Handler) GetObjectACL(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: GetObjectACL")

	bucketPath := h.getBucketPath(r, bucketName)
	// Check if object exists
	_, err := h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Return default ACL (private to owner)
	acl := AccessControlPolicy{
		Owner: Owner{
			ID:          "maxiofs",
			DisplayName: "MaxIOFS",
		},
		AccessControlList: AccessControlList{
			Grants: []Grant{
				{
					Grantee: Grantee{
						Type:        "CanonicalUser",
						ID:          "maxiofs",
						DisplayName: "MaxIOFS",
					},
					Permission: "FULL_CONTROL",
				},
			},
		},
	}

	h.writeXMLResponse(w, http.StatusOK, acl)
}

// PutObjectACL sets the object ACL
func (h *Handler) PutObjectACL(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
	}).Debug("S3 API: PutObjectACL")

	bucketPath := h.getBucketPath(r, bucketName)
	// Check if object exists
	_, err := h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Parse the XML ACL (basic validation)
	var acl AccessControlPolicy
	if err := xml.NewDecoder(r.Body).Decode(&acl); err != nil {
		h.writeError(w, "MalformedXML", "The XML is not well-formed", objectKey, r)
		return
	}
	defer r.Body.Close()

	// For MVP, we just accept the ACL but don't enforce it
	// Full ACL implementation would require storing and enforcing permissions
	logrus.WithField("object", objectKey).Debug("ACL set (not enforced in MVP)")

	w.WriteHeader(http.StatusOK)
}

// CopyObject copies an object from one location to another
func (h *Handler) CopyObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	destBucket := vars["bucket"]
	destKey := vars["object"]

	// Parse source from x-amz-copy-source header
	copySource := r.Header.Get("x-amz-copy-source")
	if copySource == "" {
		h.writeError(w, "InvalidArgument", "Copy source header is required", destKey, r)
		return
	}

	logrus.WithFields(logrus.Fields{
		"source": copySource,
		"dest_bucket": destBucket,
		"dest_key": destKey,
	}).Debug("S3 API: CopyObject")

	// Parse source bucket and key from copy source
	// Format: /source-bucket/source-key
	if len(copySource) < 2 || copySource[0] != '/' {
		h.writeError(w, "InvalidArgument", "Invalid copy source format", destKey, r)
		return
	}

	copySource = copySource[1:] // Remove leading slash
	slashIdx := 0
	for i, c := range copySource {
		if c == '/' {
			slashIdx = i
			break
		}
	}

	if slashIdx == 0 {
		h.writeError(w, "InvalidArgument", "Invalid copy source format", destKey, r)
		return
	}

	sourceBucket := copySource[:slashIdx]
	sourceKey := copySource[slashIdx+1:]

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

	// Copy metadata
	headers := make(http.Header)
	headers.Set("Content-Type", sourceObj.ContentType)
	for k, v := range sourceObj.Metadata {
		headers.Set(k, v)
	}

	destBucketPath := h.getBucketPath(r, destBucket)
	// Put object at destination
	destObj, err := h.objectManager.PutObject(r.Context(), destBucketPath, destKey, reader, headers)
	if err != nil {
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The destination bucket does not exist", destBucket, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), destKey, r)
		return
	}

	// Return copy result
	type CopyObjectResult struct {
		XMLName      xml.Name  `xml:"CopyObjectResult"`
		LastModified time.Time `xml:"LastModified"`
		ETag         string    `xml:"ETag"`
	}

	result := CopyObjectResult{
		LastModified: destObj.LastModified,
		ETag:         destObj.ETag,
	}

	h.writeXMLResponse(w, http.StatusOK, result)
}
