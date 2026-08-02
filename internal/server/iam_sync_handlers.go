package server

// Receiving end of IAM cluster synchronization, plus the tombstone helper the
// IAM handler uses when it deletes something.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleReceiveIAMSync applies a batch of IAM entities pushed by a peer.
// POST /api/internal/cluster/iam-sync
//
// Entities are upserted, never replaced wholesale, and an incoming row only
// wins when it is at least as new as the local one. Deletions are applied last
// and recorded locally, so a peer that has not yet heard about a delete cannot
// bring the entity back on its next push.
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

	removed := s.applyIAMDeletions(ctx, payload.Deletions, sourceNodeID)

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
		if tombstoned, _ := cluster.HasDeletion(ctx, s.db, cluster.EntityTypeIAMPolicy, p.Name); tombstoned {
			continue
		}
		if !s.iamIncomingIsNewer(ctx, `SELECT updated_at FROM iam_policies WHERE name = ?`, p.Name, p.UpdatedAt) {
			continue
		}

		if _, err := s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO iam_policies
			(name, arn, path, description, default_version_id, is_builtin, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, p.Name, p.ARN, p.Path, nullableString(p.Description), p.DefaultVersionID,
			boolToInt(p.IsBuiltin), p.CreatedAt, p.UpdatedAt); err != nil {
			logrus.WithError(err).WithField("policy", p.Name).Error("Failed to store synchronized IAM policy")
			continue
		}

		// The versions of a policy only ever change through that policy's own
		// actions, so the sending node's set is authoritative for the row it
		// just won with.
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
		if tombstoned, _ := cluster.HasDeletion(ctx, s.db, cluster.EntityTypeIAMRole, role.Name); tombstoned {
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
		id := iamAttachmentTombstoneID(a.PolicyName, a.TargetType, a.TargetID)
		if tombstoned, _ := cluster.HasDeletion(ctx, s.db, cluster.EntityTypeIAMAttachment, id); tombstoned {
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
		id := iamInlineTombstoneID(p.TargetType, p.TargetID, p.Name)
		if tombstoned, _ := cluster.HasDeletion(ctx, s.db, cluster.EntityTypeIAMInlinePolicy, id); tombstoned {
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

func (s *Server) applyIAMDeletions(ctx context.Context, deletions map[string][]string, sourceNodeID string) int {
	removed := 0
	for entityType, ids := range deletions {
		for _, id := range ids {
			if err := cluster.RecordDeletion(ctx, s.db, entityType, id, sourceNodeID); err != nil {
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
		policyName, targetType, targetID, ok := splitIAMCompositeID(id)
		if !ok {
			return false
		}
		_, err = s.db.ExecContext(ctx, `
			DELETE FROM iam_policy_attachments
			WHERE policy_name = ? AND target_type = ? AND target_id = ?
		`, policyName, targetType, targetID)
	case cluster.EntityTypeIAMInlinePolicy:
		targetType, targetID, name, ok := splitIAMCompositeID(id)
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
// local one. A row that does not exist locally is always accepted; otherwise
// the newer updated_at wins, and a tie is accepted so two nodes that wrote the
// same value in the same second still converge.
func (s *Server) iamIncomingIsNewer(ctx context.Context, query, key string, incomingUpdatedAt int64) bool {
	var localUpdated int64
	if err := s.db.QueryRowContext(ctx, query, key).Scan(&localUpdated); err != nil {
		return true
	}
	return incomingUpdatedAt >= localUpdated
}

// --- tombstone recording, used by the IAM handler ---

// recordIAMDeletion writes the tombstone that stops a deleted IAM entity from
// being resurrected by a peer that still has it, and pushes the change out.
// Outside a cluster it is a cheap no-op row.
func (s *Server) recordIAMDeletion(ctx context.Context, entityType, entityID string) {
	nodeID := ""
	if s.clusterManager != nil {
		if id, err := s.clusterManager.GetLocalNodeID(ctx); err == nil {
			nodeID = id
		}
	}
	if err := cluster.RecordDeletion(ctx, s.db, entityType, entityID, nodeID); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"entity_type": entityType,
			"entity_id":   entityID,
		}).Warn("Failed to record IAM deletion tombstone")
	}
	s.triggerIAMSync(ctx)
}

// afterIAMWrite runs the side effects every IAM mutation shares: mark the local
// write for the cluster's staleness tracking, and push the new state out so a
// client that lands on a different node behind a load balancer does not find an
// identity that has no permissions yet.
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

// iamInlinePolicyNames and iamAttachedPolicyNames read what is about to be
// deleted alongside its owner. They are called BEFORE the delete, because
// afterwards there is nothing left to enumerate and the tombstones would be
// missing — leaving a peer free to push the dead entity's policies back.
func (s *Server) iamInlinePolicyNames(ctx context.Context, im auth.IAMManager, targetType, targetID string) []string {
	if targetID == "" {
		return nil
	}
	policies, err := im.ListIAMInlinePolicies(ctx, targetType, targetID)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(policies))
	for _, p := range policies {
		names = append(names, p.Name)
	}
	return names
}

func (s *Server) iamAttachedPolicyNames(ctx context.Context, im auth.IAMManager, targetType, targetID string) []string {
	if targetID == "" {
		return nil
	}
	policies, err := im.ListAttachedIAMPolicies(ctx, targetType, targetID)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(policies))
	for _, p := range policies {
		names = append(names, p.Name)
	}
	return names
}

func iamAttachmentTombstoneID(policyName, targetType, targetID string) string {
	return policyName + "/" + targetType + "/" + targetID
}

func iamInlineTombstoneID(targetType, targetID, name string) string {
	return targetType + "/" + targetID + "/" + name
}

// splitIAMCompositeID reverses the three-part tombstone identifiers above.
func splitIAMCompositeID(id string) (string, string, string, bool) {
	first := indexByte(id, '/')
	if first < 0 {
		return "", "", "", false
	}
	second := indexByte(id[first+1:], '/')
	if second < 0 {
		return "", "", "", false
	}
	second += first + 1
	return id[:first], id[first+1 : second], id[second+1:], true
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
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
