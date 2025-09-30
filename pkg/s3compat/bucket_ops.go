package s3compat

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/sirupsen/logrus"
)

// Note: We use bucket.Policy directly instead of defining our own structures

// GetBucketPolicy retrieves the bucket policy
func (h *Handler) GetBucketPolicy(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	logrus.WithField("bucket", bucketName).Debug("S3 API: GetBucketPolicy")

	policy, err := h.bucketManager.GetBucketPolicy(r.Context(), bucketName)
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
		h.writeError(w, "InvalidRequest", "Failed to read request body", bucketName, r)
		return
	}
	defer r.Body.Close()

	// Validate JSON format
	var policyDoc bucket.Policy
	if err := json.Unmarshal(body, &policyDoc); err != nil {
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
	if err := h.bucketManager.SetBucketPolicy(r.Context(), bucketName, &policyDoc); err != nil {
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
	if err := h.bucketManager.SetBucketPolicy(r.Context(), bucketName, nil); err != nil {
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
	XMLName xml.Name         `xml:"LifecycleConfiguration"`
	Rules   []LifecycleRule  `xml:"Rule"`
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

	lifecycleConfig, err := h.bucketManager.GetLifecycle(r.Context(), bucketName)
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
			xmlRule.Expiration = exp
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
		internalRule := bucket.LifecycleRule{
			ID:     rule.ID,
			Status: rule.Status,
			Filter: bucket.LifecycleFilter{
				Prefix: rule.Prefix,
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
			internalRule.Expiration = exp
		}

		if rule.AbortIncompleteMultipartUpload != nil {
			internalRule.AbortIncompleteMultipartUpload = &bucket.LifecycleAbortIncompleteMultipartUpload{
				DaysAfterInitiation: rule.AbortIncompleteMultipartUpload.DaysAfterInitiation,
			}
		}

		lifecycleConfig.Rules[i] = internalRule
	}

	// Set the lifecycle configuration
	if err := h.bucketManager.SetLifecycle(r.Context(), bucketName, lifecycleConfig); err != nil {
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
	if err := h.bucketManager.SetLifecycle(r.Context(), bucketName, nil); err != nil {
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

	corsConfig, err := h.bucketManager.GetCORS(r.Context(), bucketName)
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
	if err := h.bucketManager.SetCORS(r.Context(), bucketName, corsConfig); err != nil {
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
	if err := h.bucketManager.SetCORS(r.Context(), bucketName, nil); err != nil {
		if err == bucket.ErrBucketNotFound {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
