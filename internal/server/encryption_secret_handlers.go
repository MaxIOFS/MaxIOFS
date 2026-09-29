package server

import (
	"encoding/json"
	"net/http"

	"github.com/sirupsen/logrus"
)

// handleEncryptionSecretFingerprint answers another node with the fingerprint
// of this node's encryption secret.
// GET /api/internal/cluster/encryption-secret
func (s *Server) handleEncryptionSecretFingerprint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"fingerprint": s.encSecret.Fingerprint()}) //nolint:errcheck
}

// handleReceiveEncryptionSecret adopts the coordinator's encryption secret,
// re-encrypting the credentials this node stores. Only the coordinator sets
// it.
// POST /api/internal/cluster/encryption-secret
func (s *Server) handleReceiveEncryptionSecret(w http.ResponseWriter, r *http.Request) {
	sender, _ := r.Context().Value("cluster_node_id").(string)
	if s.leaderMgr == nil || sender == "" || s.leaderMgr.LeaderID(r.Context()) != sender {
		http.Error(w, "only the coordinator sets the encryption secret", http.StatusConflict)
		return
	}
	var body struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Secret == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	unreadable, err := s.encSecret.Adopt(r.Context(), body.Secret)
	if err != nil {
		logrus.WithError(err).Error("Failed to adopt the coordinator's encryption secret")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(unreadable) > 0 {
		logrus.WithField("credentials", unreadable).
			Error("Stored credentials no encryption secret decrypts; enter them again")
	}
	logrus.WithField("coordinator", sender).Info("Adopted the coordinator's encryption secret")
	w.WriteHeader(http.StatusNoContent)
}
