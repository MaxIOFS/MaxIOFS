package bucket

import (
	"maps"
	"slices"
	"time"

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
		Website:           toMetadataWebsite(b.Website),
		Logging:           toMetadataLogging(b.Logging),

		// Tags and metadata
		Tags:     b.Tags,
		Metadata: b.Metadata,

		// Metrics
		ObjectCount: b.ObjectCount,
		TotalSize:   b.TotalSize,

		// Per-bucket quota
		Quota: b.Quota,

		// HA replication
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
		CreatedAt: mb.CreatedAt,

		// Configuration
		Versioning:        fromMetadataVersioning(mb.Versioning),
		ObjectLock:        fromMetadataObjectLock(mb.ObjectLock),
		Policy:            fromMetadataPolicy(mb.Policy),
		Lifecycle:         fromMetadataLifecycle(mb.Lifecycle),
		CORS:              fromMetadataCORS(mb.CORS),
		Encryption:        fromMetadataEncryption(mb.Encryption),
		PublicAccessBlock: fromMetadataPublicAccessBlock(mb.PublicAccessBlock),
		Website:           fromMetadataWebsite(mb.Website),
		Logging:           fromMetadataLogging(mb.Logging),

		// Tags and metadata
		Tags:     mb.Tags,
		Metadata: mb.Metadata,

		// Metrics
		ObjectCount: mb.ObjectCount,
		TotalSize:   mb.TotalSize,

		// Per-bucket quota
		Quota: mb.Quota,

		// HA replication
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

// toMetadataLifecycleRule stores a rule: a date as RFC 3339, an unset number
// of days as 0.
func toMetadataLifecycleRule(r *LifecycleRule) metadata.LifecycleRule {
	rule := metadata.LifecycleRule{
		ID:     r.ID,
		Status: r.Status,
		Filter: &metadata.LifecycleFilter{
			Prefix:                r.Filter.Prefix,
			ObjectSizeGreaterThan: r.Filter.ObjectSizeGreaterThan,
			ObjectSizeLessThan:    r.Filter.ObjectSizeLessThan,
			Form:                  r.Filter.Form,
		},
	}
	if len(r.Filter.Tags) > 0 {
		rule.Filter.Tags = make(map[string]string, len(r.Filter.Tags))
		for _, tag := range r.Filter.Tags {
			rule.Filter.Tags[tag.Key] = tag.Value
		}
	}

	if e := r.Expiration; e != nil {
		rule.Expiration = &metadata.LifecycleExpiration{
			Days:                      intOrZero(e.Days),
			Date:                      storedDate(e.Date),
			ExpiredObjectDeleteMarker: e.ExpiredObjectDeleteMarker != nil && *e.ExpiredObjectDeleteMarker,
		}
	}
	for _, t := range r.Transitions {
		rule.Transitions = append(rule.Transitions, metadata.LifecycleTransition{
			Days:         intOrZero(t.Days),
			Date:         storedDate(t.Date),
			StorageClass: t.StorageClass,
		})
	}
	if n := r.NoncurrentVersionExpiration; n != nil {
		rule.NoncurrentVersionExpiration = &metadata.NoncurrentExpiration{
			NoncurrentDays:          n.NoncurrentDays,
			NewerNoncurrentVersions: n.NewerNoncurrentVersions,
		}
	}
	for _, t := range r.NoncurrentVersionTransitions {
		rule.NoncurrentVersionTransitions = append(rule.NoncurrentVersionTransitions, metadata.NoncurrentTransition{
			NoncurrentDays:          t.NoncurrentDays,
			NewerNoncurrentVersions: t.NewerNoncurrentVersions,
			StorageClass:            t.StorageClass,
		})
	}
	if a := r.AbortIncompleteMultipartUpload; a != nil {
		rule.AbortIncompleteMultipartUpload = &metadata.AbortMultipartMetadata{
			DaysAfterInitiation: a.DaysAfterInitiation,
		}
	}
	return rule
}

// fromMetadataLifecycleRule reads a stored rule. An expiration of 0 days is
// one by date or of delete markers only; a transition of 0 days without a
// date moves the objects at once.
func fromMetadataLifecycleRule(r *metadata.LifecycleRule) LifecycleRule {
	rule := LifecycleRule{
		ID:     r.ID,
		Status: r.Status,
	}

	if f := r.Filter; f != nil {
		rule.Filter = LifecycleFilter{
			Prefix:                f.Prefix,
			ObjectSizeGreaterThan: f.ObjectSizeGreaterThan,
			ObjectSizeLessThan:    f.ObjectSizeLessThan,
			Form:                  f.Form,
		}
		tags := maps.Clone(f.Tags)
		if f.And != nil {
			if rule.Filter.Prefix == "" {
				rule.Filter.Prefix = f.And.Prefix
			}
			if tags == nil && len(f.And.Tags) > 0 {
				tags = maps.Clone(f.And.Tags)
			}
		}
		for _, key := range slices.Sorted(maps.Keys(tags)) {
			rule.Filter.Tags = append(rule.Filter.Tags, Tag{Key: key, Value: tags[key]})
		}
	}

	// An expiration stored with nothing to say, its date lost before dates
	// were kept, is none.
	if e := r.Expiration; e != nil && (e.Days > 0 || readDate(e.Date) != nil || e.ExpiredObjectDeleteMarker) {
		rule.Expiration = &LifecycleExpiration{Date: readDate(e.Date)}
		if e.Days > 0 {
			days := e.Days
			rule.Expiration.Days = &days
		}
		if e.ExpiredObjectDeleteMarker {
			marker := true
			rule.Expiration.ExpiredObjectDeleteMarker = &marker
		}
	}
	for _, t := range r.Transitions {
		transition := LifecycleTransition{Date: readDate(t.Date), StorageClass: t.StorageClass}
		if transition.Date == nil {
			days := t.Days
			transition.Days = &days
		}
		rule.Transitions = append(rule.Transitions, transition)
	}
	if n := r.NoncurrentVersionExpiration; n != nil {
		rule.NoncurrentVersionExpiration = &NoncurrentVersionExpiration{
			NoncurrentDays:          n.NoncurrentDays,
			NewerNoncurrentVersions: n.NewerNoncurrentVersions,
		}
	}
	for _, t := range r.NoncurrentVersionTransitions {
		rule.NoncurrentVersionTransitions = append(rule.NoncurrentVersionTransitions, NoncurrentVersionTransition{
			NoncurrentDays:          t.NoncurrentDays,
			NewerNoncurrentVersions: t.NewerNoncurrentVersions,
			StorageClass:            t.StorageClass,
		})
	}
	if a := r.AbortIncompleteMultipartUpload; a != nil {
		rule.AbortIncompleteMultipartUpload = &LifecycleAbortIncompleteMultipartUpload{
			DaysAfterInitiation: a.DaysAfterInitiation,
		}
	}
	return rule
}

func intOrZero(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

func storedDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func readDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
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

// Website conversion
func toMetadataWebsite(w *WebsiteConfig) *metadata.WebsiteMetadata {
	if w == nil {
		return nil
	}
	rules := make([]metadata.WebsiteRoutingRuleMetadata, len(w.RoutingRules))
	for i, rr := range w.RoutingRules {
		rules[i] = metadata.WebsiteRoutingRuleMetadata{
			Condition: metadata.WebsiteRoutingConditionMetadata{
				HTTPErrorCodeReturnedEquals: rr.Condition.HTTPErrorCodeReturnedEquals,
				KeyPrefixEquals:             rr.Condition.KeyPrefixEquals,
			},
			Redirect: metadata.WebsiteRoutingRedirectMetadata{
				HostName:             rr.Redirect.HostName,
				HTTPRedirectCode:     rr.Redirect.HTTPRedirectCode,
				Protocol:             rr.Redirect.Protocol,
				ReplaceKeyPrefixWith: rr.Redirect.ReplaceKeyPrefixWith,
				ReplaceKeyWith:       rr.Redirect.ReplaceKeyWith,
			},
		}
	}
	return &metadata.WebsiteMetadata{
		IndexDocument: w.IndexDocument,
		ErrorDocument: w.ErrorDocument,
		RoutingRules:  rules,
	}
}

func fromMetadataWebsite(w *metadata.WebsiteMetadata) *WebsiteConfig {
	if w == nil {
		return nil
	}
	rules := make([]WebsiteRoutingRule, len(w.RoutingRules))
	for i, rr := range w.RoutingRules {
		rules[i] = WebsiteRoutingRule{
			Condition: WebsiteRoutingCondition{
				HTTPErrorCodeReturnedEquals: rr.Condition.HTTPErrorCodeReturnedEquals,
				KeyPrefixEquals:             rr.Condition.KeyPrefixEquals,
			},
			Redirect: WebsiteRoutingRedirect{
				HostName:             rr.Redirect.HostName,
				HTTPRedirectCode:     rr.Redirect.HTTPRedirectCode,
				Protocol:             rr.Redirect.Protocol,
				ReplaceKeyPrefixWith: rr.Redirect.ReplaceKeyPrefixWith,
				ReplaceKeyWith:       rr.Redirect.ReplaceKeyWith,
			},
		}
	}
	return &WebsiteConfig{
		IndexDocument: w.IndexDocument,
		ErrorDocument: w.ErrorDocument,
		RoutingRules:  rules,
	}
}

// toMetadataNotification converts bucket.NotificationConfig to metadata.NotificationMetadata.
func toMetadataNotification(c *NotificationConfig) *metadata.NotificationMetadata {
	if c == nil {
		return nil
	}
	conv := func(targets []NotificationTarget) []metadata.NotificationTargetMetadata {
		out := make([]metadata.NotificationTargetMetadata, len(targets))
		for i, t := range targets {
			m := metadata.NotificationTargetMetadata{
				ID:       t.ID,
				Endpoint: t.Endpoint,
				Events:   t.Events,
			}
			if t.Filter != nil {
				m.Filter = &metadata.NotificationFilterMetadata{
					Prefix: t.Filter.Prefix,
					Suffix: t.Filter.Suffix,
				}
			}
			out[i] = m
		}
		return out
	}
	return &metadata.NotificationMetadata{
		TopicConfigurations:  conv(c.TopicConfigurations),
		QueueConfigurations:  conv(c.QueueConfigurations),
		LambdaConfigurations: conv(c.LambdaConfigurations),
	}
}

// fromMetadataNotification converts metadata.NotificationMetadata to bucket.NotificationConfig.
func fromMetadataNotification(m *metadata.NotificationMetadata) *NotificationConfig {
	if m == nil {
		return &NotificationConfig{}
	}
	conv := func(targets []metadata.NotificationTargetMetadata) []NotificationTarget {
		out := make([]NotificationTarget, len(targets))
		for i, t := range targets {
			nt := NotificationTarget{
				ID:       t.ID,
				Endpoint: t.Endpoint,
				Events:   t.Events,
			}
			if t.Filter != nil {
				nt.Filter = &NotificationFilter{
					Prefix: t.Filter.Prefix,
					Suffix: t.Filter.Suffix,
				}
			}
			out[i] = nt
		}
		return out
	}
	return &NotificationConfig{
		TopicConfigurations:  conv(m.TopicConfigurations),
		QueueConfigurations:  conv(m.QueueConfigurations),
		LambdaConfigurations: conv(m.LambdaConfigurations),
	}
}

// Logging conversion
func toMetadataLogging(l *LoggingConfig) *metadata.LoggingMetadata {
	if l == nil {
		return nil
	}
	return &metadata.LoggingMetadata{
		TargetBucket: l.TargetBucket,
		TargetPrefix: l.TargetPrefix,
	}
}

func fromMetadataLogging(l *metadata.LoggingMetadata) *LoggingConfig {
	if l == nil {
		return nil
	}
	return &LoggingConfig{
		TargetBucket: l.TargetBucket,
		TargetPrefix: l.TargetPrefix,
	}
}
