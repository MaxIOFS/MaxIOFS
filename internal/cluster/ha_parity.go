package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// errSuperseded: the write being spread was replaced here by a later one.
var errSuperseded = errors.New("the write was replaced by a later one")

// spread keeps the data of a write made here with parity, held here alone so
// far: cut in shards over the nodes that take data, with fewer data shards
// when fewer nodes do, or in whole copies short of nodes for shards. A write
// is never kept on one node: too few copies is ErrClusterDegraded. The other
// nodes are then sent its entry.
func (h *HAObjectManager) spread(ctx context.Context, p *placement, obj *object.Object) error {
	target, err := h.mgr.writeTarget(ctx, p.class, obj.Size)
	if err != nil {
		return err
	}
	client := NewProxyClient(h.mgr.GetTLSConfig())
	copies := target.copies
	if target.sharded() {
		layout := target.layout
		if len(p.takers) < layout.Width() {
			layout.Data = len(p.takers) - layout.Parity
		}
		if layout.Data >= 2 {
			nodes := p.takers[:layout.Width()]
			errs, err := h.stageWrite(ctx, client, p, obj, layout, nodes)
			if errors.Is(err, errSuperseded) {
				return nil
			}
			if err != nil {
				return err
			}
			stored := 0
			var missed []string
			for i, e := range errs {
				if e == nil {
					stored++
					continue
				}
				logrus.WithError(e).WithFields(logrus.Fields{"node_id": nodes[i].ID, "bucket": obj.Bucket, "key": obj.Key, "shard": i}).
					Warn("HA: a node did not take the shard of a write")
				if nodes[i].ID != p.localID {
					h.mgr.markNodeUnavailable(ctx, nodes[i].ID, "HA shard")
					missed = append(missed, nodes[i].ID)
				}
			}
			// One shard more than the data shards keeps the write when a node
			// is lost; the shards missing are made again (see the scrubber).
			if stored > layout.Data {
				h.mgr.noteMissedWrites(ctx, "", obj.LastModified, missed...)
				return h.placeWrite(ctx, client, p, obj, nodeIDs(nodes), layout)
			}
			defer h.dropStaged(ctx, client, p.localID, withLayout(obj, layout, nodeIDs(nodes), 1), nodes, errs)
		}
		copies = target.layout.Parity + 1
	}
	return h.spreadCopies(ctx, client, p, obj, copies)
}

// stageWrite stages the shards of the write made here, read from its copy
// here, on nodes.
func (h *HAObjectManager) stageWrite(ctx context.Context, client *ProxyClient, p *placement, obj *object.Object, layout erasure.Layout, nodes []*Node) ([]error, error) {
	data, err := h.openHeld(ctx, obj)
	if err != nil {
		return nil, err
	}
	defer data.Close()
	return h.stageShards(ctx, client, obj, data, layout, nodes, 1, p.localID), nil
}

// openHeld opens the whole copy this node holds of the write obj is;
// errSuperseded when the write here is a later one.
func (h *HAObjectManager) openHeld(ctx context.Context, obj *object.Object) (io.ReadCloser, error) {
	var held *object.Object
	var data io.ReadCloser
	var err error
	if obj.VersionID != "" {
		held, data, err = h.Manager.GetObject(ctx, obj.Bucket, obj.Key, obj.VersionID)
	} else {
		held, data, err = h.Manager.GetObject(ctx, obj.Bucket, obj.Key)
	}
	if err != nil {
		if obj.VersionID == "" && (errors.Is(err, object.ErrObjectNotFound) || errors.As(err, new(*object.DataElsewhereError))) {
			return nil, errSuperseded
		}
		return nil, fmt.Errorf("re-read the write: %w", err)
	}
	if held.ETag != obj.ETag || held.LastModified.Unix() != obj.LastModified.Unix() || held.WrittenAt != obj.WrittenAt {
		data.Close()
		return nil, errSuperseded
	}
	return data, nil
}

// storeCopyHere stores a whole copy of obj, its data read from data, as a
// copy another node sends naming locations at the placement gen.
func (h *HAObjectManager) storeCopyHere(ctx context.Context, obj *object.Object, data io.Reader, gen int64, locations []string) error {
	named := *obj
	named.Locations, named.LocationsGen = locations, gen
	named.ECData, named.ECParity, named.ECBlock = 0, 0, 0
	headers := wholeCopyHeaders(obj.Bucket, &named)
	_, err := h.Manager.PutObject(ReplicaWriteContext(ctx, headers), obj.Bucket, obj.Key, data, headers)
	return err
}

// spreadCopies keeps the write made here in whole copies: this node's and
// those of the nodes that take data, copies in all.
func (h *HAObjectManager) spreadCopies(ctx context.Context, client *ProxyClient, p *placement, obj *object.Object, copies int) error {
	locations := []string{p.localID}
	for _, n := range p.takers {
		if len(locations) >= copies {
			break
		}
		if n.ID == p.localID {
			continue
		}
		named := append(slices.Clone(locations), n.ID)
		if err := sendNamingCopy(ctx, client, h.Manager, n, p.localID, obj.Bucket, obj.Key, obj.VersionID, 1, named); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"node_id": n.ID, "bucket": obj.Bucket, "key": obj.Key}).
				Warn("HA: a node did not take the copy of a write")
			h.mgr.markNodeUnavailable(ctx, n.ID, "HA copy")
			h.mgr.noteMissedWrites(ctx, "", obj.LastModified, n.ID)
			continue
		}
		locations = named
	}
	if len(locations) < min(copies, 2) {
		logrus.WithFields(logrus.Fields{"bucket": obj.Bucket, "key": obj.Key, "copies": len(locations)}).
			Error("HA: a write with parity reached no other node — failing write")
		return ErrClusterDegraded
	}
	return h.placeWrite(ctx, client, p, obj, locations, erasure.Layout{})
}

// placeWrite records where the data of the write made here is, here and in
// obj, and sends the other nodes its entry.
func (h *HAObjectManager) placeWrite(ctx context.Context, client *ProxyClient, p *placement, obj *object.Object, locations []string, layout erasure.Layout) error {
	writer, ok := ReplicaWriter(h.Manager)
	if !ok {
		return fmt.Errorf("this node keeps no entries")
	}
	err := writer.SetLocations(ctx, obj.Bucket, obj.Key, object.LocationsChange{
		VersionID: obj.VersionID, ETag: obj.ETag, LastModified: obj.LastModified.Unix(), WrittenAt: obj.WrittenAt,
		Locations: locations, Gen: 1, Layout: layout,
	})
	if errors.Is(err, object.ErrObjectNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	*obj = *withLayout(obj, layout, locations, 1)
	h.sendEntry(ctx, client, p, obj.Bucket, obj.Key, obj.VersionID, obj.LastModified)
	return nil
}

// dropStaged removes the shards stored on nodes for a placement of obj that
// did not land.
func (h *HAObjectManager) dropStaged(ctx context.Context, client *ProxyClient, localID string, obj *object.Object, nodes []*Node, errs []error) {
	for i, n := range nodes {
		if errs[i] != nil {
			continue
		}
		info := object.ShardOf(obj, i, obj.LocationsGen)
		var err error
		if n.ID == localID {
			if store, ok := ShardStoreOf(h.Manager); ok {
				err = store.DeleteShard(ctx, info)
			}
		} else {
			err = deleteShardOn(ctx, client, n, localID, info)
		}
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"node_id": n.ID, "bucket": obj.Bucket, "key": obj.Key, "shard": i}).
				Debug("HA: a shard of a placement that did not land was not removed")
		}
	}
}
