package object

import (
	"github.com/maxiofs/maxiofs/internal/metadata"
)

// toMetadataObject converts an object.Object to metadata.ObjectMetadata
func toMetadataObject(o *Object) *metadata.ObjectMetadata {
	if o == nil {
		return nil
	}

	metaObj := &metadata.ObjectMetadata{
		Key:          o.Key,
		Bucket:       o.Bucket,
		Size:         o.Size,
		LastModified: o.LastModified,
		ETag:         o.ETag,
		ContentType:  o.ContentType,
		Metadata:     o.Metadata,
		StorageClass: o.StorageClass,
		VersionID:    o.VersionID,
	}

	// Object Lock - Retention
	if o.Retention != nil {
		metaObj.Retention = &metadata.RetentionMetadata{
			Mode:            o.Retention.Mode,
			RetainUntilDate: o.Retention.RetainUntilDate,
		}
	}

	// Object Lock - Legal Hold
	if o.LegalHold != nil {
		metaObj.LegalHold = (o.LegalHold.Status == LegalHoldStatusOn)
	}

	// Tags
	if o.Tags != nil && len(o.Tags.Tags) > 0 {
		tags := make(map[string]string)
		for _, tag := range o.Tags.Tags {
			tags[tag.Key] = tag.Value
		}
		metaObj.Tags = tags
	}

	// ACL
	if o.ACL != nil {
		metaObj.ACL = toMetadataACL(o.ACL)
	}

	return metaObj
}

// fromMetadataObject converts a metadata.ObjectMetadata to object.Object
func fromMetadataObject(mo *metadata.ObjectMetadata) *Object {
	if mo == nil {
		return nil
	}

	obj := &Object{
		Key:          mo.Key,
		Bucket:       mo.Bucket,
		Size:         mo.Size,
		LastModified: mo.LastModified,
		ETag:         mo.ETag,
		ContentType:  mo.ContentType,
		Metadata:     mo.Metadata,
		StorageClass: mo.StorageClass,
		VersionID:    mo.VersionID,
	}

	// Object Lock - Retention
	if mo.Retention != nil {
		obj.Retention = &RetentionConfig{
			Mode:            mo.Retention.Mode,
			RetainUntilDate: mo.Retention.RetainUntilDate,
		}
	}

	// Object Lock - Legal Hold
	if mo.LegalHold {
		obj.LegalHold = &LegalHoldConfig{
			Status: LegalHoldStatusOn,
		}
	} else {
		obj.LegalHold = &LegalHoldConfig{
			Status: LegalHoldStatusOff,
		}
	}

	// Tags
	if len(mo.Tags) > 0 {
		tags := make([]Tag, 0, len(mo.Tags))
		for k, v := range mo.Tags {
			tags = append(tags, Tag{Key: k, Value: v})
		}
		obj.Tags = &TagSet{Tags: tags}
	}

	// ACL
	if mo.ACL != nil {
		obj.ACL = fromMetadataACL(mo.ACL)
	}

	return obj
}

// toMetadataACL converts object.ACL to metadata.ACLMetadata
func toMetadataACL(acl *ACL) *metadata.ACLMetadata {
	if acl == nil {
		return nil
	}

	metaACL := &metadata.ACLMetadata{
		Owner: &metadata.Owner{
			ID:          acl.Owner.ID,
			DisplayName: acl.Owner.DisplayName,
		},
	}

	if len(acl.Grants) > 0 {
		metaACL.Grants = make([]metadata.Grant, len(acl.Grants))
		for i, grant := range acl.Grants {
			metaACL.Grants[i] = metadata.Grant{
				Grantee: &metadata.Grantee{
					Type:         grant.Grantee.Type,
					ID:           grant.Grantee.ID,
					DisplayName:  grant.Grantee.DisplayName,
					EmailAddress: grant.Grantee.EmailAddress,
					URI:          grant.Grantee.URI,
				},
				Permission: grant.Permission,
			}
		}
	}

	return metaACL
}

// fromMetadataACL converts metadata.ACLMetadata to object.ACL
func fromMetadataACL(acl *metadata.ACLMetadata) *ACL {
	if acl == nil {
		return nil
	}

	objACL := &ACL{}

	if acl.Owner != nil {
		objACL.Owner = Owner{
			ID:          acl.Owner.ID,
			DisplayName: acl.Owner.DisplayName,
		}
	}

	if len(acl.Grants) > 0 {
		objACL.Grants = make([]Grant, len(acl.Grants))
		for i, grant := range acl.Grants {
			grantee := Grantee{}
			if grant.Grantee != nil {
				grantee = Grantee{
					Type:         grant.Grantee.Type,
					ID:           grant.Grantee.ID,
					DisplayName:  grant.Grantee.DisplayName,
					EmailAddress: grant.Grantee.EmailAddress,
					URI:          grant.Grantee.URI,
				}
			}
			objACL.Grants[i] = Grant{
				Grantee:    grantee,
				Permission: grant.Permission,
			}
		}
	}

	return objACL
}

// toMetadataMultipartUpload converts object.MultipartUpload to metadata.MultipartUploadMetadata
func toMetadataMultipartUpload(mu *MultipartUpload) *metadata.MultipartUploadMetadata {
	if mu == nil {
		return nil
	}

	metaMU := &metadata.MultipartUploadMetadata{
		UploadID:     mu.UploadID,
		Bucket:       mu.Bucket,
		Key:          mu.Key,
		Initiated:    mu.Initiated,
		StorageClass: mu.StorageClass,
		Metadata:     mu.Metadata,
	}

	// Note: Parts are stored separately in BadgerDB, not in the upload metadata
	return metaMU
}

// fromMetadataMultipartUpload converts metadata.MultipartUploadMetadata to object.MultipartUpload
func fromMetadataMultipartUpload(mu *metadata.MultipartUploadMetadata) *MultipartUpload {
	if mu == nil {
		return nil
	}

	objMU := &MultipartUpload{
		UploadID:     mu.UploadID,
		Bucket:       mu.Bucket,
		Key:          mu.Key,
		Initiated:    mu.Initiated,
		StorageClass: mu.StorageClass,
		Metadata:     mu.Metadata,
		Parts:        []Part{}, // Parts must be fetched separately via ListParts
	}

	return objMU
}
