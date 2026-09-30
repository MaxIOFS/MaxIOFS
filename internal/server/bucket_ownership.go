package server

import (
	"context"
	"strings"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// clientBuckets is the bucket manager clients reach through the console and
// the S3 API. A bucket a client deletes takes with it what it owns in the
// node's database: shares, replication rules, inventory configurations,
// permissions and the policies naming it. A bucket a client creates starts
// without what a former bucket of that name left. A bucket removed because it
// moved to another node, or because another node deleted it, is removed below
// this manager and keeps them.
type clientBuckets struct {
	bucket.Manager
	s *Server
}

func (b *clientBuckets) CreateBucket(ctx context.Context, tenantID, name, ownerID string) error {
	if err := b.Manager.CreateBucket(ctx, tenantID, name, ownerID); err != nil {
		return err
	}
	b.s.dropBucketRows(ctx, tenantID, name)
	return nil
}

func (b *clientBuckets) DeleteBucket(ctx context.Context, tenantID, name string) error {
	if err := b.Manager.DeleteBucket(ctx, tenantID, name); err != nil {
		return err
	}
	b.s.forgetBucket(ctx, tenantID, name)
	return nil
}

func (b *clientBuckets) ForceDeleteBucket(ctx context.Context, tenantID, name string) error {
	if err := b.Manager.ForceDeleteBucket(ctx, tenantID, name); err != nil {
		return err
	}
	b.s.forgetBucket(ctx, tenantID, name)
	return nil
}

// forgetBucket removes what a deleted bucket owned: its rows and the policies
// naming it.
func (s *Server) forgetBucket(ctx context.Context, tenantID, name string) {
	s.dropBucketRows(ctx, tenantID, name)
	s.recordBucketOwnerPolicy(name, tenantID, "", false)
}

// dropBucketRows deletes the shares, replication rules, inventory
// configurations and permissions of a bucket. Each is reported to the other
// nodes as its manager reports any deletion.
func (s *Server) dropBucketRows(ctx context.Context, tenantID, name string) {
	log := logrus.WithFields(logrus.Fields{"bucket": name, "tenant_id": tenantID})
	if s.shareManager != nil {
		if err := s.shareManager.DeleteBucketShares(ctx, name, tenantID); err != nil {
			log.WithError(err).Warn("Failed to delete the shares of a bucket")
		}
	}
	if s.replicationManager != nil {
		if err := s.replicationManager.DeleteBucketRules(ctx, tenantID, name); err != nil {
			log.WithError(err).Warn("Failed to delete the replication rules of a bucket")
		}
	}
	if s.inventoryManager != nil {
		if _, err := s.inventoryManager.DeleteBucketConfigs(ctx, name, tenantID); err != nil {
			log.WithError(err).Warn("Failed to delete the inventory configurations of a bucket")
		}
	}
	if err := s.dropBucketPermissions(ctx, tenantID, name); err != nil {
		log.WithError(err).Warn("Failed to delete the permissions of a bucket")
	}
}

// dropBucketPermissions deletes the permission rows of a bucket and records
// each deletion for the other nodes. The policies they were written as go
// with the bucket's policies.
func (s *Server) dropBucketPermissions(ctx context.Context, tenantID, name string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM bucket_permissions WHERE bucket_name = ? AND bucket_tenant_id = ?`, name, tenantID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 0 {
		return rows.Err()
	}
	nodeID := ""
	if s.clusterManager != nil {
		nodeID, _ = s.clusterManager.GetLocalNodeID(ctx)
	}
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM bucket_permissions WHERE id = ?`, id); err != nil {
			return err
		}
		if err := cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeBucketPermission, id, nodeID, cluster.DeletedAfter(0)); err != nil {
			return err
		}
	}
	if s.bucketPermissionSyncMgr != nil {
		s.bucketPermissionSyncMgr.TriggerSync(ctx)
	}
	return nil
}

// dropBucketLocalState removes what this node alone keeps of a bucket it no
// longer holds: its event notification configuration and its integrity scans.
func (s *Server) dropBucketLocalState(ctx context.Context, tenantID, name string) {
	if s.notificationManager != nil {
		if err := s.notificationManager.DeleteConfiguration(ctx, tenantID, name); err != nil {
			logrus.WithError(err).WithField("bucket", name).Warn("Failed to delete the notification configuration of a bucket")
		}
	}
	kv, ok := s.metadataStore.(metadata.RawKVStore)
	if !ok {
		return
	}
	bucketPath := name
	if tenantID != "" {
		bucketPath = tenantID + "/" + name
	}
	if err := kv.DeleteRaw(ctx, integrityScanKey(bucketPath)); err != nil && err != metadata.ErrNotFound {
		logrus.WithError(err).WithField("bucket", name).Warn("Failed to delete the integrity scans of a bucket")
	}
}

// dropOrphanedBucketState removes the notification configurations and
// integrity scans this node keeps for buckets it does not hold: earlier
// releases left them when a bucket was deleted, and a new bucket of the name
// took them.
func (s *Server) dropOrphanedBucketState(ctx context.Context) {
	kv, ok := s.metadataStore.(metadata.RawKVStore)
	if !ok {
		return
	}
	for _, prefix := range []string{"notification:", "integrity_scans:"} {
		var orphaned []string
		err := kv.RawScan(ctx, prefix, "", func(key string, _ []byte) bool {
			bucketPath := strings.TrimPrefix(key, prefix)
			tenantID, name := "", bucketPath
			if i := strings.Index(bucketPath, "/"); i >= 0 {
				tenantID, name = bucketPath[:i], bucketPath[i+1:]
			}
			if _, err := s.metadataStore.GetBucket(ctx, tenantID, name); err == metadata.ErrBucketNotFound {
				orphaned = append(orphaned, bucketPath)
			}
			return true
		})
		if err != nil {
			logrus.WithError(err).Warn("Failed to look for the state of deleted buckets")
			continue
		}
		for _, bucketPath := range orphaned {
			tenantID, name := "", bucketPath
			if i := strings.Index(bucketPath, "/"); i >= 0 {
				tenantID, name = bucketPath[:i], bucketPath[i+1:]
			}
			s.dropBucketLocalState(ctx, tenantID, name)
		}
		if len(orphaned) > 0 {
			logrus.WithFields(logrus.Fields{"kind": strings.TrimSuffix(prefix, ":"), "buckets": len(orphaned)}).
				Info("Removed the state of deleted buckets")
		}
	}
}
