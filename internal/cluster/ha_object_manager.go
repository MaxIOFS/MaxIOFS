package cluster

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
const HALastModifiedHeader = "X-HA-Last-Modified"

// HAObjectLockHeader marks a legacy replica transfer whose object-lock headers
// are the primary's stored state: the replica keeps them as they are and adds
// no bucket default.
const HAObjectLockHeader = "X-HA-Object-Lock"

// HAObjectAttributesHeader carries, base64 JSON, what a legacy transfer's
// headers cannot: the version's ETag, tags, ACL and restore state.
const HAObjectAttributesHeader = "X-HA-Object-Attributes"

// Raw (ciphertext) replication headers.
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

// SetHAObjectLock forwards the object's retention and legal hold as stored, as
// the raw transfer does. Only what is set is sent: an older replica refuses
// any lock header on a bucket without Object Lock.
func SetHAObjectLock(h http.Header, obj *object.Object) {
	h.Set(HAObjectLockHeader, "true")
	if r := obj.Retention; r != nil {
		h.Set("x-amz-object-lock-mode", r.Mode)
		h.Set("x-amz-object-lock-retain-until-date", r.RetainUntilDate.UTC().Format(time.RFC3339Nano))
	}
	if obj.LegalHold != nil && obj.LegalHold.Status == object.LegalHoldStatusOn {
		h.Set("x-amz-object-lock-legal-hold", object.LegalHoldStatusOn)
	}
}

// SetHAAttributes forwards the attributes of obj a legacy transfer's headers
// do not carry.
func SetHAAttributes(h http.Header, obj *object.Object) {
	if data, err := json.Marshal(object.AttributesOf(obj)); err == nil {
		h.Set(HAObjectAttributesHeader, base64.StdEncoding.EncodeToString(data))
	}
}

// setHAChecksum forwards the object's client checksum (x-amz-checksum-*) on a
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

// ReplicaWriteContext marks a write as a copy of another node's object and
// carries what the transfer headers pin: the version ID, the modification
// time, the attributes and, when the sender marks it complete, the object-lock
// state.
func ReplicaWriteContext(ctx context.Context, h http.Header) context.Context {
	ctx = object.WithReplicaCopy(WithHAReplicaContext(ctx))
	if encoded := h.Get(HAObjectAttributesHeader); encoded != "" {
		var a object.ReplicatedAttributes
		if data, err := base64.StdEncoding.DecodeString(encoded); err == nil && json.Unmarshal(data, &a) == nil {
			ctx = object.WithReplicatedAttributes(ctx, a)
		}
	}
	if versionID := h.Get(HAObjectVersionHeader); versionID != "" {
		ctx = object.WithReplicatedVersionID(ctx, versionID)
	}
	if lm, ok := HALastModifiedFromHeader(h); ok {
		ctx = object.WithReplicatedLastModified(ctx, lm)
	}
	if h.Get(HAObjectLockHeader) == "true" {
		ctx = object.WithReplicatedObjectLock(ctx)
	}
	return ctx
}

// fanoutResult holds the outcome of a single replica fanout attempt.
type fanoutResult struct {
	nodeID string
	err    error
}

// HAObjectManager wraps object.Manager and adds HA write fanout.
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
	if err := h.fanoutPut(ctx, bucket, key, obj.VersionID, obj.LastModified); err != nil {
		h.rollbackLocalPut(ctx, bucket, key, obj.VersionID, "PutObject")
		return nil, err
	}
	return obj, nil
}

// DeleteObject deletes locally then synchronously fans the deletion out.
func (h *HAObjectManager) DeleteObject(ctx context.Context, bucket, key string, bypassGovernance bool, versionID ...string) (string, error) {
	if !isHAReplica(ctx) && !isHARollback(ctx) {
		if ok, err := h.mgr.ClusterCanAcceptWrites(ctx); err == nil && !ok {
			return "", ErrClusterDegraded
		}
	}
	written := h.lastWritten(ctx, bucket, key, versionID...)
	markerID, err := h.Manager.DeleteObject(ctx, bucket, key, bypassGovernance, versionID...)
	if err != nil {
		return "", err
	}
	if !isHARollback(ctx) {
		h.recordObjectDeletionTombstone(ctx, bucket, key, DeletedAfter(written), versionID...)
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

// lastWritten returns when the version a delete removes, or the current
// object of the key, was written (unix seconds), or 0.
func (h *HAObjectManager) lastWritten(ctx context.Context, bucket, key string, versionID ...string) int64 {
	if len(versionID) == 0 || versionID[0] == "" {
		if obj, err := h.Manager.GetObjectMetadata(ctx, bucket, key); err == nil {
			return obj.LastModified.Unix()
		}
		return 0
	}
	versions, err := h.Manager.GetObjectVersions(ctx, bucket, key)
	if err != nil {
		return 0
	}
	for _, v := range versions {
		if v.VersionID == versionID[0] {
			return v.LastModified.Unix()
		}
	}
	return 0
}

func (h *HAObjectManager) recordObjectDeletionTombstone(ctx context.Context, bucket, key string, deletedAt int64, versionID ...string) {
	nodeID, err := h.mgr.GetLocalNodeID(ctx)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket": bucket, "key": key,
		}).Warn("HA delete: failed to resolve local node for tombstone")
		return
	}

	entityType := EntityTypeObject
	entityID := ObjectTombstoneID(bucket, key)
	if len(versionID) > 0 && versionID[0] != "" {
		entityType = EntityTypeObjectVersion
		entityID = ObjectVersionTombstoneID(bucket, key, versionID[0])
	}
	if err := RecordDeletion(ctx, h.mgr.db, entityType, entityID, nodeID, deletedAt); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket": bucket, "key": key, "entity_type": entityType,
		}).Warn("HA delete: failed to record object tombstone")
	}
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
	if err := h.fanoutPut(ctx, obj.Bucket, obj.Key, obj.VersionID, obj.LastModified); err != nil {
		h.rollbackLocalPut(ctx, obj.Bucket, obj.Key, obj.VersionID, "CompleteMultipartUpload")
		return nil, err
	}
	return obj, nil
}

// ---------------------------------------------------------------------------

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
// Non-versioned overwrites cannot be safely rolled back here because DeleteObject
// would delete the current key without restoring the previous bytes. The write
// was never acknowledged, so the retention and legal hold it set do not block
// the delete.
func (h *HAObjectManager) rollbackLocalPut(ctx context.Context, bucket, key, versionID, op string) {
	if versionID == "" {
		logrus.WithFields(logrus.Fields{
			"op": op, "bucket": bucket, "key": key,
		}).Warn("HA quorum rollback skipped for non-versioned write; preserving local object")
		return
	}
	rbCtx := object.WithWriteRollback(WithHARollbackContext(ctx))
	if _, err := h.Manager.DeleteObject(rbCtx, bucket, key, true, versionID); err != nil {
		logrus.WithFields(logrus.Fields{
			"op": op, "bucket": bucket, "key": key, "version_id": versionID,
		}).WithError(err).Error("HA quorum rollback: failed to delete local copy")
	}
}

// RequiredReplicaAcks is how many peers must confirm a write: half of the
// factor's copies, rounded up, hold it, and the local copy is one of them. A
// factor of 2 is a mirror that keeps accepting writes while its peer is down;
// a factor of 3 needs one of its two peers. A peer that misses a write is
// caught up when it is back (see noteMissedWrites).
func RequiredReplicaAcks(factor int) int {
	if factor <= 1 {
		return 0
	}
	return (factor+1)/2 - 1
}

// replicaTargets returns up to factor-1 healthy non-local nodes and the number
// of them that must confirm. When fewer are healthy, every other node that is
// not healthy misses the write made at modified, and is recorded as such.
func (h *HAObjectManager) replicaTargets(ctx context.Context, modified time.Time) ([]*Node, int, bool) {
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
	if len(targets) < factor-1 {
		h.mgr.noteMissedWrites(ctx, localID, modified)
	}
	if len(targets) == 0 {
		return nil, 0, false
	}
	return targets, RequiredReplicaAcks(factor), true
}

// fanoutPut synchronously replicates the just-written object to replica nodes.
// modified is the object's modification time: a peer that misses the write is
// caught up from it.
func (h *HAObjectManager) fanoutPut(ctx context.Context, bucket, key, versionID string, modified time.Time) error {
	targets, needed, ok := h.replicaTargets(ctx, modified)
	if !ok {
		return nil
	}
	localID, _ := h.mgr.GetLocalNodeID(ctx)
	client := NewProxyClient(h.mgr.GetTLSConfig())
	ch := make(chan fanoutResult, len(targets))

	for _, node := range targets {
		go func(n *Node) {
			ch <- fanoutResult{n.ID, sendObjectVersion(ctx, client, h.Manager, n, localID, bucket, key, versionID)}
		}(node)
	}

	return h.collectAndCheckQuorum(ctx, ch, len(targets), needed, modified, "PUT", bucket, key)
}

// sendObjectVersion copies one version of key — the latest when versionID is
// empty — to node n: the stored ciphertext when n can decrypt it, otherwise the
// plaintext with every field the receiver keeps.
func sendObjectVersion(ctx context.Context, client *ProxyClient, objects object.Manager, n *Node, localID, bucket, key, versionID string) error {
	if raw, ok := objects.(object.RawObjectAccessor); ok {
		if sent, err := sendRawReplica(ctx, client, raw, n, localID, bucket, key, versionID); sent {
			return err
		}
		// Not eligible (plaintext/legacy/local-KEK object) or the replica
		// declined raw — fall through to the legacy path.
	}

	// RACE-04: pin the re-read to the version that was just written.
	// Without versionID, a concurrent PutObject could have created a newer
	// version by now, and we would replicate the wrong data.
	var obj *object.Object
	var reader io.ReadCloser
	var readErr error
	if versionID != "" {
		obj, reader, readErr = objects.GetObject(ctx, bucket, key, versionID)
	} else {
		obj, reader, readErr = objects.GetObject(ctx, bucket, key)
	}
	if readErr != nil {
		return fmt.Errorf("re-read for fanout: %w", readErr)
	}
	defer reader.Close()

	url := fmt.Sprintf("%s/api/internal/cluster/ha/objects/%s", n.Endpoint, escapeHAObjectKey(key))
	req, err := client.CreateAuthenticatedRequest(ctx, "PUT", url, reader, localID, n.NodeToken)
	if err != nil {
		return err
	}
	req.Header.Set("X-MaxIOFS-HA-Replica", "true")
	req.Header.Set(HABucketHeader, bucket)
	if obj.VersionID != "" {
		req.Header.Set(HAObjectVersionHeader, obj.VersionID)
	}
	setHALastModified(req.Header, obj)
	setHAChecksum(req.Header, obj)
	SetHAObjectLock(req.Header, obj)
	SetHAAttributes(req.Header, obj)
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
		return err
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// sendRawReplica attempts the ciphertext transfer of the pinned version to
func sendRawReplica(ctx context.Context, client *ProxyClient, raw object.RawObjectAccessor, n *Node, localID, bucket, key, versionID string) (sent bool, err error) {
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
	// The sidecar's etag is the MD5 of the bytes on disk, which is exactly what
	// is being streamed — the digest costs nothing and the peer can check it.
	var req *http.Request
	var rErr error
	if etag := sidecar["etag"]; etag != "" {
		req, rErr = client.CreateAuthenticatedRequestWithDigest(ctx, "PUT", url, reader, localID, n.NodeToken, "md5:"+etag)
	} else {
		req, rErr = client.CreateAuthenticatedRequest(ctx, "PUT", url, reader, localID, n.NodeToken)
	}
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
	deleted := time.Now()
	targets, needed, ok := h.replicaTargets(ctx, deleted)
	if !ok {
		return nil
	}
	localID, _ := h.mgr.GetLocalNodeID(ctx)
	client := NewProxyClient(h.mgr.GetTLSConfig())
	ch := make(chan fanoutResult, len(targets))

	for _, node := range targets {
		go func(n *Node) {
			ch <- fanoutResult{n.ID, sendHADelete(ctx, client, n, localID, bucket, key, specificVersionID, deleteMarkerVersionID, time.Time{})}
		}(node)
	}

	return h.collectAndCheckQuorum(ctx, ch, len(targets), needed, deleted, "DELETE", bucket, key)
}

// sendHADelete replays a delete on node n: one version when specificVersionID
// is set, otherwise the key, with deleteMarkerVersionID pinning the marker a
// versioned bucket creates and markedAt, when known, its time.
func sendHADelete(ctx context.Context, client *ProxyClient, n *Node, localID, bucket, key, specificVersionID, deleteMarkerVersionID string, markedAt time.Time) error {
	url := fmt.Sprintf("%s/api/internal/cluster/ha/objects/%s", n.Endpoint, escapeHAObjectKey(key))
	req, err := client.CreateAuthenticatedRequest(ctx, "DELETE", url, nil, localID, n.NodeToken)
	if err != nil {
		return err
	}
	req.Header.Set("X-MaxIOFS-HA-Replica", "true")
	req.Header.Set(HABucketHeader, bucket)
	if specificVersionID != "" {
		req.Header.Set(HAObjectVersionHeader, specificVersionID)
	}
	if deleteMarkerVersionID != "" {
		req.Header.Set(HADeleteMarkerVersionHeader, deleteMarkerVersionID)
		if !markedAt.IsZero() && markedAt.Unix() > 0 {
			req.Header.Set(HALastModifiedHeader, strconv.FormatInt(markedAt.Unix(), 10))
		}
	}
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// collectAndCheckQuorum drains all fanout results, marks failed nodes
// unavailable and as having missed the write made at modified, and returns
// ErrClusterDegraded when successes < needed.
func (h *HAObjectManager) collectAndCheckQuorum(ctx context.Context, ch <-chan fanoutResult, total, needed int, modified time.Time, op, bucket, key string) error {
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
		h.mgr.noteMissedWrites(ctx, "", modified, r.nodeID)
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

// HAMetadataOp describes a metadata-only operation to replay on replica nodes.
type HAMetadataOp struct {
	Op        string          `json:"op"`
	Key       string          `json:"key"`
	VersionID string          `json:"version_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// fanoutMetadata delivers a metadata-only change to every other live node
// before the request returns, so a client's successive changes arrive in
// order. A node that is not healthy, that fails, or that still has changes
// waiting gets the change queued behind them instead; the queue is delivered
// in order when the node is caught up.
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
	nodes, err := h.mgr.ListNodes(ctx)
	if err != nil {
		logrus.WithError(err).Error("HA metadata fanout: cannot list nodes; replicas may diverge")
		return
	}
	body, err := json.Marshal(op)
	if err != nil {
		logrus.WithError(err).Warn("HA metadata fanout: failed to marshal op")
		return
	}

	client := NewProxyClient(h.mgr.GetTLSConfig())
	var wg sync.WaitGroup
	for _, n := range nodes {
		if n.ID == localID || n.HealthStatus == HealthStatusDead {
			continue
		}
		if n.HealthStatus != HealthStatusHealthy || h.mgr.hasQueuedMetadataOps(ctx, n.ID) {
			h.mgr.queueMetadataOp(ctx, n.ID, bucket, body)
			continue
		}
		wg.Add(1)
		go func(n *Node) {
			defer wg.Done()
			err := sendMetadataOp(ctx, client, n, localID, bucket, body)
			switch {
			case err == nil:
			case errors.Is(err, errMetadataOpRefused):
				logrus.WithError(err).WithFields(logrus.Fields{
					"node_id": n.ID, "op": op.Op, "bucket": bucket, "key": op.Key,
				}).Error("HA metadata fanout: a replica refused the change")
			default:
				logrus.WithError(err).WithFields(logrus.Fields{
					"node_id": n.ID, "op": op.Op, "bucket": bucket, "key": op.Key,
				}).Warn("HA metadata fanout: queued for a replica that did not take it")
				h.mgr.queueMetadataOp(ctx, n.ID, bucket, body)
			}
		}(n)
	}
	wg.Wait()
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
