package cluster

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// HALocationsHeader carries, comma-separated, the nodes that hold a copy's
// data on a legacy transfer; a raw transfer carries them in its entry.
const HALocationsHeader = "X-HA-Locations"

// HAETagHeader carries the ETag of the object a node serves another: the
// reader checks it is the data of the entry it holds.
const HAETagHeader = "X-HA-ETag"

// placement is where a write made here goes: the nodes that hold its data,
// this node first, and the other healthy nodes, which hold its entry.
type placement struct {
	localID string
	factor  int
	holders []string
	remote  []*Node
	entries []*Node
}

// place chooses the nodes of a write made here when every node holds every
// bucket (a replication factor above 1): this node and the healthy nodes with
// the most free space hold its data, up to the factor; the other healthy
// nodes hold its entry. A node neither healthy nor dead misses the write and
// is caught up when it is back.
func (h *HAObjectManager) place(ctx context.Context) (*placement, bool) {
	factor, err := h.mgr.GetReplicationFactor(ctx)
	if err != nil || factor <= 1 {
		return nil, false
	}
	localID, err := h.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return nil, false
	}
	healthy, err := h.mgr.GetHealthyNodes(ctx)
	if err != nil {
		return nil, false
	}
	h.mgr.noteMissedWrites(ctx, localID, time.Now())

	var others []*Node
	for _, n := range healthy {
		if n.ID != localID {
			others = append(others, n)
		}
	}
	slices.SortStableFunc(others, func(a, b *Node) int {
		if c := cmp.Compare(freeSpace(b), freeSpace(a)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	p := &placement{localID: localID, factor: factor, holders: []string{localID}}
	for _, n := range others {
		if len(p.holders) < factor {
			p.holders = append(p.holders, n.ID)
			p.remote = append(p.remote, n)
		} else {
			p.entries = append(p.entries, n)
		}
	}
	return p, true
}

// freeSpace is a node's free space as its last health check found it, or -1
// when it reported none.
func freeSpace(n *Node) int64 {
	if n.CapacityTotal <= 0 {
		return -1
	}
	return n.CapacityTotal - n.CapacityUsed
}

// replicate sends a write made here to the nodes of p: its data to the nodes
// that hold it, then, once enough copies hold it, its entry to the others. A
// node that fails is unavailable from then on and is caught up when it is
// back. Too few copies is ErrClusterDegraded.
func (h *HAObjectManager) replicate(ctx context.Context, p *placement, bucket, key, versionID string, modified time.Time) error {
	client := NewProxyClient(h.mgr.GetTLSConfig())
	ch := make(chan fanoutResult, len(p.remote))
	for _, n := range p.remote {
		go func(n *Node) {
			ch <- fanoutResult{n.ID, sendObjectVersion(ctx, client, h.Manager, n, p.localID, bucket, key, versionID)}
		}(n)
	}
	if err := h.collectAndCheckQuorum(ctx, ch, len(p.remote), RequiredReplicaAcks(p.factor), modified, "PUT", bucket, key); err != nil {
		h.noteEntriesMissed(ctx, p, modified)
		return err
	}
	h.sendEntry(ctx, client, p, bucket, key, versionID, modified)
	return nil
}

// noteEntriesMissed records the nodes that hold a write's entry only as having
// missed it.
func (h *HAObjectManager) noteEntriesMissed(ctx context.Context, p *placement, modified time.Time) {
	if len(p.entries) == 0 {
		return
	}
	ids := make([]string, len(p.entries))
	for i, n := range p.entries {
		ids[i] = n.ID
	}
	h.mgr.noteMissedWrites(ctx, "", modified, ids...)
}

// sendEntry sends a write's entry to the nodes that do not hold its data.
func (h *HAObjectManager) sendEntry(ctx context.Context, client *ProxyClient, p *placement, bucket, key, versionID string, modified time.Time) {
	if len(p.entries) == 0 {
		return
	}
	writer, ok := h.Manager.(object.ReplicaMetadataWriter)
	if !ok {
		h.noteEntriesMissed(ctx, p, modified)
		return
	}
	entry, err := writer.ObjectEntry(ctx, bucket, key, versionID)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key}).
			Warn("HA: the entry of a write could not be read to send it")
		h.noteEntriesMissed(ctx, p, modified)
		return
	}
	body, err := json.Marshal(entry)
	if err != nil {
		h.noteEntriesMissed(ctx, p, modified)
		return
	}
	var wg sync.WaitGroup
	for _, n := range p.entries {
		wg.Add(1)
		go func(n *Node) {
			defer wg.Done()
			if err := sendObjectEntry(ctx, client, n, p.localID, bucket, body); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{"node_id": n.ID, "bucket": bucket, "key": key}).
					Warn("HA: a node did not take the entry of a write")
				h.mgr.markNodeUnavailable(ctx, n.ID, "HA entry")
				h.mgr.noteMissedWrites(ctx, "", modified, n.ID)
			}
		}(n)
	}
	wg.Wait()
}

// sendObjectEntry sends n the entry of an object whose data it does not hold.
func sendObjectEntry(ctx context.Context, client *ProxyClient, n *Node, localID, bucket string, body []byte) error {
	target := fmt.Sprintf("%s/api/internal/cluster/ha/object-entry", n.Endpoint)
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodPut, target, bytes.NewReader(body), localID, n.NodeToken)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HABucketHeader, bucket)
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

// GetObject reads an object. One whose data other nodes hold is read from
// them, from the part asked for.
func (h *HAObjectManager) GetObject(ctx context.Context, bucket, key string, versionID ...string) (*object.Object, io.ReadCloser, error) {
	obj, reader, err := h.Manager.GetObject(ctx, bucket, key, versionID...)
	var elsewhere *object.DataElsewhereError
	if !errors.As(err, &elsewhere) || isHAReplica(ctx) {
		return obj, reader, err
	}
	if !h.mgr.IsClusterEnabled() {
		return nil, nil, object.ErrObjectNotFound
	}
	remote, err := h.openRemote(ctx, bucket, key, elsewhere.Object, elsewhere.Locations)
	if err != nil {
		return nil, nil, err
	}
	return elsewhere.Object, remote, nil
}

// openRemote opens the data of obj on the first of the nodes holding it that
// serves it, healthy nodes first. None is object.ErrDataUnavailable.
func (h *HAObjectManager) openRemote(ctx context.Context, bucket, key string, obj *object.Object, locations []string) (io.ReadCloser, error) {
	localID, err := h.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return nil, err
	}
	var holders []*Node
	for _, id := range locations {
		if id == localID {
			continue
		}
		if n, err := h.mgr.GetNode(ctx, id); err == nil && n.HealthStatus != HealthStatusDead {
			holders = append(holders, n)
		}
	}
	slices.SortStableFunc(holders, func(a, b *Node) int {
		return cmp.Compare(boolRank(a.HealthStatus != HealthStatusHealthy), boolRank(b.HealthStatus != HealthStatusHealthy))
	})
	client := NewProxyClient(h.mgr.GetTLSConfig())
	r := &remoteReader{size: obj.Size, open: func(offset int64) (io.ReadCloser, error) {
		for _, n := range holders {
			body, err := readObjectData(ctx, client, n, localID, bucket, key, obj, offset)
			if err == nil {
				return body, nil
			}
			logrus.WithError(err).WithFields(logrus.Fields{"node_id": n.ID, "bucket": bucket, "key": key}).
				Debug("HA: a node holding the data did not serve it")
		}
		return nil, object.ErrDataUnavailable
	}}
	if r.body, err = r.open(0); err != nil {
		return nil, err
	}
	return r, nil
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// readObjectData opens the data of obj on n from offset. The data must be that
// of obj: a node that holds another write of the key does not serve it.
func readObjectData(ctx context.Context, client *ProxyClient, n *Node, localID, bucket, key string, obj *object.Object, offset int64) (io.ReadCloser, error) {
	target := fmt.Sprintf("%s/api/internal/cluster/ha/objects/%s?bucket=%s", n.Endpoint, escapeHAObjectKey(key), url.QueryEscape(bucket))
	if obj.VersionID != "" {
		target += "&versionId=" + url.QueryEscape(obj.VersionID)
	}
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodGet, target, nil, localID, n.NodeToken)
	if err != nil {
		return nil, err
	}
	want := http.StatusOK
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		want = http.StatusPartialContent
	}
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != want || resp.Header.Get(HAETagHeader) != obj.ETag {
		resp.Body.Close()
		return nil, fmt.Errorf("status %d, ETag %q", resp.StatusCode, resp.Header.Get(HAETagHeader))
	}
	return resp.Body, nil
}

// remoteReader reads an object's data from the nodes that hold it. A Seek
// before reading opens the data again from there, so a range is read alone.
type remoteReader struct {
	open   func(offset int64) (io.ReadCloser, error)
	body   io.ReadCloser
	offset int64
	size   int64
}

func (r *remoteReader) Read(p []byte) (int, error) {
	if r.body == nil {
		body, err := r.open(r.offset)
		if err != nil {
			return 0, err
		}
		r.body = body
	}
	n, err := r.body.Read(p)
	r.offset += int64(n)
	return n, err
}

func (r *remoteReader) Seek(offset int64, whence int) (int64, error) {
	var to int64
	switch whence {
	case io.SeekStart:
		to = offset
	case io.SeekCurrent:
		to = r.offset + offset
	case io.SeekEnd:
		to = r.size + offset
	default:
		return r.offset, fmt.Errorf("invalid whence %d", whence)
	}
	if to < 0 {
		return r.offset, fmt.Errorf("negative position %d", to)
	}
	if to != r.offset && r.body != nil {
		r.body.Close()
		r.body = nil
	}
	r.offset = to
	return to, nil
}

func (r *remoteReader) Close() error {
	if r.body == nil {
		return nil
	}
	return r.body.Close()
}
