package cluster

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"maps"
	"math"
	"slices"

	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

const (
	rebalanceEnabledKey = "ha.rebalance_enabled"
	// balanceThreshold is how far a node's fill may be from its class's.
	balanceThreshold = 0.10
)

type nodeFill struct {
	id    string
	held  int64
	free  int64
	takes bool
}

// balance is what each node above its class's fill gives away, in bytes and
// as a share of what it holds, and what each node below it takes.
type balance struct {
	excess  map[string]float64
	share   map[string]float64
	deficit map[string]float64
}

type move struct {
	from string
	to   *Node
}

func newBalance() *balance {
	return &balance{excess: map[string]float64{}, share: map[string]float64{}, deficit: map[string]float64{}}
}

// balanceOf is nil while every node of a class is within the threshold of the
// class's fill; past it, every node is brought to it.
func balanceOf(nodes []nodeFill) *balance {
	var held, room float64
	for _, n := range nodes {
		held += float64(n.held)
		room += float64(n.held + n.free)
	}
	if held <= 0 || room <= 0 {
		return nil
	}
	fill := held / room
	if !slices.ContainsFunc(nodes, func(n nodeFill) bool {
		room := float64(n.held + n.free)
		return math.Abs(float64(n.held)-fill*room) > balanceThreshold*room
	}) {
		return nil
	}
	b := newBalance()
	var excess, deficit float64
	for _, n := range nodes {
		switch d := float64(n.held) - fill*float64(n.held+n.free); {
		case d > 0:
			b.excess[n.id] = d
			excess += d
		case d < 0 && n.takes:
			b.deficit[n.id] = -d
			deficit -= d
		}
	}
	if deficit == 0 {
		return nil
	}
	scale := min(1, deficit/excess)
	for _, n := range nodes {
		if d, ok := b.excess[n.id]; ok {
			b.excess[n.id] = d * scale
			b.share[n.id] = d * scale / float64(n.held)
		}
	}
	return b
}

func writeID(bucket, key, versionID string) string {
	return bucket + "\x00" + key + "\x00" + versionID
}

// draw is a number in [0, 1), the same on every node for write w and salt.
func draw(w, salt string) float64 {
	sum := sha256.Sum256([]byte(w + "\x00" + salt))
	return float64(binary.BigEndian.Uint64(sum[:8])>>11) / (1 << 53)
}

// moves are the holders of write w that give it away, as many as their shares
// add up to, each with the node that takes its copy or shard.
func (b *balance) moves(w string, holders []string, bytes int64, receivers []*Node) []move {
	if bytes <= 0 {
		return nil
	}
	var givers []string
	var total float64
	for _, id := range holders {
		if b.share[id] > 0 && float64(bytes) <= b.excess[id] {
			givers = append(givers, id)
			total += b.share[id]
		}
	}
	count := int(total)
	if draw(w, "count") < total-float64(count) {
		count++
	}
	if count == 0 {
		return nil
	}
	// Exponential keys draw the givers by share, without replacement.
	key := func(id string) float64 { return -math.Log(1-draw(w, "from:"+id)) / b.share[id] }
	slices.SortFunc(givers, func(x, y string) int { return cmp.Compare(key(x), key(y)) })
	taken := slices.Clone(holders)
	var moves []move
	for _, id := range givers[:min(count, len(givers))] {
		to := b.receiver(w, bytes, receivers, taken)
		if to == nil {
			break
		}
		taken = append(taken, to.ID)
		moves = append(moves, move{from: id, to: to})
	}
	return moves
}

// receiver is drawn among nodes not in taken by what each lacks.
func (b *balance) receiver(w string, bytes int64, nodes []*Node, taken []string) *Node {
	var best *Node
	var first float64
	for _, n := range nodes {
		lack := b.deficit[n.ID]
		if lack < float64(bytes) || slices.Contains(taken, n.ID) {
			continue
		}
		if k := -math.Log(1-draw(w, "to:"+n.ID)) / lack; best == nil || k < first {
			best, first = n, k
		}
	}
	return best
}

func (m *Manager) rebalanceEnabled(ctx context.Context) bool {
	v, err := GetGlobalConfig(ctx, m.db, rebalanceEnabledKey)
	return err != nil || v == "" || v == "true" || v == "1"
}

// classesToBalance are the nodes of each class that can be spread again. A
// class is left as it is while one of its nodes is out of service, which may
// be back soon, drained, or of unknown room.
func classesToBalance(view *classView, all []*Node) map[string][]*Node {
	classes := make(map[string][]*Node)
	for _, n := range all {
		if n.HealthStatus != HealthStatusDead {
			classes[view.node(n)] = append(classes[view.node(n)], n)
		}
	}
	for class, nodes := range classes {
		if len(nodes) < 2 || slices.ContainsFunc(nodes, func(n *Node) bool {
			return n.Drained || !n.InService() || freeSpace(n) < 0
		}) {
			delete(classes, class)
		}
	}
	return classes
}

func (w *HASyncWorker) planBalance(ctx context.Context, r *placementRepair) *balance {
	if !w.mgr.rebalanceEnabled(ctx) {
		return nil
	}
	classes := classesToBalance(r.view, r.all)
	if len(classes) == 0 {
		return nil
	}
	held, err := w.countHeld(ctx)
	if err != nil {
		logrus.WithError(err).Warn("HASyncWorker: the writes are not spread again: what each node holds was not counted")
		return nil
	}
	plan := newBalance()
	rooms := make(map[string]int64)
	for class, nodes := range classes {
		fills := make([]nodeFill, len(nodes))
		for i, n := range nodes {
			fills[i] = nodeFill{id: n.ID, held: held[n.ID], free: freeSpace(n), takes: n.TakesData()}
			rooms[n.ID] = fills[i].held + fills[i].free
		}
		b := balanceOf(fills)
		if b == nil {
			continue
		}
		var bytes float64
		for id, e := range b.excess {
			bytes += e
			plan.excess[id], plan.share[id] = e, b.share[id]
		}
		maps.Copy(plan.deficit, b.deficit)
		logrus.WithFields(logrus.Fields{"class": class, "bytes": int64(bytes)}).
			Debug("HASyncWorker: the writes of a class are spread again over its nodes")
	}
	w.mgr.noteRooms(rooms)
	if len(plan.share) == 0 {
		return nil
	}
	return plan
}

// countHeld is the bytes each node holds, as the entries name them.
func (w *HASyncWorker) countHeld(ctx context.Context) (map[string]int64, error) {
	held := make(map[string]int64)
	err := w.eachPage(ctx, func(ctx context.Context, bucket string, keys []string) error {
		for _, key := range keys {
			writes, err := keyWrites(ctx, w.objMgr, bucket, key)
			if err != nil {
				return err
			}
			for _, v := range writes {
				if v.IsDeleteMarker {
					continue
				}
				for _, id := range v.Locations {
					held[id] += heldBytes(v)
				}
			}
		}
		return nil
	})
	return held, err
}

// balanceWrite moves what the plan draws of write v when this node is the
// first of its holders and the write needs no repair: its holders all in
// service in its class, its copies the factor's, its shards the layout's.
func (r *placementRepair) balanceWrite(ctx context.Context, bucket, key string, v object.ObjectVersion) error {
	if r.balance == nil || v.IsDeleteMarker || len(v.Locations) == 0 || slices.Min(v.Locations) != r.localID {
		return nil
	}
	class := r.view.of(v.StorageClass)
	for _, id := range v.Locations {
		n, ok := r.nodes[id]
		if !ok || !n.InService() || n.Drained || r.view.node(n) != class {
			return nil
		}
	}
	target := r.target(v)
	layout, sharded := v.Object.Erasure()
	switch {
	case sharded && target.sharded():
		wider, _ := fit(target.layout, len(r.shardNodes(class)))
		if layout.Data == wider.Data && layout.Parity == wider.Parity {
			return r.balanceShards(ctx, bucket, key, v, layout, class)
		}
	case !sharded && !target.sharded() && len(v.Locations) == target.copies:
		return r.balanceCopies(ctx, bucket, key, v, class)
	}
	return nil
}

// balanceCopies makes the drawn copies on their new nodes, then drops them
// from the nodes that give them.
func (r *placementRepair) balanceCopies(ctx context.Context, bucket, key string, v object.ObjectVersion, class string) error {
	moves := r.balance.moves(writeID(bucket, key, v.VersionID), v.Locations, heldBytes(v), r.classCandidates(class, true))
	if len(moves) == 0 {
		return nil
	}
	if held, err := r.writer.HoldsData(ctx, bucket, key, v.VersionID); err != nil || !held {
		return err
	}
	staying := slices.Clone(v.Locations)
	receivers := make([]*Node, len(moves))
	for i, m := range moves {
		staying = slices.DeleteFunc(staying, func(id string) bool { return id == m.from })
		receivers[i] = m.to
	}
	factor := len(v.Locations)
	gen, locations := r.copyTo(ctx, bucket, key, v, receivers, staying, factor, v.LocationsGen)
	for _, m := range moves {
		if len(locations) < factor {
			locations = append(locations, m.from)
		}
	}
	if gen == v.LocationsGen {
		return nil
	}
	if err := r.ha.SetLocations(ctx, bucket, key, locationsChange(v, locations, gen+1)); err != nil {
		return err
	}
	for _, n := range receivers {
		if slices.Contains(locations, n.ID) {
			r.balanced++
			r.balancedBytes += heldBytes(v)
		}
	}
	return nil
}

// balanceShards makes the drawn shards on their new nodes, copied from the
// nodes that give them, then names the new nodes.
func (r *placementRepair) balanceShards(ctx context.Context, bucket, key string, v object.ObjectVersion, layout erasure.Layout, class string) error {
	moves := r.balance.moves(writeID(bucket, key, v.VersionID), v.Locations, heldBytes(v), r.classCandidates(class, true))
	if len(moves) == 0 {
		return nil
	}
	want := make(map[int]*Node, len(moves))
	for _, m := range moves {
		want[slices.Index(v.Locations, m.from)] = m.to
	}
	gen := v.LocationsGen + 1
	locations := slices.Clone(v.Locations)
	var errs error
	for i, err := range r.moveShards(ctx, &v.Object, want, gen) {
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"node_id": want[i].ID, "bucket": bucket, "key": key, "shard": i}).
				Warn("HASyncWorker: a shard was not moved to a node")
			errs = errors.Join(errs, err)
			continue
		}
		locations[i] = want[i].ID
		r.balanced++
		r.balancedBytes += heldBytes(v)
	}
	if slices.Equal(locations, v.Locations) {
		return errs
	}
	change := locationsChange(v, locations, gen)
	change.Layout = layout
	return errors.Join(errs, r.ha.SetLocations(ctx, bucket, key, change))
}
