package cluster

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"

	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// HAShardHeader carries, base64 JSON, the shard a request stores, reads or
// removes (object.ShardInfo).
const HAShardHeader = "X-HA-Shard"

// EncodeShardInfo is the value of HAShardHeader for info.
func EncodeShardInfo(info object.ShardInfo) string {
	data, _ := json.Marshal(info)
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeShardInfo reads the shard an HAShardHeader names.
func DecodeShardInfo(value string) (object.ShardInfo, error) {
	var info object.ShardInfo
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return info, fmt.Errorf("invalid shard header: %w", err)
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return info, fmt.Errorf("invalid shard header: %w", err)
	}
	return info, nil
}

// ShardStoreOf is the manager that keeps this node's shards, below the HA
// layer.
func ShardStoreOf(objects object.Manager) (object.ShardStore, bool) {
	if ha, ok := objects.(*HAObjectManager); ok {
		objects = ha.Manager
	}
	store, ok := objects.(object.ShardStore)
	return store, ok
}

func shardURL(n *Node, info object.ShardInfo) string {
	return fmt.Sprintf("%s/api/internal/cluster/ha/shards/%s?bucket=%s", n.Endpoint, escapeHAObjectKey(info.Key), url.QueryEscape(info.Bucket))
}

// sendShard stores on n a shard whose bytes body gives.
func sendShard(ctx context.Context, client *ProxyClient, n *Node, localID string, info object.ShardInfo, body io.Reader) error {
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodPut, shardURL(n, info), body, localID, n.NodeToken)
	if err != nil {
		return err
	}
	req.Header.Set(HABucketHeader, info.Bucket)
	req.Header.Set(HAShardHeader, EncodeShardInfo(info))
	req.ContentLength = info.Layout.ShardSize(info.Size)
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, msg)
	}
	return nil
}

// readShardFrom opens from offset the shard n holds.
func readShardFrom(ctx context.Context, client *ProxyClient, n *Node, localID string, info object.ShardInfo, offset int64) (io.ReadCloser, error) {
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodGet, shardURL(n, info), nil, localID, n.NodeToken)
	if err != nil {
		return nil, err
	}
	req.Header.Set(HAShardHeader, EncodeShardInfo(info))
	want := http.StatusOK
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		want = http.StatusPartialContent
	}
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != want {
		resp.Body.Close()
		return nil, fmt.Errorf("node %s answered %d for shard %d", n.ID, resp.StatusCode, info.Index)
	}
	return resp.Body, nil
}

// deleteShardOn removes from n a shard staged for a placement that did not
// land.
func deleteShardOn(ctx context.Context, client *ProxyClient, n *Node, localID string, info object.ShardInfo) error {
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodDelete, shardURL(n, info), nil, localID, n.NodeToken)
	if err != nil {
		return err
	}
	req.Header.Set(HAShardHeader, EncodeShardInfo(info))
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

// withLayout is obj cut by layout over nodes, at placement gen.
func withLayout(obj *object.Object, layout erasure.Layout, nodes []string, gen int64) *object.Object {
	cut := *obj
	cut.ECData, cut.ECParity, cut.ECBlock = layout.Data, layout.Parity, layout.Block
	cut.Locations, cut.LocationsGen = nodes, gen
	return &cut
}

// nodeIDs are the IDs of nodes.
func nodeIDs(nodes []*Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

// shardSource opens the shards of obj: this node's from its disk, the others'
// from the nodes holding them that admit takes. left reports whether admit
// left out a holder that is not dead.
func (h *HAObjectManager) shardSource(ctx context.Context, client *ProxyClient, obj *object.Object, localID string, admit func(*Node) bool) (source erasure.Source, left bool, err error) {
	store, ok := ShardStoreOf(h.Manager)
	if !ok {
		return nil, false, fmt.Errorf("this node keeps no shards")
	}
	nodes := make([]*Node, len(obj.Locations))
	for i, id := range obj.Locations {
		if id == localID {
			continue
		}
		n, err := h.mgr.GetNode(ctx, id)
		switch {
		case err != nil:
		case admit(n):
			nodes[i] = n
		case n.HealthStatus != HealthStatusDead:
			left = true
		}
	}
	return func(index int, offset int64) (io.ReadCloser, error) {
		if index >= len(obj.Locations) {
			return nil, fmt.Errorf("no node holds shard %d", index)
		}
		info := object.ShardOf(obj, index, obj.LocationsGen)
		if obj.Locations[index] == localID {
			return store.OpenShard(ctx, info, offset)
		}
		if nodes[index] == nil {
			return nil, fmt.Errorf("node %s holding shard %d is not in service", obj.Locations[index], index)
		}
		return readShardFrom(ctx, client, nodes[index], localID, info, offset)
	}, left, nil
}

// notDead takes every holder but a dead one: this node's view of a holder's
// health can be behind it.
func notDead(n *Node) bool { return n.HealthStatus != HealthStatusDead }

// openShards reads obj from its shards, on the holders in service and, when
// they are too few, on every holder not dead. Too few of them answering is
// object.ErrDataUnavailable, before a byte is read.
func (h *HAObjectManager) openShards(ctx context.Context, obj *object.Object) (io.ReadCloser, error) {
	layout, _ := obj.Erasure()
	localID, err := h.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return nil, err
	}
	client := NewProxyClient(h.mgr.GetTLSConfig())
	for _, admit := range []func(*Node) bool{(*Node).InService, notDead} {
		source, left, err := h.shardSource(ctx, client, obj, localID, admit)
		if err != nil {
			return nil, err
		}
		r := erasure.NewReader(layout, obj.Size, source)
		err = r.Ready()
		if err == nil {
			return r, nil
		}
		r.Close()
		if !errors.Is(err, erasure.ErrTooFewShards) {
			return nil, err
		}
		if !left {
			logrus.WithError(err).WithFields(logrus.Fields{"bucket": obj.Bucket, "key": obj.Key}).
				Warn("HA: too few shards of an object answered")
			return nil, object.ErrDataUnavailable
		}
	}
	return nil, object.ErrDataUnavailable
}

// shardSink stores one shard on its node, its bytes written to w.
type shardSink struct {
	w    *io.PipeWriter
	done chan error
}

// sinkTo starts storing on n, this node when it is localID, the shard info
// names; the bytes written to the sink are its bytes.
func (h *HAObjectManager) sinkTo(ctx context.Context, client *ProxyClient, n *Node, localID string, info object.ShardInfo) *shardSink {
	pr, pw := io.Pipe()
	s := &shardSink{w: pw, done: make(chan error, 1)}
	go func() {
		var err error
		if n.ID == localID {
			store, ok := ShardStoreOf(h.Manager)
			if !ok {
				err = fmt.Errorf("this node keeps no shards")
			} else {
				err = store.PutShard(ctx, info, pr)
			}
		} else {
			err = sendShard(ctx, client, n, localID, info, pr)
		}
		if err == nil {
			err = io.EOF
		}
		// A sink that stops early fails the writes made to it.
		pr.CloseWithError(err)
		if err == io.EOF {
			err = nil
		}
		s.done <- err
	}()
	return s
}

// finish ends the bytes written to s, with the error of the side that wrote
// them, and returns why its node did not store the shard, if it did not.
func (s *shardSink) finish(writeErr error) error {
	if writeErr != nil {
		s.w.CloseWithError(writeErr)
	} else {
		s.w.Close()
	}
	return errors.Join(writeErr, <-s.done)
}

// stageShards cuts the size bytes data gives of obj by layout and stores
// shard i on nodes[i] for the placement gen, without changing the entry of
// the write anywhere. It returns each shard's error.
func (h *HAObjectManager) stageShards(ctx context.Context, client *ProxyClient, obj *object.Object, data io.Reader,
	layout erasure.Layout, nodes []*Node, gen int64, localID string) []error {
	cut := withLayout(obj, layout, nodeIDs(nodes), gen)
	sinks := make([]*shardSink, len(nodes))
	dst := make([]io.Writer, len(nodes))
	for i, n := range nodes {
		sinks[i] = h.sinkTo(ctx, client, n, localID, object.ShardOf(cut, i, gen))
		dst[i] = sinks[i].w
	}
	failed, err := erasure.Encode(layout, data, obj.Size, dst)
	errs := make([]error, len(nodes))
	for i, s := range sinks {
		writeErr := err
		if writeErr == nil && failed != nil {
			writeErr = failed[i]
		}
		errs[i] = s.finish(writeErr)
	}
	return errs
}

// rebuildShards makes each shard of want again from the other shards of obj
// and stores it on its node of want for the placement gen. It returns each
// shard's error.
func (h *HAObjectManager) rebuildShards(ctx context.Context, client *ProxyClient, obj *object.Object, want map[int]*Node, gen int64, localID string) map[int]error {
	errs := make(map[int]error, len(want))
	layout, _ := obj.Erasure()
	source, _, err := h.shardSource(ctx, client, obj, localID, (*Node).InService)
	if err != nil {
		for i := range want {
			errs[i] = err
		}
		return errs
	}
	moved := slices.Clone(obj.Locations)
	for i, n := range want {
		moved[i] = n.ID
	}
	cut := withLayout(obj, layout, moved, gen)
	sinks := make(map[int]*shardSink, len(want))
	dst := make(map[int]io.Writer, len(want))
	for i, n := range want {
		sinks[i] = h.sinkTo(ctx, client, n, localID, object.ShardOf(cut, i, gen))
		dst[i] = sinks[i].w
	}
	err = erasure.Rebuild(layout, obj.Size, source, dst)
	for i, s := range sinks {
		errs[i] = s.finish(err)
	}
	return errs
}
