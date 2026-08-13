package server

// Console API handlers for STS temporary credentials. See docs/API.md.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// startSTSSessionSweep periodically deletes sessions that expired more than an
func (s *Server) startSTSSessionSweep(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Sweep once at startup rather than waiting a whole interval: a server that
	// was down for a while comes back with sessions that expired long ago.
	if removed, err := s.authManager.SweepExpiredSTSSessions(ctx); err == nil && removed > 0 {
		logrus.WithField("count", removed).Info("Swept expired STS sessions")
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed, err := s.authManager.SweepExpiredSTSSessions(ctx)
			if err != nil {
				logrus.WithError(err).Warn("Failed to sweep expired STS sessions")
				continue
			}
			if removed > 0 {
				logrus.WithField("count", removed).Debug("Swept expired STS sessions")
			}
		}
	}
}

// handleReceiveSTSSessionSync applies a batch of temporary credentials pushed
// by a peer node: upsert every session, then delete the tombstoned ones.
// POST /api/internal/cluster/sts-session-sync
func (s *Server) handleReceiveSTSSessionSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sourceNodeID, ok := ctx.Value("cluster_node_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var payload cluster.STSSessionSyncPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	now := time.Now().Unix()
	applied := 0
	for _, sess := range payload.Sessions {
		// Never resurrect a session this node already knows was revoked, and
		// don't bother storing one that is already expired.
		if hasDeletion, _ := cluster.HasDeletion(ctx, s.db, cluster.EntityTypeSTSSession, sess.TempAccessKeyID); hasDeletion {
			continue
		}
		if sess.ExpiresAt <= now {
			continue
		}

		var policy, roleARN, roleSessionName interface{}
		if sess.SessionPolicy != "" {
			policy = sess.SessionPolicy
		}
		if sess.RoleARN != "" {
			roleARN = sess.RoleARN
		}
		if sess.RoleSessionName != "" {
			roleSessionName = sess.RoleSessionName
		}

		// A batch from a node that predates role sessions carries no mode; treat
		// it as a plain session, which is what it was there.
		policyMode := sess.PolicyMode
		if policyMode == "" {
			policyMode = auth.STSPolicyModeRestrict
		}

		_, err := s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO sts_sessions
			(temp_access_key_id, secret_access_key, session_token, user_id, session_policy,
			 role_arn, role_session_name, policy_mode, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, sess.TempAccessKeyID, sess.SecretAccessKey, sess.SessionToken,
			sess.UserID, policy, roleARN, roleSessionName, policyMode,
			sess.CreatedAt, sess.ExpiresAt)
		if err != nil {
			logrus.WithError(err).WithField("temp_access_key_id", sess.TempAccessKeyID).
				Error("Failed to store synchronized STS session")
			continue
		}
		applied++
	}

	revoked := 0
	for _, keyID := range payload.Deletions {
		// Record the tombstone locally too, so a later sync batch from a node
		// that hasn't learned about the revocation cannot bring it back.
		if err := cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeSTSSession, keyID, sourceNodeID); err != nil {
			logrus.WithError(err).Warn("Failed to record STS session tombstone")
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sts_sessions WHERE temp_access_key_id = ?`, keyID); err != nil {
			logrus.WithError(err).Warn("Failed to delete revoked STS session")
			continue
		}
		revoked++
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"applied":        applied,
		"revoked":        revoked,
	}).Debug("STS session sync batch applied")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"applied": applied,
		"revoked": revoked,
	})
}

// stsSessionResponse is the issuance payload. The secret and token are shown
// once, at issuance, and never returned by any listing.
type stsSessionResponse struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken"`
	ExpiresAt       int64  `json:"expiresAt"`
}

// stsSessionListItem is the scrubbed view used by listings. The session policy
// is not credential material, so it is returned as written — a caller needs to
// see how narrow a credential it issued.
type stsSessionListItem struct {
	AccessKeyID   string `json:"accessKeyId"`
	UserID        string `json:"userId"`
	Username      string `json:"username,omitempty"`
	SessionPolicy string `json:"sessionPolicy,omitempty"`
	CreatedAt     int64  `json:"createdAt"`
	ExpiresAt     int64  `json:"expiresAt"`
}

// handleIssueSTSSession issues temporary credentials for the requesting user.
// Sessions are always self-issued — no impersonation.
func (s *Server) handleIssueSTSSession(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	// Temporary credentials are S3 credentials; gate them on the same
	// capability that governs a user managing their own keys.
	if !s.isAdmin(currentUser) &&
		!auth.CheckCapabilityInContext(r.Context(), s.authManager, auth.CapKeysManageOwn) {
		s.writeError(w, "You do not have permission to manage API keys", http.StatusForbidden)
		return
	}

	var req struct {
		DurationSeconds int             `json:"durationSeconds"`
		SessionPolicy   json.RawMessage `json:"sessionPolicy,omitempty"`
	}
	// An empty body is valid and means "use the default duration".
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// The policy travels either as a JSON object or as a JSON string holding
	// the document — SDK users tend to send the second.
	sessionPolicy := ""
	if len(req.SessionPolicy) > 0 {
		var asString string
		if err := json.Unmarshal(req.SessionPolicy, &asString); err == nil {
			sessionPolicy = asString
		} else {
			sessionPolicy = string(req.SessionPolicy)
		}
	}

	session, err := s.authManager.IssueSTSSession(r.Context(), currentUser.ID, req.DurationSeconds, sessionPolicy)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrSTSInvalidPolicy):
			s.writeError(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, auth.ErrSTSInvalidDuration):
			s.writeError(w, "Session duration must be between 15 minutes and the configured maximum", http.StatusBadRequest)
		case errors.Is(err, auth.ErrSTSTooManySessions):
			s.writeError(w, "Too many active sessions. Revoke unused sessions before issuing new ones.", http.StatusTooManyRequests)
		case errors.Is(err, auth.ErrUserInactive):
			s.writeError(w, "Access denied", http.StatusForbidden)
		default:
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.touchLocalWriteAt(r.Context())

	// Push to all cluster nodes immediately so a client using the credential
	// right away doesn't hit a node that hasn't run its periodic sync yet.
	if s.stsSessionSyncMgr != nil {
		s.stsSessionSyncMgr.TriggerSync(r.Context())
	}

	s.writeJSON(w, stsSessionResponse{
		AccessKeyID:     session.TempAccessKeyID,
		SecretAccessKey: session.SecretAccessKey,
		SessionToken:    session.SessionToken,
		ExpiresAt:       session.ExpiresAt,
	})
}

// handleListSTSSessions lists the caller's sessions; global admins may pass
// ?all=true to see every user's sessions.
func (s *Server) handleListSTSSessions(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	var (
		sessions []*auth.STSSession
		err      error
	)
	if r.URL.Query().Get("all") == "true" && s.isGlobalAdmin(currentUser) {
		sessions, err = s.authManager.ListAllSTSSessions(r.Context())
	} else {
		sessions, err = s.authManager.ListSTSSessions(r.Context(), currentUser.ID)
	}
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	items := make([]stsSessionListItem, 0, len(sessions))
	for _, sess := range sessions {
		item := stsSessionListItem{
			AccessKeyID:   sess.TempAccessKeyID,
			UserID:        sess.UserID,
			SessionPolicy: sess.SessionPolicy,
			CreatedAt:     sess.CreatedAt,
			ExpiresAt:     sess.ExpiresAt,
		}
		if user, uErr := s.authManager.GetUser(r.Context(), sess.UserID); uErr == nil && user != nil {
			item.Username = user.Username
		}
		items = append(items, item)
	}

	s.writeJSON(w, items)
}

// handleRevokeSTSSession deletes a session. Ownership is enforced in the auth
// manager: non-admins can only revoke their own.
func (s *Server) handleRevokeSTSSession(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	keyID := mux.Vars(r)["keyId"]
	err := s.authManager.RevokeSTSSession(r.Context(), keyID, currentUser.ID, s.isGlobalAdmin(currentUser))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrSTSSessionNotFound):
			s.writeError(w, "Session not found", http.StatusNotFound)
		case errors.Is(err, auth.ErrAccessDenied):
			s.writeError(w, "Access denied", http.StatusForbidden)
		default:
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.touchLocalWriteAt(r.Context())

	// Propagate the revocation immediately; the periodic sync is the fallback.
	if s.stsSessionSyncMgr != nil {
		s.stsSessionSyncMgr.TriggerSync(r.Context())
	}

	s.writeJSON(w, map[string]string{"message": "Session revoked"})
}
