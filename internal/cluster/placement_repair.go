package cluster

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// SetLocations records the nodes that hold a write's data, here and on every
// other node. A node it no longer names removes its file.
func (h *HAObjectManager) SetLocations(ctx context.Context, bucket, key string, change object.LocationsChange) error {
	writer, ok := ReplicaWriter(h.Manager)
	if !ok {
		return fmt.Errorf("this node keeps no entries")
	}
	if err := writer.SetLocations(ctx, bucket, key, change); err != nil {
		return err
	}
	if isHAReplica(ctx) {
		return nil
	}
	data, err := json.Marshal(change)
	if err != nil {
		return err
	}
	h.fanoutMetadata(ctx, bucket, HAMetadataOp{Op: "set-locations", Key: key, VersionID: change.VersionID, Data: data})
	return nil
}

// StartRepair runs one repair of the copies unless one is running. It outlives
// the request that asks for it and stops with the worker.
func (w *HASyncWorker) StartRepair(ctx context.Context) {
	if !w.repairing.CompareAndSwap(false, true) {
		return
	}
	ctx = context.WithoutCancel(ctx)
	started := w.Spawn(func() {
		defer w.repairing.Store(false)
		if err := w.RepairPlacement(ctx); err != nil {
			logrus.WithError(err).Warn("HASyncWorker: the copies were not all repaired")
		}
	})
	if !started {
		w.repairing.Store(false)
	}
}

// RepairPlacement makes the copies of the writes this node holds match the
// replication factor. A write that lost holders to dead or removed nodes, or
// to a raised factor, is copied to the healthy nodes with the most free space;
// one with more holders than the factor keeps this node and those with the
// most free space. Each write is repaired by the first of its holders that is
// neither dead nor removed, so that one node acts on it.
func (w *HASyncWorker) RepairPlacement(ctx context.Context) error {
	if !w.mgr.IsClusterEnabled() {
		return nil
	}
	factor, err := w.mgr.GetReplicationFactor(ctx)
	if err != nil || factor <= 1 {
		return err
	}
	ha, ok := w.objMgr.(*HAObjectManager)
	if !ok {
		return nil
	}
	writer, ok := ReplicaWriter(w.objMgr)
	if !ok {
		return nil
	}
	localID, err := w.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return err
	}
	all, err := w.mgr.ListNodes(ctx)
	if err != nil {
		return err
	}
	healthy, err := w.mgr.GetHealthyNodes(ctx)
	if err != nil {
		return err
	}
	r := &placementRepair{
		ha: ha, writer: writer, objects: w.objMgr, factor: factor, localID: localID,
		nodes: make(map[string]*Node, len(all)), client: NewProxyClient(w.mgr.GetTLSConfig()),
	}
	for _, n := range all {
		r.nodes[n.ID] = n
	}
	for _, n := range healthy {
		if n.ID != localID {
			r.candidates = append(r.candidates, n)
		}
	}
	slices.SortStableFunc(r.candidates, func(a, b *Node) int {
		if c := cmp.Compare(freeSpace(b), freeSpace(a)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})

	buckets, err := w.bucketMgr.ListBuckets(ctx, "")
	if err != nil {
		return fmt.Errorf("list buckets: %w", err)
	}
	stopped := w.Stopped()
	var errs error
	for _, b := range buckets {
		bp := bucketPath(b)
		marker := ""
		for {
			entries, next, err := w.keys.ListObjects(ctx, bp, "", marker, syncPageSize)
			if err != nil {
				errs = errors.Join(errs, fmt.Errorf("list %s: %w", bp, err))
				break
			}
			for _, e := range entries {
				select {
				case <-stopped:
					return errs
				default:
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				errs = errors.Join(errs, r.repairKey(ctx, bp, e.Key))
			}
			if next == "" {
				break
			}
			marker = next
		}
	}
	return errs
}

type placementRepair struct {
	ha         *HAObjectManager
	writer     object.ReplicaMetadataWriter
	objects    object.Manager
	client     *ProxyClient
	factor     int
	localID    string
	nodes      map[string]*Node
	candidates []*Node // healthy nodes but this one, most free space first
}

func (r *placementRepair) repairKey(ctx context.Context, bucket, key string) error {
	versions, err := keyVersions(ctx, r.objects, bucket, key)
	if err != nil {
		return err
	}
	if current, err := r.objects.GetObjectMetadata(ctx, bucket, key); err == nil && current.VersionID == "" {
		versions = append(versions, object.ObjectVersion{Object: *current})
	}
	var errs error
	for _, v := range versions {
		if !v.IsDeleteMarker {
			errs = errors.Join(errs, r.repairWrite(ctx, bucket, key, v))
		}
	}
	return errs
}

// repairWrite repairs the copies of one write when this node is the first of
// its living holders and holds its data.
func (r *placementRepair) repairWrite(ctx context.Context, bucket, key string, v object.ObjectVersion) error {
	if len(v.Locations) == 0 || !slices.Contains(v.Locations, r.localID) {
		return nil
	}
	var kept []string
	for _, id := range v.Locations {
		if n, ok := r.nodes[id]; ok && n.HealthStatus != HealthStatusDead {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 || slices.Min(kept) != r.localID {
		return nil
	}
	if len(kept) == len(v.Locations) && len(kept) == r.factor {
		return nil
	}
	if held, err := r.writer.HoldsData(ctx, bucket, key, v.VersionID); err != nil || !held {
		return err
	}

	// Each new copy names its node with a number of its own; every node is
	// then told the holders with a higher one.
	gen, locations := v.LocationsGen, slices.Clone(kept)
	for _, n := range r.candidates {
		if len(locations) >= r.factor {
			break
		}
		if slices.Contains(locations, n.ID) {
			continue
		}
		gen++
		named := append(slices.Clone(locations), n.ID)
		if err := sendNamingCopy(ctx, r.client, r.objects, n, r.localID, bucket, key, v.VersionID, gen, named); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"node_id": n.ID, "bucket": bucket, "key": key}).
				Warn("HASyncWorker: a new copy was not made on a node")
			continue
		}
		locations = named
	}
	if len(locations) > r.factor {
		others := slices.DeleteFunc(slices.Clone(locations), func(id string) bool { return id == r.localID })
		slices.SortStableFunc(others, func(a, b string) int {
			return cmp.Compare(r.free(b), r.free(a))
		})
		locations = append([]string{r.localID}, others[:r.factor-1]...)
	}
	if gen == v.LocationsGen && slices.Equal(locations, v.Locations) {
		return nil
	}
	return r.ha.SetLocations(ctx, bucket, key, object.LocationsChange{
		VersionID:    v.VersionID,
		ETag:         v.ETag,
		LastModified: v.LastModified.Unix(),
		WrittenAt:    v.WrittenAt,
		Locations:    locations,
		Gen:          gen + 1,
	})
}

func (r *placementRepair) free(id string) int64 {
	if n, ok := r.nodes[id]; ok {
		return freeSpace(n)
	}
	return -1
}
