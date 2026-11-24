package acl

// IsPublicGroup reports a grantee group AWS S3 counts as public: everyone, or
// every authenticated user.
func IsPublicGroup(uri string) bool {
	return uri == GroupAllUsers || uri == GroupAuthenticatedUsers
}

// IsPublic reports whether the ACL grants anything to a public group.
func (a *ACL) IsPublic() bool {
	if a == nil {
		return false
	}
	for _, g := range a.Grants {
		if IsPublicGroup(g.Grantee.URI) {
			return true
		}
	}
	return false
}

// IsPublicCannedACL reports a canned ACL that grants to a public group.
func IsPublicCannedACL(cannedACL string) bool {
	return (&ACL{Grants: GetCannedACLGrants(cannedACL, "", "")}).IsPublic()
}

// OwnerOnly reports whether the ACL grants nothing to anyone but its owner: the
// only ACL a bucket whose ACLs are disabled takes, as in AWS.
func (a *ACL) OwnerOnly() bool {
	if a == nil {
		return true
	}
	for _, g := range a.Grants {
		if g.Grantee.Type != GranteeTypeCanonicalUser || g.Grantee.ID != a.Owner.ID {
			return false
		}
	}
	return true
}

// OwnerOnlyCannedACL reports a canned ACL, or none, that leaves everything to
// the owner.
func OwnerOnlyCannedACL(cannedACL string) bool {
	return cannedACL == "" || cannedACL == CannedACLPrivate || cannedACL == CannedACLBucketOwnerFullControl
}
