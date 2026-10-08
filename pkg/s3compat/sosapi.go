package s3compat

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/sirupsen/logrus"
)

// SOSAPI (Smart Object Storage API) constants
const (
	// System object path for VEEAM SOSAPI
	systemXMLObject   = ".system-d26a9498-cb7c-4a87-a44a-8ae204f5ba6c/system.xml"
	capacityXMLObject = ".system-d26a9498-cb7c-4a87-a44a-8ae204f5ba6c/capacity.xml"

	// VEEAM User-Agent detection
	veeamAgentSubstr = "APN/1.0 Veeam/1.0"
)

// APIEndpoints defines IAM and STS endpoints (optional, used when IAMSTS capability is true)
type APIEndpoints struct {
	IAMEndpoint string `xml:"IAMEndpoint"`
	STSEndpoint string `xml:"STSEndpoint"`
}

// SystemInfo represents VEEAM SOSAPI system.xml structure
// SystemInfo structure for SOSAPI system.xml
type SystemInfo struct {
	XMLName              xml.Name `xml:"SystemInfo" json:"-"`
	ProtocolVersion      string   `xml:"ProtocolVersion"`
	ModelName            string   `xml:"ModelName"`
	ProtocolCapabilities struct {
		CapacityInfo   bool `xml:"CapacityInfo"`
		UploadSessions bool `xml:"UploadSessions"`
		IAMSTS         bool `xml:"IAMSTS"`
	} `xml:"ProtocolCapabilities"`
	APIEndpoints          *APIEndpoints `xml:"APIEndpoints,omitempty"`
	SystemRecommendations struct {
		S3ConcurrentTaskLimit    int `xml:"S3ConcurrentTaskLimit,omitempty"`
		S3MultiObjectDeleteLimit int `xml:"S3MultiObjectDeleteLimit,omitempty"`
		StorageCurrentTaskLimit  int `xml:"StorageCurrentTaskLimit,omitempty"`
		KBBlockSize              int `xml:"KbBlockSize"`
	} `xml:"SystemRecommendations"`
}

// CapacityInfo represents VEEAM SOSAPI capacity.xml structure
type CapacityInfo struct {
	XMLName   xml.Name `xml:"CapacityInfo"`
	Capacity  int64    `xml:"Capacity"`
	Available int64    `xml:"Available"`
	Used      int64    `xml:"Used"`
}

// isVeeamSOSAPIObject checks if the object path is a VEEAM SOSAPI special file
func isVeeamSOSAPIObject(objectKey string) bool {
	return strings.HasSuffix(objectKey, systemXMLObject) ||
		strings.HasSuffix(objectKey, capacityXMLObject) ||
		objectKey == systemXMLObject ||
		objectKey == capacityXMLObject
}

// isVeeamClient checks if the User-Agent indicates a VEEAM client
func isVeeamClient(userAgent string) bool {
	return strings.Contains(userAgent, veeamAgentSubstr) ||
		strings.Contains(strings.ToLower(userAgent), "veeam")
}

// generateSystemXML generates the SOSAPI system.xml content.
func generateSystemXML(iamSTSEndpoint string) ([]byte, error) {
	sysInfo := SystemInfo{
		ProtocolVersion: `"1.0"`,
		ModelName:       `"MaxIOFS"`,
	}

	// Initialize inline ProtocolCapabilities
	sysInfo.ProtocolCapabilities.CapacityInfo = true
	sysInfo.ProtocolCapabilities.UploadSessions = false // Disabled - not fully implemented yet

	if iamSTSEndpoint != "" {
		sysInfo.ProtocolCapabilities.IAMSTS = true
		sysInfo.APIEndpoints = &APIEndpoints{
			IAMEndpoint: iamSTSEndpoint,
			STSEndpoint: iamSTSEndpoint,
		}
	}

	// Initialize inline SystemRecommendations (ONLY KbBlockSize)
	sysInfo.SystemRecommendations.KBBlockSize = 4096
	// MaxIOFS does NOT set S3ConcurrentTaskLimit, S3MultiObjectDeleteLimit, StorageCurrentTaskLimit
	// Those fields are omitempty and left as 0 (omitted from XML)

	// Use xml.Marshal WITHOUT indentation (compact XML format)
	output, err := xml.Marshal(sysInfo)
	if err != nil {
		return nil, err
	}

	xmlData := []byte(xml.Header + string(output))

	// Log the generated XML for debugging
	logrus.WithFields(logrus.Fields{
		"protocol_version": sysInfo.ProtocolVersion,
		"model_name":       sysInfo.ModelName,
		"xml_length":       len(xmlData),
	}).Info("Generated SOSAPI system.xml - MaxIOFS S3-compatible storage")

	return xmlData, nil
}

// generateCapacityXML generates the SOSAPI capacity.xml content
// totalCapacity and availableCapacity should be in bytes
func generateCapacityXML(totalCapacity, availableCapacity int64) ([]byte, error) {
	used := totalCapacity - availableCapacity
	if used < 0 {
		used = 0
	}

	capInfo := CapacityInfo{
		Capacity:  totalCapacity,
		Available: availableCapacity,
		Used:      used,
	}

	output, err := xml.MarshalIndent(capInfo, "", "  ")
	if err != nil {
		return nil, err
	}

	return []byte(xml.Header + string(output)), nil
}

// getSOSAPIVirtualObject returns the content for SOSAPI virtual objects.
// bucketName/tenantID identify the bucket VEEAM is querying so that a per-bucket
// quota can be reported as the advertised capacity.
func (h *Handler) getSOSAPIVirtualObject(ctx context.Context, bucketName, tenantID, objectKey string) ([]byte, string, error) {
	// Check if it's a system.xml file (with or without prefix path)
	if strings.HasSuffix(objectKey, systemXMLObject) || objectKey == systemXMLObject {
		endpoint := ""
		if h.iamSTSEndpoint != nil {
			endpoint = h.iamSTSEndpoint()
		}
		data, err := generateSystemXML(endpoint)
		if err != nil {
			return nil, "", err
		}
		return data, "application/xml", nil
	}

	// Check if it's a capacity.xml file (with or without prefix path)
	if strings.HasSuffix(objectKey, capacityXMLObject) || objectKey == capacityXMLObject {
		totalCapacity := int64(1024 * 1024 * 1024 * 1024)    // Default: 1TB
		availableCapacity := int64(900 * 1024 * 1024 * 1024) // Default: 900GB

		reported := false

		// 1) Per-bucket quota.
		if h.bucketManager != nil && bucketName != "" {
			if info, err := h.bucketManager.GetBucketInfo(ctx, tenantID, bucketName); err == nil && info != nil && info.Quota != nil && info.Quota.MaxSizeBytes > 0 {
				totalCapacity = info.Quota.MaxSizeBytes
				used := info.TotalSize
				availableCapacity = totalCapacity - used
				if availableCapacity < 0 {
					availableCapacity = 0
				}
				reported = true
				logrus.WithFields(logrus.Fields{
					"bucket":      bucketName,
					"quota_bytes": totalCapacity,
					"used_bytes":  used,
					"free_bytes":  availableCapacity,
				}).Info("SOSAPI capacity calculated from bucket quota")
			}
		}

		// 2) The quota of the bucket's tenant, when the bucket has none, less
		//    what the tenant uses on every node.
		if !reported && tenantID != "" && h.authManager != nil {
			tenant, err := h.authManager.GetTenant(ctx, tenantID)
			if err != nil {
				logrus.WithError(err).WithField("tenantID", tenantID).Error("Failed to get tenant for SOSAPI capacity")
			} else if tenant.MaxStorageBytes > 0 {
				totalCapacity = tenant.MaxStorageBytes
				usedCapacity := tenant.CurrentStorageBytes
				if h.tenantUsage != nil {
					usedCapacity = h.tenantUsage(ctx, tenant)
				}
				availableCapacity = totalCapacity - usedCapacity
				if availableCapacity < 0 {
					availableCapacity = 0
				}
				reported = true
				logrus.WithFields(logrus.Fields{
					"tenant_id":   tenantID,
					"quota_bytes": totalCapacity,
					"used_bytes":  usedCapacity,
					"free_bytes":  availableCapacity,
				}).Info("SOSAPI capacity calculated from tenant quota")
			}
		}

		// 3) The room of a cluster whose nodes all hold every bucket.
		if !reported && h.clusterSpace != nil {
			if capacity, available, ok := h.clusterSpace(ctx); ok {
				totalCapacity, availableCapacity = capacity, available
				reported = true
				logrus.WithFields(logrus.Fields{
					"total_bytes": totalCapacity,
					"free_bytes":  availableCapacity,
				}).Info("SOSAPI capacity calculated from the cluster")
			}
		}

		// 4) The disk of this node, where the bucket is.
		if !reported && h.dataDir != "" {
			diskInfo, err := disk.Usage(h.dataDir)
			if err != nil {
				logrus.WithError(err).Warn("Failed to get disk usage, using defaults")
			} else {
				totalCapacity = int64(diskInfo.Total)
				availableCapacity = int64(diskInfo.Free)
				logrus.WithFields(logrus.Fields{
					"total_bytes": totalCapacity,
					"free_bytes":  availableCapacity,
					"used_bytes":  int64(diskInfo.Used),
					"data_dir":    h.dataDir,
				}).Info("SOSAPI capacity calculated from disk")
			}
		}

		data, err := generateCapacityXML(totalCapacity, availableCapacity)
		if err != nil {
			return nil, "", err
		}
		return data, "application/xml", nil
	}

	return nil, "", fmt.Errorf("unknown SOSAPI object: %s", objectKey)
}
