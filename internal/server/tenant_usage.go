package server

import (
	"context"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/sirupsen/logrus"
)

// tenantBucketCount is the number of buckets a tenant has, on every node of
// the cluster.
func (s *Server) tenantBucketCount(ctx context.Context, tenantID string) (int64, error) {
	if s.clusterManager != nil && s.clusterManager.IsClusterEnabled() && s.bucketAggregator != nil {
		buckets, err := s.bucketAggregator.ListAllBucketsFromAllNodes(ctx, tenantID)
		if err != nil {
			return 0, err
		}
		return int64(len(buckets)), nil
	}
	buckets, err := s.bucketManager.ListBuckets(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	return int64(len(buckets)), nil
}

// tenantStorage is the storage a tenant uses. Every node of a cluster holds the
// entry of every object, so this node's count is the cluster's.
func (s *Server) tenantStorage(_ context.Context, tenant *auth.Tenant) int64 {
	return tenant.CurrentStorageBytes
}

// refreshTenantStorage sets each tenant's storage on this node to what its
// buckets here hold, as the stats reconciler just counted them. Usage kept by
// increments drifts when a write or delete is counted wrongly or not at all;
// counting the buckets brings it back.
func (s *Server) refreshTenantStorage(ctx context.Context) {
	setter, ok := s.authManager.(interface {
		SetTenantStorage(ctx context.Context, tenantID string, bytes int64) error
	})
	if !ok {
		return
	}
	tenants, err := s.authManager.ListTenants(ctx)
	if err != nil {
		logrus.WithError(err).Warn("Stats reconciler: failed to list tenants")
		return
	}
	buckets, err := s.metadataStore.ListBuckets(ctx, "")
	if err != nil {
		logrus.WithError(err).Warn("Stats reconciler: failed to list buckets")
		return
	}
	stored := make(map[string]int64, len(tenants))
	for _, b := range buckets {
		if b.TenantID != "" {
			stored[b.TenantID] += b.TotalSize
		}
	}
	for _, t := range tenants {
		if err := setter.SetTenantStorage(ctx, t.ID, stored[t.ID]); err != nil {
			logrus.WithError(err).WithField("tenant", t.ID).Warn("Stats reconciler: failed to set tenant storage")
		}
	}
}
