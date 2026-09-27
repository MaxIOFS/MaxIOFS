package object

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
)

func defaultWriteRetention(bucket *metadata.BucketMetadata) (*RetentionConfig, error) {
	lock := bucket.ObjectLock
	if lock == nil || !lock.Enabled || lock.Rule == nil || lock.Rule.DefaultRetention == nil {
		return nil, nil
	}
	rule := lock.Rule.DefaultRetention
	if rule.Mode != RetentionModeCompliance && rule.Mode != RetentionModeGovernance {
		return nil, ErrInvalidRetentionMode
	}
	until := rule.RetainUntilDate
	switch {
	case rule.Days != nil && *rule.Days > 0 && rule.Years == nil:
		until = time.Now().AddDate(0, 0, *rule.Days)
	case rule.Years != nil && *rule.Years > 0 && rule.Days == nil:
		until = time.Now().AddDate(*rule.Years, 0, 0)
	case rule.Days != nil || rule.Years != nil || !until.After(time.Now()):
		return nil, fmt.Errorf("invalid default bucket retention period")
	}
	return &RetentionConfig{Mode: rule.Mode, RetainUntilDate: until}, nil
}

type replicatedObjectLockKey struct{}

// WithReplicatedObjectLock marks the next write's object-lock headers as the
// source's stored state. They are kept as they are, a date that passed in
// transit included, and no bucket default is added when they are absent.
func WithReplicatedObjectLock(ctx context.Context) context.Context {
	return context.WithValue(ctx, replicatedObjectLockKey{}, true)
}

func isReplicatedObjectLock(ctx context.Context) bool {
	v, _ := ctx.Value(replicatedObjectLockKey{}).(bool)
	return v
}

func writeObjectLock(ctx context.Context, bucket *metadata.BucketMetadata, headers http.Header) (*RetentionConfig, *LegalHoldConfig, error) {
	replica := isReplicatedObjectLock(ctx)
	mode, date := headers.Get("x-amz-object-lock-mode"), headers.Get("x-amz-object-lock-retain-until-date")
	status := headers.Get("x-amz-object-lock-legal-hold")
	if !replica && (mode != "" || date != "" || status != "") {
		if bucket.ObjectLock == nil || !bucket.ObjectLock.Enabled {
			return nil, nil, ErrNoRetentionConfiguration
		}
	}
	var retention *RetentionConfig
	var err error
	if mode != "" || date != "" {
		if mode != RetentionModeCompliance && mode != RetentionModeGovernance {
			return nil, nil, ErrInvalidRetentionMode
		}
		until, err := time.Parse(time.RFC3339, date)
		if err != nil || !replica && !until.After(time.Now()) {
			return nil, nil, ErrRetentionDateInPast
		}
		retention = &RetentionConfig{Mode: mode, RetainUntilDate: until}
	} else if !replica {
		retention, err = defaultWriteRetention(bucket)
		if err != nil {
			return nil, nil, err
		}
	}
	var hold *LegalHoldConfig
	if status != "" {
		if status != LegalHoldStatusOn && status != LegalHoldStatusOff {
			return nil, nil, ErrInvalidLegalHoldStatus
		}
		hold = &LegalHoldConfig{Status: status}
	}
	return retention, hold, nil
}
