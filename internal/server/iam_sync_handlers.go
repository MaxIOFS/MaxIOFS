package server

// Receiving end of IAM cluster synchronization, plus the tombstone helper the
// IAM handler uses when it deletes something.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleReceiveIAMSync applies a batch of IAM entities pushed by a peer.
func (s *Server) handleReceiveIAMSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var payload cluster.IAMSyncPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	applied := 0
	applied += s.applyIAMPolicies(ctx, payload.Policies)
	applied += s.applyIAMRoles(ctx, payload.Roles)
	applied += s.applyIAMAttachments(ctx, payload.Attachments)
	applied += s.applyIAMInlinePolicies(ctx, payload.Inline)

	removed := s.applyIAMDeletions(ctx, &payload, sourceNodeID)

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"applied":        applied,
		"removed":        removed,
	}).Debug("Applied IAM sync batch")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"applied": applied, "removed": removed})
}

func (s *Server) applyIAMPolicies(ctx context.Context, policies []*cluster.IAMPolicyData) int {
	applied := 0
	for _, p := range policies {
		if cluster.DeletionSupersedes(ctx, s.db, cluster.EntityTypeIAMPolicy, p.Name, p.UpdatedAt) {
			continue
		}
		if !s.iamIncomingIsNewer(ctx, `SELECT updated_at FROM iam_policies WHERE name = ?`, p.Name, p.UpdatedAt) {
			continue
		}

		if _, err := s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO iam_policies
			(name, arn, path, description, default_version_id, is_builtin, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, p.Name, p.ARN, p.Path, nullableString(p.Description), p.DefaultVersionID,
			boolToInt(p.IsBuiltin), nullableString(p.TenantID), p.CreatedAt, p.UpdatedAt); err != nil {
			logrus.WithError(err).WithField("policy", p.Name).Error("Failed to store synchronized IAM policy")
			continue
		}

		if _, err := s.db.ExecContext(ctx, `DELETE FROM iam_policy_versions WHERE policy_name = ?`, p.Name); err != nil {
			logrus.WithError(err).Warn("Failed to clear IAM policy versions before sync")
			continue
		}
		for _, v := range p.Versions {
			if _, err := s.db.ExecContext(ctx, `
				INSERT OR REPLACE INTO iam_policy_versions (policy_name, version_id, document, created_at)
				VALUES (?, ?, ?, ?)
			`, p.Name, v.VersionID, v.Document, v.CreatedAt); err != nil {
				logrus.WithError(err).Warn("Failed to store synchronized IAM policy version")
			}
		}
		applied++
	}
	return applied
}

func (s *Server) applyIAMRoles(ctx context.Context, roles []*cluster.IAMRoleData) int {
	applied := 0
	for _, role := range roles {
		if cluster.DeletionSupersedes(ctx, s.db, cluster.EntityTypeIAMRole, role.Name, role.UpdatedAt) {
			continue
		}
		if !s.iamIncomingIsNewer(ctx, `SELECT updated_at FROM iam_roles WHERE name = ?`, role.Name, role.UpdatedAt) {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO iam_roles
			(name, arn, path, description, assume_role_policy, max_session_duration, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, role.Name, role.ARN, role.Path, nullableString(role.Description), role.AssumeRolePolicy,
			role.MaxSessionDuration, nullableString(role.TenantID), role.CreatedAt, role.UpdatedAt); err != nil {
			logrus.WithError(err).WithField("role", role.Name).Error("Failed to store synchronized IAM role")
			continue
		}
		applied++
	}
	return applied
}

func (s *Server) applyIAMAttachments(ctx context.Context, attachments []*cluster.IAMAttachmentData) int {
	applied := 0
	for _, a := range attachments {
		id := cluster.IAMAttachmentID(a.PolicyName, a.TargetType, a.TargetID)
		if cluster.DeletionSupersedes(ctx, s.db, cluster.EntityTypeIAMAttachment, id, a.AttachedAt) {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO iam_policy_attachments (policy_name, target_type, target_id, attached_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(policy_name, target_type, target_id) DO NOTHING
		`, a.PolicyName, a.TargetType, a.TargetID, a.AttachedAt); err != nil {
			logrus.WithError(err).Warn("Failed to store synchronized IAM attachment")
			continue
		}
		applied++
	}
	return applied
}

func (s *Server) applyIAMInlinePolicies(ctx context.Context, inline []*cluster.IAMInlinePolicyData) int {
	applied := 0
	for _, p := range inline {
		id := cluster.IAMInlinePolicyID(p.TargetType, p.TargetID, p.Name)
		if cluster.DeletionSupersedes(ctx, s.db, cluster.EntityTypeIAMInlinePolicy, id, p.UpdatedAt) {
			continue
		}

		var localUpdated int64
		err := s.db.QueryRowContext(ctx, `
			SELECT updated_at FROM iam_inline_policies
			WHERE target_type = ? AND target_id = ? AND name = ?
		`, p.TargetType, p.TargetID, p.Name).Scan(&localUpdated)
		if err == nil && localUpdated > p.UpdatedAt {
			continue
		}

		if _, err := s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO iam_inline_policies
			(target_type, target_id, name, document, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, p.TargetType, p.TargetID, p.Name, p.Document, p.CreatedAt, p.UpdatedAt); err != nil {
			logrus.WithError(err).Warn("Failed to store synchronized IAM inline policy")
			continue
		}
		applied++
	}
	return applied
}

// applyIAMDeletions records the deletions a peer sends and removes each
// entity this node holds that did not change after its deletion. An earlier
// release sends no time: the deletion log carries it.
func (s *Server) applyIAMDeletions(ctx context.Context, payload *cluster.IAMSyncPayload, sourceNodeID string) int {
	removed := 0
	for entityType, ids := range payload.Deletions {
		for _, id := range ids {
			deletedAt := payload.DeletedAt[entityType][id]
			if deletedAt <= 0 {
				deletedAt = cluster.DeletionTime(ctx, s.db, entityType, id)
			}
			if deletedAt <= 0 || cluster.EntityIsNewerThanTombstone(ctx, s.db, entityType, id, deletedAt) {
				continue
			}
			if err := cluster.RecordDeletion(ctx, s.db, entityType, id, sourceNodeID, deletedAt); err != nil {
				logrus.WithError(err).Warn("Failed to record IAM tombstone")
			}
			if s.deleteIAMEntityLocally(ctx, entityType, id) {
				removed++
			}
		}
	}
	return removed
}

// deleteIAMEntityLocally removes whichever entity a tombstone names.
func (s *Server) deleteIAMEntityLocally(ctx context.Context, entityType, id string) bool {
	var err error
	switch entityType {
	case cluster.EntityTypeIAMPolicy:
		_, err = s.db.ExecContext(ctx, `DELETE FROM iam_policy_versions WHERE policy_name = ?`, id)
		if err == nil {
			_, err = s.db.ExecContext(ctx, `DELETE FROM iam_policies WHERE name = ?`, id)
		}
	case cluster.EntityTypeIAMRole:
		_, err = s.db.ExecContext(ctx, `DELETE FROM iam_roles WHERE name = ?`, id)
	case cluster.EntityTypeIAMAttachment:
		policyName, targetType, targetID, ok := cluster.SplitIAMID(id)
		if !ok {
			return false
		}
		_, err = s.db.ExecContext(ctx, `
			DELETE FROM iam_policy_attachments
			WHERE policy_name = ? AND target_type = ? AND target_id = ?
		`, policyName, targetType, targetID)
	case cluster.EntityTypeIAMInlinePolicy:
		targetType, targetID, name, ok := cluster.SplitIAMID(id)
		if !ok {
			return false
		}
		_, err = s.db.ExecContext(ctx, `
			DELETE FROM iam_inline_policies
			WHERE target_type = ? AND target_id = ? AND name = ?
		`, targetType, targetID, name)
	default:
		return false
	}

	if err != nil {
		logrus.WithError(err).WithField("entity_type", entityType).Warn("Failed to apply IAM deletion")
		return false
	}
	return true
}

// iamIncomingIsNewer reports whether a synchronized row should overwrite the
func (s *Server) iamIncomingIsNewer(ctx context.Context, query, key string, incomingUpdatedAt int64) bool {
	var localUpdated int64
	if err := s.db.QueryRowContext(ctx, query, key).Scan(&localUpdated); err != nil {
		return true
	}
	return incomingUpdatedAt >= localUpdated
}

// --- tombstone recording, used by the IAM handler ---

// iamDeletion is an IAM entity a request is about to delete, with the time of
// its last change, read while it is still there.
type iamDeletion struct {
	entityType, id string
	lastChange     int64
}

// changedAt returns when this node's copy of an entity last changed (unix
// seconds), read before a deletion dates itself after it; 0 when unknown.
func (s *Server) changedAt(ctx context.Context, entityType, id string) int64 {
	if s.db == nil {
		return 0
	}
	at, _ := cluster.EntityUpdatedAt(ctx, s.db, entityType, id)
	return at
}

// iamAboutToDelete reads when each named IAM entity last changed, before the
// request deletes it.
func (s *Server) iamAboutToDelete(ctx context.Context, entityType string, ids ...string) []iamDeletion {
	out := make([]iamDeletion, 0, len(ids))
	for _, id := range ids {
		out = append(out, iamDeletion{entityType: entityType, id: id, lastChange: s.changedAt(ctx, entityType, id)})
	}
	return out
}

// recordIAMDeletions writes, once the entities are gone, the tombstones that
// stop a peer that still has them from bringing them back, each dated after
// the entity's last change, and pushes them out.
func (s *Server) recordIAMDeletions(ctx context.Context, deletions ...[]iamDeletion) {
	nodeID := ""
	if s.clusterManager != nil {
		if id, err := s.clusterManager.GetLocalNodeID(ctx); err == nil {
			nodeID = id
		}
	}
	for _, list := range deletions {
		for _, d := range list {
			if s.db == nil {
				break
			}
			if err := cluster.RecordDeletion(ctx, s.db, d.entityType, d.id, nodeID, cluster.DeletedAfter(d.lastChange)); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"entity_type": d.entityType,
					"entity_id":   d.id,
				}).Warn("Failed to record IAM deletion tombstone")
			}
		}
	}
	s.triggerIAMSync(ctx)
}

// afterIAMWrite runs the side effects every IAM mutation shares: mark the local
func (s *Server) afterIAMWrite(ctx context.Context) {
	s.touchLocalWriteAt(ctx)
	s.triggerIAMSync(ctx)
}

// triggerIAMSync pushes local IAM state to peers right away, so an identity or
// policy created here works on every node without waiting for the interval.
func (s *Server) triggerIAMSync(ctx context.Context) {
	if s.iamSyncMgr != nil {
		s.iamSyncMgr.TriggerSync(ctx)
	}
}

// sqlQueryer is a database or a transaction.
type sqlQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// iamHeldByTarget reads, from q, the inline policies and attachments an
// identity about to be deleted holds, with the time each last changed.
func iamHeldByTarget(ctx context.Context, q sqlQueryer, targetType, targetID string) []iamDeletion {
	var out []iamDeletion
	for _, part := range []struct {
		entityType, query string
		id                func(name string) string
	}{
		{cluster.EntityTypeIAMInlinePolicy,
			`SELECT name, updated_at FROM iam_inline_policies WHERE target_type = ? AND target_id = ?`,
			func(name string) string { return cluster.IAMInlinePolicyID(targetType, targetID, name) }},
		{cluster.EntityTypeIAMAttachment,
			`SELECT policy_name, attached_at FROM iam_policy_attachments WHERE target_type = ? AND target_id = ?`,
			func(name string) string { return cluster.IAMAttachmentID(name, targetType, targetID) }},
	} {
		rows, err := q.QueryContext(ctx, part.query, targetType, targetID)
		if err != nil {
			continue
		}
		for rows.Next() {
			var name string
			var at int64
			if rows.Scan(&name, &at) == nil {
				out = append(out, iamDeletion{entityType: part.entityType, id: part.id(name), lastChange: at})
			}
		}
		rows.Close()
	}
	return out
}

// iamHeldByTargets is iamHeldByTarget on the server's database.
func (s *Server) iamHeldByTargets(ctx context.Context, targetType, targetID string) []iamDeletion {
	if s.db == nil {
		return nil
	}
	return iamHeldByTarget(ctx, s.db, targetType, targetID)
}

// iamHeldByTenantUsers reads what the users of a tenant about to be deleted
// hold.
func (s *Server) iamHeldByTenantUsers(ctx context.Context, tenantID string) []iamDeletion {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM users WHERE tenant_id = ?`, tenantID)
	if err != nil {
		return nil
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var out []iamDeletion
	for _, id := range ids {
		out = append(out, iamHeldByTarget(ctx, s.db, auth.IAMTargetUser, id)...)
	}
	return out
}

// iamHeldBy reads the inline policies and attachments an identity about to be
// deleted holds, with the time each last changed.
func (s *Server) iamHeldBy(ctx context.Context, im auth.IAMManager, targetType, targetID string) (inline, attached []iamDeletion) {
	if targetID == "" {
		return nil, nil
	}
	if policies, err := im.ListIAMInlinePolicies(ctx, targetType, targetID); err == nil {
		for _, p := range policies {
			inline = append(inline, s.iamAboutToDelete(ctx, cluster.EntityTypeIAMInlinePolicy,
				cluster.IAMInlinePolicyID(targetType, targetID, p.Name))...)
		}
	}
	if policies, err := im.ListAttachedIAMPolicies(ctx, targetType, targetID); err == nil {
		for _, p := range policies {
			attached = append(attached, s.iamAboutToDelete(ctx, cluster.EntityTypeIAMAttachment,
				cluster.IAMAttachmentID(p.Name, targetType, targetID))...)
		}
	}
	return inline, attached
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
