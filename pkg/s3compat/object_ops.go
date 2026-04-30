package s3compat

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// Object Lock XML structures
type ObjectLockConfiguration struct {
	XMLName           xml.Name        `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ObjectLockConfiguration"`
	ObjectLockEnabled string          `xml:"ObjectLockEnabled"`
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
	versionID := r.URL.Query().Get("versionId")
	retention, err := h.objectManager.GetObjectRetention(r.Context(), bucketPath, objectKey, versionID)
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
	versionID := r.URL.Query().Get("versionId")
	// Set the retention, targeting a specific version if versionId is provided
	if err := h.objectManager.SetObjectRetention(r.Context(), bucketPath, objectKey, retention, versionID); err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		if err == object.ErrRetentionLocked {
			h.writeError(w, "AccessDenied", "The retention period is locked and cannot be modified", objectKey, r)
			return
		}
		if err == object.ErrCannotShortenCompliance {
			h.writeError(w, "AccessDenied", "Cannot shorten retention period for COMPLIANCE mode Object Lock", objectKey, r)
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
	versionID := r.URL.Query().Get("versionId")
	legalHold, err := h.objectManager.GetObjectLegalHold(r.Context(), bucketPath, objectKey, versionID)
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
	versionID := r.URL.Query().Get("versionId")
	// Set the legal hold, targeting a specific version if versionId is provided
	if err := h.objectManager.SetObjectLegalHold(r.Context(), bucketPath, objectKey, legalHold, versionID); err != nil {
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

// getObjectAttributesResponse is the AWS XML response for GetObjectAttributes.
type getObjectAttributesResponse struct {
	XMLName      xml.Name               `xml:"GetObjectAttributesResponse"`
	ETag         string                 `xml:"ETag,omitempty"`
	StorageClass string                 `xml:"StorageClass,omitempty"`
	ObjectSize   *int64                 `xml:"ObjectSize,omitempty"`
	Checksum     *objectAttributesCksum `xml:"Checksum,omitempty"`
	ObjectParts  *objectAttributesParts `xml:"ObjectParts,omitempty"`
}

type objectAttributesCksum struct {
	ChecksumCRC32  string `xml:"ChecksumCRC32,omitempty"`
	ChecksumCRC32C string `xml:"ChecksumCRC32C,omitempty"`
	ChecksumSHA1   string `xml:"ChecksumSHA1,omitempty"`
	ChecksumSHA256 string `xml:"ChecksumSHA256,omitempty"`
}

type objectAttributesParts struct {
	TotalPartsCount int `xml:"TotalPartsCount"`
}

// GetObjectAttributes returns object metadata without downloading the object body.
// The caller specifies which attributes are needed via x-amz-object-attributes header.
func (h *Handler) GetObjectAttributes(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)
	tenantID := h.resolveBucketTenantID(r, bucketName)
	bucketPath := h.getBucketPath(r, bucketName)

	user, userExists := auth.GetUserFromContext(r.Context())
	if !h.validateHeadBucketReadPermission(w, r, user, userExists, tenantID, bucketName, objectKey) {
		return
	}

	// Optional versionId
	versionID := r.URL.Query().Get("versionId")

	var obj *object.Object
	var err error
	if versionID != "" {
		var reader io.ReadCloser
		obj, reader, err = h.objectManager.GetObject(r.Context(), bucketPath, objectKey, versionID)
		if reader != nil {
			reader.Close()
		}
	} else {
		obj, err = h.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
	}
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Parse requested attributes (comma-separated)
	requested := make(map[string]bool)
	for _, attr := range strings.Split(r.Header.Get("x-amz-object-attributes"), ",") {
		requested[strings.TrimSpace(attr)] = true
	}

	resp := getObjectAttributesResponse{}

	if requested["ETag"] {
		resp.ETag = obj.ETag
	}
	if requested["StorageClass"] {
		resp.StorageClass = obj.StorageClass
	}
	if requested["ObjectSize"] {
		size := obj.Size
		resp.ObjectSize = &size
	}
	if requested["Checksum"] && obj.ChecksumAlgorithm != "" && obj.ChecksumValue != "" {
		ck := &objectAttributesCksum{}
		switch strings.ToUpper(obj.ChecksumAlgorithm) {
		case "CRC32":
			ck.ChecksumCRC32 = obj.ChecksumValue
		case "CRC32C":
			ck.ChecksumCRC32C = obj.ChecksumValue
		case "SHA1":
			ck.ChecksumSHA1 = obj.ChecksumValue
		case "SHA256":
			ck.ChecksumSHA256 = obj.ChecksumValue
		}
		resp.Checksum = ck
	}
	if requested["ObjectParts"] {
		// Infer part count from multipart ETag format: <md5>-<N>
		if idx := strings.LastIndex(obj.ETag, "-"); idx >= 0 {
			partStr := obj.ETag[idx+1:]
			partCount := 0
			for _, ch := range partStr {
				if ch < '0' || ch > '9' {
					partCount = 0
					break
				}
				partCount = partCount*10 + int(ch-'0')
			}
			if partCount > 0 {
				resp.ObjectParts = &objectAttributesParts{TotalPartsCount: partCount}
			}
		}
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))
	if obj.VersionID != "" {
		w.Header().Set("x-amz-version-id", obj.VersionID)
	}
	if obj.SSEAlgorithm != "" {
		w.Header().Set("x-amz-server-side-encryption", obj.SSEAlgorithm)
	}
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(resp) //nolint:errcheck
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
	versionID := r.URL.Query().Get("versionId")

	// Use GetObjectTagging for consistency and clarity
	tags, err := h.objectManager.GetObjectTagging(r.Context(), bucketPath, objectKey, versionID)
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

	if tags != nil {
		for _, tag := range tags.Tags {
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

	if h.authManager != nil && !auth.CheckCapabilityInContext(r.Context(), h.authManager, auth.CapObjectManageTags) {
		h.writeError(w, "AccessDenied", "You do not have permission to manage object tags", objectKey, r)
		return
	}

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
	versionID := r.URL.Query().Get("versionId")

	// FIX: Use SetObjectTagging instead of UpdateObjectMetadata
	// SetObjectTagging properly saves tags to the metadata store
	if err := h.objectManager.SetObjectTagging(r.Context(), bucketPath, objectKey, tags, versionID); err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
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

	if h.authManager != nil && !auth.CheckCapabilityInContext(r.Context(), h.authManager, auth.CapObjectManageTags) {
		h.writeError(w, "AccessDenied", "You do not have permission to manage object tags", objectKey, r)
		return
	}

	bucketPath := h.getBucketPath(r, bucketName)
	versionID := r.URL.Query().Get("versionId")

	// FIX: Use DeleteObjectTagging instead of UpdateObjectMetadata
	if err := h.objectManager.DeleteObjectTagging(r.Context(), bucketPath, objectKey, versionID); err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
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
	versionID := r.URL.Query().Get("versionId")

	// Get ACL from object manager
	aclData, err := h.objectManager.GetObjectACL(r.Context(), bucketPath, objectKey, versionID)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// Convert object.ACL to S3 XML format
	// The object manager already returns object.ACL, we need to convert to S3 format
	s3ACL := AccessControlPolicy{
		Owner: Owner{
			ID:          aclData.Owner.ID,
			DisplayName: aclData.Owner.DisplayName,
		},
		AccessControlList: AccessControlList{
			Grants: make([]Grant, len(aclData.Grants)),
		},
	}

	for i, grant := range aclData.Grants {
		s3ACL.AccessControlList.Grants[i] = Grant{
			Grantee: Grantee{
				Type:         grant.Grantee.Type,
				ID:           grant.Grantee.ID,
				DisplayName:  grant.Grantee.DisplayName,
				EmailAddress: grant.Grantee.EmailAddress,
				URI:          grant.Grantee.URI,
			},
			Permission: grant.Permission,
		}
	}

	h.writeXMLResponse(w, http.StatusOK, s3ACL)
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
	versionID := r.URL.Query().Get("versionId")

	// Check for canned ACL header
	cannedACL := r.Header.Get("x-amz-acl")

	var aclData *object.ACL

	if cannedACL != "" {
		// Use canned ACL
		if !acl.IsValidCannedACL(cannedACL) {
			h.writeError(w, "InvalidArgument", "Invalid canned ACL: "+cannedACL, objectKey, r)
			return
		}

		// Create ACL from canned ACL using helper function
		ownerID, ownerDisplayName := "maxiofs", "MaxIOFS"
		if user, ok := auth.GetUserFromContext(r.Context()); ok && user != nil {
			ownerID = user.ID
			ownerDisplayName = user.Username
		}
		grants := acl.GetCannedACLGrants(cannedACL, ownerID, ownerDisplayName)
		if grants == nil {
			h.writeError(w, "InvalidArgument", "Invalid canned ACL: "+cannedACL, objectKey, r)
			return
		}

		// Convert acl.Grant to object.Grant
		objectGrants := make([]object.Grant, len(grants))
		for i, g := range grants {
			objectGrants[i] = object.Grant{
				Grantee: object.Grantee{
					Type:         string(g.Grantee.Type),
					ID:           g.Grantee.ID,
					DisplayName:  g.Grantee.DisplayName,
					EmailAddress: g.Grantee.EmailAddress,
					URI:          g.Grantee.URI,
				},
				Permission: string(g.Permission),
			}
		}

		aclData = &object.ACL{
			Owner: object.Owner{
				ID:          ownerID,
				DisplayName: ownerDisplayName,
			},
			Grants: objectGrants,
		}
	} else {
		// Parse the XML ACL
		body, err := io.ReadAll(r.Body)
		if err != nil {
			h.writeError(w, "InvalidRequest", "Failed to read request body", objectKey, r)
			return
		}
		defer r.Body.Close()

		var s3ACL AccessControlPolicy
		if err := xml.Unmarshal(body, &s3ACL); err != nil {
			logrus.WithError(err).Error("PutObjectACL: Failed to parse XML")
			h.writeError(w, "MalformedXML", "The XML is not well-formed", objectKey, r)
			return
		}

		// Convert S3 ACL to object.ACL
		objectGrants := make([]object.Grant, len(s3ACL.AccessControlList.Grants))
		for i, grant := range s3ACL.AccessControlList.Grants {
			objectGrants[i] = object.Grant{
				Grantee: object.Grantee{
					Type:         grant.Grantee.Type,
					ID:           grant.Grantee.ID,
					DisplayName:  grant.Grantee.DisplayName,
					EmailAddress: grant.Grantee.EmailAddress,
					URI:          grant.Grantee.URI,
				},
				Permission: grant.Permission,
			}
		}

		aclData = &object.ACL{
			Owner: object.Owner{
				ID:          s3ACL.Owner.ID,
				DisplayName: s3ACL.Owner.DisplayName,
			},
			Grants: objectGrants,
		}
	}

	// Set ACL using object manager
	if err := h.objectManager.SetObjectACL(r.Context(), bucketPath, objectKey, aclData, versionID); err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

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
		"source":      copySource,
		"dest_bucket": destBucket,
		"dest_key":    destKey,
	}).Info("S3 API: CopyObject - received request")

	sourceBucket, sourceKey, copySourceVersionID, err := parseCopySourceHeader(copySource)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"copySource": copySource,
			"error":      err,
		}).Error("CopyObject: invalid copy source")
		h.writeError(w, "InvalidArgument", fmt.Sprintf("Invalid copy source format: %s", copySource), destKey, r)
		return
	}

	if sourceBucket == "" || sourceKey == "" {
		logrus.WithFields(logrus.Fields{
			"sourceBucket": sourceBucket,
			"sourceKey":    sourceKey,
		}).Error("CopyObject: Empty bucket or key")
		h.writeError(w, "InvalidArgument", "Invalid copy source: empty bucket or key", destKey, r)
		return
	}

	user, userExists := auth.GetUserFromContext(r.Context())
	sourceTenantID := h.resolveBucketTenantID(r, sourceBucket)
	destTenantID := h.resolveBucketTenantID(r, destBucket)

	if h.authManager != nil && userExists && r.Header.Get("Authorization") != "" {
		if !auth.CheckCapabilityInContext(r.Context(), h.authManager, auth.CapObjectDownload) {
			h.writeError(w, "AccessDenied", "You do not have permission to download objects", sourceKey, r)
			return
		}
		if !auth.CheckCapabilityInContext(r.Context(), h.authManager, auth.CapObjectUpload) {
			h.writeError(w, "AccessDenied", "You do not have permission to upload objects", destKey, r)
			return
		}
	}

	if !h.validateBucketReadPermission(w, r, user, userExists, false, false, "", sourceTenantID, sourceBucket, sourceKey) {
		return
	}
	if !h.validateBucketWritePermission(r, user, userExists, destTenantID, destBucket) {
		h.writeError(w, "AccessDenied", "Access Denied", destKey, r)
		return
	}

	sourceBucketPath := h.getBucketPath(r, sourceBucket)
	// Get source object, requesting a specific version if indicated in the copy source.
	var sourceObj *object.Object
	var reader io.ReadCloser
	if copySourceVersionID != "" {
		var getErr error
		sourceObj, reader, getErr = h.objectManager.GetObject(r.Context(), sourceBucketPath, sourceKey, copySourceVersionID)
		if getErr != nil {
			if getErr == object.ErrObjectNotFound {
				h.writeError(w, "NoSuchKey", "The specified source key does not exist", sourceKey, r)
				return
			}
			h.writeError(w, "InternalError", getErr.Error(), sourceKey, r)
			return
		}
	} else {
		var getErr error
		sourceObj, reader, getErr = h.objectManager.GetObject(r.Context(), sourceBucketPath, sourceKey)
		if getErr != nil {
			if getErr == object.ErrObjectNotFound {
				h.writeError(w, "NoSuchKey", "The specified source key does not exist", sourceKey, r)
				return
			}
			h.writeError(w, "InternalError", getErr.Error(), sourceKey, r)
			return
		}
	}
	defer reader.Close()

	if !h.validateObjectReadPermission(w, r, user, userExists, false, "", sourceTenantID, sourceBucketPath, sourceBucket, sourceKey) {
		return
	}

	logrus.WithFields(logrus.Fields{
		"source_bucket": sourceBucket,
		"source_key":    sourceKey,
		"dest_bucket":   destBucket,
		"dest_key":      destKey,
		"source_size":   sourceObj.Size,
		"source_etag":   sourceObj.ETag,
	}).Info("CopyObject: Starting copy operation")

	if !h.validateCopySourceConditionals(w, r, sourceObj, sourceKey) {
		return
	}

	// Build destination metadata based on x-amz-metadata-directive (default: COPY).
	directive := strings.ToUpper(r.Header.Get("x-amz-metadata-directive"))
	if directive == "" {
		directive = "COPY"
	}
	if directive != "COPY" && directive != "REPLACE" {
		h.writeError(w, "InvalidArgument", "x-amz-metadata-directive must be COPY or REPLACE", destKey, r)
		return
	}

	taggingDirective := strings.ToUpper(r.Header.Get("x-amz-tagging-directive"))
	if taggingDirective == "" {
		taggingDirective = "COPY"
	}
	if taggingDirective != "COPY" && taggingDirective != "REPLACE" {
		h.writeError(w, "InvalidArgument", "x-amz-tagging-directive must be COPY or REPLACE", destKey, r)
		return
	}
	replacementTags, err := parseS3TaggingHeader(r.Header.Get("x-amz-tagging"))
	if err != nil {
		h.writeError(w, "InvalidTag", err.Error(), destKey, r)
		return
	}

	headers := make(http.Header)
	if directive == "REPLACE" {
		// Caller is setting fresh metadata — use headers from the request.
		ct := r.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		headers.Set("Content-Type", ct)
		for k, vals := range r.Header {
			lk := strings.ToLower(k)
			if strings.HasPrefix(lk, "x-amz-meta-") {
				headers[k] = vals
			}
		}
		// Preserve S3 system response headers from request if provided
		for _, h := range []string{"Content-Disposition", "Content-Encoding", "Cache-Control", "Content-Language"} {
			if v := r.Header.Get(h); v != "" {
				headers.Set(h, v)
			}
		}
	} else {
		// COPY — preserve source object metadata.
		headers.Set("Content-Type", sourceObj.ContentType)
		// Propagate S3 system response headers
		if sourceObj.ContentDisposition != "" {
			headers.Set("Content-Disposition", sourceObj.ContentDisposition)
		}
		if sourceObj.ContentEncoding != "" {
			headers.Set("Content-Encoding", sourceObj.ContentEncoding)
		}
		if sourceObj.CacheControl != "" {
			headers.Set("Cache-Control", sourceObj.CacheControl)
		}
		if sourceObj.ContentLanguage != "" {
			headers.Set("Content-Language", sourceObj.ContentLanguage)
		}
		// Propagate user-defined metadata
		for k, v := range sourceObj.Metadata {
			headers.Set("X-Amz-Meta-"+k, v)
		}
	}

	destBucketPath := h.getBucketPath(r, destBucket)
	// IMPORTANT: Use streaming copy instead of loading all data into memory
	// This prevents OOM errors and timeouts with large checkpoint files
	// The reader is directly passed to PutObject which handles the streaming internally
	destObj, err := h.objectManager.PutObject(r.Context(), destBucketPath, destKey, reader, headers)
	if err != nil {
		if err == object.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The destination bucket does not exist", destBucket, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), destKey, r)
		return
	}

	// Apply canned ACL from x-amz-acl header if present (e.g. --acl public-read on copy).
	if cannedACL := r.Header.Get("x-amz-acl"); cannedACL != "" {
		h.applyObjectCannedACLHeader(r.Context(), destBucketPath, destKey, cannedACL)
	}

	switch taggingDirective {
	case "COPY":
		if sourceTags, tagErr := h.objectManager.GetObjectTagging(r.Context(), sourceBucketPath, sourceKey, copySourceVersionID); tagErr == nil && sourceTags != nil && len(sourceTags.Tags) > 0 {
			if setErr := h.objectManager.SetObjectTagging(r.Context(), destBucketPath, destKey, sourceTags); setErr != nil {
				h.writeError(w, "InternalError", setErr.Error(), destKey, r)
				return
			}
		}
	case "REPLACE":
		if replacementTags != nil {
			if setErr := h.objectManager.SetObjectTagging(r.Context(), destBucketPath, destKey, replacementTags); setErr != nil {
				h.writeError(w, "InternalError", setErr.Error(), destKey, r)
				return
			}
		}
	}

	// Set version ID response headers before writing XML body
	// x-amz-version-id: version ID of the newly created destination object
	if destObj.VersionID != "" {
		w.Header().Set("x-amz-version-id", destObj.VersionID)
	}
	// x-amz-copy-source-version-id: version ID of the source object that was copied
	if sourceObj.VersionID != "" {
		w.Header().Set("x-amz-copy-source-version-id", sourceObj.VersionID)
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

	// Fire s3:ObjectCreated:Copy notification asynchronously.
	h.fireNotifications(r.Context(), destBucket, destTenantID, destKey, "s3:ObjectCreated:Copy", destObj.ETag, destObj.Size)
}

func parseCopySourceHeader(copySource string) (bucketName, objectKey, versionID string, err error) {
	if copySource == "" {
		return "", "", "", fmt.Errorf("copy source is empty")
	}
	if copySource[0] == '/' {
		copySource = copySource[1:]
	}

	slashIdx := strings.Index(copySource, "/")
	if slashIdx == -1 {
		return "", "", "", fmt.Errorf("missing bucket/key separator")
	}

	bucketName = copySource[:slashIdx]
	keyAndQuery := copySource[slashIdx+1:]
	if bucketName == "" || keyAndQuery == "" {
		return "", "", "", fmt.Errorf("empty bucket or key")
	}

	keyRaw := keyAndQuery
	if qIdx := strings.Index(keyAndQuery, "?"); qIdx >= 0 {
		keyRaw = keyAndQuery[:qIdx]
		queryValues, parseErr := url.ParseQuery(keyAndQuery[qIdx+1:])
		if parseErr != nil {
			return "", "", "", fmt.Errorf("invalid copy source query: %w", parseErr)
		}
		versionID = queryValues.Get("versionId")
	}

	objectKey, err = url.PathUnescape(keyRaw)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid source key encoding: %w", err)
	}
	if objectKey == "" {
		return "", "", "", fmt.Errorf("empty object key")
	}

	return bucketName, objectKey, versionID, nil
}

func (h *Handler) validateCopySourceConditionals(w http.ResponseWriter, r *http.Request, sourceObj *object.Object, sourceKey string) bool {
	// Evaluate copy-source conditional headers (AWS S3 spec §CopyObject).
	// All conditions are checked against the SOURCE object's ETag / LastModified.
	if ifMatch := r.Header.Get("x-amz-copy-source-if-match"); ifMatch != "" {
		want := strings.Trim(ifMatch, "\"")
		got := strings.Trim(sourceObj.ETag, "\"")
		if want != got {
			h.writeError(w, "PreconditionFailed", "At least one of the pre-conditions you specified did not hold", sourceKey, r)
			return false
		}
	}
	if ifNoneMatch := r.Header.Get("x-amz-copy-source-if-none-match"); ifNoneMatch != "" {
		want := strings.Trim(ifNoneMatch, "\"")
		got := strings.Trim(sourceObj.ETag, "\"")
		if want == got {
			h.writeError(w, "PreconditionFailed", "At least one of the pre-conditions you specified did not hold", sourceKey, r)
			return false
		}
	}
	if ifModifiedSince := r.Header.Get("x-amz-copy-source-if-modified-since"); ifModifiedSince != "" {
		t, parseErr := http.ParseTime(ifModifiedSince)
		if parseErr == nil && !sourceObj.LastModified.After(t) {
			h.writeError(w, "PreconditionFailed", "At least one of the pre-conditions you specified did not hold", sourceKey, r)
			return false
		}
	}
	if ifUnmodifiedSince := r.Header.Get("x-amz-copy-source-if-unmodified-since"); ifUnmodifiedSince != "" {
		t, parseErr := http.ParseTime(ifUnmodifiedSince)
		if parseErr == nil && sourceObj.LastModified.After(t) {
			h.writeError(w, "PreconditionFailed", "At least one of the pre-conditions you specified did not hold", sourceKey, r)
			return false
		}
	}
	return true
}
