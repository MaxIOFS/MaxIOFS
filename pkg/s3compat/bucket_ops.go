package s3compat

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/sirupsen/logrus"
)

// Note: We use bucket.Policy directly instead of defining our own structures

// GetBucketPolicy retrieves the bucket policy
func (h *Handler) GetBucketPolicy(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: GetBucketPolicy")

	tenantID := h.getTenantIDFromRequest(r)
	policy, err := h.bucketManager.GetBucketPolicy(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		if err == bucket.ErrPolicyNotFound {
			h.writeError(w, "NoSuchBucketPolicy", "The bucket policy does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	if policy == nil {
		h.writeError(w, "NoSuchBucketPolicy", "The bucket policy does not exist", bucketName, r)
		return
	}

	// Convert policy to JSON
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		h.writeError(w, "InternalError", "Failed to marshal policy", bucketName, r)
		return
	}

	// Return the policy as JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(policyJSON)
}

// PutBucketPolicy sets the bucket policy
func (h *Handler) PutBucketPolicy(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: PutBucketPolicy")

	// Read the policy document from request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logrus.WithError(err).Error("Failed to read request body")
		h.writeError(w, "InvalidRequest", "Failed to read request body", bucketName, r)
		return
	}
	defer r.Body.Close()

	// Strip UTF-8 BOM if present (PowerShell adds BOM by default)
	// Handle both normal BOM (EF BB BF) and double-encoded BOM (C3 AF C2 BB C2 BF)
	body = bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
	body = bytes.TrimPrefix(body, []byte{0xC3, 0xAF, 0xC2, 0xBB, 0xC2, 0xBF})

	// Validate JSON format
	var policyDoc bucket.Policy
	if err := json.Unmarshal(body, &policyDoc); err != nil {
		logrus.WithError(err).Error("PutBucketPolicy: Failed to parse policy JSON")
		h.writeError(w, "MalformedPolicy", "The policy is not valid JSON", bucketName, r)
		return
	}

	// Validate policy structure
	if policyDoc.Version == "" {
		h.writeError(w, "MalformedPolicy", "Policy must contain a Version field", bucketName, r)
		return
	}

	if len(policyDoc.Statement) == 0 {
		h.writeError(w, "MalformedPolicy", "Policy must contain at least one Statement", bucketName, r)
		return
	}

	// Set the policy
	tenantID := h.getTenantIDFromRequest(r)
	if err := h.bucketManager.SetBucketPolicy(r.Context(), tenantID, bucketName, &policyDoc); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteBucketPolicy removes the bucket policy
func (h *Handler) DeleteBucketPolicy(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: DeleteBucketPolicy")

	// Delete the policy by setting it to nil
	tenantID := h.getTenantIDFromRequest(r)
	if err := h.bucketManager.SetBucketPolicy(r.Context(), tenantID, bucketName, nil); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Bucket Lifecycle XML structures
type LifecycleConfiguration struct {
	XMLName xml.Name        `xml:"LifecycleConfiguration"`
	Rules   []LifecycleRule `xml:"Rule"`
}

type LifecycleRule struct {
	ID                             string                          `xml:"ID"`
	Status                         string                          `xml:"Status"`
	Prefix                         string                          `xml:"Prefix,omitempty"`
	Filter                         *LifecycleFilter                `xml:"Filter,omitempty"`
	Expiration                     *LifecycleExpiration            `xml:"Expiration,omitempty"`
	Transition                     []LifecycleTransition           `xml:"Transition,omitempty"`
	NoncurrentVersionExpiration    *NoncurrentVersionExpiration    `xml:"NoncurrentVersionExpiration,omitempty"`
	NoncurrentVersionTransition    []NoncurrentVersionTransition   `xml:"NoncurrentVersionTransition,omitempty"`
	AbortIncompleteMultipartUpload *AbortIncompleteMultipartUpload `xml:"AbortIncompleteMultipartUpload,omitempty"`
}

type LifecycleFilter struct {
	Prefix string              `xml:"Prefix,omitempty"`
	Tag    *LifecycleFilterTag `xml:"Tag,omitempty"`
	And    *LifecycleFilterAnd `xml:"And,omitempty"`
}

type LifecycleFilterTag struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

type LifecycleFilterAnd struct {
	Prefix string               `xml:"Prefix,omitempty"`
	Tags   []LifecycleFilterTag `xml:"Tag,omitempty"`
}

type LifecycleExpiration struct {
	Days                      int    `xml:"Days,omitempty"`
	Date                      string `xml:"Date,omitempty"`
	ExpiredObjectDeleteMarker bool   `xml:"ExpiredObjectDeleteMarker,omitempty"`
}

type LifecycleTransition struct {
	Days         int    `xml:"Days,omitempty"`
	Date         string `xml:"Date,omitempty"`
	StorageClass string `xml:"StorageClass"`
}

type NoncurrentVersionExpiration struct {
	NoncurrentDays int `xml:"NoncurrentDays"`
}

type NoncurrentVersionTransition struct {
	NoncurrentDays int    `xml:"NoncurrentDays"`
	StorageClass   string `xml:"StorageClass"`
}

type AbortIncompleteMultipartUpload struct {
	DaysAfterInitiation int `xml:"DaysAfterInitiation"`
}

// GetBucketLifecycle retrieves the bucket lifecycle configuration
func (h *Handler) GetBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: GetBucketLifecycle")

	tenantID := h.getTenantIDFromRequest(r)
	lifecycleConfig, err := h.bucketManager.GetLifecycle(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		if err == bucket.ErrLifecycleNotFound {
			h.writeError(w, "NoSuchLifecycleConfiguration", "The lifecycle configuration does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	if lifecycleConfig == nil || len(lifecycleConfig.Rules) == 0 {
		h.writeError(w, "NoSuchLifecycleConfiguration", "The lifecycle configuration does not exist", bucketName, r)
		return
	}

	// Convert internal lifecycle config to XML structure
	xmlConfig := LifecycleConfiguration{
		Rules: make([]LifecycleRule, len(lifecycleConfig.Rules)),
	}

	for i, rule := range lifecycleConfig.Rules {
		xmlRule := LifecycleRule{
			ID:     rule.ID,
			Status: rule.Status,
			Prefix: rule.Filter.Prefix,
		}

		if rule.Expiration != nil {
			exp := &LifecycleExpiration{}
			if rule.Expiration.Days != nil {
				exp.Days = *rule.Expiration.Days
			}
			if rule.Expiration.Date != nil {
				exp.Date = rule.Expiration.Date.Format(time.RFC3339)
			}
			if rule.Expiration.ExpiredObjectDeleteMarker != nil {
				exp.ExpiredObjectDeleteMarker = *rule.Expiration.ExpiredObjectDeleteMarker
			}
			xmlRule.Expiration = exp
		}

		if rule.NoncurrentVersionExpiration != nil {
			xmlRule.NoncurrentVersionExpiration = &NoncurrentVersionExpiration{
				NoncurrentDays: rule.NoncurrentVersionExpiration.NoncurrentDays,
			}
		}

		if rule.AbortIncompleteMultipartUpload != nil {
			xmlRule.AbortIncompleteMultipartUpload = &AbortIncompleteMultipartUpload{
				DaysAfterInitiation: rule.AbortIncompleteMultipartUpload.DaysAfterInitiation,
			}
		}

		xmlConfig.Rules[i] = xmlRule
	}

	h.writeXMLResponse(w, http.StatusOK, xmlConfig)
}

// PutBucketLifecycle sets the bucket lifecycle configuration
func (h *Handler) PutBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: PutBucketLifecycle")

	// Parse the XML lifecycle configuration
	var xmlConfig LifecycleConfiguration
	if err := xml.NewDecoder(r.Body).Decode(&xmlConfig); err != nil {
		h.writeError(w, "MalformedXML", "The XML is not well-formed", bucketName, r)
		return
	}
	defer r.Body.Close()

	// Validate lifecycle configuration
	if len(xmlConfig.Rules) == 0 {
		h.writeError(w, "InvalidRequest", "Lifecycle configuration must contain at least one rule", bucketName, r)
		return
	}

	// Convert XML structure to internal lifecycle config
	lifecycleConfig := &bucket.LifecycleConfig{
		Rules: make([]bucket.LifecycleRule, len(xmlConfig.Rules)),
	}

	for i, rule := range xmlConfig.Rules {
		// Resolve prefix from either the legacy top-level <Prefix> element (old-style)
		// or from the modern <Filter><Prefix> element (sent by aws-cli, Terraform, SDKv2).
		// Both represent the same concept; prefer the Filter field when it is present.
		prefix := rule.Prefix
		if prefix == "" && rule.Filter != nil {
			prefix = rule.Filter.Prefix
		}

		internalRule := bucket.LifecycleRule{
			ID:     rule.ID,
			Status: rule.Status,
			Filter: bucket.LifecycleFilter{
				Prefix: prefix,
			},
		}

		if rule.Expiration != nil {
			exp := &bucket.LifecycleExpiration{}
			if rule.Expiration.Days > 0 {
				days := rule.Expiration.Days
				exp.Days = &days
			}
			if rule.Expiration.Date != "" {
				parsedDate, err := time.Parse(time.RFC3339, rule.Expiration.Date)
				if err == nil {
					exp.Date = &parsedDate
				}
			}
			if rule.Expiration.ExpiredObjectDeleteMarker {
				expiredMarker := true
				exp.ExpiredObjectDeleteMarker = &expiredMarker
			}
			internalRule.Expiration = exp
		}

		if rule.NoncurrentVersionExpiration != nil {
			internalRule.NoncurrentVersionExpiration = &bucket.NoncurrentVersionExpiration{
				NoncurrentDays: rule.NoncurrentVersionExpiration.NoncurrentDays,
			}
		}

		if rule.AbortIncompleteMultipartUpload != nil {
			internalRule.AbortIncompleteMultipartUpload = &bucket.LifecycleAbortIncompleteMultipartUpload{
				DaysAfterInitiation: rule.AbortIncompleteMultipartUpload.DaysAfterInitiation,
			}
		}

		lifecycleConfig.Rules[i] = internalRule
	}

	// Set the lifecycle configuration
	tenantID := h.getTenantIDFromRequest(r)
	if err := h.bucketManager.SetLifecycle(r.Context(), tenantID, bucketName, lifecycleConfig); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketLifecycle removes the bucket lifecycle configuration
func (h *Handler) DeleteBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: DeleteBucketLifecycle")

	// Delete the lifecycle configuration by setting it to nil
	tenantID := h.getTenantIDFromRequest(r)
	if err := h.bucketManager.SetLifecycle(r.Context(), tenantID, bucketName, nil); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Bucket CORS XML structures
type CORSConfiguration struct {
	XMLName   xml.Name   `xml:"CORSConfiguration"`
	CORSRules []CORSRule `xml:"CORSRule"`
}

type CORSRule struct {
	AllowedOrigins []string `xml:"AllowedOrigin"`
	AllowedMethods []string `xml:"AllowedMethod"`
	AllowedHeaders []string `xml:"AllowedHeader,omitempty"`
	ExposeHeaders  []string `xml:"ExposeHeader,omitempty"`
	MaxAgeSeconds  int      `xml:"MaxAgeSeconds,omitempty"`
}

// GetBucketCORS retrieves the bucket CORS configuration
func (h *Handler) GetBucketCORS(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: GetBucketCORS")

	tenantID := h.getTenantIDFromRequest(r)
	corsConfig, err := h.bucketManager.GetCORS(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		if err == bucket.ErrCORSNotFound {
			h.writeError(w, "NoSuchCORSConfiguration", "The CORS configuration does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	if corsConfig == nil || len(corsConfig.CORSRules) == 0 {
		h.writeError(w, "NoSuchCORSConfiguration", "The CORS configuration does not exist", bucketName, r)
		return
	}

	// Convert internal CORS config to XML structure
	xmlConfig := CORSConfiguration{
		CORSRules: make([]CORSRule, len(corsConfig.CORSRules)),
	}

	for i, rule := range corsConfig.CORSRules {
		xmlRule := CORSRule{
			AllowedOrigins: rule.AllowedOrigins,
			AllowedMethods: rule.AllowedMethods,
			AllowedHeaders: rule.AllowedHeaders,
			ExposeHeaders:  rule.ExposeHeaders,
		}
		if rule.MaxAgeSeconds != nil {
			xmlRule.MaxAgeSeconds = *rule.MaxAgeSeconds
		}
		xmlConfig.CORSRules[i] = xmlRule
	}

	h.writeXMLResponse(w, http.StatusOK, xmlConfig)
}

// PutBucketCORS sets the bucket CORS configuration
func (h *Handler) PutBucketCORS(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: PutBucketCORS")

	// Parse the XML CORS configuration
	var xmlConfig CORSConfiguration
	if err := xml.NewDecoder(r.Body).Decode(&xmlConfig); err != nil {
		h.writeError(w, "MalformedXML", "The XML is not well-formed", bucketName, r)
		return
	}
	defer r.Body.Close()

	// Validate CORS configuration
	if len(xmlConfig.CORSRules) == 0 {
		h.writeError(w, "InvalidRequest", "CORS configuration must contain at least one rule", bucketName, r)
		return
	}

	// Convert XML structure to internal CORS config
	corsConfig := &bucket.CORSConfig{
		CORSRules: make([]bucket.CORSRule, len(xmlConfig.CORSRules)),
	}

	for i, rule := range xmlConfig.CORSRules {
		// Validate required fields
		if len(rule.AllowedOrigins) == 0 {
			h.writeError(w, "InvalidRequest", "Each CORS rule must have at least one AllowedOrigin", bucketName, r)
			return
		}
		if len(rule.AllowedMethods) == 0 {
			h.writeError(w, "InvalidRequest", "Each CORS rule must have at least one AllowedMethod", bucketName, r)
			return
		}

		internalRule := bucket.CORSRule{
			AllowedOrigins: rule.AllowedOrigins,
			AllowedMethods: rule.AllowedMethods,
			AllowedHeaders: rule.AllowedHeaders,
			ExposeHeaders:  rule.ExposeHeaders,
		}
		if rule.MaxAgeSeconds > 0 {
			maxAge := rule.MaxAgeSeconds
			internalRule.MaxAgeSeconds = &maxAge
		}
		corsConfig.CORSRules[i] = internalRule
	}

	// Set the CORS configuration
	tenantID := h.getTenantIDFromRequest(r)
	if err := h.bucketManager.SetCORS(r.Context(), tenantID, bucketName, corsConfig); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketCORS removes the bucket CORS configuration
func (h *Handler) DeleteBucketCORS(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: DeleteBucketCORS")

	// Delete the CORS configuration by setting it to nil
	tenantID := h.getTenantIDFromRequest(r)
	if err := h.bucketManager.SetCORS(r.Context(), tenantID, bucketName, nil); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetBucketTagging retrieves the bucket tags
func (h *Handler) GetBucketTagging(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: GetBucketTagging")

	tenantID := h.getTenantIDFromRequest(r)
	bucketData, err := h.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	// Build TagSet response
	type Tag struct {
		Key   string `xml:"Key"`
		Value string `xml:"Value"`
	}

	type TagSet struct {
		XMLName xml.Name `xml:"TagSet"`
		Tags    []Tag    `xml:"Tag"`
	}

	type Tagging struct {
		XMLName xml.Name `xml:"Tagging"`
		TagSet  TagSet   `xml:"TagSet"`
	}

	response := Tagging{
		TagSet: TagSet{
			Tags: []Tag{},
		},
	}

	if bucketData.Tags != nil && len(bucketData.Tags) > 0 {
		for key, value := range bucketData.Tags {
			response.TagSet.Tags = append(response.TagSet.Tags, Tag{
				Key:   key,
				Value: value,
			})
		}
	}

	h.writeXMLResponse(w, http.StatusOK, response)
}

// PutBucketTagging sets the bucket tags
func (h *Handler) PutBucketTagging(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: PutBucketTagging")

	// Read the tagging XML from request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logrus.WithError(err).Error("Failed to read request body")
		h.writeError(w, "InvalidRequest", "Failed to read request body", bucketName, r)
		return
	}
	defer r.Body.Close()

	// Parse XML
	type Tag struct {
		Key   string `xml:"Key"`
		Value string `xml:"Value"`
	}

	type TagSet struct {
		Tags []Tag `xml:"Tag"`
	}

	type Tagging struct {
		XMLName xml.Name `xml:"Tagging"`
		TagSet  TagSet   `xml:"TagSet"`
	}

	var tagging Tagging
	if err := xml.Unmarshal(body, &tagging); err != nil {
		logrus.WithError(err).Error("PutBucketTagging: Failed to parse XML")
		h.writeError(w, "MalformedXML", "The XML is not well-formed", bucketName, r)
		return
	}

	// Convert to map
	tags := make(map[string]string)
	for _, tag := range tagging.TagSet.Tags {
		tags[tag.Key] = tag.Value
	}

	// Update bucket tags
	tenantID := h.getTenantIDFromRequest(r)
	if err := h.bucketManager.SetBucketTags(r.Context(), tenantID, bucketName, tags); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteBucketTagging deletes all bucket tags
func (h *Handler) DeleteBucketTagging(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: DeleteBucketTagging")

	tenantID := h.getTenantIDFromRequest(r)
	if err := h.bucketManager.SetBucketTags(r.Context(), tenantID, bucketName, nil); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetBucketACL retrieves the bucket ACL
func (h *Handler) GetBucketACL(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: GetBucketACL")

	tenantID := h.getTenantIDFromRequest(r)

	// Get ACL from bucket manager
	aclInterface, err := h.bucketManager.GetBucketACL(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	// Convert to internal ACL type
	aclData, ok := aclInterface.(*acl.ACL)
	if !ok || aclData == nil {
		h.writeError(w, "InternalError", "Invalid ACL data", bucketName, r)
		return
	}

	// Convert to S3 XML format
	s3ACL := aclData.ToS3Format()

	h.writeXMLResponse(w, http.StatusOK, s3ACL)
}

// PutBucketACL sets the bucket ACL
func (h *Handler) PutBucketACL(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: PutBucketACL")

	tenantID := h.getTenantIDFromRequest(r)

	// Check for canned ACL header
	cannedACL := r.Header.Get("x-amz-acl")

	var aclData *acl.ACL

	if cannedACL != "" {
		// Use canned ACL
		if !acl.IsValidCannedACL(cannedACL) {
			h.writeError(w, "InvalidArgument", "Invalid canned ACL: "+cannedACL, bucketName, r)
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
			h.writeError(w, "InvalidArgument", "Invalid canned ACL: "+cannedACL, bucketName, r)
			return
		}

		aclData = &acl.ACL{
			Owner: acl.Owner{
				ID:          ownerID,
				DisplayName: ownerDisplayName,
			},
			Grants:    grants,
			CannedACL: cannedACL,
		}
	} else {
		// Parse XML ACL from body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			h.writeError(w, "InvalidRequest", "Failed to read request body", bucketName, r)
			return
		}
		defer r.Body.Close()

		var s3ACL acl.S3AccessControlPolicy
		if err := xml.Unmarshal(body, &s3ACL); err != nil {
			logrus.WithError(err).Error("PutBucketACL: Failed to parse XML")
			h.writeError(w, "MalformedXML", "The XML is not well-formed", bucketName, r)
			return
		}

		// Convert from S3 format to internal format
		aclData = acl.FromS3Format(&s3ACL)
	}

	// Set ACL using bucket manager
	if err := h.bucketManager.SetBucketACL(r.Context(), tenantID, bucketName, aclData); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------------------
// Bucket sub-resource stubs — these sub-resources are not implemented by
// MaxIOFS but must return well-formed AWS-compatible responses so that tools
// like aws-cli, Terraform, and SDK probes do not fall through to ListObjects.
// ---------------------------------------------------------------------------

// notifXMLTarget is the XML representation of a single notification target.
type notifXMLTarget struct {
	ID       string         `xml:"Id,omitempty"`
	Endpoint string         `xml:",omitempty"` // TopicArn / Queue / CloudFunction depending on parent element
	Events   []string       `xml:"Event"`
	Filter   *notifXMLFilter `xml:"Filter,omitempty"`
}
type notifXMLFilter struct {
	Rules []notifXMLFilterRule `xml:"S3Key>FilterRule"`
}
type notifXMLFilterRule struct {
	Name  string `xml:"Name"`
	Value string `xml:"Value"`
}
type notificationConfigurationXML struct {
	XMLName              xml.Name         `xml:"NotificationConfiguration"`
	TopicConfigurations  []notifXMLTopic  `xml:"TopicConfiguration"`
	QueueConfigurations  []notifXMLQueue  `xml:"QueueConfiguration"`
	LambdaConfigurations []notifXMLLambda `xml:"CloudFunctionConfiguration"`
}
type notifXMLTopic struct {
	ID     string          `xml:"Id,omitempty"`
	Topic  string          `xml:"Topic"`
	Events []string        `xml:"Event"`
	Filter *notifXMLFilter `xml:"Filter,omitempty"`
}
type notifXMLQueue struct {
	ID     string          `xml:"Id,omitempty"`
	Queue  string          `xml:"Queue"`
	Events []string        `xml:"Event"`
	Filter *notifXMLFilter `xml:"Filter,omitempty"`
}
type notifXMLLambda struct {
	ID            string          `xml:"Id,omitempty"`
	CloudFunction string          `xml:"CloudFunction"`
	Events        []string        `xml:"Event"`
	Filter        *notifXMLFilter `xml:"Filter,omitempty"`
}

// notifFilterFromXML converts the XML filter to prefix/suffix strings.
func notifFilterFromXML(f *notifXMLFilter) *bucket.NotificationFilter {
	if f == nil {
		return nil
	}
	nf := &bucket.NotificationFilter{}
	for _, rule := range f.Rules {
		switch strings.ToLower(rule.Name) {
		case "prefix":
			nf.Prefix = rule.Value
		case "suffix":
			nf.Suffix = rule.Value
		}
	}
	return nf
}

// notifFilterToXML converts prefix/suffix to the XML filter structure.
func notifFilterToXML(f *bucket.NotificationFilter) *notifXMLFilter {
	if f == nil || (f.Prefix == "" && f.Suffix == "") {
		return nil
	}
	var rules []notifXMLFilterRule
	if f.Prefix != "" {
		rules = append(rules, notifXMLFilterRule{Name: "prefix", Value: f.Prefix})
	}
	if f.Suffix != "" {
		rules = append(rules, notifXMLFilterRule{Name: "suffix", Value: f.Suffix})
	}
	return &notifXMLFilter{Rules: rules}
}

// GetBucketNotification returns the stored notification configuration for a bucket.
func (h *Handler) GetBucketNotification(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	io.Copy(io.Discard, r.Body) //nolint:errcheck

	tenantID := h.getTenantIDFromRequest(r)
	cfg, err := h.bucketManager.GetNotification(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	out := notificationConfigurationXML{}
	if cfg != nil {
		for _, t := range cfg.TopicConfigurations {
			out.TopicConfigurations = append(out.TopicConfigurations, notifXMLTopic{
				ID: t.ID, Topic: t.Endpoint, Events: t.Events, Filter: notifFilterToXML(t.Filter),
			})
		}
		for _, t := range cfg.QueueConfigurations {
			out.QueueConfigurations = append(out.QueueConfigurations, notifXMLQueue{
				ID: t.ID, Queue: t.Endpoint, Events: t.Events, Filter: notifFilterToXML(t.Filter),
			})
		}
		for _, t := range cfg.LambdaConfigurations {
			out.LambdaConfigurations = append(out.LambdaConfigurations, notifXMLLambda{
				ID: t.ID, CloudFunction: t.Endpoint, Events: t.Events, Filter: notifFilterToXML(t.Filter),
			})
		}
	}
	h.writeXMLResponse(w, http.StatusOK, out)
}

// PutBucketNotification stores the notification configuration for a bucket.
func (h *Handler) PutBucketNotification(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, "MalformedXML", "Failed to read request body", bucketName, r)
		return
	}

	var xmlCfg notificationConfigurationXML
	if err := xml.Unmarshal(body, &xmlCfg); err != nil {
		h.writeError(w, "MalformedXML", "Invalid notification configuration XML", bucketName, r)
		return
	}

	cfg := &bucket.NotificationConfig{}
	for _, t := range xmlCfg.TopicConfigurations {
		if err := validateNotificationEndpoint(t.Topic); err != nil {
			h.writeError(w, "InvalidArgument", "TopicConfiguration: "+err.Error(), bucketName, r)
			return
		}
		cfg.TopicConfigurations = append(cfg.TopicConfigurations, bucket.NotificationTarget{
			ID: t.ID, Endpoint: t.Topic, Events: t.Events, Filter: notifFilterFromXML(t.Filter),
		})
	}
	for _, t := range xmlCfg.QueueConfigurations {
		if err := validateNotificationEndpoint(t.Queue); err != nil {
			h.writeError(w, "InvalidArgument", "QueueConfiguration: "+err.Error(), bucketName, r)
			return
		}
		cfg.QueueConfigurations = append(cfg.QueueConfigurations, bucket.NotificationTarget{
			ID: t.ID, Endpoint: t.Queue, Events: t.Events, Filter: notifFilterFromXML(t.Filter),
		})
	}
	for _, t := range xmlCfg.LambdaConfigurations {
		if err := validateNotificationEndpoint(t.CloudFunction); err != nil {
			h.writeError(w, "InvalidArgument", "LambdaConfiguration: "+err.Error(), bucketName, r)
			return
		}
		cfg.LambdaConfigurations = append(cfg.LambdaConfigurations, bucket.NotificationTarget{
			ID: t.ID, Endpoint: t.CloudFunction, Events: t.Events, Filter: notifFilterFromXML(t.Filter),
		})
	}

	tenantID := h.getTenantIDFromRequest(r)
	if err := h.bucketManager.SetNotification(r.Context(), tenantID, bucketName, cfg); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// websiteXML holds the S3-compatible XML structures for website configuration.
type websiteIndexDocument struct {
	Suffix string `xml:"Suffix"`
}
type websiteErrorDocument struct {
	Key string `xml:"Key"`
}
type websiteRoutingCondition struct {
	HTTPErrorCodeReturnedEquals string `xml:"HttpErrorCodeReturnedEquals,omitempty"`
	KeyPrefixEquals             string `xml:"KeyPrefixEquals,omitempty"`
}
type websiteRoutingRedirect struct {
	HostName             string `xml:"HostName,omitempty"`
	HTTPRedirectCode     string `xml:"HttpRedirectCode,omitempty"`
	Protocol             string `xml:"Protocol,omitempty"`
	ReplaceKeyPrefixWith string `xml:"ReplaceKeyPrefixWith,omitempty"`
	ReplaceKeyWith       string `xml:"ReplaceKeyWith,omitempty"`
}
type websiteRoutingRule struct {
	Condition *websiteRoutingCondition `xml:"Condition,omitempty"`
	Redirect  websiteRoutingRedirect   `xml:"Redirect"`
}
type websiteConfiguration struct {
	XMLName       xml.Name              `xml:"WebsiteConfiguration"`
	IndexDocument *websiteIndexDocument `xml:"IndexDocument,omitempty"`
	ErrorDocument *websiteErrorDocument `xml:"ErrorDocument,omitempty"`
	RoutingRules  []websiteRoutingRule  `xml:"RoutingRules>RoutingRule,omitempty"`
}

// GetBucketWebsite returns the static website configuration for the bucket.
func (h *Handler) GetBucketWebsite(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"tenant": tenantID,
	}).Debug("S3 API: GetBucketWebsite")

	websiteCfg, err := h.bucketManager.GetWebsite(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		if err == bucket.ErrWebsiteNotFound {
			h.writeError(w, "NoSuchWebsiteConfiguration",
				"The specified bucket does not have a website configuration", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	resp := websiteConfiguration{
		IndexDocument: &websiteIndexDocument{Suffix: websiteCfg.IndexDocument},
	}
	if websiteCfg.ErrorDocument != "" {
		resp.ErrorDocument = &websiteErrorDocument{Key: websiteCfg.ErrorDocument}
	}
	for _, rr := range websiteCfg.RoutingRules {
		rule := websiteRoutingRule{
			Redirect: websiteRoutingRedirect{
				HostName:             rr.Redirect.HostName,
				HTTPRedirectCode:     rr.Redirect.HTTPRedirectCode,
				Protocol:             rr.Redirect.Protocol,
				ReplaceKeyPrefixWith: rr.Redirect.ReplaceKeyPrefixWith,
				ReplaceKeyWith:       rr.Redirect.ReplaceKeyWith,
			},
		}
		if rr.Condition.HTTPErrorCodeReturnedEquals != "" || rr.Condition.KeyPrefixEquals != "" {
			rule.Condition = &websiteRoutingCondition{
				HTTPErrorCodeReturnedEquals: rr.Condition.HTTPErrorCodeReturnedEquals,
				KeyPrefixEquals:             rr.Condition.KeyPrefixEquals,
			}
		}
		resp.RoutingRules = append(resp.RoutingRules, rule)
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// PutBucketWebsite stores a static website configuration for the bucket.
func (h *Handler) PutBucketWebsite(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"tenant": tenantID,
	}).Debug("S3 API: PutBucketWebsite")

	var xmlCfg websiteConfiguration
	if err := xml.NewDecoder(r.Body).Decode(&xmlCfg); err != nil {
		h.writeError(w, "MalformedXML", "The XML you provided was not well-formed or did not validate against the published schema", bucketName, r)
		return
	}

	if xmlCfg.IndexDocument == nil || xmlCfg.IndexDocument.Suffix == "" {
		h.writeError(w, "InvalidArgument", "A non-empty IndexDocument Suffix is required for website configuration", bucketName, r)
		return
	}

	websiteCfg := &bucket.WebsiteConfig{
		IndexDocument: xmlCfg.IndexDocument.Suffix,
	}
	if xmlCfg.ErrorDocument != nil {
		websiteCfg.ErrorDocument = xmlCfg.ErrorDocument.Key
	}
	for _, rr := range xmlCfg.RoutingRules {
		rule := bucket.WebsiteRoutingRule{
			Redirect: bucket.WebsiteRoutingRedirect{
				HostName:             rr.Redirect.HostName,
				HTTPRedirectCode:     rr.Redirect.HTTPRedirectCode,
				Protocol:             rr.Redirect.Protocol,
				ReplaceKeyPrefixWith: rr.Redirect.ReplaceKeyPrefixWith,
				ReplaceKeyWith:       rr.Redirect.ReplaceKeyWith,
			},
		}
		if rr.Condition != nil {
			rule.Condition = bucket.WebsiteRoutingCondition{
				HTTPErrorCodeReturnedEquals: rr.Condition.HTTPErrorCodeReturnedEquals,
				KeyPrefixEquals:             rr.Condition.KeyPrefixEquals,
			}
		}
		websiteCfg.RoutingRules = append(websiteCfg.RoutingRules, rule)
	}

	if err := h.bucketManager.SetWebsite(r.Context(), tenantID, bucketName, websiteCfg); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketWebsite removes the static website configuration from the bucket.
func (h *Handler) DeleteBucketWebsite(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"tenant": tenantID,
	}).Debug("S3 API: DeleteBucketWebsite")

	if err := h.bucketManager.DeleteWebsite(r.Context(), tenantID, bucketName); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		// ErrWebsiteNotFound on DELETE is a no-op (idempotent per AWS spec)
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetBucketAccelerateConfiguration returns an empty AccelerateConfiguration
// (Transfer Acceleration is not supported; Status is omitted which means Suspended).
func (h *Handler) GetBucketAccelerateConfiguration(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + //nolint:errcheck
		`<AccelerateConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status/></AccelerateConfiguration>`))
}

// PutBucketAccelerateConfiguration accepts an acceleration configuration (no-op).
func (h *Handler) PutBucketAccelerateConfiguration(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	w.WriteHeader(http.StatusOK)
}

// GetBucketRequestPayment returns BucketOwner as the payer (Requester Pays not supported).
func (h *Handler) GetBucketRequestPayment(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + //nolint:errcheck
		`<RequestPaymentConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` +
		`<Payer>BucketOwner</Payer>` +
		`</RequestPaymentConfiguration>`))
}

// PutBucketRequestPayment accepts a request payment configuration (no-op).
func (h *Handler) PutBucketRequestPayment(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	w.WriteHeader(http.StatusOK)
}

// sseConfiguration is the AWS XML envelope for GetBucketEncryption / PutBucketEncryption.
type sseConfiguration struct {
	XMLName xml.Name `xml:"ServerSideEncryptionConfiguration"`
	Rule    sseRule  `xml:"Rule"`
}

type sseRule struct {
	ApplyServerSideEncryptionByDefault sseDefault `xml:"ApplyServerSideEncryptionByDefault"`
	BucketKeyEnabled                   bool       `xml:"BucketKeyEnabled,omitempty"`
}

type sseDefault struct {
	SSEAlgorithm   string `xml:"SSEAlgorithm"`
	KMSMasterKeyID string `xml:"KMSMasterKeyID,omitempty"`
}

// GetBucketEncryption returns the server-side encryption configuration for the bucket.
func (h *Handler) GetBucketEncryption(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	encCfg, err := h.bucketManager.GetEncryption(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		if err == bucket.ErrEncryptionNotFound {
			h.writeError(w, "ServerSideEncryptionConfigurationNotFoundError",
				"The server side encryption configuration was not found", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	resp := sseConfiguration{
		Rule: sseRule{
			ApplyServerSideEncryptionByDefault: sseDefault{
				SSEAlgorithm:   encCfg.Type,
				KMSMasterKeyID: encCfg.KMSKeyID,
			},
		},
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// PutBucketEncryption stores a server-side encryption configuration for the bucket.
func (h *Handler) PutBucketEncryption(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	var xmlCfg sseConfiguration
	if err := xml.NewDecoder(r.Body).Decode(&xmlCfg); err != nil {
		h.writeError(w, "MalformedXML", "The XML you provided was not well-formed or did not validate against the published schema", bucketName, r)
		return
	}

	algo := xmlCfg.Rule.ApplyServerSideEncryptionByDefault.SSEAlgorithm
	if algo != "AES256" && algo != "aws:kms" {
		h.writeError(w, "InvalidArgument", "Unknown SSEAlgorithm: "+algo, bucketName, r)
		return
	}

	encCfg := &bucket.EncryptionConfig{
		Type:     algo,
		KMSKeyID: xmlCfg.Rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID,
	}
	if err := h.bucketManager.SetEncryption(r.Context(), tenantID, bucketName, encCfg); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketEncryption removes the server-side encryption configuration from the bucket.
func (h *Handler) DeleteBucketEncryption(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	if err := h.bucketManager.DeleteEncryption(r.Context(), tenantID, bucketName); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetBucketReplication returns ReplicationConfigurationNotFoundError
// (cross-region replication via S3 API not supported; MaxIOFS uses its own cluster replication).
func (h *Handler) GetBucketReplication(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	vars := mux.Vars(r)
	h.writeError(w, "ReplicationConfigurationNotFoundError",
		"The replication configuration was not found", vars["bucket"], r)
}

// PutBucketReplication returns NotImplemented — use the MaxIOFS replication API instead.
func (h *Handler) PutBucketReplication(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	vars := mux.Vars(r)
	h.writeError(w, "NotImplemented",
		"S3 bucket replication is not supported. Use the MaxIOFS cluster replication API.", vars["bucket"], r)
}

// DeleteBucketReplication removes a replication configuration (no-op).
func (h *Handler) DeleteBucketReplication(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	w.WriteHeader(http.StatusNoContent)
}

// bucketLoggingStatus is the AWS XML envelope for GetBucketLogging / PutBucketLogging.
type bucketLoggingStatus struct {
	XMLName        xml.Name            `xml:"BucketLoggingStatus"`
	LoggingEnabled *loggingEnabledXML  `xml:"LoggingEnabled,omitempty"`
}

type loggingEnabledXML struct {
	TargetBucket string `xml:"TargetBucket"`
	TargetPrefix string `xml:"TargetPrefix"`
}

// GetBucketLogging returns the server access logging configuration for the bucket.
func (h *Handler) GetBucketLogging(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	cfg, err := h.bucketManager.GetLogging(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		// No logging configured → return empty status (AWS behaviour)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><BucketLoggingStatus/>`)) //nolint:errcheck
		return
	}

	resp := bucketLoggingStatus{
		LoggingEnabled: &loggingEnabledXML{
			TargetBucket: cfg.TargetBucket,
			TargetPrefix: cfg.TargetPrefix,
		},
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// PutBucketLogging stores a server access logging configuration for the bucket.
// An empty <BucketLoggingStatus/> (no <LoggingEnabled> element) disables logging.
func (h *Handler) PutBucketLogging(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	var xmlCfg bucketLoggingStatus
	if err := xml.NewDecoder(r.Body).Decode(&xmlCfg); err != nil {
		h.writeError(w, "MalformedXML", "The XML you provided was not well-formed", bucketName, r)
		return
	}

	if xmlCfg.LoggingEnabled == nil || xmlCfg.LoggingEnabled.TargetBucket == "" {
		// Disable logging
		if err := h.bucketManager.DeleteLogging(r.Context(), tenantID, bucketName); err != nil && err != bucket.ErrLoggingNotFound {
			if err == bucket.ErrBucketNotFound {
				h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
				return
			}
			h.writeError(w, "InternalError", err.Error(), bucketName, r)
			return
		}
	} else {
		cfg := &bucket.LoggingConfig{
			TargetBucket: xmlCfg.LoggingEnabled.TargetBucket,
			TargetPrefix: xmlCfg.LoggingEnabled.TargetPrefix,
		}
		if err := h.bucketManager.SetLogging(r.Context(), tenantID, bucketName, cfg); err != nil {
			if err == bucket.ErrBucketNotFound {
				h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
				return
			}
			h.writeError(w, "InternalError", err.Error(), bucketName, r)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

// publicAccessBlockXML is the AWS XML envelope for GetPublicAccessBlock / PutPublicAccessBlock.
type publicAccessBlockXML struct {
	XMLName               xml.Name `xml:"PublicAccessBlockConfiguration"`
	BlockPublicAcls       bool     `xml:"BlockPublicAcls"`
	IgnorePublicAcls      bool     `xml:"IgnorePublicAcls"`
	BlockPublicPolicy     bool     `xml:"BlockPublicPolicy"`
	RestrictPublicBuckets bool     `xml:"RestrictPublicBuckets"`
}

// GetPublicAccessBlock returns the public access block configuration for the bucket.
func (h *Handler) GetPublicAccessBlock(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	cfg, err := h.bucketManager.GetPublicAccessBlock(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		if err == bucket.ErrPublicAccessBlockNotFound {
			h.writeError(w, "NoSuchPublicAccessBlockConfiguration",
				"The public access block configuration was not found", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	resp := publicAccessBlockXML{
		BlockPublicAcls:       cfg.BlockPublicAcls,
		IgnorePublicAcls:      cfg.IgnorePublicAcls,
		BlockPublicPolicy:     cfg.BlockPublicPolicy,
		RestrictPublicBuckets: cfg.RestrictPublicBuckets,
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// PutPublicAccessBlock stores a public access block configuration for the bucket.
func (h *Handler) PutPublicAccessBlock(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	var xmlCfg publicAccessBlockXML
	if err := xml.NewDecoder(r.Body).Decode(&xmlCfg); err != nil {
		h.writeError(w, "MalformedXML", "The XML you provided was not well-formed", bucketName, r)
		return
	}

	cfg := &bucket.PublicAccessBlock{
		BlockPublicAcls:       xmlCfg.BlockPublicAcls,
		IgnorePublicAcls:      xmlCfg.IgnorePublicAcls,
		BlockPublicPolicy:     xmlCfg.BlockPublicPolicy,
		RestrictPublicBuckets: xmlCfg.RestrictPublicBuckets,
	}
	if err := h.bucketManager.SetPublicAccessBlock(r.Context(), tenantID, bucketName, cfg); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeletePublicAccessBlock removes the public access block configuration from the bucket.
func (h *Handler) DeletePublicAccessBlock(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	tenantID := h.getTenantIDFromRequest(r)

	if err := h.bucketManager.DeletePublicAccessBlock(r.Context(), tenantID, bucketName); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
