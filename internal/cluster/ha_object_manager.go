package cluster

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// HABucketHeader is the HTTP header used to pass the full bucket path
// (e.g. "tenant/bucket" or "bucket") on HA fanout requests.
const HABucketHeader = "X-HA-Bucket"
const HAObjectVersionHeader = "X-HA-Version-ID"
const HADeleteMarkerVersionHeader = "X-HA-Delete-Marker-Version-ID"

// HALastModifiedHeader carries the primary's LastModified (unix seconds) on
// legacy (decrypt/re-encrypt) transfers so replicas store the same timestamp
// instead of their receive time. Raw transfers carry it inside the Pebble
// metadata payload and do not need this header.
const HALastModifiedHeader = "X-HA-Last-Modified"

// Raw (ciphertext) replication headers. When HARawHeader is "true" the body
// is the stored ciphertext as-is; the sidecar and Pebble metadata travel
// base64(JSON)-encoded so the replica stores an identical copy without
// decrypting/re-encrypting.
const HARawHeader = "X-HA-Raw"
const HARawSidecarHeader = "X-HA-Raw-Sidecar"
const HARawObjectMetaHeader = "X-HA-Raw-Object-Meta"

// setHALastModified attaches the primary's modification timestamp to a legacy
// replica transfer (request or response headers).
func setHALastModified(h http.Header, obj *object.Object) {
	if obj != nil && !obj.LastModified.IsZero() && obj.LastModified.Unix() > 0 {
		h.Set(HALastModifiedHeader, strconv.FormatInt(obj.LastModified.Unix(), 10))
	}
}

// setHAChecksum forwards the object's client checksum (x-amz-checksum-*) on a
// legacy replica transfer so the replica's Pebble entry keeps the same
// ChecksumAlgorithm/ChecksumValue as the primary (GetObjectAttributes parity).
// The receiver recomputes the checksum over the plaintext body and validates
// it against this value, so a corrupted transfer is also caught.
func setHAChecksum(h http.Header, obj *object.Object) {
	if obj == nil || obj.ChecksumAlgorithm == "" || obj.ChecksumValue == "" {
		return
	}
	h.Set("x-amz-checksum-algorithm", obj.ChecksumAlgorithm)
	h.Set("x-amz-checksum-"+strings.ToLower(obj.ChecksumAlgorithm), obj.ChecksumValue)
}

// HALastModifiedFromHeader parses the primary's modification timestamp from a
// legacy replica transfer. Returns false when absent or malformed.
func HALastModifiedFromHeader(h http.Header) (time.Time, bool) {
	v := h.Get(HALastModifiedHeader)
	if v == "" {
		return time.Time{}, false
	}
	ts, err := strconv.ParseInt(v, 10, 64)
	if err != nil || ts <= 0 {
		return time.Time{}, false
	}
	return time.Unix(ts, 0), true
}

// haReplicaKey is the unexported context key that marks a request as an HA replica write.
type haReplicaKey struct{}

// WithHAReplicaContext returns a child context marked as a replica write.
// It also sets the object-layer quota-enforcement bypass so that the
// underlying PutObject updates the local quota counter without re-enforcing
// the limit (the primary already validated the quota for this write).
// HTTP handlers on replica nodes set this before calling any write operation
// so that HAObjectManager skips re-fanout and avoids infinite loops.
func WithHAReplicaContext(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, haReplicaKey{}, true)
	ctx = object.WithBypassQuotaEnforcement(ctx)
	return ctx
}

func isHAReplica(ctx context.Context) bool {
	v, _ := ctx.Value(haReplicaKey{}).(bool)
	return v
}

// haRollbackKey marks a Manager call that is undoing a quorum-failed write.
// HAObjectManager checks this to skip re-fanout when deleting the local copy.
type haRollbackKey struct{}

// WithHARollbackContext returns a child context marked as a quorum-failure rollback.
// Used internally to suppress fanout while the wrapper undoes a local write.
func WithHARollbackContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, haRollbackKey{}, true)
}

func isHARollback(ctx context.Context) bool {
	v, _ := ctx.Value(haRollbackKey{}).(bool)
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

// PutObject writes locally then synchronously replicates to the quorum.
// If the cluster cannot satisfy the replication factor, the local write is
// rolled back and ErrClusterDegraded is returned so the caller can emit 503.
func (h *HAObjectManager) PutObject(ctx context.Context, bucket, key string, data io.Reader, headers http.Header) (*object.Object, error) {
	if !isHAReplica(ctx) && !isHARollback(ctx) {
		if ok, err := h.mgr.ClusterCanAcceptWrites(ctx); err == nil && !ok {
			return nil, ErrClusterDegraded
		}
	}
	obj, err := h.Manager.PutObject(ctx, bucket, key, data, headers)
	if err != nil {
		return nil, err
	}
	if isHAReplica(ctx) || isHARollback(ctx) {
		return obj, nil
	}
	if err := h.fanoutPut(ctx, bucket, key, obj.VersionID); err != nil {
		h.rollbackLocalPut(ctx, bucket, key, "PutObject")
		return nil, err
	}
	return obj, nil
}

// DeleteObject deletes locally then synchronously fans the deletion out.
// On quorum failure the local delete is NOT rolled back (delete is a tombstone
// that anti-entropy will reconcile); ErrClusterDegraded is returned so the
// client can retry.
func (h *HAObjectManager) DeleteObject(ctx context.Context, bucket, key string, bypassGovernance bool, versionID ...string) (string, error) {
	if !isHAReplica(ctx) && !isHARollback(ctx) {
		if ok, err := h.mgr.ClusterCanAcceptWrites(ctx); err == nil && !ok {
			return "", ErrClusterDegraded
		}
	}
	markerID, err := h.Manager.DeleteObject(ctx, bucket, key, bypassGovernance, versionID...)
	if err != nil {
		return "", err
	}
	if isHAReplica(ctx) || isHARollback(ctx) {
		return markerID, nil
	}
	specificVersionID := ""
	if len(versionID) > 0 {
		specificVersionID = versionID[0]
	}
	if err := h.fanoutDelete(ctx, bucket, key, specificVersionID, markerID); err != nil {
		return markerID, err
	}
	return markerID, nil
}

// CompleteMultipartUpload finalises locally then synchronously replicates the
// assembled object. Quorum failure rolls back the local object and returns
// ErrClusterDegraded.
func (h *HAObjectManager) CompleteMultipartUpload(ctx context.Context, uploadID string, parts []object.Part) (*object.Object, error) {
	if !isHAReplica(ctx) && !isHARollback(ctx) {
		if ok, err := h.mgr.ClusterCanAcceptWrites(ctx); err == nil && !ok {
			return nil, ErrClusterDegraded
		}
	}
	obj, err := h.Manager.CompleteMultipartUpload(ctx, uploadID, parts)
	if err != nil {
		return nil, err
	}
	if isHAReplica(ctx) || isHARollback(ctx) {
		return obj, nil
	}
	if err := h.fanoutPut(ctx, obj.Bucket, obj.Key, obj.VersionID); err != nil {
		h.rollbackLocalPut(ctx, obj.Bucket, obj.Key, "CompleteMultipartUpload")
		return nil, err
	}
	return obj, nil
}

// ---------------------------------------------------------------------------
// RawObjectAccessor delegation
// ---------------------------------------------------------------------------
// HAObjectManager embeds the object.Manager INTERFACE, so methods outside that
// interface are not promoted. Replica nodes hold the wrapper as their
// objectManager, and the raw-replication receive path type-asserts
// object.RawObjectAccessor — these delegating methods make the wrapper
// satisfy it against the underlying manager.

func (h *HAObjectManager) GetObjectRaw(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, map[string]string, *metadata.ObjectMetadata, error) {
	raw, ok := h.Manager.(object.RawObjectAccessor)
	if !ok {
		return nil, nil, nil, fmt.Errorf("underlying manager does not support raw access")
	}
	return raw.GetObjectRaw(ctx, bucket, key, versionID)
}

func (h *HAObjectManager) PutObjectRaw(ctx context.Context, bucket, key string, data io.Reader, sidecar map[string]string, metaObj *metadata.ObjectMetadata) error {
	raw, ok := h.Manager.(object.RawObjectAccessor)
	if !ok {
		return fmt.Errorf("underlying manager does not support raw access")
	}
	return raw.PutObjectRaw(ctx, bucket, key, data, sidecar, metaObj)
}

func (h *HAObjectManager) CanReplicateRaw(sidecar map[string]string) bool {
	raw, ok := h.Manager.(object.RawObjectAccessor)
	if !ok {
		return false
	}
	return raw.CanReplicateRaw(sidecar)
}

// rollbackLocalPut deletes the just-written local copy after a quorum failure.
// Uses WithHARollbackContext to suppress fanout of the rollback delete.
// Failures are logged but not surfaced — the original ErrClusterDegraded already
// tells the client to retry, and any leftover local copy will be reconciled by
// anti-entropy.
func (h *HAObjectManager) rollbackLocalPut(ctx context.Context, bucket, key, op string) {
	rbCtx := WithHARollbackContext(ctx)
	if _, err := h.Manager.DeleteObject(rbCtx, bucket, key, true); err != nil {
		logrus.WithFields(logrus.Fields{
			"op": op, "bucket": bucket, "key": key,
		}).WithError(err).Error("HA quorum rollback: failed to delete local copy")
	}
}

// ---------------------------------------------------------------------------
// Internal fanout helpers
// ---------------------------------------------------------------------------

// replicaTargets returns up to factor-1 healthy non-local nodes and the number
// of peer confirmations needed to satisfy quorum: neededReplicas = ceil(factor/2) - 1.
// Returns (nil, 0, false) when replication is inactive.
//
// Quorum table (total writes = 1 local + neededReplicas):
//
//	factor=2 → neededReplicas=0  (best-effort: local write succeeds even when peer is down)
//	factor=3 → neededReplicas=1  (2-of-3: tolerates 1 failure)
//	factor=4 → neededReplicas=1  (2-of-4: tolerates 2 failures)
//	factor=5 → neededReplicas=2  (3-of-5: tolerates 2 failures)
//
// factor=2 deliberately uses neededReplicas=0. Requiring 1 peer confirmation
// with only 2 cluster nodes would block ALL writes whenever the single peer is
// unreachable, which is worse than best-effort. The stale-object reconciler
// catches divergence once the peer recovers.
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

// fanoutPut synchronously replicates the just-written object to replica nodes.
// versionID must be the ID of the version that was just written locally so the
// re-read is pinned to that exact version — avoiding RACE-04 where a concurrent
// PutObject between the local write and the re-read causes the wrong (newer)
// version to be replicated.
// Returns ErrClusterDegraded when fewer than `needed` replicas confirm.
// Returns nil when replication is inactive (factor=1, cluster disabled, or
// factor=2 which needs zero replica confirmations).
func (h *HAObjectManager) fanoutPut(ctx context.Context, bucket, key, versionID string) error {
	targets, needed, ok := h.replicaTargets(ctx)
	if !ok {
		return nil
	}
	localID, _ := h.mgr.GetLocalNodeID(ctx)
	client := NewProxyClient(h.mgr.GetTLSConfig())
	ch := make(chan fanoutResult, len(targets))

	// Raw (ciphertext) transfer capability of the underlying manager.
	rawAccessor, _ := h.Manager.(object.RawObjectAccessor)

	for _, node := range targets {
		go func(n *Node) {
			// Prefer the raw ciphertext path: objects envelope-encrypted with a
			// cluster-shared KEK replicate as stored bytes + sidecar, with no
			// decrypt on this node and no re-encrypt on the replica.
			if rawAccessor != nil {
				sent, rawErr := h.sendRawReplica(ctx, client, rawAccessor, n, localID, bucket, key, versionID)
				if sent {
					ch <- fanoutResult{n.ID, rawErr}
					return
				}
				// Not eligible (plaintext/legacy/local-KEK object) or the
				// replica declined raw — fall through to the legacy path.
			}

			// RACE-04: pin the re-read to the version that was just written.
			// Without versionID, a concurrent PutObject could have created a newer
			// version by now, and we would replicate the wrong data.
			var obj *object.Object
			var reader io.ReadCloser
			var readErr error
			if versionID != "" {
				obj, reader, readErr = h.Manager.GetObject(ctx, bucket, key, versionID)
			} else {
				obj, reader, readErr = h.Manager.GetObject(ctx, bucket, key)
			}
			if readErr != nil {
				ch <- fanoutResult{n.ID, fmt.Errorf("re-read for fanout: %w", readErr)}
				return
			}
			defer reader.Close()

			url := fmt.Sprintf("%s/api/internal/cluster/ha/objects/%s", n.Endpoint, escapeHAObjectKey(key))
			req, err := client.CreateAuthenticatedRequest(ctx, "PUT", url, reader, localID, n.NodeToken)
			if err != nil {
				ch <- fanoutResult{n.ID, err}
				return
			}
			req.Header.Set("X-MaxIOFS-HA-Replica", "true")
			req.Header.Set(HABucketHeader, bucket)
			if obj.VersionID != "" {
				req.Header.Set(HAObjectVersionHeader, obj.VersionID)
			}
			setHALastModified(req.Header, obj)
			setHAChecksum(req.Header, obj)
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

	return h.collectAndCheckQuorum(ctx, ch, len(targets), needed, "PUT", bucket, key)
}

// sendRawReplica attempts the ciphertext transfer of the pinned version to
// one replica. Returns sent=false (nil error) when the object is not eligible
// for raw replication or the replica declined it (HTTP 412) — the caller then
// falls back to the legacy decrypt/re-encrypt path. sent=true means the raw
// attempt is authoritative: err (nil or not) is the node's fanout result.
func (h *HAObjectManager) sendRawReplica(ctx context.Context, client *ProxyClient, raw object.RawObjectAccessor, n *Node, localID, bucket, key, versionID string) (sent bool, err error) {
	reader, sidecar, metaObj, readErr := raw.GetObjectRaw(ctx, bucket, key, versionID)
	if readErr != nil {
		// Let the legacy path surface the read error consistently.
		return false, nil
	}
	defer reader.Close()

	if !raw.CanReplicateRaw(sidecar) {
		return false, nil
	}

	sidecarJSON, jErr := json.Marshal(sidecar)
	if jErr != nil {
		return false, nil
	}
	metaJSON, jErr := json.Marshal(metaObj)
	if jErr != nil {
		return false, nil
	}

	url := fmt.Sprintf("%s/api/internal/cluster/ha/objects/%s", n.Endpoint, escapeHAObjectKey(key))
	req, rErr := client.CreateAuthenticatedRequest(ctx, "PUT", url, reader, localID, n.NodeToken)
	if rErr != nil {
		return true, rErr
	}
	req.Header.Set("X-MaxIOFS-HA-Replica", "true")
	req.Header.Set(HABucketHeader, bucket)
	req.Header.Set(HARawHeader, "true")
	req.Header.Set(HARawSidecarHeader, base64.StdEncoding.EncodeToString(sidecarJSON))
	req.Header.Set(HARawObjectMetaHeader, base64.StdEncoding.EncodeToString(metaJSON))
	if ciphertextSize, pErr := strconv.ParseInt(sidecar["size"], 10, 64); pErr == nil {
		req.ContentLength = ciphertextSize
	}

	resp, dErr := client.DoAuthenticatedRequest(req)
	if dErr != nil {
		return true, dErr
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		// Replica cannot decrypt this KEK version (should not happen once the
		// join distributed the cluster keys) — fall back to legacy transfer.
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return true, fmt.Errorf("raw replica status %d", resp.StatusCode)
	}
	return true, nil
}

// fanoutDelete synchronously replicates the deletion. Same semantics as
// fanoutPut: returns ErrClusterDegraded when fewer than `needed` replicas
// confirm.
func (h *HAObjectManager) fanoutDelete(ctx context.Context, bucket, key, specificVersionID, deleteMarkerVersionID string) error {
	targets, needed, ok := h.replicaTargets(ctx)
	if !ok {
		return nil
	}
	localID, _ := h.mgr.GetLocalNodeID(ctx)
	client := NewProxyClient(h.mgr.GetTLSConfig())
	ch := make(chan fanoutResult, len(targets))

	for _, node := range targets {
		go func(n *Node) {
			url := fmt.Sprintf("%s/api/internal/cluster/ha/objects/%s", n.Endpoint, escapeHAObjectKey(key))
			req, err := client.CreateAuthenticatedRequest(ctx, "DELETE", url, nil, localID, n.NodeToken)
			if err != nil {
				ch <- fanoutResult{n.ID, err}
				return
			}
			req.Header.Set("X-MaxIOFS-HA-Replica", "true")
			req.Header.Set(HABucketHeader, bucket)
			if specificVersionID != "" {
				req.Header.Set(HAObjectVersionHeader, specificVersionID)
			}
			if deleteMarkerVersionID != "" {
				req.Header.Set(HADeleteMarkerVersionHeader, deleteMarkerVersionID)
			}

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

	return h.collectAndCheckQuorum(ctx, ch, len(targets), needed, "DELETE", bucket, key)
}

// collectAndCheckQuorum drains all fanout results, marks failed nodes
// unavailable, and returns ErrClusterDegraded when successes < needed.
func (h *HAObjectManager) collectAndCheckQuorum(ctx context.Context, ch <-chan fanoutResult, total, needed int, op, bucket, key string) error {
	success := 0
	for i := 0; i < total; i++ {
		r := <-ch
		if r.err == nil {
			success++
			continue
		}
		logrus.WithFields(logrus.Fields{
			"node_id": r.nodeID, "op": op, "bucket": bucket, "key": key,
		}).WithError(r.err).Warn("HA fanout failed — marking node unavailable")
		now := time.Now()
		h.mgr.db.ExecContext(ctx, //nolint:errcheck
			`UPDATE cluster_nodes SET health_status = ?, updated_at = ? WHERE id = ?`,
			HealthStatusUnavailable, now, r.nodeID,
		)
	}
	if success < needed {
		logrus.WithFields(logrus.Fields{
			"op": op, "bucket": bucket, "key": key,
			"needed": needed, "got": success,
		}).Error("HA quorum not reached — failing write")
		return ErrClusterDegraded
	}
	return nil
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
		go h.fanoutMetadataToNode(client, n, bucket, localID, op, body)
	}
}

// fanoutMetadataToNode sends a metadata op to a single replica with up to 3
// attempts and a 5-second per-attempt timeout. On permanent failure it logs at
// Error level so the divergence is visible in the operator log.
func (h *HAObjectManager) fanoutMetadataToNode(client *ProxyClient, node *Node, bucket, localID string, op HAMetadataOp, body []byte) {
	const maxAttempts = 3
	const perAttemptTimeout = 5 * time.Second
	const baseRetryDelay = 200 * time.Millisecond

	url := fmt.Sprintf("%s/api/internal/cluster/ha/metadata-op", node.Endpoint)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		opCtx, cancel := context.WithTimeout(context.Background(), perAttemptTimeout)
		req, err := client.CreateAuthenticatedRequest(opCtx, "POST", url, bytes.NewReader(body), localID, node.NodeToken)
		if err != nil {
			cancel()
			logrus.WithError(err).WithField("node_id", node.ID).Warn("HA metadata fanout: request creation failed")
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HABucketHeader, bucket)
		resp, err := client.DoAuthenticatedRequest(req)
		cancel()

		if err == nil && resp.StatusCode < 300 {
			resp.Body.Close()
			return
		}

		if err != nil {
			if attempt < maxAttempts {
				time.Sleep(baseRetryDelay * time.Duration(attempt))
				continue
			}
			logrus.WithError(err).WithFields(logrus.Fields{
				"node_id": node.ID, "op": op.Op, "bucket": bucket, "attempts": attempt,
			}).Error("HA metadata fanout: all retries exhausted — metadata may be diverged on this replica")
			return
		}
		// Non-2xx response
		resp.Body.Close()
		if attempt < maxAttempts {
			time.Sleep(baseRetryDelay * time.Duration(attempt))
			continue
		}
		logrus.WithFields(logrus.Fields{
			"node_id": node.ID, "op": op.Op, "bucket": bucket, "status": resp.StatusCode, "attempts": attempt,
		}).Error("HA metadata fanout: all retries exhausted — metadata may be diverged on this replica")
	}
}

// UpdateObjectMetadata fans out user-metadata updates.
func (h *HAObjectManager) UpdateObjectMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error {
	if err := h.Manager.UpdateObjectMetadata(ctx, bucket, key, metadata); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		data, err := json.Marshal(metadata)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key}).Warn("HA fanout: failed to marshal metadata, skipping replica sync")
		} else {
			h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "update-metadata", Key: key, Data: data})
		}
	}
	return nil
}

// SetObjectTagging fans out tag writes.
func (h *HAObjectManager) SetObjectTagging(ctx context.Context, bucket, key string, tags *object.TagSet, versionID ...string) error {
	if err := h.Manager.SetObjectTagging(ctx, bucket, key, tags, versionID...); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		vid := ""
		if len(versionID) > 0 {
			vid = versionID[0]
		}
		data, err := json.Marshal(tags)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key}).Warn("HA fanout: failed to marshal tags, skipping replica sync")
		} else {
			h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-tagging", Key: key, VersionID: vid, Data: data})
		}
	}
	return nil
}

// DeleteObjectTagging fans out tag deletions.
func (h *HAObjectManager) DeleteObjectTagging(ctx context.Context, bucket, key string, versionID ...string) error {
	if err := h.Manager.DeleteObjectTagging(ctx, bucket, key, versionID...); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		vid := ""
		if len(versionID) > 0 {
			vid = versionID[0]
		}
		h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "delete-tagging", Key: key, VersionID: vid})
	}
	return nil
}

// SetObjectACL fans out ACL writes.
func (h *HAObjectManager) SetObjectACL(ctx context.Context, bucket, key string, acl *object.ACL, versionID ...string) error {
	if err := h.Manager.SetObjectACL(ctx, bucket, key, acl, versionID...); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		vid := ""
		if len(versionID) > 0 {
			vid = versionID[0]
		}
		data, err := json.Marshal(acl)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key}).Warn("HA fanout: failed to marshal ACL, skipping replica sync")
		} else {
			h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-acl", Key: key, VersionID: vid, Data: data})
		}
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
		data, err := json.Marshal(config)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key}).Warn("HA fanout: failed to marshal retention config, skipping replica sync")
		} else {
			h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-retention", Key: key, VersionID: vid, Data: data})
		}
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
		data, err := json.Marshal(config)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key}).Warn("HA fanout: failed to marshal legal-hold config, skipping replica sync")
		} else {
			h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-legal-hold", Key: key, VersionID: vid, Data: data})
		}
	}
	return nil
}

// SetRestoreStatus fans out restore-status writes.
func (h *HAObjectManager) SetRestoreStatus(ctx context.Context, bucket, key string, status string, expiresAt *time.Time, versionID ...string) error {
	if err := h.Manager.SetRestoreStatus(ctx, bucket, key, status, expiresAt, versionID...); err != nil {
		return err
	}
	if !isHAReplica(ctx) {
		vid := ""
		if len(versionID) > 0 {
			vid = versionID[0]
		}
		type payload struct {
			Status    string     `json:"status"`
			ExpiresAt *time.Time `json:"expires_at,omitempty"`
		}
		data, err := json.Marshal(payload{Status: status, ExpiresAt: expiresAt})
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key}).Warn("HA fanout: failed to marshal restore status, skipping replica sync")
		} else {
			h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-restore-status", Key: key, VersionID: vid, Data: data})
		}
	}
	return nil
}
