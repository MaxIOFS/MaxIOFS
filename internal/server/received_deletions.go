package server

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
)

// applyReceivedDeletion takes a deletion another node made at deletedAt (unix
// seconds): the deletion is recorded with its time and this node's copy is
// removed, unless the copy changed at or after it. Every channel a deletion
// arrives by applies it here: the deletion log, the IAM and STS payloads and
// the delete calls of each kind of entity.
func (s *Server) applyReceivedDeletion(ctx context.Context, entityType, id string, deletedAt int64, source string) (removed bool, err error) {
	if id == "" || deletedAt <= 0 {
		return false, nil
	}
	if cluster.EntityIsNewerThanTombstone(ctx, s.db, entityType, id, deletedAt) {
		return false, nil
	}
	if err := cluster.RecordDeletion(ctx, s.db, entityType, id, source, deletedAt); err != nil {
		return false, err
	}
	return s.deleteEntityLocally(ctx, entityType, id, deletedAt, source)
}

// deleteEntityLocally removes this node's copy of an entity deleted on another
// node, with what goes with it: the database removes what references it, but
// a tenant's users, which it makes global. Objects are left to the catch-up
// and the anti-entropy, which compare them with their copies on the other
// nodes.
func (s *Server) deleteEntityLocally(ctx context.Context, entityType, id string, deletedAt int64, source string) (bool, error) {
	switch entityType {
	case cluster.EntityTypeTenant:
		return s.inTx(ctx, func(tx *sql.Tx) (int64, error) {
			if _, err := tx.ExecContext(ctx, `DELETE FROM access_keys WHERE user_id IN (SELECT id FROM users WHERE tenant_id = ?)`, id); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE tenant_id = ?`, id); err != nil {
				return 0, err
			}
			return execAffected(ctx, tx, `DELETE FROM tenants WHERE id = ?`, id)
		})
	case cluster.EntityTypeUser:
		return s.deleteRows(ctx, `DELETE FROM users WHERE id = ?`, id)
	case cluster.EntityTypeAccessKey:
		return s.deleteRows(ctx, `DELETE FROM access_keys WHERE access_key_id = ?`, id)
	case cluster.EntityTypeBucketPermission:
		return s.deleteRows(ctx, `DELETE FROM bucket_permissions WHERE id = ?`, id)
	case cluster.EntityTypeIDPProvider:
		return s.deleteRows(ctx, `DELETE FROM identity_providers WHERE id = ?`, id)
	case cluster.EntityTypeGroupMapping:
		return s.deleteRows(ctx, `DELETE FROM idp_group_mappings WHERE id = ?`, id)
	case cluster.EntityTypeGroup:
		return s.inTx(ctx, func(tx *sql.Tx) (int64, error) { return deleteGroupRows(ctx, tx, id) })
	case cluster.EntityTypeSTSSession:
		return s.deleteRows(ctx, `DELETE FROM sts_sessions WHERE temp_access_key_id = ?`, id)
	case cluster.EntityTypeIAMPolicy, cluster.EntityTypeIAMRole, cluster.EntityTypeIAMAttachment, cluster.EntityTypeIAMInlinePolicy:
		// A failure is logged there; an ID that names nothing must not stop
		// the deletions that follow it.
		return s.deleteIAMEntityLocally(ctx, entityType, id), nil
	case cluster.EntityTypeClusterNode:
		if localID, err := s.clusterManager.GetLocalNodeID(ctx); err == nil && localID == id {
			return false, nil
		}
		return true, s.clusterManager.RemoveNodeAt(ctx, id, deletedAt, source)
	}
	return false, nil
}

func (s *Server) deleteRows(ctx context.Context, query, id string) (bool, error) {
	n, err := execAffected(ctx, s.db, query, id)
	return n > 0, err
}

func (s *Server) inTx(ctx context.Context, fn func(tx *sql.Tx) (int64, error)) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck
	n, err := fn(tx)
	if err != nil {
		return false, err
	}
	return n > 0, tx.Commit()
}

func execAffected(ctx context.Context, q interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, query string, args ...any) (int64, error) {
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// deleteGroupRows removes a group, with its members and the bucket permissions
// granted to it (the database removes those), and its IAM policies.
func deleteGroupRows(ctx context.Context, tx *sql.Tx, groupID string) (int64, error) {
	n, err := execAffected(ctx, tx, `DELETE FROM groups WHERE id = ?`, groupID)
	if err != nil {
		return 0, fmt.Errorf("delete group: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM iam_inline_policies WHERE target_type = ? AND target_id = ?`, auth.IAMTargetGroup, groupID); err != nil {
		return 0, fmt.Errorf("delete group inline policies: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM iam_policy_attachments WHERE target_type = ? AND target_id = ?`, auth.IAMTargetGroup, groupID); err != nil {
		return 0, fmt.Errorf("delete group policy attachments: %w", err)
	}
	return n, nil
}
