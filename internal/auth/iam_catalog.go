package auth

import "fmt"

// EnsurePermissionCatalog creates the catalogue table if it is missing and adds
// any permission it does not hold yet.
func (s *SQLiteStore) EnsurePermissionCatalog() error {
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS iam_permissions (
			action TEXT PRIMARY KEY,
			action_group TEXT NOT NULL,
			label TEXT NOT NULL,
			description TEXT,
			resource_scoped INTEGER NOT NULL DEFAULT 1,
			display_order INTEGER NOT NULL DEFAULT 0
		)
	`); err != nil {
		return fmt.Errorf("failed to create the permission catalogue: %w", err)
	}
	if _, err := s.db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_iam_permissions_group ON iam_permissions(action_group)`); err != nil {
		return err
	}

	// INSERT OR IGNORE: a label an operator changed is left alone, while a
	// permission added by a later version still appears.
	for i, p := range permissionCatalog {
		scoped := 0
		if p.ResourceScoped {
			scoped = 1
		}
		if _, err := s.db.Exec(`
			INSERT OR IGNORE INTO iam_permissions
			(action, action_group, label, description, resource_scoped, display_order)
			VALUES (?, ?, ?, ?, ?, ?)
		`, p.Action, p.Group, p.Label, nullString(p.Description), scoped, i); err != nil {
			return fmt.Errorf("failed to seed permission %s: %w", p.Action, err)
		}
	}
	return nil
}

// permissionCatalog is everything that can be granted, in display order.
var permissionCatalog = []CatalogPermission{
	{Action: "maxiofs:SuperAdmin", Group: "administration", Label: "Super administrator",
		Description: "Every permission on every tenant and every bucket, including granting them to others"},
	{Action: "maxiofs:TenantAdmin", Group: "administration", Label: "Tenant administrator",
		Description: "Every permission inside one tenant. Only a super administrator can grant it"},
	{Action: "console:Access", Group: "administration", Label: "Sign in to the console"},
	{Action: "console:ManageOwnKeys", Group: "administration", Label: "Manage own access keys",
		Description: "Create and revoke their own access keys and temporary credentials"},
	{Action: "iam:*", Group: "administration", Label: "Manage identities and policies",
		Description: "Create users, policies and roles through the console or the IAM API"},

	{Action: "s3:ListAllMyBuckets", Group: "read", Label: "List buckets"},
	{Action: "s3:ListBucket", Group: "read", Label: "List objects in a bucket", ResourceScoped: true},
	{Action: "s3:GetBucketLocation", Group: "read", Label: "Read bucket location", ResourceScoped: true},
	{Action: "s3:GetObject", Group: "read", Label: "Download objects", ResourceScoped: true},
	{Action: "s3:GetObjectAcl", Group: "read", Label: "Read object ACL", ResourceScoped: true},

	{Action: "s3:PutObject", Group: "write", Label: "Upload objects", ResourceScoped: true},
	{Action: "s3:PutObjectAcl", Group: "write", Label: "Change object ACL", ResourceScoped: true},
	{Action: "s3:RestoreObject", Group: "write", Label: "Restore archived objects", ResourceScoped: true},
	{Action: "s3:AbortMultipartUpload", Group: "write", Label: "Abort multipart uploads", ResourceScoped: true},
	{Action: "s3:ListMultipartUploadParts", Group: "write", Label: "List parts of an upload", ResourceScoped: true},
	{Action: "s3:ListBucketMultipartUploads", Group: "write", Label: "List uploads in progress", ResourceScoped: true},

	{Action: "s3:DeleteObject", Group: "delete", Label: "Delete objects", ResourceScoped: true},
	{Action: "s3:DeleteBucket", Group: "delete", Label: "Delete buckets", ResourceScoped: true},

	{Action: "s3:GetObjectTagging", Group: "tagging", Label: "Read object tags", ResourceScoped: true},
	{Action: "s3:PutObjectTagging", Group: "tagging", Label: "Write object tags", ResourceScoped: true},
	{Action: "s3:DeleteObjectTagging", Group: "tagging", Label: "Delete object tags", ResourceScoped: true},
	{Action: "s3:GetBucketTagging", Group: "tagging", Label: "Read bucket tags", ResourceScoped: true},
	{Action: "s3:PutBucketTagging", Group: "tagging", Label: "Write bucket tags", ResourceScoped: true},
	{Action: "s3:DeleteBucketTagging", Group: "tagging", Label: "Delete bucket tags", ResourceScoped: true},

	{Action: "s3:ListBucketVersions", Group: "versioning", Label: "List object versions", ResourceScoped: true},
	{Action: "s3:GetObjectVersion", Group: "versioning", Label: "Download a specific version", ResourceScoped: true},
	{Action: "s3:DeleteObjectVersion", Group: "versioning", Label: "Delete a specific version", ResourceScoped: true},
	{Action: "s3:GetBucketVersioning", Group: "versioning", Label: "Read versioning setting", ResourceScoped: true},
	{Action: "s3:PutBucketVersioning", Group: "versioning", Label: "Change versioning setting", ResourceScoped: true},

	{Action: "s3:GetObjectRetention", Group: "object-lock", Label: "Read retention", ResourceScoped: true},
	{Action: "s3:PutObjectRetention", Group: "object-lock", Label: "Set retention",
		Description: "Decide how long an object stays immutable", ResourceScoped: true},
	{Action: "s3:GetObjectLegalHold", Group: "object-lock", Label: "Read legal hold", ResourceScoped: true},
	{Action: "s3:PutObjectLegalHold", Group: "object-lock", Label: "Set legal hold", ResourceScoped: true},
	{Action: "s3:BypassGovernanceRetention", Group: "object-lock", Label: "Override governance retention",
		Description: "Delete or overwrite an object still under governance-mode protection", ResourceScoped: true},
	{Action: "s3:GetBucketObjectLockConfiguration", Group: "object-lock",
		Label: "Read the bucket's Object Lock settings", ResourceScoped: true},
	{Action: "s3:PutBucketObjectLockConfiguration", Group: "object-lock",
		Label: "Change the bucket's default retention",
		Description: "Set how long EVERY new upload stays immutable — a compliance-mode " +
			"default cannot be undone by anyone, including administrators", ResourceScoped: true},

	{Action: "s3:CreateBucket", Group: "bucket-config", Label: "Create buckets"},
	{Action: "s3:GetBucketAcl", Group: "bucket-config", Label: "Read bucket ACL", ResourceScoped: true},
	{Action: "s3:PutBucketAcl", Group: "bucket-config", Label: "Change bucket ACL", ResourceScoped: true},
	{Action: "s3:GetBucketLifecycle", Group: "bucket-config", Label: "Read lifecycle rules", ResourceScoped: true},
	{Action: "s3:PutBucketLifecycle", Group: "bucket-config", Label: "Change lifecycle rules", ResourceScoped: true},
	{Action: "s3:DeleteBucketLifecycle", Group: "bucket-config", Label: "Delete lifecycle rules", ResourceScoped: true},
	{Action: "s3:GetBucketCORS", Group: "bucket-config", Label: "Read CORS rules", ResourceScoped: true},
	{Action: "s3:PutBucketCORS", Group: "bucket-config", Label: "Change CORS rules", ResourceScoped: true},
	{Action: "s3:DeleteBucketCORS", Group: "bucket-config", Label: "Delete CORS rules", ResourceScoped: true},

	// Subresources that used to borrow another permission. Replication is
	// listed first because it is the one that moves data off the system.
	{Action: "s3:GetBucketReplication", Group: "bucket-config", Label: "Read replication rules", ResourceScoped: true},
	{Action: "s3:PutBucketReplication", Group: "bucket-config", Label: "Change replication rules",
		Description: "A replication rule names where this bucket's contents are copied to", ResourceScoped: true},
	{Action: "s3:GetEncryptionConfiguration", Group: "bucket-config", Label: "Read encryption settings", ResourceScoped: true},
	{Action: "s3:PutEncryptionConfiguration", Group: "bucket-config", Label: "Change encryption settings", ResourceScoped: true},
	{Action: "s3:GetBucketLogging", Group: "bucket-config", Label: "Read access-logging settings", ResourceScoped: true},
	{Action: "s3:PutBucketLogging", Group: "bucket-config", Label: "Change access-logging settings", ResourceScoped: true},
	{Action: "s3:GetBucketWebsite", Group: "bucket-config", Label: "Read website settings", ResourceScoped: true},
	{Action: "s3:PutBucketWebsite", Group: "bucket-config", Label: "Change website settings", ResourceScoped: true},
	{Action: "s3:DeleteBucketWebsite", Group: "bucket-config", Label: "Delete website settings", ResourceScoped: true},
	{Action: "s3:GetBucketNotification", Group: "bucket-config", Label: "Read event notifications", ResourceScoped: true},
	{Action: "s3:PutBucketNotification", Group: "bucket-config", Label: "Change event notifications", ResourceScoped: true},
	{Action: "s3:GetInventoryConfiguration", Group: "bucket-config", Label: "Read inventory settings", ResourceScoped: true},
	{Action: "s3:PutInventoryConfiguration", Group: "bucket-config", Label: "Change inventory settings", ResourceScoped: true},
	{Action: "s3:GetAccelerateConfiguration", Group: "bucket-config", Label: "Read transfer acceleration", ResourceScoped: true},
	{Action: "s3:PutAccelerateConfiguration", Group: "bucket-config", Label: "Change transfer acceleration", ResourceScoped: true},
	{Action: "s3:GetBucketRequestPayment", Group: "bucket-config", Label: "Read requester-pays setting", ResourceScoped: true},
	{Action: "s3:PutBucketRequestPayment", Group: "bucket-config", Label: "Change requester-pays setting", ResourceScoped: true},

	{Action: "s3:GetBucketPublicAccessBlock", Group: "bucket-policy", Label: "Read public-access block", ResourceScoped: true},
	{Action: "s3:PutBucketPublicAccessBlock", Group: "bucket-policy", Label: "Change public-access block",
		Description: "Decides whether bucket policies and ACLs may make objects public", ResourceScoped: true},
	{Action: "s3:GetBucketOwnershipControls", Group: "bucket-policy", Label: "Read ownership controls", ResourceScoped: true},
	{Action: "s3:PutBucketOwnershipControls", Group: "bucket-policy", Label: "Change ownership controls", ResourceScoped: true},

	{Action: "s3:GetBucketPolicy", Group: "bucket-policy", Label: "Read bucket policy", ResourceScoped: true},
	{Action: "s3:PutBucketPolicy", Group: "bucket-policy", Label: "Change bucket policy", ResourceScoped: true},
	{Action: "s3:DeleteBucketPolicy", Group: "bucket-policy", Label: "Delete bucket policy", ResourceScoped: true},
}
