package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// HABucketHeader is the HTTP header used to pass the full bucket path
// (e.g. "tenant/bucket" or "bucket") on HA fanout requests.
const HABucketHeader = "X-HA-Bucket"

// haReplicaKey is the unexported context key that marks a request as an HA replica write.
type haReplicaKey struct{}

// WithHAReplicaContext returns a child context marked as a replica write.
// HTTP handlers on replica nodes set this before calling any write operation
// so that HAObjectManager skips re-fanout and avoids infinite loops.
func WithHAReplicaContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, haReplicaKey{}, true)
}

func isHAReplica(ctx context.Context) bool {
	v, _ := ctx.Value(haReplicaKey{}).(bool)
	return v
}

// fanoutResult holds the outcome of a single replica fanout attempt.
type fanoutResult struct {
	nodeID string
	err    error
}

// HAObjectManager wraps object.Manager and adds HA write fanout.
// Read and metadata-only operations are delegated unchanged to the underlying
// manager. PutObject, DeleteObject and CompleteMultipartUpload fan out to
// replica nodes after a successful local write.
type HAObjectManager struct {
	object.Manager
	mgr *Manager
}

// NewHAObjectManager wraps m with HA write fanout backed by the cluster Manager.
// Returns object.Manager so it is a drop-in replacement.
func NewHAObjectManager(m object.Manager, mgr *Manager) object.Manager {
	return &HAObjectManager{Manager: m, mgr: mgr}
}

// PutObject writes locally then fans out to replica nodes.
func (h *HAObjectManager) PutObject(ctx context.Context, bucket, key string, data io.Reader, headers http.Header) (*object.Object, error) {
	obj, err := h.Manager.PutObject(ctx, bucket, key, data, headers)
	if err != nil {
		return nil, err
	}
	if !isHAReplica(ctx) {
		h.fanoutPut(ctx, bucket, key)
	}
	return obj, nil
}

// DeleteObject deletes locally then fans the deletion out.
func (h *HAObjectManager) DeleteObject(ctx context.Context, bucket, key string, bypassGovernance bool, versionID ...string) (string, error) {
	markerID, err := h.Manager.DeleteObject(ctx, bucket, key, bypassGovernance, versionID...)
	if err != nil {
		return "", err
	}
	if !isHAReplica(ctx) {
		h.fanoutDelete(ctx, bucket, key)
	}
	return markerID, nil
}

// CompleteMultipartUpload finalises locally then fans the assembled object out.
func (h *HAObjectManager) CompleteMultipartUpload(ctx context.Context, uploadID string, parts []object.Part) (*object.Object, error) {
	obj, err := h.Manager.CompleteMultipartUpload(ctx, uploadID, parts)
	if err != nil {
		return nil, err
	}
	if !isHAReplica(ctx) {
		h.fanoutPut(ctx, obj.Bucket, obj.Key)
	}
	return obj, nil
}

// ---------------------------------------------------------------------------
// Internal fanout helpers
// ---------------------------------------------------------------------------

// replicaTargets returns up to factor-1 healthy non-local nodes and the number
// of replica confirmations needed to satisfy quorum (ceil(factor/2) - 1).
// Returns (nil, 0, false) when replication is inactive.
func (h *HAObjectManager) replicaTargets(ctx context.Context) ([]*Node, int, bool) {
	if !h.mgr.IsClusterEnabled() {
		return nil, 0, false
	}
	factor, err := h.mgr.GetReplicationFactor(ctx)
	if err != nil || factor <= 1 {
		return nil, 0, false
	}
	localID, err := h.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return nil, 0, false
	}
	healthy, err := h.mgr.GetHealthyNodes(ctx)
	if err != nil {
		return nil, 0, false
	}
	var targets []*Node
	for _, n := range healthy {
		if n.ID == localID {
			continue
		}
		targets = append(targets, n)
		if len(targets) == factor-1 {
			break
		}
	}
	if len(targets) == 0 {
		return nil, 0, false
	}
	neededReplicas := (factor+1)/2 - 1
	return targets, neededReplicas, true
}

func (h *HAObjectManager) fanoutPut(ctx context.Context, bucket, key string) {
	targets, needed, ok := h.replicaTargets(ctx)
	if !ok {
		return
	}
	localID, _ := h.mgr.GetLocalNodeID(ctx)
	client := NewProxyClient(h.mgr.GetTLSConfig())
	ch := make(chan fanoutResult, len(targets))

	for _, node := range targets {
		go func(n *Node) {
			obj, reader, readErr := h.Manager.GetObject(ctx, bucket, key)
			if readErr != nil {
				ch <- fanoutResult{n.ID, fmt.Errorf("re-read for fanout: %w", readErr)}
				return
			}
			defer reader.Close()

			url := fmt.Sprintf("%s/api/internal/ha/objects/%s", n.Endpoint, key)
			req, err := client.CreateAuthenticatedRequest(ctx, "PUT", url, reader, localID, n.NodeToken)
			if err != nil {
				ch <- fanoutResult{n.ID, err}
				return
			}
			req.Header.Set("X-MaxIOFS-HA-Replica", "true")
			req.Header.Set(HABucketHeader, bucket)
			req.Header.Set("Content-Type", obj.ContentType)
			if obj.ContentDisposition != "" {
				req.Header.Set("Content-Disposition", obj.ContentDisposition)
			}
			if obj.ContentEncoding != "" {
				req.Header.Set("Content-Encoding", obj.ContentEncoding)
			}
			if obj.CacheControl != "" {
				req.Header.Set("Cache-Control", obj.CacheControl)
			}
			if obj.ContentLanguage != "" {
				req.Header.Set("Content-Language", obj.ContentLanguage)
			}
			if obj.StorageClass != "" {
				req.Header.Set("x-amz-storage-class", obj.StorageClass)
			}
			for k, v := range obj.Metadata {
				req.Header.Set("x-amz-meta-"+k, v)
			}
			req.ContentLength = obj.Size

			resp, err := client.DoAuthenticatedRequest(req)
			if err != nil {
				ch <- fanoutResult{n.ID, err}
				return
			}
			resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				ch <- fanoutResult{n.ID, fmt.Errorf("status %d", resp.StatusCode)}
				return
			}
			ch <- fanoutResult{n.ID, nil}
		}(node)
	}

	h.collectAndLog(ctx, ch, len(targets), needed, "PUT", bucket, key)
}

func (h *HAObjectManager) fanoutDelete(ctx context.Context, bucket, key string) {
	targets, needed, ok := h.replicaTargets(ctx)
	if !ok {
		return
	}
	localID, _ := h.mgr.GetLocalNodeID(ctx)
	client := NewProxyClient(h.mgr.GetTLSConfig())
	ch := make(chan fanoutResult, len(targets))

	for _, node := range targets {
		go func(n *Node) {
			url := fmt.Sprintf("%s/api/internal/ha/objects/%s", n.Endpoint, key)
			req, err := client.CreateAuthenticatedRequest(ctx, "DELETE", url, nil, localID, n.NodeToken)
			if err != nil {
				ch <- fanoutResult{n.ID, err}
				return
			}
			req.Header.Set("X-MaxIOFS-HA-Replica", "true")
			req.Header.Set(HABucketHeader, bucket)

			resp, err := client.DoAuthenticatedRequest(req)
			if err != nil {
				ch <- fanoutResult{n.ID, err}
				return
			}
			resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				ch <- fanoutResult{n.ID, fmt.Errorf("status %d", resp.StatusCode)}
				return
			}
			ch <- fanoutResult{n.ID, nil}
		}(node)
	}

	h.collectAndLog(ctx, ch, len(targets), needed, "DELETE", bucket, key)
}

// collectAndLog drains all results, marks failed nodes unavailable and logs quorum misses.
func (h *HAObjectManager) collectAndLog(ctx context.Context, ch <-chan fanoutResult, total, needed int, op, bucket, key string) {
	success := 0
	for i := 0; i < total; i++ {
		r := <-ch
		if r.err == nil {
			success++
		} else {
			logrus.WithFields(logrus.Fields{
				"node_id": r.nodeID, "op": op, "bucket": bucket, "key": key,
			}).WithError(r.err).Warn("HA fanout failed — marking node unavailable")
			now := time.Now()
			h.mgr.db.ExecContext(ctx, //nolint:errcheck
				`UPDATE cluster_nodes SET health_status = ?, updated_at = ? WHERE id = ?`,
				HealthStatusUnavailable, now, r.nodeID,
			)
		}
	}
	if success < needed {
		logrus.WithFields(logrus.Fields{
			"op": op, "bucket": bucket, "key": key,
			"needed": needed, "got": success,
		}).Error("HA quorum not reached — object written locally only")
	}
}

// ---------------------------------------------------------------------------
// Metadata-only fanout (Item D)
// Covers: UpdateObjectMetadata, SetObjectTagging, DeleteObjectTagging,
//         SetObjectACL, SetObjectRetention, SetObjectLegalHold, SetRestoreStatus
// These operations touch only Pebble metadata — no physical file transfer.
// Fanout is best-effort: failures are logged but do NOT fail the original op.
// ---------------------------------------------------------------------------

// HAMetadataOp describes a metadata-only operation to replay on replica nodes.
type HAMetadataOp struct {
	Op        string          `json:"op"`
	Key       string          `json:"key"`
	VersionID string          `json:"version_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

func (h *HAObjectManager) fanoutMetadata(ctx context.Context, bucket string, op HAMetadataOp) {
	if !h.mgr.IsClusterEnabled() {
		return
	}
	factor, err := h.mgr.GetReplicationFactor(ctx)
	if err != nil || factor <= 1 {
		return
	}
	localID, err := h.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return
	}
	healthy, err := h.mgr.GetHealthyNodes(ctx)
	if err != nil {
		return
	}

	body, err := json.Marshal(op)
	if err != nil {
		logrus.WithError(err).Warn("HA metadata fanout: failed to marshal op")
		return
	}

	client := NewProxyClient(h.mgr.GetTLSConfig())
	for _, n := range healthy {
		if n.ID == localID {
			continue
		}
		go func(node *Node) {
			url := fmt.Sprintf("%s/api/internal/ha/metadata-op", node.Endpoint)
			req, err := client.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(body), localID, node.NodeToken)
			if err != nil {
				logrus.WithError(err).WithField("node_id", node.ID).Warn("HA metadata fanout: request creation failed")
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(HABucketHeader, bucket)
			resp, err := client.DoAuthenticatedRequest(req)
			if err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{"node_id": node.ID, "op": op.Op}).
					Warn("HA metadata fanout: request failed")
				return
			}
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				logrus.WithFields(logrus.Fields{"node_id": node.ID, "op": op.Op, "status": resp.StatusCode}).
					Warn("HA metadata fanout: unexpected status")
			}
		}(n)
	}
}

// UpdateObjectMetadata fans out user-metadata updates.
func (h *HAObjectManager) UpdateObjectMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error {
	if err := h.Manager.UpdateObjectMetadata(ctx, bucket, key, metadata); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		data, _ := json.Marshal(metadata)
		h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "update-metadata", Key: key, Data: data})
	}
	return nil
}

// SetObjectTagging fans out tag writes.
func (h *HAObjectManager) SetObjectTagging(ctx context.Context, bucket, key string, tags *object.TagSet) error {
	if err := h.Manager.SetObjectTagging(ctx, bucket, key, tags); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		data, _ := json.Marshal(tags)
		h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-tagging", Key: key, Data: data})
	}
	return nil
}

// DeleteObjectTagging fans out tag deletions.
func (h *HAObjectManager) DeleteObjectTagging(ctx context.Context, bucket, key string) error {
	if err := h.Manager.DeleteObjectTagging(ctx, bucket, key); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "delete-tagging", Key: key})
	}
	return nil
}

// SetObjectACL fans out ACL writes.
func (h *HAObjectManager) SetObjectACL(ctx context.Context, bucket, key string, acl *object.ACL) error {
	if err := h.Manager.SetObjectACL(ctx, bucket, key, acl); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		data, _ := json.Marshal(acl)
		h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-acl", Key: key, Data: data})
	}
	return nil
}

// SetObjectRetention fans out retention config writes.
func (h *HAObjectManager) SetObjectRetention(ctx context.Context, bucket, key string, config *object.RetentionConfig, versionID ...string) error {
	if err := h.Manager.SetObjectRetention(ctx, bucket, key, config, versionID...); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		vid := ""
		if len(versionID) > 0 {
			vid = versionID[0]
		}
		data, _ := json.Marshal(config)
		h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-retention", Key: key, VersionID: vid, Data: data})
	}
	return nil
}

// SetObjectLegalHold fans out legal-hold writes.
func (h *HAObjectManager) SetObjectLegalHold(ctx context.Context, bucket, key string, config *object.LegalHoldConfig, versionID ...string) error {
	if err := h.Manager.SetObjectLegalHold(ctx, bucket, key, config, versionID...); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		vid := ""
		if len(versionID) > 0 {
			vid = versionID[0]
		}
		data, _ := json.Marshal(config)
		h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-legal-hold", Key: key, VersionID: vid, Data: data})
	}
	return nil
}

// SetRestoreStatus fans out restore-status writes.
func (h *HAObjectManager) SetRestoreStatus(ctx context.Context, bucket, key string, status string, expiresAt *time.Time) error {
	if err := h.Manager.SetRestoreStatus(ctx, bucket, key, status, expiresAt); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		type payload struct {
			Status    string     `json:"status"`
			ExpiresAt *time.Time `json:"expires_at,omitempty"`
		}
		data, _ := json.Marshal(payload{Status: status, ExpiresAt: expiresAt})
		h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-restore-status", Key: key, Data: data})
	}
	return nil
}
