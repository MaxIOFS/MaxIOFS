package acl

// GetCannedACLGrants returns the grants for a canned ACL
// Returns nil if the canned ACL is not valid
func GetCannedACLGrants(cannedACL string, ownerID, ownerDisplayName string) []Grant {
	switch cannedACL {
	case CannedACLPrivate:
		return []Grant{
			{
				Grantee: Grantee{
					Type:        GranteeTypeCanonicalUser,
					ID:          ownerID,
					DisplayName: ownerDisplayName,
				},
				Permission: PermissionFullControl,
			},
		}

	case CannedACLPublicRead:
		return []Grant{
			{
				Grantee: Grantee{
					Type:        GranteeTypeCanonicalUser,
					ID:          ownerID,
					DisplayName: ownerDisplayName,
				},
				Permission: PermissionFullControl,
			},
			{
				Grantee: Grantee{
					Type: GranteeTypeGroup,
					URI:  GroupAllUsers,
				},
				Permission: PermissionRead,
			},
		}

	case CannedACLPublicReadWrite:
		return []Grant{
			{
				Grantee: Grantee{
					Type:        GranteeTypeCanonicalUser,
					ID:          ownerID,
					DisplayName: ownerDisplayName,
				},
				Permission: PermissionFullControl,
			},
			{
				Grantee: Grantee{
					Type: GranteeTypeGroup,
					URI:  GroupAllUsers,
				},
				Permission: PermissionRead,
			},
			{
				Grantee: Grantee{
					Type: GranteeTypeGroup,
					URI:  GroupAllUsers,
				},
				Permission: PermissionWrite,
			},
		}

	case CannedACLAuthenticatedRead:
		return []Grant{
			{
				Grantee: Grantee{
					Type:        GranteeTypeCanonicalUser,
					ID:          ownerID,
					DisplayName: ownerDisplayName,
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
		}

	case CannedACLBucketOwnerRead:
		// For objects: gives owner FULL_CONTROL and bucket owner READ
		// For simplicity, we'll just give owner FULL_CONTROL
		// In production, this would need bucket owner context
		return []Grant{
			{
				Grantee: Grantee{
					Type:        GranteeTypeCanonicalUser,
					ID:          ownerID,
					DisplayName: ownerDisplayName,
				},
				Permission: PermissionFullControl,
			},
		}

	case CannedACLBucketOwnerFullControl:
		// For objects: gives owner and bucket owner FULL_CONTROL
		// For simplicity, we'll just give owner FULL_CONTROL
		// In production, this would need bucket owner context
		return []Grant{
			{
				Grantee: Grantee{
					Type:        GranteeTypeCanonicalUser,
					ID:          ownerID,
					DisplayName: ownerDisplayName,
				},
				Permission: PermissionFullControl,
			},
		}

	case CannedACLLogDeliveryWrite:
		return []Grant{
			{
				Grantee: Grantee{
					Type:        GranteeTypeCanonicalUser,
					ID:          ownerID,
					DisplayName: ownerDisplayName,
				},
				Permission: PermissionFullControl,
			},
			{
				Grantee: Grantee{
					Type: GranteeTypeGroup,
					URI:  GroupLogDelivery,
				},
				Permission: PermissionWrite,
			},
			{
				Grantee: Grantee{
					Type: GranteeTypeGroup,
					URI:  GroupLogDelivery,
				},
				Permission: PermissionReadACP,
			},
		}

	default:
		return nil
	}
}

// CreateDefaultACL creates a default private ACL for an owner
func CreateDefaultACL(ownerID, ownerDisplayName string) *ACL {
	return &ACL{
		Owner: Owner{
			ID:          ownerID,
			DisplayName: ownerDisplayName,
		},
		Grants: []Grant{
			{
				Grantee: Grantee{
					Type:        GranteeTypeCanonicalUser,
					ID:          ownerID,
					DisplayName: ownerDisplayName,
				},
				Permission: PermissionFullControl,
			},
		},
		CannedACL: CannedACLPrivate,
	}
}
