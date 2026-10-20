package cluster

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/storageclass"
)

// parityConfigKey turns parity on: the data of a write is cut in data and
// parity shards over the nodes instead of kept in whole copies.
const parityConfigKey = "ha.parity"

// Protection is how the cluster keeps the data of its writes: Factor whole
// copies or, with Parity, the shards of erasure.For the nodes holding data.
type Protection struct {
	Factor int
	Parity bool
}

// GetProtection returns how the cluster keeps the data of its writes.
func (m *Manager) GetProtection(ctx context.Context) (Protection, error) {
	factor, err := m.GetReplicationFactor(ctx)
	if err != nil {
		return Protection{}, err
	}
	val, err := GetGlobalConfig(ctx, m.db, parityConfigKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Protection{}, err
	}
	return Protection{Factor: factor, Parity: val == "true"}, nil
}

// SetParity turns parity on or off; off, the factor keeps whole copies.
func (m *Manager) SetParity(ctx context.Context, on bool) error {
	return SetGlobalConfig(ctx, m.db, parityConfigKey, strconv.FormatBool(on))
}

// ParityLayout is the layout of the shards of a write with parity in
// STANDARD: erasure.For its nodes that hold data. Fewer than three nodes have
// none.
func (m *Manager) ParityLayout(ctx context.Context) (erasure.Layout, bool, error) {
	return m.ClassLayout(ctx, storageclass.Standard)
}

// ClassLayout is the layout of the shards of a write with parity in class:
// erasure.For the nodes of the class that hold data.
func (m *Manager) ClassLayout(ctx context.Context, class string) (erasure.Layout, bool, error) {
	view, err := m.classView(ctx)
	if err != nil {
		return erasure.Layout{}, false, err
	}
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		return erasure.Layout{}, false, err
	}
	layout, ok := view.layout(nodes, view.of(class))
	return layout, ok, nil
}

// dataTarget is how the data of a write is to be kept: copies whole copies,
// or the shards of layout when its Data is above 0.
type dataTarget struct {
	copies int
	layout erasure.Layout
}

func (t dataTarget) sharded() bool { return t.layout.Data > 0 }

// tolerance is the nodes whose loss the data survives.
func (t dataTarget) tolerance() int {
	if t.sharded() {
		return t.layout.Parity
	}
	return t.copies - 1
}

// targetOf is how the data of a write of size bytes is to be kept. With
// parity, an empty write keeps as many whole copies as the shards' parity
// plus one, and a cluster with too few nodes for shards keeps two.
func targetOf(p Protection, layout erasure.Layout, haveLayout bool, size int64) dataTarget {
	switch {
	case !p.Parity:
		return dataTarget{copies: p.Factor}
	case !haveLayout:
		return dataTarget{copies: 2}
	case size <= 0:
		return dataTarget{copies: layout.Parity + 1}
	}
	return dataTarget{layout: layout}
}

// writeTarget is how the data of a write of storage class sc and size bytes
// made now is to be kept.
func (m *Manager) writeTarget(ctx context.Context, sc string, size int64) (dataTarget, error) {
	view, err := m.classView(ctx)
	if err != nil {
		return dataTarget{}, err
	}
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		return dataTarget{}, err
	}
	return view.target(nodes, view.of(sc), size), nil
}

// requiredNodes is the nodes taking data the protection needs: the factor,
// or three for parity.
func (p Protection) requiredNodes() int {
	if p.Parity {
		return 3
	}
	return p.Factor
}

// redundant reports whether the protection keeps more than one copy's worth.
func (p Protection) redundant() bool {
	return p.Parity || p.Factor > 1
}

// quorumFactor is the factor whose quorum the delete of a write of storage
// class sc needs: the factor of whole copies, or the parity of the shards
// plus one.
func (m *Manager) quorumFactor(ctx context.Context, sc string) (int, error) {
	view, err := m.classView(ctx)
	if err != nil {
		return 0, err
	}
	class := view.of(sc)
	p := view.protection[class]
	if !p.Parity {
		return p.Factor, nil
	}
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		return 0, err
	}
	layout, ok := view.layout(nodes, class)
	if !ok {
		return 2, nil
	}
	return layout.Parity + 1, nil
}
