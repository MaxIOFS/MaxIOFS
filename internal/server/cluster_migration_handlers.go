package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// The target side of a bucket migration. Every step comes from the node the
// bucket lives on, which must be the node that signed the request.

// POST /api/internal/cluster/migration/stage
func (s *Server) handleMigrationStage(w http.ResponseWriter, r *http.Request) {
	var st cluster.MigrationStage
	if !decodeMigrationStep(w, r, &st, &st.SourceNodeID) {
		return
	}
	answerMigrationStep(w, s.migrationTarget.Stage(r.Context(), st), nil)
}

// POST /api/internal/cluster/migration/manifest
func (s *Server) handleMigrationManifest(w http.ResponseWriter, r *http.Request) {
	var req cluster.MigrationManifestRequest
	if !decodeMigrationStep(w, r, &req, nil) {
		return
	}
	manifests, err := s.migrationTarget.Manifests(r.Context(), req)
	answerMigrationStep(w, err, manifests)
}

// POST /api/internal/cluster/migration/commit
func (s *Server) handleMigrationCommit(w http.ResponseWriter, r *http.Request) {
	var c cluster.MigrationCommit
	if !decodeMigrationStep(w, r, &c, &c.SourceNodeID) {
		return
	}
	answerMigrationStep(w, s.migrationTarget.Commit(r.Context(), c), nil)
}

// POST /api/internal/cluster/migration/abort
func (s *Server) handleMigrationAbort(w http.ResponseWriter, r *http.Request) {
	var a cluster.MigrationAbort
	if !decodeMigrationStep(w, r, &a, &a.SourceNodeID) {
		return
	}
	answerMigrationStep(w, s.migrationTarget.Abort(r.Context(), a), nil)
}

// decodeMigrationStep reads a step's body. A step that names its source must
// name the node that sent it.
func decodeMigrationStep(w http.ResponseWriter, r *http.Request, into any, source *string) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if source != nil {
		if sender, _ := r.Context().Value("cluster_node_id").(string); sender == "" || sender != *source {
			http.Error(w, "the step names another source node than the one that sent it", http.StatusForbidden)
			return false
		}
	}
	return true
}

func answerMigrationStep(w http.ResponseWriter, err error, result any) {
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, cluster.ErrMigrationInvalid):
			status = http.StatusBadRequest
		case errors.Is(err, cluster.ErrMigrationNotFound):
			status = http.StatusNotFound
		case errors.Is(err, cluster.ErrMigrationConflict):
			status = http.StatusConflict
		default:
			logrus.WithError(err).Error("Bucket migration step failed")
		}
		http.Error(w, err.Error(), status)
		return
	}
	if result == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result) //nolint:errcheck
}
