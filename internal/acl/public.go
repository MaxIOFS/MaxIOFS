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
