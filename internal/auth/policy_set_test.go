package auth

import "testing"

// The account boundary lives in the engine, so these cases are about what the
// engine refuses regardless of which handler asks.
func TestAccountBoundary_EnforcedByTheEngine(t *testing.T) {
	allowEverything := []string{
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"*","Resource":"*"}]}`,
	}

	cases := []struct {
		name           string
		principal      string
		resourceTenant string
		action         string
		allowed        bool
	}{
		{"inside its own account", "t1", "t1", ActionPutObject, true},
		{"a tenant cannot reach the shared namespace", "t1", "", ActionPutObject, false},
		{"a tenant-less principal owns the shared namespace", "", "", ActionPutObject, true},
		{"reading across accounts", "t1", "t2", ActionGetObject, false},
		{"writing across accounts", "t1", "t2", ActionPutObject, false},
		{"a global operator without super admin cannot cross", "", "t2", ActionGetObject, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			documents := allowEverything
			if tc.principal == "" {
				// A tenant-less principal holding a plain S3 grant: broad, but
				// not the super administrator that crossing requires.
				documents = []string{
					`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`,
				}
			}
			set := &PolicySet{UserID: "u", TenantID: tc.principal, Documents: documents}
			got := set.Allows(AccessRequest{
				Action:   tc.action,
				Resource: "arn:aws:s3:::shared-name/key.txt",
				Owner:    OwnedBy(tc.resourceTenant),
			})
			if got != tc.allowed {
				t.Fatalf("allowed = %v, want %v — a policy allowing everything must not cross an account", got, tc.allowed)
			}
		})
	}
}

// A super administrator audits another account, and only reads it.
func TestAccountBoundary_SuperAdminReadsAcrossAndWritesNowhere(t *testing.T) {
	set := &PolicySet{
		UserID:   "root",
		TenantID: "",
		Documents: []string{
			`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"*","Resource":"*"}]}`,
		},
	}

	read := set.Allows(AccessRequest{
		Action: ActionGetObject, Resource: "arn:aws:s3:::b/k", Owner: OwnedBy("t2"),
	})
	if !read {
		t.Fatal("a super administrator must be able to audit another account")
	}

	write := set.Allows(AccessRequest{
		Action: ActionPutObject, Resource: "arn:aws:s3:::b/k", Owner: OwnedBy("t2"),
	})
	if write {
		t.Fatal("auditing is not owning: a super administrator must not write into another account")
	}
}

// Forgetting to say who owns the resource must deny, not open the boundary.
//
// The owner used to be a plain string, where an unset field and the shared
// namespace were the same value — so a caller that forgot it got the permissive
// reading of the two. That is the mistake the boundary exists to make
// impossible, so it is pinned here.
func TestAccountBoundary_AnUnstatedOwnerIsRefused(t *testing.T) {
	set := &PolicySet{UserID: "u", TenantID: "t1", Documents: []string{
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"*","Resource":"*"}]}`,
	}}

	if set.Allows(AccessRequest{Action: ActionPutObject, Resource: "arn:aws:s3:::b/k"}) {
		t.Fatal("a request whose owner nobody resolved must be refused")
	}

	// Said out loud, the same request is judged on its merits.
	if !set.Allows(AccessRequest{
		Action: ActionPutObject, Resource: "arn:aws:s3:::b/k", Owner: OwnedBy("t1"),
	}) {
		t.Fatal("a resource in the principal's own account must still be allowed")
	}

	// And the shared namespace is a deliberate answer, not an omission; a tenant
	// still cannot claim it as its own namespace.
	if set.Allows(AccessRequest{
		Action: ActionPutObject, Resource: "arn:aws:s3:::b/k", Owner: OwnedBy(""),
	}) {
		t.Fatal("a tenant principal must not reach the shared namespace")
	}
}
