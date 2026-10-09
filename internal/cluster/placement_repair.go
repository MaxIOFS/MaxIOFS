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
// neither dead nor removed, so that one node acts on it. A write that names
// no node, made before the cluster kept them, is first given the nodes that
// hold it (see adopt).
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
			r.peers = append(r.peers, n)
			if n.TakesData() {
				r.candidates = append(r.candidates, n)
			}
		}
	}
	slices.SortStableFunc(r.candidates, func(a, b *Node) int {
		if c := cmp.Compare(freeSpace(b), freeSpace(a)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return w.eachPage(ctx, r.repairPage)
}

// eachPage calls fn with each page of keys of every bucket this node holds,
// until the worker stops. A bucket that cannot be listed is passed over; the
// errors are joined.
func (w *HASyncWorker) eachPage(ctx context.Context, fn func(ctx context.Context, bucket string, keys []string) error) error {
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
			select {
			case <-stopped:
				return errs
			default:
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			entries, next, err := w.keys.ListObjects(ctx, bp, "", marker, syncPageSize)
			if err != nil {
				errs = errors.Join(errs, fmt.Errorf("list %s: %w", bp, err))
				break
			}
			keys := make([]string, len(entries))
			for i, e := range entries {
				keys[i] = e.Key
			}
			errs = errors.Join(errs, fn(ctx, bp, keys))
			if next == "" {
				break
			}
			marker = next
		}
	}
	return errs
}

// keyWrites are the versions and delete markers of key and, when it has no
// version ID, its current object.
func keyWrites(ctx context.Context, objects object.Manager, bucket, key string) ([]object.ObjectVersion, error) {
	versions, err := keyVersions(ctx, objects, bucket, key)
	if err != nil {
		return nil, err
	}
	if current, err := objects.GetObjectMetadata(ctx, bucket, key); err == nil && current.VersionID == "" {
		versions = append(versions, object.ObjectVersion{Object: *current})
	}
	return versions, nil
}

// unplaced reports whether v is a write whose entry names no node.
func unplaced(v object.ObjectVersion) bool {
	return !v.IsDeleteMarker && len(v.Locations) == 0
}

// locationsChange sets the locations of write v, numbered gen.
func locationsChange(v object.ObjectVersion, locations []string, gen int64) object.LocationsChange {
	return object.LocationsChange{
		VersionID:    v.VersionID,
		ETag:         v.ETag,
		LastModified: v.LastModified.Unix(),
		WrittenAt:    v.WrittenAt,
		Locations:    locations,
		Gen:          gen,
	}
}

type placementRepair struct {
	ha         *HAObjectManager
	writer     object.ReplicaMetadataWriter
	objects    object.Manager
	client     *ProxyClient
	factor     int
	localID    string
	nodes      map[string]*Node
	peers      []*Node // the other nodes in service
	candidates []*Node // the other nodes that take data, most free space first
}

// repairPage repairs the copies of the writes of keys, once those that name no
// node are given their holders.
func (r *placementRepair) repairPage(ctx context.Context, bucket string, keys []string) error {
	var errs error
	writes := make(map[string][]object.ObjectVersion, len(keys))
	var unplacedKeys []string
	for _, key := range keys {
		ws, err := keyWrites(ctx, r.objects, bucket, key)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		writes[key] = ws
		if slices.ContainsFunc(ws, unplaced) {
			unplacedKeys = append(unplacedKeys, key)
		}
	}
	if len(unplacedKeys) > 0 {
		errs = errors.Join(errs, r.adopt(ctx, bucket, unplacedKeys, writes))
	}
	for _, key := range keys {
		for _, v := range writes[key] {
			if !v.IsDeleteMarker {
				errs = errors.Join(errs, r.repairWrite(ctx, bucket, key, v))
			}
		}
	}
	return errs
}

// adopt gives the writes of keys that name no node, and whose data this node
// holds, the nodes that hold them, as every other healthy node tells; without
// the answer of each, the writes wait for the next repair. Each is adopted by
// the first of its holders (see adoptWrite).
func (r *placementRepair) adopt(ctx context.Context, bucket string, keys []string, writes map[string][]object.ObjectVersion) error {
	held := make(map[string]map[string]ChecksumEntry, len(r.peers))
	for _, n := range r.peers {
		entries, _, err := fetchPeerChecksums(ctx, r.client, n, r.localID, bucket, keys, true)
		if err != nil {
			return fmt.Errorf("ask node %s which writes it holds: %w", n.ID, err)
		}
		byKey := make(map[string]ChecksumEntry, len(entries))
		for _, e := range entries {
			byKey[e.Key] = e
		}
		held[n.ID] = byKey
	}
	var errs error
	for _, key := range keys {
		for _, v := range writes[key] {
			if !unplaced(v) {
				continue
			}
			ok, err := r.writer.HoldsData(ctx, bucket, key, v.VersionID)
			if err != nil || !ok {
				errs = errors.Join(errs, err)
				continue
			}
			holders := []string{r.localID}
			for _, n := range r.peers {
				if holdsWrite(held[n.ID][key], v) {
					holders = append(holders, n.ID)
				}
			}
			errs = errors.Join(errs, r.adoptWrite(ctx, bucket, key, v, holders))
		}
	}
	return errs
}

// holdsWrite reports whether a node's entry of a key says it holds the data of
// write v.
func holdsWrite(e ChecksumEntry, v object.ObjectVersion) bool {
	if v.VersionID != "" {
		return slices.Contains(e.Held, v.VersionID)
	}
	return e.Found && e.HoldsCurrent && e.ETag == v.ETag && e.LastModified == v.LastModified.Unix() && e.WrittenAt == v.WrittenAt
}

// adoptWrite gives write v, which names no node, its holders as locations when
// this node is the first of them, trimmed to the factor; the copies a write
// with fewer holders lacks are then made.
func (r *placementRepair) adoptWrite(ctx context.Context, bucket, key string, v object.ObjectVersion, holders []string) error {
	if slices.Min(holders) != r.localID {
		return nil
	}
	locations := r.keep(holders)
	if err := r.ha.SetLocations(ctx, bucket, key, locationsChange(v, locations, 1)); err != nil {
		return err
	}
	v.Locations, v.LocationsGen = locations, 1
	return r.repairWrite(ctx, bucket, key, v)
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
	locations = r.keep(locations)
	if gen == v.LocationsGen && slices.Equal(locations, v.Locations) {
		return nil
	}
	return r.ha.SetLocations(ctx, bucket, key, locationsChange(v, locations, gen+1))
}

// keep is locations trimmed to the factor: this node and the nodes with the
// most free space.
func (r *placementRepair) keep(locations []string) []string {
	if len(locations) <= r.factor {
		return locations
	}
	others := slices.DeleteFunc(slices.Clone(locations), func(id string) bool { return id == r.localID })
	slices.SortStableFunc(others, func(a, b string) int {
		return cmp.Compare(r.free(b), r.free(a))
	})
	return append([]string{r.localID}, others[:r.factor-1]...)
}

func (r *placementRepair) free(id string) int64 {
	if n, ok := r.nodes[id]; ok {
		return freeSpace(n)
	}
	return -1
}
