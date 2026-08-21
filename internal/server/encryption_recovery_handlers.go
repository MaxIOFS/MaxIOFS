package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/sirupsen/logrus"
)

// handleReceiveKEKSync adopts cluster-shared encryption keys pushed by a peer
func (s *Server) handleReceiveKEKSync(w http.ResponseWriter, r *http.Request) {
	if s.kekStore == nil {
		http.Error(w, "encryption key store not available", http.StatusServiceUnavailable)
		return
	}

	var payload struct {
		Keys         []kek.KeyRecord `json:"keys"`
		SourceNodeID string          `json:"source_node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.kekStore.AdoptClusterKeys(payload.Keys); err != nil {
		logrus.WithError(err).WithField("source_node_id", payload.SourceNodeID).
			Error("Failed to adopt synced encryption KEKs")
		// 409 only for a genuine key conflict (same version, different
		// material); malformed or otherwise invalid payloads are 400.
		status := http.StatusBadRequest
		if errors.Is(err, kek.ErrKeyConflict) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// requireGlobalAdmin resolves the requesting user and enforces global-admin
// access. Returns nil (after writing the error response) when access is denied.
func (s *Server) requireGlobalAdmin(w http.ResponseWriter, r *http.Request) *auth.User {
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return nil
	}
	if !auth.IsAdminUser(r.Context()) || user.TenantID != "" {
		s.writeError(w, "Forbidden: only global admins can manage encryption recovery", http.StatusForbidden)
		return nil
	}
	return user
}

// handleEncryptionRecoveryStatus returns the KEK version and whether the
// recovery bundle has ever been downloaded.
// GET /api/v1/settings/encryption/recovery-status
func (s *Server) handleEncryptionRecoveryStatus(w http.ResponseWriter, r *http.Request) {
	if user := s.requireGlobalAdmin(w, r); user == nil {
		return
	}
	if s.kekStore == nil {
		s.writeError(w, "Encryption key store is not available", http.StatusServiceUnavailable)
		return
	}

	_, kekVersion := s.kekStore.CurrentKEK()
	downloadedAt, err := s.kekStore.BundleDownloadedAt()
	if err != nil {
		logrus.WithError(err).Error("Failed to read recovery bundle download status")
		s.writeError(w, "Failed to read recovery status", http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"kekVersion":       kekVersion,
		"bundleDownloaded": downloadedAt > 0,
	}
	if downloadedAt > 0 {
		resp["downloadedAt"] = time.Unix(downloadedAt, 0).UTC().Format(time.RFC3339)
	}
	s.writeJSON(w, resp)
}

// handleRotateKEK creates a new current KEK version.
func (s *Server) handleRotateKEK(w http.ResponseWriter, r *http.Request) {
	user := s.requireGlobalAdmin(w, r)
	if user == nil {
		return
	}
	if s.kekStore == nil {
		s.writeError(w, "Encryption key store is not available", http.StatusServiceUnavailable)
		return
	}

	clusterEnabled := s.clusterManager != nil && s.clusterManager.IsClusterEnabled()
	newVersion, err := s.kekStore.Rotate(clusterEnabled)
	if err != nil {
		logrus.WithError(err).Error("KEK rotation failed")
		s.writeError(w, "Failed to rotate encryption key: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logrus.WithFields(logrus.Fields{"user": user.Username, "kek_version": newVersion}).
		Info("Encryption KEK rotated by admin")

	syncCtx := s.serverCtx
	if syncCtx == nil {
		syncCtx = context.Background()
	}
	if clusterEnabled && s.globalConfigSyncMgr != nil {
		s.globalConfigSyncMgr.SyncKEKs(syncCtx)
	}

	// Kick the worker so existing DEKs start re-wrapping immediately.
	if !s.encWorkerRunning.Load() {
		bg := s.serverCtx
		if bg == nil {
			bg = context.Background()
		}
		go s.runEncryptionPass(bg)
	}

	s.writeJSON(w, map[string]interface{}{
		"newVersion": newVersion,
	})
}

// handleDownloadRecoveryBundle exports the KEK as a passphrase-encrypted
// bundle file and marks it as downloaded.
// POST /api/v1/settings/encryption/recovery-bundle  body: {"passphrase": "..."}
func (s *Server) handleDownloadRecoveryBundle(w http.ResponseWriter, r *http.Request) {
	user := s.requireGlobalAdmin(w, r)
	if user == nil {
		return
	}
	if s.kekStore == nil {
		s.writeError(w, "Encryption key store is not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(req.Passphrase) < kek.MinBundlePassphraseLen {
		s.writeError(w, fmt.Sprintf("Passphrase must be at least %d characters", kek.MinBundlePassphraseLen), http.StatusBadRequest)
		return
	}

	bundle, err := s.kekStore.ExportBundle(req.Passphrase)
	if err != nil {
		logrus.WithError(err).Error("Failed to export encryption recovery bundle")
		s.writeError(w, "Failed to export recovery bundle", http.StatusInternalServerError)
		return
	}

	if err := s.kekStore.MarkBundleDownloaded(); err != nil {
		// The download still proceeds — losing the tracking flag only means
		// the banner stays visible.
		logrus.WithError(err).Warn("Failed to record recovery bundle download")
	}

	logrus.WithField("user", user.Username).Info("Encryption recovery bundle downloaded")

	filename := fmt.Sprintf("maxiofs-recovery-bundle-%s.json", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bundle)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bundle)
}
