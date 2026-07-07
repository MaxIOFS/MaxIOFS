package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/sirupsen/logrus"
)

// Encryption recovery bundle endpoints (global admin only).
//
// The KEK lives in the SQLite database; if the database is lost without a
// backup of the key, every envelope-encrypted object becomes unreadable.
// These endpoints let the admin export the KEK as a passphrase-encrypted
// recovery bundle to store OUTSIDE the system, and report whether that has
// been done (the console shows a banner until it has).

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
