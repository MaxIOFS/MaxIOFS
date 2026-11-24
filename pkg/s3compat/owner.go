package s3compat

import (
	"context"

	"github.com/maxiofs/maxiofs/internal/acl"
)

// bucketOwner is the owner S3 shows for a bucket and for what it holds: the
// bucket's owner, by ID alone, as AWS shows owners since November 2025.
func (h *Handler) bucketOwner(ctx context.Context, tenantID, bucketName string) Owner {
	if info, err := h.bucketManager.GetBucketInfo(ctx, tenantID, bucketName); err == nil && info.OwnerID != "" {
		return Owner{ID: info.OwnerID}
	}
	return Owner{ID: "maxiofs"}
}

// presentedACL is aclData as S3 shows it: owned by owner, a grant to whoever
// stored it as owner shown as a grant to owner, no display names.
func presentedACL(aclData *acl.ACL, owner Owner) *acl.ACL {
	shown := *aclData
	shown.Owner = acl.Owner{ID: owner.ID}
	shown.Grants = make([]acl.Grant, len(aclData.Grants))
	for i, grant := range aclData.Grants {
		grant.Grantee.DisplayName = ""
		if grant.Grantee.Type == acl.GranteeTypeCanonicalUser && grant.Grantee.ID == aclData.Owner.ID {
			grant.Grantee.ID = owner.ID
		}
		shown.Grants[i] = grant
	}
	return &shown
}

// ownerFullControl is the grant of FULL_CONTROL to the owner of id.
func ownerFullControl(id string) acl.Grant {
	return acl.Grant{
		Grantee:    acl.Grantee{Type: acl.GranteeTypeCanonicalUser, ID: id},
		Permission: acl.PermissionFullControl,
	}
}
