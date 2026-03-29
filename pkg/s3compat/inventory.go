package s3compat

// S3 BucketInventory API — GET/PUT/DELETE /{bucket}?inventory&id=xxx
//                           GET /{bucket}?inventory  (list all)
//
// The inventory backend already exists in internal/inventory/ and is exposed
// through the Console API. These handlers add the standard S3 wire format on
// top, so tools like the AWS CLI, Terraform, and SDK-based pipelines work
// without any changes.

import (
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/inventory"
)

// ============================================================================
// XML types (AWS S3 InventoryConfiguration wire format)
// ============================================================================

type inventoryConfigXML struct {
	XMLName                xml.Name                      `xml:"InventoryConfiguration"`
	Xmlns                  string                        `xml:"xmlns,attr,omitempty"`
	ID                     string                        `xml:"Id"`
	IsEnabled              bool                          `xml:"IsEnabled"`
	Destination            inventoryDestinationXML       `xml:"Destination"`
	Schedule               inventoryScheduleXML          `xml:"Schedule"`
	IncludedObjectVersions string                        `xml:"IncludedObjectVersions"`
	OptionalFields         *inventoryOptionalFieldsXML   `xml:"OptionalFields,omitempty"`
}

type inventoryDestinationXML struct {
	S3BucketDestination inventoryS3DestinationXML `xml:"S3BucketDestination"`
}

type inventoryS3DestinationXML struct {
	Bucket string `xml:"Bucket"` // arn:aws:s3:::bucket-name
	Format string `xml:"Format"` // CSV | JSON
	Prefix string `xml:"Prefix,omitempty"`
}

type inventoryScheduleXML struct {
	Frequency string `xml:"Frequency"` // Daily | Weekly
}

type inventoryOptionalFieldsXML struct {
	Fields []string `xml:"Field"`
}

type listInventoryConfigurationsResultXML struct {
	XMLName              xml.Name             `xml:"ListInventoryConfigurationsResult"`
	Xmlns                string               `xml:"xmlns,attr,omitempty"`
	InventoryConfigs     []inventoryConfigXML `xml:"InventoryConfiguration"`
	IsTruncated          bool                 `xml:"IsTruncated"`
}

// ============================================================================
// Field name translation
// ============================================================================

// s3FieldToInternal maps S3 OptionalFields names to our internal field constants.
var s3FieldToInternal = map[string]string{
	"Size":                  inventory.FieldSize,
	"LastModifiedDate":      inventory.FieldLastModified,
	"ETag":                  inventory.FieldETag,
	"StorageClass":          inventory.FieldStorageClass,
	"IsMultipartUploaded":   inventory.FieldIsMultipartUploaded,
	"EncryptionStatus":      inventory.FieldEncryptionStatus,
	"ReplicationStatus":     inventory.FieldReplicationStatus,
	"ObjectACL":             inventory.FieldObjectACL,
	// version-related fields come from IncludedObjectVersions, not OptionalFields,
	// but accept them here too for forward compatibility
	"VersionId": inventory.FieldVersionID,
	"IsLatest":  inventory.FieldIsLatest,
}

var internalFieldToS3 = map[string]string{
	inventory.FieldSize:                 "Size",
	inventory.FieldLastModified:         "LastModifiedDate",
	inventory.FieldETag:                 "ETag",
	inventory.FieldStorageClass:         "StorageClass",
	inventory.FieldIsMultipartUploaded:  "IsMultipartUploaded",
	inventory.FieldEncryptionStatus:     "EncryptionStatus",
	inventory.FieldReplicationStatus:    "ReplicationStatus",
	inventory.FieldObjectACL:            "ObjectACL",
	inventory.FieldVersionID:            "VersionId",
	inventory.FieldIsLatest:             "IsLatest",
}

// internalToXML converts an InventoryConfig to the S3 XML representation.
func internalToXML(cfg *inventory.InventoryConfig) inventoryConfigXML {
	// Format: lowercase internal → uppercase S3
	format := strings.ToUpper(cfg.Format) // csv→CSV, json→JSON

	// Frequency: daily→Daily, weekly→Weekly
	freqLower := strings.ToLower(cfg.Frequency)
	freq := strings.ToUpper(freqLower[:1]) + freqLower[1:]

	// Bucket ARN
	bucketARN := "arn:aws:s3:::" + cfg.DestinationBucket

	// IncludedObjectVersions: if version_id or is_latest in fields → "All", else "Current"
	includedVersions := "Current"
	var optFields []string
	for _, f := range cfg.IncludedFields {
		if f == inventory.FieldBucketName || f == inventory.FieldObjectKey {
			continue // always included, not in OptionalFields
		}
		if f == inventory.FieldVersionID || f == inventory.FieldIsLatest {
			includedVersions = "All"
			continue // controlled by IncludedObjectVersions, not OptionalFields
		}
		if s3Name, ok := internalFieldToS3[f]; ok {
			optFields = append(optFields, s3Name)
		}
	}

	x := inventoryConfigXML{
		Xmlns:                  "http://s3.amazonaws.com/doc/2006-03-01/",
		ID:                     cfg.ID,
		IsEnabled:              cfg.Enabled,
		IncludedObjectVersions: includedVersions,
		Destination: inventoryDestinationXML{
			S3BucketDestination: inventoryS3DestinationXML{
				Bucket: bucketARN,
				Format: format,
				Prefix: cfg.DestinationPrefix,
			},
		},
		Schedule: inventoryScheduleXML{Frequency: freq},
	}
	if len(optFields) > 0 {
		x.OptionalFields = &inventoryOptionalFieldsXML{Fields: optFields}
	}
	return x
}

// xmlToInternal converts an S3 XML InventoryConfiguration to our internal model.
// bucketName and tenantID are injected from the request context.
func xmlToInternal(x inventoryConfigXML, bucketName, tenantID string) *inventory.InventoryConfig {
	// Strip ARN prefix from bucket name
	dest := x.Destination.S3BucketDestination.Bucket
	if idx := strings.LastIndex(dest, ":"); idx >= 0 {
		dest = dest[idx+1:]
	}

	// Format: CSV→csv, JSON→json
	format := strings.ToLower(x.Destination.S3BucketDestination.Format)
	if format != "csv" && format != "json" {
		format = "csv" // safe default
	}

	// Frequency: Daily→daily, Weekly→weekly
	freq := strings.ToLower(x.Schedule.Frequency)

	// Build included fields: always include bucket_name and object_key
	fields := []string{inventory.FieldBucketName, inventory.FieldObjectKey}

	// IncludedObjectVersions: All → add version_id and is_latest
	if strings.EqualFold(x.IncludedObjectVersions, "All") {
		fields = append(fields, inventory.FieldVersionID, inventory.FieldIsLatest)
	}

	// Map OptionalFields
	if x.OptionalFields != nil {
		for _, s3Name := range x.OptionalFields.Fields {
			if internal, ok := s3FieldToInternal[s3Name]; ok {
				// skip version fields already added above
				if internal == inventory.FieldVersionID || internal == inventory.FieldIsLatest {
					continue
				}
				fields = append(fields, internal)
			}
		}
	}

	scheduleTime := "00:00" // default
	// S3 API doesn't expose schedule time in XML, use midnight as default

	return &inventory.InventoryConfig{
		ID:                x.ID,
		BucketName:        bucketName,
		TenantID:          tenantID,
		Enabled:           x.IsEnabled,
		Frequency:         freq,
		Format:            format,
		DestinationBucket: dest,
		DestinationPrefix: x.Destination.S3BucketDestination.Prefix,
		IncludedFields:    fields,
		ScheduleTime:      scheduleTime,
	}
}

// ============================================================================
// Handlers
// ============================================================================

// GetBucketInventoryConfiguration handles GET /{bucket}?inventory&id={id}
func (h *Handler) GetBucketInventoryConfiguration(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	id := r.URL.Query().Get("id")

	addS3CompatHeaders(w)

	if h.inventoryManager == nil {
		h.writeError(w, "NotImplemented", "Inventory is not configured", bucketName, r)
		return
	}

	tenantID := h.getTenantIDFromRequest(r)

	cfg, err := h.inventoryManager.GetConfigByID(r.Context(), id, tenantID)
	if err != nil {
		h.writeError(w, "NoSuchConfiguration", "The specified inventory configuration does not exist", id, r)
		return
	}

	h.writeXMLResponse(w, http.StatusOK, internalToXML(cfg))
}

// PutBucketInventoryConfiguration handles PUT /{bucket}?inventory&id={id}
func (h *Handler) PutBucketInventoryConfiguration(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	id := r.URL.Query().Get("id")

	addS3CompatHeaders(w)

	if h.inventoryManager == nil {
		h.writeError(w, "NotImplemented", "Inventory is not configured", bucketName, r)
		return
	}

	if id == "" {
		h.writeError(w, "InvalidArgument", "The id query parameter is required", bucketName, r)
		return
	}

	tenantID := h.getTenantIDFromRequest(r)

	var x inventoryConfigXML
	if err := xml.NewDecoder(r.Body).Decode(&x); err != nil {
		h.writeError(w, "MalformedXML", "The XML you provided was not well-formed", bucketName, r)
		return
	}

	// The id in the URL takes precedence over any Id in the XML body
	x.ID = id

	cfg := xmlToInternal(x, bucketName, tenantID)

	// Validate
	if cfg.DestinationBucket == "" {
		h.writeError(w, "InvalidArgument", "Destination bucket is required", bucketName, r)
		return
	}
	if cfg.DestinationBucket == bucketName {
		h.writeError(w, "InvalidArgument", "Destination bucket cannot be the same as the source bucket", bucketName, r)
		return
	}
	if cfg.Frequency != "daily" && cfg.Frequency != "weekly" {
		h.writeError(w, "InvalidArgument", "Schedule frequency must be Daily or Weekly", bucketName, r)
		return
	}
	if cfg.Format != "csv" && cfg.Format != "json" {
		h.writeError(w, "InvalidArgument", "Format must be CSV or JSON", bucketName, r)
		return
	}
	if !inventory.ValidateIncludedFields(cfg.IncludedFields) {
		h.writeError(w, "InvalidArgument", "One or more OptionalFields values are invalid", bucketName, r)
		return
	}

	if err := h.inventoryManager.UpsertConfigByID(r.Context(), cfg); err != nil {
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketInventoryConfiguration handles DELETE /{bucket}?inventory&id={id}
func (h *Handler) DeleteBucketInventoryConfiguration(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	id := r.URL.Query().Get("id")

	addS3CompatHeaders(w)

	if h.inventoryManager == nil {
		h.writeError(w, "NotImplemented", "Inventory is not configured", bucketName, r)
		return
	}

	tenantID := h.getTenantIDFromRequest(r)

	if err := h.inventoryManager.DeleteConfigByID(r.Context(), id, tenantID); err != nil {
		h.writeError(w, "NoSuchConfiguration", "The specified inventory configuration does not exist", id, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListBucketInventoryConfigurations handles GET /{bucket}?inventory (no id param)
func (h *Handler) ListBucketInventoryConfigurations(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	addS3CompatHeaders(w)

	if h.inventoryManager == nil {
		// Return empty list rather than error — bucket has no inventory configs
		result := listInventoryConfigurationsResultXML{
			Xmlns:       "http://s3.amazonaws.com/doc/2006-03-01/",
			IsTruncated: false,
		}
		h.writeXMLResponse(w, http.StatusOK, result)
		return
	}

	tenantID := h.getTenantIDFromRequest(r)

	configs, err := h.inventoryManager.ListConfigsByBucket(r.Context(), bucketName, tenantID)
	if err != nil {
		h.writeError(w, "InternalError", err.Error(), bucketName, r)
		return
	}

	xmlConfigs := make([]inventoryConfigXML, 0, len(configs))
	for _, cfg := range configs {
		xmlConfigs = append(xmlConfigs, internalToXML(cfg))
	}

	result := listInventoryConfigurationsResultXML{
		Xmlns:            "http://s3.amazonaws.com/doc/2006-03-01/",
		InventoryConfigs: xmlConfigs,
		IsTruncated:      false,
	}
	h.writeXMLResponse(w, http.StatusOK, result)
}
