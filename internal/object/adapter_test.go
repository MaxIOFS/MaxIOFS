package object

import (
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
)

// TestToMetadataObject tests conversion from Object to ObjectMetadata
func TestToMetadataObject(t *testing.T) {
	t.Run("Nil object", func(t *testing.T) {
		result := toMetadataObject(nil)
		assert.Nil(t, result)
	})

	t.Run("Basic object without extras", func(t *testing.T) {
		obj := &Object{
			Key:          "test-key",
			Bucket:       "test-bucket",
			Size:         1024,
			LastModified: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			ETag:         "abc123",
			ContentType:  "text/plain",
			Metadata:     map[string]string{"custom": "value"},
			StorageClass: StorageClassStandard,
			VersionID:    "v1",
		}

		result := toMetadataObject(obj)

		assert.NotNil(t, result)
		assert.Equal(t, "test-key", result.Key)
		assert.Equal(t, "test-bucket", result.Bucket)
		assert.Equal(t, int64(1024), result.Size)
		assert.Equal(t, "abc123", result.ETag)
		assert.Equal(t, "text/plain", result.ContentType)
		assert.Equal(t, StorageClassStandard, result.StorageClass)
		assert.Equal(t, "v1", result.VersionID)
		assert.Equal(t, map[string]string{"custom": "value"}, result.Metadata)
		assert.Nil(t, result.Retention)
		assert.False(t, result.LegalHold)
	})

	t.Run("Object with retention", func(t *testing.T) {
		retainDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		obj := &Object{
			Key:    "test-key",
			Bucket: "test-bucket",
			Retention: &RetentionConfig{
				Mode:            RetentionModeGovernance,
				RetainUntilDate: retainDate,
			},
		}

		result := toMetadataObject(obj)

		assert.NotNil(t, result.Retention)
		assert.Equal(t, RetentionModeGovernance, result.Retention.Mode)
		assert.Equal(t, retainDate, result.Retention.RetainUntilDate)
	})

	t.Run("Object with legal hold ON", func(t *testing.T) {
		obj := &Object{
			Key:    "test-key",
			Bucket: "test-bucket",
			LegalHold: &LegalHoldConfig{
				Status: LegalHoldStatusOn,
			},
		}

		result := toMetadataObject(obj)
		assert.True(t, result.LegalHold)
	})

	t.Run("Object with legal hold OFF", func(t *testing.T) {
		obj := &Object{
			Key:    "test-key",
			Bucket: "test-bucket",
			LegalHold: &LegalHoldConfig{
				Status: LegalHoldStatusOff,
			},
		}

		result := toMetadataObject(obj)
		assert.False(t, result.LegalHold)
	})

	t.Run("Object with tags", func(t *testing.T) {
		obj := &Object{
			Key:    "test-key",
			Bucket: "test-bucket",
			Tags: &TagSet{
				Tags: []Tag{
					{Key: "environment", Value: "production"},
					{Key: "owner", Value: "admin"},
				},
			},
		}

		result := toMetadataObject(obj)

		assert.NotNil(t, result.Tags)
		assert.Len(t, result.Tags, 2)
		assert.Equal(t, "production", result.Tags["environment"])
		assert.Equal(t, "admin", result.Tags["owner"])
	})

	t.Run("Object with ACL", func(t *testing.T) {
		obj := &Object{
			Key:    "test-key",
			Bucket: "test-bucket",
			ACL: &ACL{
				Owner: Owner{
					ID:          "owner-123",
					DisplayName: "Test Owner",
				},
				Grants: []Grant{
					{
						Grantee: Grantee{
							Type:        GranteeTypeCanonicalUser,
							ID:          "user-456",
							DisplayName: "Test User",
						},
						Permission: PermissionRead,
					},
				},
			},
		}

		result := toMetadataObject(obj)

		assert.NotNil(t, result.ACL)
		assert.NotNil(t, result.ACL.Owner)
		assert.Equal(t, "owner-123", result.ACL.Owner.ID)
		assert.Equal(t, "Test Owner", result.ACL.Owner.DisplayName)
		assert.Len(t, result.ACL.Grants, 1)
		assert.Equal(t, PermissionRead, result.ACL.Grants[0].Permission)
	})
}

// TestFromMetadataObject tests conversion from ObjectMetadata to Object
func TestFromMetadataObject(t *testing.T) {
	t.Run("Nil metadata object", func(t *testing.T) {
		result := fromMetadataObject(nil)
		assert.Nil(t, result)
	})

	t.Run("Basic metadata object without extras", func(t *testing.T) {
		metaObj := &metadata.ObjectMetadata{
			Key:          "test-key",
			Bucket:       "test-bucket",
			Size:         1024,
			LastModified: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			ETag:         "abc123",
			ContentType:  "text/plain",
			Metadata:     map[string]string{"custom": "value"},
			StorageClass: StorageClassStandard,
			VersionID:    "v1",
		}

		result := fromMetadataObject(metaObj)

		assert.NotNil(t, result)
		assert.Equal(t, "test-key", result.Key)
		assert.Equal(t, "test-bucket", result.Bucket)
		assert.Equal(t, int64(1024), result.Size)
		assert.Equal(t, "abc123", result.ETag)
		assert.Equal(t, "text/plain", result.ContentType)
		assert.Equal(t, StorageClassStandard, result.StorageClass)
		assert.Equal(t, "v1", result.VersionID)
		assert.Equal(t, map[string]string{"custom": "value"}, result.Metadata)
		assert.Nil(t, result.Retention)
		assert.NotNil(t, result.LegalHold)
		assert.Equal(t, LegalHoldStatusOff, result.LegalHold.Status)
	})

	t.Run("Metadata object with retention", func(t *testing.T) {
		retainDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		metaObj := &metadata.ObjectMetadata{
			Key:    "test-key",
			Bucket: "test-bucket",
			Retention: &metadata.RetentionMetadata{
				Mode:            RetentionModeCompliance,
				RetainUntilDate: retainDate,
			},
		}

		result := fromMetadataObject(metaObj)

		assert.NotNil(t, result.Retention)
		assert.Equal(t, RetentionModeCompliance, result.Retention.Mode)
		assert.Equal(t, retainDate, result.Retention.RetainUntilDate)
	})

	t.Run("Metadata object with legal hold ON", func(t *testing.T) {
		metaObj := &metadata.ObjectMetadata{
			Key:       "test-key",
			Bucket:    "test-bucket",
			LegalHold: true,
		}

		result := fromMetadataObject(metaObj)

		assert.NotNil(t, result.LegalHold)
		assert.Equal(t, LegalHoldStatusOn, result.LegalHold.Status)
	})

	t.Run("Metadata object with legal hold OFF", func(t *testing.T) {
		metaObj := &metadata.ObjectMetadata{
			Key:       "test-key",
			Bucket:    "test-bucket",
			LegalHold: false,
		}

		result := fromMetadataObject(metaObj)

		assert.NotNil(t, result.LegalHold)
		assert.Equal(t, LegalHoldStatusOff, result.LegalHold.Status)
	})

	t.Run("Metadata object with tags", func(t *testing.T) {
		metaObj := &metadata.ObjectMetadata{
			Key:    "test-key",
			Bucket: "test-bucket",
			Tags: map[string]string{
				"environment": "staging",
				"team":        "backend",
			},
		}

		result := fromMetadataObject(metaObj)

		assert.NotNil(t, result.Tags)
		assert.Len(t, result.Tags.Tags, 2)

		// Tags might be in any order since they come from a map
		tagMap := make(map[string]string)
		for _, tag := range result.Tags.Tags {
			tagMap[tag.Key] = tag.Value
		}
		assert.Equal(t, "staging", tagMap["environment"])
		assert.Equal(t, "backend", tagMap["team"])
	})

	t.Run("Metadata object with ACL", func(t *testing.T) {
		metaObj := &metadata.ObjectMetadata{
			Key:    "test-key",
			Bucket: "test-bucket",
			ACL: &metadata.ACLMetadata{
				Owner: &metadata.Owner{
					ID:          "owner-789",
					DisplayName: "Meta Owner",
				},
				Grants: []metadata.Grant{
					{
						Grantee: &metadata.Grantee{
							Type: GranteeTypeGroup,
							URI:  GroupAllUsers,
						},
						Permission: PermissionRead,
					},
				},
			},
		}

		result := fromMetadataObject(metaObj)

		assert.NotNil(t, result.ACL)
		assert.Equal(t, "owner-789", result.ACL.Owner.ID)
		assert.Equal(t, "Meta Owner", result.ACL.Owner.DisplayName)
		assert.Len(t, result.ACL.Grants, 1)
		assert.Equal(t, GranteeTypeGroup, result.ACL.Grants[0].Grantee.Type)
		assert.Equal(t, GroupAllUsers, result.ACL.Grants[0].Grantee.URI)
	})
}

// TestToMetadataACL tests ACL conversion to metadata
func TestToMetadataACL(t *testing.T) {
	t.Run("Nil ACL", func(t *testing.T) {
		result := toMetadataACL(nil)
		assert.Nil(t, result)
	})

	t.Run("ACL with owner only", func(t *testing.T) {
		acl := &ACL{
			Owner: Owner{
				ID:          "owner-123",
				DisplayName: "Test Owner",
			},
			Grants: []Grant{},
		}

		result := toMetadataACL(acl)

		assert.NotNil(t, result)
		assert.NotNil(t, result.Owner)
		assert.Equal(t, "owner-123", result.Owner.ID)
		assert.Equal(t, "Test Owner", result.Owner.DisplayName)
		assert.Len(t, result.Grants, 0)
	})

	t.Run("ACL with grants", func(t *testing.T) {
		acl := &ACL{
			Owner: Owner{
				ID:          "owner-456",
				DisplayName: "Owner",
			},
			Grants: []Grant{
				{
					Grantee: Grantee{
						Type:        GranteeTypeCanonicalUser,
						ID:          "user-1",
						DisplayName: "User One",
					},
					Permission: PermissionFullControl,
				},
				{
					Grantee: Grantee{
						Type: GranteeTypeGroup,
						URI:  GroupAuthenticatedUsers,
					},
					Permission: PermissionRead,
				},
			},
		}

		result := toMetadataACL(acl)

		assert.NotNil(t, result)
		assert.Len(t, result.Grants, 2)
		assert.Equal(t, PermissionFullControl, result.Grants[0].Permission)
		assert.Equal(t, "user-1", result.Grants[0].Grantee.ID)
		assert.Equal(t, PermissionRead, result.Grants[1].Permission)
		assert.Equal(t, GroupAuthenticatedUsers, result.Grants[1].Grantee.URI)
	})
}

// TestFromMetadataACL tests ACL conversion from metadata
func TestFromMetadataACL(t *testing.T) {
	t.Run("Nil ACL metadata", func(t *testing.T) {
		result := fromMetadataACL(nil)
		assert.Nil(t, result)
	})

	t.Run("ACL metadata with owner only", func(t *testing.T) {
		aclMeta := &metadata.ACLMetadata{
			Owner: &metadata.Owner{
				ID:          "owner-789",
				DisplayName: "Meta Owner",
			},
			Grants: []metadata.Grant{},
		}

		result := fromMetadataACL(aclMeta)

		assert.NotNil(t, result)
		assert.Equal(t, "owner-789", result.Owner.ID)
		assert.Equal(t, "Meta Owner", result.Owner.DisplayName)
		assert.Len(t, result.Grants, 0)
	})

	t.Run("ACL metadata with grants", func(t *testing.T) {
		aclMeta := &metadata.ACLMetadata{
			Owner: &metadata.Owner{
				ID:          "owner-abc",
				DisplayName: "ABC Owner",
			},
			Grants: []metadata.Grant{
				{
					Grantee: &metadata.Grantee{
						Type:         GranteeTypeCanonicalUser,
						ID:           "user-xyz",
						DisplayName:  "XYZ User",
						EmailAddress: "user@example.com",
					},
					Permission: PermissionWriteACP,
				},
			},
		}

		result := fromMetadataACL(aclMeta)

		assert.NotNil(t, result)
		assert.Len(t, result.Grants, 1)
		assert.Equal(t, GranteeTypeCanonicalUser, result.Grants[0].Grantee.Type)
		assert.Equal(t, "user-xyz", result.Grants[0].Grantee.ID)
		assert.Equal(t, "user@example.com", result.Grants[0].Grantee.EmailAddress)
		assert.Equal(t, PermissionWriteACP, result.Grants[0].Permission)
	})
}

// TestToMetadataMultipartUpload tests multipart upload conversion to metadata
func TestToMetadataMultipartUpload(t *testing.T) {
	t.Run("Nil multipart upload", func(t *testing.T) {
		result := toMetadataMultipartUpload(nil)
		assert.Nil(t, result)
	})

	t.Run("Basic multipart upload", func(t *testing.T) {
		initiated := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		mu := &MultipartUpload{
			UploadID:     "upload-123",
			Bucket:       "test-bucket",
			Key:          "large-file.bin",
			Initiated:    initiated,
			StorageClass: StorageClassStandard,
			Metadata: map[string]string{
				"user": "admin",
			},
			Parts: []Part{
				{PartNumber: 1, ETag: "etag1", Size: 5242880},
				{PartNumber: 2, ETag: "etag2", Size: 5242880},
			},
		}

		result := toMetadataMultipartUpload(mu)

		assert.NotNil(t, result)
		assert.Equal(t, "upload-123", result.UploadID)
		assert.Equal(t, "test-bucket", result.Bucket)
		assert.Equal(t, "large-file.bin", result.Key)
		assert.Equal(t, initiated, result.Initiated)
		assert.Equal(t, StorageClassStandard, result.StorageClass)
		assert.Equal(t, map[string]string{"user": "admin"}, result.Metadata)
		// Note: Parts are not included in metadata, stored separately
	})
}

// TestFromMetadataMultipartUpload tests multipart upload conversion from metadata
func TestFromMetadataMultipartUpload(t *testing.T) {
	t.Run("Nil multipart upload metadata", func(t *testing.T) {
		result := fromMetadataMultipartUpload(nil)
		assert.Nil(t, result)
	})

	t.Run("Basic multipart upload metadata", func(t *testing.T) {
		initiated := time.Date(2025, 1, 1, 14, 30, 0, 0, time.UTC)
		muMeta := &metadata.MultipartUploadMetadata{
			UploadID:     "upload-456",
			Bucket:       "uploads-bucket",
			Key:          "video.mp4",
			Initiated:    initiated,
			StorageClass: StorageClassGlacier,
			Metadata: map[string]string{
				"type": "video",
			},
		}

		result := fromMetadataMultipartUpload(muMeta)

		assert.NotNil(t, result)
		assert.Equal(t, "upload-456", result.UploadID)
		assert.Equal(t, "uploads-bucket", result.Bucket)
		assert.Equal(t, "video.mp4", result.Key)
		assert.Equal(t, initiated, result.Initiated)
		assert.Equal(t, StorageClassGlacier, result.StorageClass)
		assert.Equal(t, map[string]string{"type": "video"}, result.Metadata)
		assert.NotNil(t, result.Parts)
		assert.Len(t, result.Parts, 0)
	})
}

// TestRoundTripConversion tests that conversions are reversible
func TestRoundTripConversion(t *testing.T) {
	t.Run("Object to metadata and back", func(t *testing.T) {
		original := &Object{
			Key:          "test-key",
			Bucket:       "test-bucket",
			Size:         2048,
			ETag:         "def456",
			ContentType:  "application/json",
			StorageClass: StorageClassStandardIA,
			Retention: &RetentionConfig{
				Mode:            RetentionModeGovernance,
				RetainUntilDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			},
			LegalHold: &LegalHoldConfig{
				Status: LegalHoldStatusOn,
			},
			Tags: &TagSet{
				Tags: []Tag{
					{Key: "project", Value: "test"},
				},
			},
		}

		// Convert to metadata
		metaObj := toMetadataObject(original)
		assert.NotNil(t, metaObj)

		// Convert back
		result := fromMetadataObject(metaObj)
		assert.NotNil(t, result)

		// Verify key fields
		assert.Equal(t, original.Key, result.Key)
		assert.Equal(t, original.Bucket, result.Bucket)
		assert.Equal(t, original.Size, result.Size)
		assert.Equal(t, original.ETag, result.ETag)
		assert.Equal(t, original.Retention.Mode, result.Retention.Mode)
		assert.Equal(t, original.LegalHold.Status, result.LegalHold.Status)
	})
}
