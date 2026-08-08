package auth

// The policy catalogue: the single vocabulary in which every permission in
// MaxIOFS is expressed.
//
// There is one kind of user and one set of permissions per user. A request is
// the same request whether it arrived on a console session or signed with one
// of that user's access keys — the keys carry the user's permissions, they do
// not have permissions of their own.
//
// Actions are namespaced by the service they belong to:
//
//	s3:*       object and bucket operations
//	console:*  operations that only exist in the console (creating a bucket
//	           through the UI, managing one's own keys)
//	iam:*      managing identities, policies and roles
//
// The namespace is not a division between kinds of user. It exists because a
// policy has to be able to name every operation; without it, the operations
// outside S3 would need a second mechanism, which is exactly what this replaces.

// Non-S3 actions. These complete the vocabulary so that a single policy set can
// describe everything a user may do.
const (
	// ActionSuperAdmin and ActionTenantAdmin are administration itself: who may
	// act across every tenant, and who may act inside one.
	ActionSuperAdmin  = "maxiofs:SuperAdmin"
	ActionTenantAdmin = "maxiofs:TenantAdmin"

	ActionConsoleAccess = "console:Access"
	ActionManageOwnKeys = "console:ManageOwnKeys"
	ActionIAMManage     = "iam:*"
)

// Built-in policy names. Roles are built out of these, and they are what the
// console offers when an operator edits a role's permissions.
const (
	PolicyFullAccess           = "FullAccess"
	PolicyBucketCreate         = "BucketCreate"
	PolicyBucketDelete         = "BucketDelete"
	PolicyBucketConfigure      = "BucketConfigure"
	PolicyBucketManagePolicy   = "BucketManagePolicy"
	PolicyObjectRead           = "ObjectRead"
	PolicyObjectWrite          = "ObjectWrite"
	PolicyObjectDelete         = "ObjectDelete"
	PolicyObjectManageTags     = "ObjectManageTags"
	PolicyObjectManageVersions = "ObjectManageVersions"
	PolicyObjectLockManage     = "ObjectLockManage"
	PolicyObjectLockBypass     = "ObjectLockBypassGovernance"
	PolicyConsoleAccess        = "ConsoleAccess"
	PolicyManageOwnKeys        = "ManageOwnKeys"
	PolicyIAMManage            = "IAMManage"
)

// catalogEntry is one shipped policy: a name, a human description and the
// actions it permits on every resource.
type catalogEntry struct {
	Name        string
	Description string
	Actions     []string
}

// PolicyCatalog is every permission the system can express, one policy per
// coherent group of actions.
//
// The granularity is deliberate. Object Lock is three separate policies rather
// than part of "write", because permission to store an object into an immutable
// bucket, permission to set how long it stays immutable, and permission to
// override that retention are three different decisions, and an operator who
// grants the first should not be silently granting the third.
var PolicyCatalog = []catalogEntry{
	{
		Name:        PolicyFullAccess,
		Description: "Every operation, on every resource",
		Actions:     []string{"*"},
	},
	{
		Name:        PolicyBucketCreate,
		Description: "Create buckets",
		Actions:     []string{ActionCreateBucket},
	},
	{
		Name:        PolicyBucketDelete,
		Description: "Delete buckets",
		Actions:     []string{ActionDeleteBucket},
	},
	{
		Name:        PolicyBucketConfigure,
		Description: "Change bucket settings: versioning, lifecycle, CORS, tagging, ACLs",
		Actions: []string{
			ActionGetBucketVersioning, ActionPutBucketVersioning,
			ActionGetBucketLifecycle, ActionPutBucketLifecycle, ActionDeleteBucketLifecycle,
			ActionGetBucketCORS, ActionPutBucketCORS, ActionDeleteBucketCORS,
			ActionGetBucketTagging, ActionPutBucketTagging, ActionDeleteBucketTagging,
			ActionGetBucketAcl, ActionPutBucketAcl,
		},
	},
	{
		Name:        PolicyBucketManagePolicy,
		Description: "Read and write bucket policies",
		Actions:     []string{ActionGetBucketPolicy, ActionPutBucketPolicy, ActionDeleteBucketPolicy},
	},
	{
		Name:        PolicyObjectRead,
		Description: "List buckets and read objects",
		Actions: []string{
			ActionListAllMyBuckets, ActionListBucket, ActionGetBucketLocation,
			ActionGetObject, ActionGetObjectAcl,
		},
	},
	{
		Name:        PolicyObjectWrite,
		Description: "Upload objects, including multipart uploads and restores",
		Actions: []string{
			ActionPutObject, ActionPutObjectAcl, ActionRestoreObject,
			ActionAbortMultipartUpload, ActionListMultipartUploadParts,
			ActionListBucketMultipartUploads,
		},
	},
	{
		Name:        PolicyObjectDelete,
		Description: "Delete objects",
		Actions:     []string{ActionDeleteObject},
	},
	{
		Name:        PolicyObjectManageTags,
		Description: "Read and write object tags",
		Actions:     []string{ActionGetObjectTagging, ActionPutObjectTagging, ActionDeleteObjectTagging},
	},
	{
		Name:        PolicyObjectManageVersions,
		Description: "List, read and delete specific object versions",
		Actions:     []string{ActionListBucketVersions, ActionGetObjectVersion, ActionDeleteObjectVersion},
	},
	{
		Name:        PolicyObjectLockManage,
		Description: "Set retention and legal hold on objects in immutable buckets",
		Actions: []string{
			ActionGetObjectRetention, ActionPutObjectRetention,
			ActionGetObjectLegalHold, ActionPutObjectLegalHold,
			ActionGetBucketObjectLockConfiguration, ActionPutBucketObjectLockConfiguration,
		},
	},
	{
		Name: PolicyObjectLockBypass,
		Description: "Override governance-mode retention — deleting or overwriting " +
			"an object that is still under protection",
		Actions: []string{ActionBypassGovernanceRetention},
	},
	{
		Name:        PolicyConsoleAccess,
		Description: "Sign in to the web console",
		Actions:     []string{ActionConsoleAccess},
	},
	{
		Name:        PolicyManageOwnKeys,
		Description: "Create and revoke one's own access keys and temporary credentials",
		Actions:     []string{ActionManageOwnKeys},
	},
	{
		Name:        PolicyIAMManage,
		Description: "Manage identities, policies and roles",
		Actions:     []string{ActionIAMManage},
	},
}

// capabilityPolicies maps each legacy capability onto the catalogue policies
// that express it. This is the translation table: it is what lets an existing
// installation's role configuration be read as policies without anybody having
// to reconfigure anything.
//
// It must stay faithful — the equivalence tests compare the decisions the
// policy evaluator makes against the decisions the capability system made, for
// every capability, and a drift here fails them.
var capabilityPolicies = map[string][]string{
	CapBucketCreate:         {PolicyBucketCreate},
	CapBucketDelete:         {PolicyBucketDelete},
	CapBucketConfigure:      {PolicyBucketConfigure},
	CapBucketManagePolicy:   {PolicyBucketManagePolicy},
	CapObjectUpload:         {PolicyObjectWrite},
	CapObjectDownload:       {PolicyObjectRead},
	CapObjectDelete:         {PolicyObjectDelete},
	CapObjectManageTags:     {PolicyObjectManageTags},
	CapObjectManageVersions: {PolicyObjectManageVersions},
	CapConsoleAccess:        {PolicyConsoleAccess},
	CapKeysManageOwn:        {PolicyManageOwnKeys},
	CapIAMManage:            {PolicyIAMManage},
}

// PoliciesForCapability returns the catalogue policies a legacy capability
// translates to.
func PoliciesForCapability(capability string) []string {
	return capabilityPolicies[capability]
}

// CatalogEntry returns a shipped policy by name.
func CatalogEntry(name string) (catalogEntry, bool) {
	for _, entry := range PolicyCatalog {
		if entry.Name == name {
			return entry, true
		}
	}
	return catalogEntry{}, false
}

// Document renders a catalogue entry as a policy document covering every
// resource. Scoping to particular buckets is done by attaching a policy written
// for those buckets, not by narrowing these.
func (e catalogEntry) Document() string {
	doc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":[`
	for i, action := range e.Actions {
		if i > 0 {
			doc += ","
		}
		doc += `"` + action + `"`
	}
	doc += `],"Resource":["*"]}]}`
	return doc
}
