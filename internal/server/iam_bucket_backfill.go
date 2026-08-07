package server

// Backfilling the owner policy of buckets that existed before ownership became
// a policy.
//
// Ownership used to be an implicit fact the handlers checked. Now it is a
// policy written when a bucket is created — which leaves every bucket created
// earlier with no policy at all. Enforcing the policy without this would take
// away access to every existing bucket, so the two go together.

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

	// Listing with an empty tenant returns every bucket the node holds, which
	// is what a backfill has to cover — a bucket whose owner never got a policy
	// is invisible to authorization regardless of whose tenant it is in.
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
		if ownerID == "" || ownerType == "tenant" {
			logrus.WithFields(logrus.Fields{
				"bucket":     bucket.Name,
				"owner_type": ownerType,
			}).Warn("Skipped owner policy backfill for bucket without a user owner")
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
}
