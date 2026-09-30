package inventory

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// ObjectWriter stores an object as a client's PUT does: encrypted, counted
// against the quotas and, in a cluster, sent to the other nodes.
type ObjectWriter interface {
	PutObject(ctx context.Context, bucket, key string, data io.Reader, headers http.Header) (*object.Object, error)
}

// inventoryPageSize is how many objects a report lists at a time.
const inventoryPageSize = 1000

// ReportGenerator generates inventory reports
type ReportGenerator struct {
	bucketManager bucket.Manager
	metadataStore metadata.Store
	objects       ObjectWriter
	log           *logrus.Entry
}

// NewReportGenerator creates a new report generator
func NewReportGenerator(
	bucketManager bucket.Manager,
	metadataStore metadata.Store,
	objects ObjectWriter,
) *ReportGenerator {
	return &ReportGenerator{
		bucketManager: bucketManager,
		metadataStore: metadataStore,
		objects:       objects,
		log:           logrus.WithField("component", "inventory_generator"),
	}
}

func inventoryBucketPath(tenantID, bucketName string) string {
	if tenantID != "" {
		return tenantID + "/" + bucketName
	}
	return bucketName
}

// GenerateReport generates an inventory report for a bucket
func (g *ReportGenerator) GenerateReport(ctx context.Context, config *InventoryConfig) (*InventoryReport, error) {
	g.log.WithFields(logrus.Fields{
		"bucket": config.BucketName,
		"format": config.Format,
	}).Info("Starting inventory report generation")

	startTime := time.Now().Unix()

	// Get all objects from the bucket
	items, totalSize, err := g.collectInventoryItems(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to collect inventory items: %w", err)
	}

	// Generate report content based on format
	var reportContent []byte
	switch config.Format {
	case "csv":
		reportContent, err = g.generateCSV(items, config.IncludedFields)
	case "json":
		reportContent, err = g.generateJSON(items, config.IncludedFields)
	default:
		return nil, fmt.Errorf("unsupported format: %s", config.Format)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to generate report content: %w", err)
	}

	// Generate report path
	reportPath := g.generateReportPath(config)

	// Upload report to destination bucket
	if err := g.uploadReport(ctx, config, reportPath, reportContent); err != nil {
		return nil, fmt.Errorf("failed to upload report: %w", err)
	}

	completedTime := time.Now().Unix()

	report := &InventoryReport{
		ConfigID:    config.ID,
		BucketName:  config.BucketName,
		ReportPath:  reportPath,
		ObjectCount: int64(len(items)),
		TotalSize:   totalSize,
		Status:      "completed",
		StartedAt:   &startTime,
		CompletedAt: &completedTime,
	}

	g.log.WithFields(logrus.Fields{
		"bucket":       config.BucketName,
		"object_count": len(items),
		"total_size":   totalSize,
		"report_path":  reportPath,
	}).Info("Inventory report generation completed")

	return report, nil
}

// collectInventoryItems collects all objects from the bucket
func (g *ReportGenerator) collectInventoryItems(ctx context.Context, config *InventoryConfig) ([]*ObjectInventoryItem, int64, error) {
	sourceBucketPath := inventoryBucketPath(config.TenantID, config.BucketName)

	var items []*ObjectInventoryItem
	var totalSize int64

	// List every object in the bucket, a page at a time
	marker := ""
	for {
		objects, next, err := g.metadataStore.ListObjects(ctx, sourceBucketPath, "", marker, inventoryPageSize)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to list objects: %w", err)
		}
		for _, obj := range objects {
			isMultipart := obj.UploadID != ""
			item := &ObjectInventoryItem{
				Bucket:              config.BucketName,
				Key:                 obj.Key,
				VersionID:           obj.VersionID,
				IsLatest:            obj.VersionID == "", // If no version ID, it's the latest
				Size:                obj.Size,
				LastModified:        obj.LastModified.UTC().Format(time.RFC3339),
				ETag:                obj.ETag,
				StorageClass:        obj.StorageClass,
				IsMultipartUploaded: isMultipart,
				EncryptionStatus:    g.getEncryptionStatus(obj),
			}

			items = append(items, item)
			totalSize += obj.Size
		}
		if next == "" || len(objects) == 0 {
			break
		}
		marker = next
	}

	return items, totalSize, nil
}

// getEncryptionStatus determines encryption status from metadata
func (g *ReportGenerator) getEncryptionStatus(obj *metadata.ObjectMetadata) string {
	if obj.Metadata != nil {
		if encryption, ok := obj.Metadata["x-amz-server-side-encryption"]; ok && encryption != "" {
			return "SSE-S3"
		}
	}
	return "NONE"
}

// generateCSV generates a CSV report
func (g *ReportGenerator) generateCSV(items []*ObjectInventoryItem, includedFields []string) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Write header
	header := g.getCSVHeader(includedFields)
	if err := writer.Write(header); err != nil {
		return nil, err
	}

	// Write data rows
	for _, item := range items {
		row := g.getCSVRow(item, includedFields)
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// generateJSON generates a JSON report
func (g *ReportGenerator) generateJSON(items []*ObjectInventoryItem, includedFields []string) ([]byte, error) {
	// Filter items to include only specified fields
	filteredItems := make([]map[string]interface{}, len(items))

	for i, item := range items {
		filtered := make(map[string]interface{})

		for _, field := range includedFields {
			switch field {
			case FieldBucketName:
				filtered["bucket"] = item.Bucket
			case FieldObjectKey:
				filtered["key"] = item.Key
			case FieldVersionID:
				if item.VersionID != "" {
					filtered["version_id"] = item.VersionID
				}
			case FieldIsLatest:
				filtered["is_latest"] = item.IsLatest
			case FieldSize:
				filtered["size"] = item.Size
			case FieldLastModified:
				filtered["last_modified"] = item.LastModified
			case FieldETag:
				filtered["etag"] = item.ETag
			case FieldStorageClass:
				filtered["storage_class"] = item.StorageClass
			case FieldIsMultipartUploaded:
				filtered["is_multipart_uploaded"] = item.IsMultipartUploaded
			case FieldEncryptionStatus:
				filtered["encryption_status"] = item.EncryptionStatus
			case FieldReplicationStatus:
				if item.ReplicationStatus != "" {
					filtered["replication_status"] = item.ReplicationStatus
				}
			case FieldObjectACL:
				if item.ObjectACL != "" {
					filtered["object_acl"] = item.ObjectACL
				}
			}
		}

		filteredItems[i] = filtered
	}

	return json.MarshalIndent(filteredItems, "", "  ")
}

// getCSVHeader returns CSV header based on included fields
func (g *ReportGenerator) getCSVHeader(includedFields []string) []string {
	header := make([]string, 0, len(includedFields))

	for _, field := range includedFields {
		switch field {
		case FieldBucketName:
			header = append(header, "Bucket")
		case FieldObjectKey:
			header = append(header, "Key")
		case FieldVersionID:
			header = append(header, "VersionId")
		case FieldIsLatest:
			header = append(header, "IsLatest")
		case FieldSize:
			header = append(header, "Size")
		case FieldLastModified:
			header = append(header, "LastModifiedDate")
		case FieldETag:
			header = append(header, "ETag")
		case FieldStorageClass:
			header = append(header, "StorageClass")
		case FieldIsMultipartUploaded:
			header = append(header, "IsMultipartUploaded")
		case FieldEncryptionStatus:
			header = append(header, "EncryptionStatus")
		case FieldReplicationStatus:
			header = append(header, "ReplicationStatus")
		case FieldObjectACL:
			header = append(header, "ObjectACL")
		}
	}

	return header
}

// getCSVRow returns CSV row based on included fields
func (g *ReportGenerator) getCSVRow(item *ObjectInventoryItem, includedFields []string) []string {
	row := make([]string, 0, len(includedFields))

	for _, field := range includedFields {
		switch field {
		case FieldBucketName:
			row = append(row, item.Bucket)
		case FieldObjectKey:
			row = append(row, item.Key)
		case FieldVersionID:
			row = append(row, item.VersionID)
		case FieldIsLatest:
			row = append(row, fmt.Sprintf("%t", item.IsLatest))
		case FieldSize:
			row = append(row, fmt.Sprintf("%d", item.Size))
		case FieldLastModified:
			row = append(row, item.LastModified)
		case FieldETag:
			row = append(row, item.ETag)
		case FieldStorageClass:
			row = append(row, item.StorageClass)
		case FieldIsMultipartUploaded:
			row = append(row, fmt.Sprintf("%t", item.IsMultipartUploaded))
		case FieldEncryptionStatus:
			row = append(row, item.EncryptionStatus)
		case FieldReplicationStatus:
			row = append(row, item.ReplicationStatus)
		case FieldObjectACL:
			row = append(row, item.ObjectACL)
		}
	}

	return row
}

// generateReportPath generates a unique path for the report
func (g *ReportGenerator) generateReportPath(config *InventoryConfig) string {
	timestamp := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("inventory-%s.%s", timestamp, config.Format)

	if config.DestinationPrefix != "" {
		return strings.TrimSuffix(config.DestinationPrefix, "/") + "/" + filename
	}

	return filename
}

// uploadReport uploads the report to the destination bucket
func (g *ReportGenerator) uploadReport(ctx context.Context, config *InventoryConfig, reportPath string, content []byte) error {
	// Check if destination bucket exists
	_, err := g.bucketManager.GetBucketInfo(ctx, config.TenantID, config.DestinationBucket)
	if err != nil {
		return fmt.Errorf("destination bucket not found: %w", err)
	}

	headers := http.Header{}
	headers.Set("Content-Type", g.getContentType(config.Format))
	headers.Set("X-Amz-Meta-Generated-By", "maxiofs-inventory")
	headers.Set("X-Amz-Meta-Source-Bucket", config.BucketName)

	destinationBucketPath := inventoryBucketPath(config.TenantID, config.DestinationBucket)
	if _, err := g.objects.PutObject(ctx, destinationBucketPath, reportPath, bytes.NewReader(content), headers); err != nil {
		return fmt.Errorf("failed to store the report: %w", err)
	}
	return nil
}

// getContentType returns the content type for the report format
func (g *ReportGenerator) getContentType(format string) string {
	switch format {
	case "csv":
		return "text/csv"
	case "json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}
