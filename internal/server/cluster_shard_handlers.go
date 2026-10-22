package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// shardOfRequest is the shard a request between nodes names, and the store
// that keeps this node's shards.
func (s *Server) shardOfRequest(w http.ResponseWriter, r *http.Request) (object.ShardInfo, object.ShardStore, bool) {
	info, err := cluster.DecodeShardInfo(r.Header.Get(cluster.HAShardHeader))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return info, nil, false
	}
	if info.Bucket == "" || info.Key == "" || info.Key != mux.Vars(r)["key"] {
		http.Error(w, "the shard names another bucket or key", http.StatusBadRequest)
		return info, nil, false
	}
	store, ok := cluster.ShardStoreOf(s.objectManager)
	if !ok {
		http.Error(w, "shards are not kept on this node", http.StatusNotImplemented)
		return info, nil, false
	}
	return info, store, true
}

// handleHAReceiveShard stores a shard of a write another node cuts in shards.
// PUT /api/internal/cluster/ha/shards/{key}
func (s *Server) handleHAReceiveShard(w http.ResponseWriter, r *http.Request) {
	info, store, ok := s.shardOfRequest(w, r)
	if !ok {
		return
	}
	ctx := cluster.WithHAReplicaContext(r.Context())
	if cluster.VersionDeleted(ctx, s.db, info.Bucket, info.Key, info.VersionID, time.Unix(info.LastModified, 0)) ||
		(info.VersionID == "" && cluster.KeyDeletedAfter(ctx, s.db, info.Bucket, info.Key, time.Unix(info.LastModified, 0))) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	err := store.PutShard(ctx, info, r.Body)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, object.ErrBucketNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": info.Bucket, "key": info.Key, "shard": info.Index}).
			Error("HA: failed to store a shard")
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleHAGetShard serves a shard this node holds, from the start of a
// "bytes=N-" range.
// GET /api/internal/cluster/ha/shards/{key}
func (s *Server) handleHAGetShard(w http.ResponseWriter, r *http.Request) {
	info, store, ok := s.shardOfRequest(w, r)
	if !ok {
		return
	}
	size := info.Layout.ShardSize(info.Size)
	offset, err := haRangeStart(r.Header.Get("Range"), size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
		return
	}
	reader, err := store.OpenShard(r.Context(), info, offset)
	switch {
	case errors.Is(err, object.ErrObjectNotFound):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Length", fmt.Sprintf("%d", size-offset))
	if offset > 0 {
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	io.Copy(w, reader) //nolint:errcheck
}

// handleHADeleteShard removes a shard staged for a placement that did not
// land.
// DELETE /api/internal/cluster/ha/shards/{key}
func (s *Server) handleHADeleteShard(w http.ResponseWriter, r *http.Request) {
	info, store, ok := s.shardOfRequest(w, r)
	if !ok {
		return
	}
	if err := store.DeleteShard(r.Context(), info); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
