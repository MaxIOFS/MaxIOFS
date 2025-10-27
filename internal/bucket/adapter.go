package bucket

import (
	"github.com/maxiofs/maxiofs/internal/metadata"
)

// toMetadataBucket converts a bucket.Bucket to metadata.BucketMetadata
func toMetadataBucket(b *Bucket) *metadata.BucketMetadata {
	if b == nil {
		return nil
	}

	return &metadata.BucketMetadata{
		Name:      b.Name,
		TenantID:  b.TenantID,
		OwnerID:   b.OwnerID,
		OwnerType: b.OwnerType,
		Region:    b.Region,
		IsPublic:  b.IsPublic,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.CreatedAt, // Will be updated by metadata store

		// Configuration
		Versioning:        toMetadataVersioning(b.Versioning),
		ObjectLock:        toMetadataObjectLock(b.ObjectLock),
		Policy:            toMetadataPolicy(b.Policy),
		Lifecycle:         toMetadataLifecycle(b.Lifecycle),
		CORS:              toMetadataCORS(b.CORS),
		Encryption:        toMetadataEncryption(b.Encryption),
		PublicAccessBlock: toMetadataPublicAccessBlock(b.PublicAccessBlock),

		// Tags and metadata
		Tags:     b.Tags,
		Metadata: b.Metadata,

		// Metrics
		ObjectCount: b.ObjectCount,
		TotalSize:   b.TotalSize,
	}
}

// fromMetadataBucket converts a metadata.BucketMetadata to bucket.Bucket
func fromMetadataBucket(mb *metadata.BucketMetadata) *Bucket {
	if mb == nil {
		return nil
	}

	return &Bucket{
		Name:      mb.Name,
		TenantID:  mb.TenantID,
		OwnerID:   mb.OwnerID,
		OwnerType: mb.OwnerType,
		Region:    mb.Region,
		IsPublic:  mb.IsPublic,
		CreatedAt: mb.CreatedAt,

		// Configuration
		Versioning:        fromMetadataVersioning(mb.Versioning),
		ObjectLock:        fromMetadataObjectLock(mb.ObjectLock),
		Policy:            fromMetadataPolicy(mb.Policy),
		Lifecycle:         fromMetadataLifecycle(mb.Lifecycle),
		CORS:              fromMetadataCORS(mb.CORS),
		Encryption:        fromMetadataEncryption(mb.Encryption),
		PublicAccessBlock: fromMetadataPublicAccessBlock(mb.PublicAccessBlock),

		// Tags and metadata
		Tags:     mb.Tags,
		Metadata: mb.Metadata,

		// Metrics
		ObjectCount: mb.ObjectCount,
		TotalSize:   mb.TotalSize,
	}
}

// Versioning conversion
func toMetadataVersioning(v *VersioningConfig) *metadata.VersioningMetadata {
	if v == nil {
		return nil
	}
	return &metadata.VersioningMetadata{
		Enabled: v.Status == "Enabled",
		Status:  v.Status,
	}
}

func fromMetadataVersioning(v *metadata.VersioningMetadata) *VersioningConfig {
	if v == nil {
		return nil
	}
	return &VersioningConfig{
		Status: v.Status,
	}
}

// ObjectLock conversion
func toMetadataObjectLock(o *ObjectLockConfig) *metadata.ObjectLockMetadata {
	if o == nil {
		return nil
	}

	var rule *metadata.ObjectLockRuleMetadata
	if o.Rule != nil {
		rule = &metadata.ObjectLockRuleMetadata{
			DefaultRetention: toMetadataRetention(o.Rule.DefaultRetention),
		}
	}

	return &metadata.ObjectLockMetadata{
		Enabled: o.ObjectLockEnabled,
		Rule:    rule,
	}
}

func fromMetadataObjectLock(o *metadata.ObjectLockMetadata) *ObjectLockConfig {
	if o == nil {
		return nil
	}

	var rule *ObjectLockRule
	if o.Rule != nil {
		rule = &ObjectLockRule{
			DefaultRetention: fromMetadataRetention(o.Rule.DefaultRetention),
		}
	}

	return &ObjectLockConfig{
		ObjectLockEnabled: o.Enabled,
		Rule:              rule,
	}
}

// Retention conversion
func toMetadataRetention(r *DefaultRetention) *metadata.RetentionMetadata {
	if r == nil {
		return nil
	}
	return &metadata.RetentionMetadata{
		Mode:  r.Mode,
		Days:  r.Days,
		Years: r.Years,
	}
}

func fromMetadataRetention(r *metadata.RetentionMetadata) *DefaultRetention {
	if r == nil {
		return nil
	}
	return &DefaultRetention{
		Mode:  r.Mode,
		Days:  r.Days,
		Years: r.Years,
	}
}

// Policy conversion
func toMetadataPolicy(p *Policy) *metadata.PolicyMetadata {
	if p == nil {
		return nil
	}

	statements := make([]metadata.PolicyStatement, len(p.Statement))
	for i, stmt := range p.Statement {
		statements[i] = metadata.PolicyStatement{
			Sid:       stmt.Sid,
			Effect:    stmt.Effect,
			Principal: stmt.Principal,
			Action:    stmt.Action,
			Resource:  stmt.Resource,
			Condition: stmt.Condition,
		}
	}

	return &metadata.PolicyMetadata{
		Version:   p.Version,
		Statement: statements,
	}
}

func fromMetadataPolicy(p *metadata.PolicyMetadata) *Policy {
	if p == nil {
		return nil
	}

	statements := make([]Statement, len(p.Statement))
	for i, stmt := range p.Statement {
		statements[i] = Statement{
			Sid:       stmt.Sid,
			Effect:    stmt.Effect,
			Principal: stmt.Principal,
			Action:    stmt.Action,
			Resource:  stmt.Resource,
			Condition: stmt.Condition,
		}
	}

	return &Policy{
		Version:   p.Version,
		Statement: statements,
	}
}

// Lifecycle conversion
func toMetadataLifecycle(l *LifecycleConfig) *metadata.LifecycleMetadata {
	if l == nil {
		return nil
	}

	rules := make([]metadata.LifecycleRule, len(l.Rules))
	for i, rule := range l.Rules {
		rules[i] = toMetadataLifecycleRule(&rule)
	}

	return &metadata.LifecycleMetadata{
		Rules: rules,
	}
}

func fromMetadataLifecycle(l *metadata.LifecycleMetadata) *LifecycleConfig {
	if l == nil {
		return nil
	}

	rules := make([]LifecycleRule, len(l.Rules))
	for i, rule := range l.Rules {
		rules[i] = fromMetadataLifecycleRule(&rule)
	}

	return &LifecycleConfig{
		Rules: rules,
	}
}

func toMetadataLifecycleRule(r *LifecycleRule) metadata.LifecycleRule {
	rule := metadata.LifecycleRule{
		ID:     r.ID,
		Status: r.Status,
	}

	// Simple prefix filter
	rule.Filter = &metadata.LifecycleFilter{
		Prefix: r.Filter.Prefix,
	}

	// Expiration
	if r.Expiration != nil {
		days := 0
		if r.Expiration.Days != nil {
			days = *r.Expiration.Days
		}
		rule.Expiration = &metadata.LifecycleExpiration{
			Days: days,
		}
		if r.Expiration.ExpiredObjectDeleteMarker != nil {
			rule.Expiration.ExpiredObjectDeleteMarker = *r.Expiration.ExpiredObjectDeleteMarker
		}
	}

	// Transition
	if r.Transition != nil {
		days := 0
		if r.Transition.Days != nil {
			days = *r.Transition.Days
		}
		rule.Transitions = []metadata.LifecycleTransition{
			{
				Days:         days,
				StorageClass: r.Transition.StorageClass,
			},
		}
	}

	// NoncurrentVersionExpiration
	if r.NoncurrentVersionExpiration != nil {
		rule.NoncurrentVersionExpiration = &metadata.NoncurrentExpiration{
			NoncurrentDays: r.NoncurrentVersionExpiration.NoncurrentDays,
		}
	}

	// AbortIncompleteMultipartUpload
	if r.AbortIncompleteMultipartUpload != nil {
		rule.AbortIncompleteMultipartUpload = &metadata.AbortMultipartMetadata{
			DaysAfterInitiation: r.AbortIncompleteMultipartUpload.DaysAfterInitiation,
		}
	}

	return rule
}

func fromMetadataLifecycleRule(r *metadata.LifecycleRule) LifecycleRule {
	rule := LifecycleRule{
		ID:     r.ID,
		Status: r.Status,
	}

	// Simple prefix filter
	if r.Filter != nil {
		rule.Filter = LifecycleFilter{
			Prefix: r.Filter.Prefix,
		}
	}

	// Expiration
	if r.Expiration != nil {
		days := r.Expiration.Days
		deleteMarker := r.Expiration.ExpiredObjectDeleteMarker
		rule.Expiration = &LifecycleExpiration{
			Days:                      &days,
			ExpiredObjectDeleteMarker: &deleteMarker,
		}
	}

	// Transition (only first one)
	if len(r.Transitions) > 0 {
		t := r.Transitions[0]
		days := t.Days
		rule.Transition = &LifecycleTransition{
			Days:         &days,
			StorageClass: t.StorageClass,
		}
	}

	// NoncurrentVersionExpiration
	if r.NoncurrentVersionExpiration != nil {
		rule.NoncurrentVersionExpiration = &NoncurrentVersionExpiration{
			NoncurrentDays: r.NoncurrentVersionExpiration.NoncurrentDays,
		}
	}

	// AbortIncompleteMultipartUpload
	if r.AbortIncompleteMultipartUpload != nil {
		rule.AbortIncompleteMultipartUpload = &LifecycleAbortIncompleteMultipartUpload{
			DaysAfterInitiation: r.AbortIncompleteMultipartUpload.DaysAfterInitiation,
		}
	}

	return rule
}

// CORS conversion
func toMetadataCORS(c *CORSConfig) *metadata.CORSMetadata {
	if c == nil {
		return nil
	}

	rules := make([]metadata.CORSRule, len(c.CORSRules))
	for i, rule := range c.CORSRules {
		maxAge := 0
		if rule.MaxAgeSeconds != nil {
			maxAge = *rule.MaxAgeSeconds
		}
		rules[i] = metadata.CORSRule{
			ID:             rule.ID,
			AllowedOrigins: rule.AllowedOrigins,
			AllowedMethods: rule.AllowedMethods,
			AllowedHeaders: rule.AllowedHeaders,
			ExposeHeaders:  rule.ExposeHeaders,
			MaxAgeSeconds:  maxAge,
		}
	}

	return &metadata.CORSMetadata{
		Rules: rules,
	}
}

func fromMetadataCORS(c *metadata.CORSMetadata) *CORSConfig {
	if c == nil {
		return nil
	}

	rules := make([]CORSRule, len(c.Rules))
	for i, rule := range c.Rules {
		maxAge := rule.MaxAgeSeconds
		rules[i] = CORSRule{
			ID:             rule.ID,
			AllowedOrigins: rule.AllowedOrigins,
			AllowedMethods: rule.AllowedMethods,
			AllowedHeaders: rule.AllowedHeaders,
			ExposeHeaders:  rule.ExposeHeaders,
			MaxAgeSeconds:  &maxAge,
		}
	}

	return &CORSConfig{
		CORSRules: rules,
	}
}

// Encryption conversion (simplified)
func toMetadataEncryption(e *EncryptionConfig) *metadata.EncryptionMetadata {
	if e == nil {
		return nil
	}

	// Simplified: EncryptionConfig in bucket package is different
	// Just store the basic type and key
	sseConfig := &metadata.SSEConfig{
		SSEAlgorithm:   e.Type,
		KMSMasterKeyID: e.KMSKeyID,
	}

	return &metadata.EncryptionMetadata{
		Rules: []metadata.EncryptionRule{
			{
				ApplyServerSideEncryptionByDefault: sseConfig,
			},
		},
	}
}

func fromMetadataEncryption(e *metadata.EncryptionMetadata) *EncryptionConfig {
	if e == nil || len(e.Rules) == 0 {
		return nil
	}

	rule := e.Rules[0]
	if rule.ApplyServerSideEncryptionByDefault == nil {
		return nil
	}

	return &EncryptionConfig{
		Type:     rule.ApplyServerSideEncryptionByDefault.SSEAlgorithm,
		KMSKeyID: rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID,
	}
}

// PublicAccessBlock conversion
func toMetadataPublicAccessBlock(p *PublicAccessBlock) *metadata.PublicAccessBlockMetadata {
	if p == nil {
		return nil
	}
	return &metadata.PublicAccessBlockMetadata{
		BlockPublicAcls:       p.BlockPublicAcls,
		IgnorePublicAcls:      p.IgnorePublicAcls,
		BlockPublicPolicy:     p.BlockPublicPolicy,
		RestrictPublicBuckets: p.RestrictPublicBuckets,
	}
}

func fromMetadataPublicAccessBlock(p *metadata.PublicAccessBlockMetadata) *PublicAccessBlock {
	if p == nil {
		return nil
	}
	return &PublicAccessBlock{
		BlockPublicAcls:       p.BlockPublicAcls,
		IgnorePublicAcls:      p.IgnorePublicAcls,
		BlockPublicPolicy:     p.BlockPublicPolicy,
		RestrictPublicBuckets: p.RestrictPublicBuckets,
	}
}
