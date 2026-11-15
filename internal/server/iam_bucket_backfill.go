package server

import (
	"context"

	"github.com/sirupsen/logrus"
)

// backfillBucketOwnerPolicies writes the owner policy for every bucket that
// does not have one yet. Idempotent: the policy is upserted under a name
// derived from the bucket, so running it again rewrites the same row.
func (s *Server) backfillBucketOwnerPolicies(ctx context.Context) {
	if s.bucketManager == nil || s.authManager == nil {
		return
	}

	store, ok := s.authManager.(interface {
		GrantBucketOwnerPolicy(bucketName, ownerType, ownerID string) error
	})
	if !ok {
		return
	}

	buckets, err := s.bucketManager.ListBuckets(ctx, "")
	if err != nil {
		logrus.WithError(err).Warn("Could not list buckets to backfill their owner policies")
		return
	}

	written := 0
	for _, bucket := range buckets {
		ownerType, ownerID := bucket.OwnerType, bucket.OwnerID
		if ownerType == "" {
			ownerType = "user"
		}
		// A tenant-owned bucket has no owner policy by design: its tenant's
		// administrators already reach it through the tenant document.
		if ownerType == "tenant" {
			continue
		}
		if ownerID == "" {
			logrus.WithField("bucket", bucket.Name).
				Warn("Skipped owner policy backfill for bucket without an owner")
			continue
		}

		if err := store.GrantBucketOwnerPolicy(bucket.Name, ownerType, ownerID); err != nil {
			logrus.WithError(err).WithField("bucket", bucket.Name).
				Warn("Could not write the owner policy for an existing bucket")
			continue
		}
		written++
	}

	if written > 0 {
		logrus.WithField("buckets", written).Info("Wrote owner policies for existing buckets")
	}

	// An owner policy holds what its owner holds on its bucket: those written
	// before gave everything, and are cut to that.
	if recutter, ok := s.authManager.(interface{ RecutBucketOwnerPolicies() (int, error) }); ok {
		changed, err := recutter.RecutBucketOwnerPolicies()
		if err != nil {
			logrus.WithError(err).Warn("Could not cut every bucket owner policy to what its owner holds")
		}
		if changed > 0 {
			logrus.WithField("policies", changed).Info("Bucket owner policies cut to what their owners hold")
			s.afterIAMWrite(ctx)
		}
	}
}
