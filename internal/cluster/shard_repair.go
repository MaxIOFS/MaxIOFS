package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// heldBytes is what a node holding write v keeps of it: the whole data, or
// one shard of it.
func heldBytes(v object.ObjectVersion) int64 {
	if layout, ok := v.Object.Erasure(); ok {
		return layout.ShardSize(v.Size)
	}
	return v.Size
}

// isSharded reports whether the data of write v is cut in shards.
func isSharded(v object.ObjectVersion) bool {
	_, ok := v.Object.Erasure()
	return ok
}

// shardNodes are the nodes a write's shards go to: this node first when it
// takes data, then the others that do, the most free space first.
func (r *placementRepair) shardNodes() []*Node {
	var nodes []*Node
	if n, ok := r.nodes[r.localID]; ok && n.TakesData() {
		nodes = append(nodes, n)
	}
	return append(nodes, r.candidates...)
}

// livingHolders are the nodes write v names that are neither dead nor gone.
func (r *placementRepair) livingHolders(v object.ObjectVersion) []string {
	var living []string
	for _, id := range v.Locations {
		if n, ok := r.nodes[id]; ok && n.HealthStatus != HealthStatusDead {
			living = append(living, id)
		}
	}
	return living
}

// repairShards repairs a write kept in shards, or to be: whole copies are cut
// in shards and shards joined in whole copies as the protection asks, the
// shards of dead, removed or drained nodes are made again on other nodes, and
// shards that a cluster can no longer hold apart, or with less parity than it
// asks, are cut again. The first of the write's living holders acts. It
// reports whether this node still holds the write.
func (r *placementRepair) repairShards(ctx context.Context, bucket, key string, v object.ObjectVersion, target dataTarget) (bool, error) {
	living := r.livingHolders(v)
	if len(living) == 0 || slices.Min(living) != r.localID {
		return true, nil
	}
	layout, sharded := v.Object.Erasure()
	switch {
	case !sharded:
		return r.cutInShards(ctx, bucket, key, v, target.layout)
	case !target.sharded():
		return r.joinShards(ctx, bucket, key, v, target.copies)
	}
	return r.mendShards(ctx, bucket, key, v, layout, target.layout)
}

// fit is layout with fewer data shards when fewer nodes take data; ok is
// false when they are too few for shards.
func fit(layout erasure.Layout, nodes int) (erasure.Layout, bool) {
	if nodes < layout.Width() {
		layout.Data = nodes - layout.Parity
	}
	return layout, layout.Data >= 2
}

// cutInShards cuts a write kept in whole copies, one of them here, in the
// shards of layout. Too few nodes for shards keep the copies.
func (r *placementRepair) cutInShards(ctx context.Context, bucket, key string, v object.ObjectVersion, layout erasure.Layout) (bool, error) {
	if held, err := r.writer.HoldsData(ctx, bucket, key, v.VersionID); err != nil || !held {
		return err != nil, err
	}
	nodes := r.shardNodes()
	layout, ok := fit(layout, len(nodes))
	if !ok {
		return r.repairCopies(ctx, bucket, key, v, layout.Parity+1)
	}
	data, err := r.ha.openHeld(ctx, &v.Object)
	if errors.Is(err, errSuperseded) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	defer data.Close()
	return r.placeShards(ctx, bucket, key, v, data, layout, nodes[:layout.Width()])
}

// placeShards stages the shards data gives of write v on nodes, then tells
// every node they hold it. Short of a shard nothing changes.
func (r *placementRepair) placeShards(ctx context.Context, bucket, key string, v object.ObjectVersion, data io.Reader, layout erasure.Layout, nodes []*Node) (bool, error) {
	gen := v.LocationsGen + 1
	errs := r.ha.stageShards(ctx, r.client, &v.Object, data, layout, nodes, gen, r.localID)
	if err := errors.Join(errs...); err != nil {
		r.ha.dropStaged(ctx, r.client, r.localID, withLayout(&v.Object, layout, nodeIDs(nodes), gen), nodes, errs)
		return true, fmt.Errorf("cut %s/%s in shards: %w", bucket, key, err)
	}
	locations := nodeIDs(nodes)
	change := locationsChange(v, locations, gen)
	change.Layout = layout
	if err := r.ha.SetLocations(ctx, bucket, key, change); err != nil {
		return true, err
	}
	return slices.Contains(locations, r.localID), nil
}

// joinShards keeps a write cut in shards in copies whole copies on the nodes
// that take data, this node first.
func (r *placementRepair) joinShards(ctx context.Context, bucket, key string, v object.ObjectVersion, copies int) (bool, error) {
	gen, locations := v.LocationsGen, []string(nil)
	var errs error
	for _, n := range r.shardNodes() {
		if len(locations) >= copies {
			break
		}
		data, err := r.ha.openShards(ctx, &v.Object)
		if err != nil {
			return true, err
		}
		gen++
		named := append(slices.Clone(locations), n.ID)
		if n.ID == r.localID {
			err = r.ha.storeCopyHere(ctx, &v.Object, data, gen, named)
		} else {
			err = sendCopyOf(ctx, r.client, n, r.localID, bucket, key, &v.Object, data, &newLocations{gen: gen, locations: named})
		}
		data.Close()
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"node_id": n.ID, "bucket": bucket, "key": key}).
				Warn("HASyncWorker: a whole copy of a write in shards was not made on a node")
			errs = errors.Join(errs, err)
			continue
		}
		locations = named
	}
	if len(locations) == 0 {
		return true, errs
	}
	if err := r.ha.SetLocations(ctx, bucket, key, locationsChange(v, locations, gen+1)); err != nil {
		return true, err
	}
	return slices.Contains(locations, r.localID), nil
}

// mendShards makes the shards of dead, removed or drained holders again on
// nodes holding none of the write, or cuts the write again when its shards
// cannot stay apart on the nodes there are or have less parity than target.
func (r *placementRepair) mendShards(ctx context.Context, bucket, key string, v object.ObjectVersion, layout, target erasure.Layout) (bool, error) {
	var replaced []int
	staying := 0
	for i, id := range v.Locations {
		n, ok := r.nodes[id]
		switch {
		case !ok || n.HealthStatus == HealthStatusDead, n.Drained:
			replaced = append(replaced, i)
		default:
			staying++
		}
	}
	var free []*Node
	for _, n := range r.shardNodes() {
		if !slices.Contains(v.Locations, n.ID) {
			free = append(free, n)
		}
	}
	if layout.Parity < target.Parity || staying+len(free) < layout.Width() {
		return r.recutShards(ctx, bucket, key, v, target)
	}
	want := make(map[int]*Node)
	for _, i := range replaced {
		if len(free) == 0 {
			// A drained holder keeps its shard until a node takes it.
			break
		}
		want[i], free = free[0], free[1:]
	}
	if len(want) == 0 {
		return true, nil
	}
	gen := v.LocationsGen + 1
	locations := slices.Clone(v.Locations)
	var errs error
	for i, err := range r.moveShards(ctx, &v.Object, want, gen) {
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"node_id": want[i].ID, "bucket": bucket, "key": key, "shard": i}).
				Warn("HASyncWorker: a shard was not made again on a node")
			errs = errors.Join(errs, err)
			continue
		}
		locations[i] = want[i].ID
	}
	if slices.Equal(locations, v.Locations) {
		return true, errs
	}
	change := locationsChange(v, locations, gen)
	change.Layout = layout
	if err := r.ha.SetLocations(ctx, bucket, key, change); err != nil {
		return true, err
	}
	return slices.Contains(locations, r.localID), errs
}

// recutShards cuts a write in shards again by target, over the nodes that
// take data; too few of them for shards keep it in whole copies.
func (r *placementRepair) recutShards(ctx context.Context, bucket, key string, v object.ObjectVersion, target erasure.Layout) (bool, error) {
	nodes := r.shardNodes()
	layout, ok := fit(target, len(nodes))
	if !ok {
		return r.joinShards(ctx, bucket, key, v, target.Parity+1)
	}
	data, err := r.ha.openShards(ctx, &v.Object)
	if err != nil {
		return true, err
	}
	defer data.Close()
	return r.placeShards(ctx, bucket, key, v, data, layout, assignShards(nodes[:layout.Width()], v.Locations))
}

// assignShards orders nodes so that none is given the index it holds a shard
// of in current: a shard cut again never replaces one in use.
func assignShards(nodes []*Node, current []string) []*Node {
	order := make([]*Node, len(nodes))
	used := make([]bool, len(nodes))
	var place func(pos int) bool
	place = func(pos int) bool {
		if pos == len(order) {
			return true
		}
		for i, n := range nodes {
			if used[i] || (pos < len(current) && current[pos] == n.ID) {
				continue
			}
			used[i], order[pos] = true, n
			if place(pos + 1) {
				return true
			}
			used[i] = false
		}
		return false
	}
	if place(0) {
		return order
	}
	return nodes
}

// moveShards puts each shard of want on its new node for the placement gen:
// copied from a drained holder still in service, made again from the other
// shards otherwise. It returns each shard's error.
func (r *placementRepair) moveShards(ctx context.Context, obj *object.Object, want map[int]*Node, gen int64) map[int]error {
	errs := make(map[int]error, len(want))
	rebuild := make(map[int]*Node)
	for _, i := range slices.Sorted(maps.Keys(want)) {
		if from, ok := r.nodes[obj.Locations[i]]; ok && from.InService() {
			err := r.copyShard(ctx, obj, i, from, want[i], gen)
			if err == nil {
				errs[i] = nil
				continue
			}
			logrus.WithError(err).WithFields(logrus.Fields{"node_id": from.ID, "bucket": obj.Bucket, "key": obj.Key, "shard": i}).
				Debug("HASyncWorker: a drained node's shard is made again from the others")
		}
		rebuild[i] = want[i]
	}
	if len(rebuild) > 0 {
		maps.Copy(errs, r.ha.rebuildShards(ctx, r.client, obj, rebuild, gen, r.localID))
	}
	return errs
}

// copyShard copies shard i of obj from the node holding it to node to, for
// the placement gen.
func (r *placementRepair) copyShard(ctx context.Context, obj *object.Object, i int, from, to *Node, gen int64) error {
	store, ok := ShardStoreOf(r.objects)
	if !ok {
		return fmt.Errorf("this node keeps no shards")
	}
	info := object.ShardOf(obj, i, obj.LocationsGen)
	var src io.ReadCloser
	var err error
	if from.ID == r.localID {
		src, err = store.OpenShard(ctx, info, 0)
	} else {
		src, err = readShardFrom(ctx, r.client, from, r.localID, info, 0)
	}
	if err != nil {
		return err
	}
	defer src.Close()
	moved := slices.Clone(obj.Locations)
	moved[i] = to.ID
	layout, _ := obj.Erasure()
	dst := object.ShardOf(withLayout(obj, layout, moved, gen), i, gen)
	if to.ID == r.localID {
		return store.PutShard(ctx, dst, src)
	}
	return sendShard(ctx, r.client, to, r.localID, dst, src)
}
