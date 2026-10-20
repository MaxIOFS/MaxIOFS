package cluster

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/storageclass"
)

// storageClassesConfigKey holds, as JSON, the storage classes the cluster
// keeps on nodes of their own.
const storageClassesConfigKey = "ha.storage_classes"

// StorageClass is a storage class the cluster keeps on nodes of its own,
// with a protection of its own. STANDARD is every node in no other class; its
// protection is the cluster's (see GetProtection).
type StorageClass struct {
	Name   string `json:"name"`
	Factor int    `json:"factor"`
	Parity bool   `json:"parity,omitempty"`
}

// Protection is how the class keeps the data of its writes.
func (c StorageClass) Protection() Protection {
	return Protection{Factor: c.Factor, Parity: c.Parity}
}

var (
	// ErrStorageClassUnknown is a class the cluster cannot keep on nodes of
	// its own, or does not.
	ErrStorageClassUnknown = errors.New("the cluster keeps no such storage class")
	// ErrStorageClassBelowProtection is a node taken out of a class that
	// would be left with fewer nodes taking data than its protection needs.
	ErrStorageClassBelowProtection = errors.New("the other nodes of the storage class that take data would be fewer than its protection needs")
)

// StorageClasses are the classes the cluster keeps on nodes of their own,
// STANDARD aside, from the warmest.
func (m *Manager) StorageClasses(ctx context.Context) ([]StorageClass, error) {
	val, err := GetGlobalConfig(ctx, m.db, storageClassesConfigKey)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && val == "") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stored []StorageClass
	if err := json.Unmarshal([]byte(val), &stored); err != nil {
		return nil, fmt.Errorf("read the storage classes: %w", err)
	}
	var classes []StorageClass
	for _, c := range stored {
		if storageclass.IsDefinable(c.Name) && validProtection(c.Protection()) &&
			!slices.ContainsFunc(classes, func(o StorageClass) bool { return o.Name == c.Name }) {
			classes = append(classes, c)
		}
	}
	slices.SortFunc(classes, func(a, b StorageClass) int {
		return cmp.Compare(slices.Index(storageclass.Definable, a.Name), slices.Index(storageclass.Definable, b.Name))
	})
	return classes, nil
}

func validProtection(p Protection) bool {
	return p.Parity || (p.Factor >= 1 && p.Factor <= 3)
}

func (m *Manager) saveStorageClasses(ctx context.Context, classes []StorageClass) error {
	if classes == nil {
		classes = []StorageClass{}
	}
	data, err := json.Marshal(classes)
	if err != nil {
		return err
	}
	return SetGlobalConfig(ctx, m.db, storageClassesConfigKey, string(data))
}

// SetStorageClass keeps a class on nodes of its own, or changes its
// protection. A class may have fewer nodes than its protection needs: its
// writes are then kept on other nodes as well (see place).
func (m *Manager) SetStorageClass(ctx context.Context, class StorageClass) error {
	if !storageclass.IsDefinable(class.Name) {
		return ErrStorageClassUnknown
	}
	if class.Parity {
		class.Factor = 1
	}
	if !validProtection(class.Protection()) {
		return fmt.Errorf("invalid replication factor %d: must be 1, 2, or 3", class.Factor)
	}
	classes, err := m.StorageClasses(ctx)
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(classes, func(c StorageClass) bool { return c.Name == class.Name }); i >= 0 {
		classes[i] = class
	} else {
		classes = append(classes, class)
	}
	return m.saveStorageClasses(ctx, classes)
}

// DeleteStorageClass stops keeping a class on nodes of its own: its nodes go
// back to STANDARD, and its objects are kept there.
func (m *Manager) DeleteStorageClass(ctx context.Context, name string) error {
	classes, err := m.StorageClasses(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(classes, func(c StorageClass) bool { return c.Name == name })
	if i < 0 {
		return ErrStorageClassUnknown
	}
	if err := m.saveStorageClasses(ctx, slices.Delete(classes, i, i+1)); err != nil {
		return err
	}
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.StorageClass == name {
			if err := m.setNodeStorageClass(ctx, n.ID, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetNodeStorageClass puts a node in a class the cluster keeps, STANDARD
// included. A node does not leave a class whose other nodes taking data
// would be fewer than its protection needs; to empty a class, it is deleted.
func (m *Manager) SetNodeStorageClass(ctx context.Context, nodeID, class string) error {
	view, err := m.classView(ctx)
	if err != nil {
		return err
	}
	if _, ok := view.protection[class]; !ok {
		return ErrStorageClassUnknown
	}
	node, err := m.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	from := view.node(node)
	if from == class {
		return nil
	}
	if node.HealthStatus != HealthStatusDead && !node.Drained {
		nodes, err := m.ListNodes(ctx)
		if err != nil {
			return err
		}
		takers := 0
		for _, n := range nodes {
			if n.ID != nodeID && n.TakesData() && view.node(n) == from {
				takers++
			}
		}
		if takers < view.protection[from].requiredNodes() {
			return ErrStorageClassBelowProtection
		}
	}
	stored := class
	if class == storageclass.Standard {
		stored = ""
	}
	return m.setNodeStorageClass(ctx, nodeID, stored)
}

// setNodeStorageClass records a node's class on every node: it travels with
// the node's other fields.
func (m *Manager) setNodeStorageClass(ctx context.Context, nodeID, class string) error {
	var changed int64
	if err := m.db.QueryRowContext(ctx, `SELECT changed_at FROM cluster_nodes WHERE id = ?`, nodeID).Scan(&changed); err != nil {
		return fmt.Errorf("read node %s: %w", nodeID, err)
	}
	_, err := m.db.ExecContext(ctx, `UPDATE cluster_nodes SET storage_class = ?, changed_at = ? WHERE id = ?`,
		class, nodeChangeAfter(changed), nodeID)
	if err == nil {
		m.log.WithFields(map[string]any{"node_id": nodeID, "storage_class": class}).Info("Node storage class changed")
	}
	return err
}

// classView is the storage classes as one decision sees them: the
// protection of each, STANDARD's among them.
type classView struct {
	protection map[string]Protection
}

func (m *Manager) classView(ctx context.Context) (*classView, error) {
	standard, err := m.GetProtection(ctx)
	if err != nil {
		return nil, err
	}
	classes, err := m.StorageClasses(ctx)
	if err != nil {
		return nil, err
	}
	v := &classView{protection: map[string]Protection{storageclass.Standard: standard}}
	for _, c := range classes {
		v.protection[c.Name] = c.Protection()
	}
	return v, nil
}

// defined reports whether the cluster keeps a class beyond STANDARD.
func (v *classView) defined() bool {
	return len(v.protection) > 1
}

// of is the class whose nodes keep an object of storage class sc: sc when the
// cluster keeps it, STANDARD otherwise.
func (v *classView) of(sc string) string {
	if _, ok := v.protection[sc]; ok {
		return sc
	}
	return storageclass.Standard
}

// node is the class node n is in.
func (v *classView) node(n *Node) string {
	return v.of(n.StorageClass)
}

// names are the classes, STANDARD first, then from the warmest.
func (v *classView) names() []string {
	names := []string{storageclass.Standard}
	for _, name := range storageclass.Definable {
		if _, ok := v.protection[name]; ok {
			names = append(names, name)
		}
	}
	return names
}

// layout is the layout of a write with parity in class: erasure.For the
// nodes of the class that hold data, neither dead nor drained.
func (v *classView) layout(nodes []*Node, class string) (erasure.Layout, bool) {
	return erasure.For(v.dataNodes(nodes, class))
}

func (v *classView) dataNodes(nodes []*Node, class string) int {
	count := 0
	for _, n := range nodes {
		if n.HealthStatus != HealthStatusDead && !n.Drained && v.node(n) == class {
			count++
		}
	}
	return count
}

// takers counts the nodes of class that take data.
func (v *classView) takers(nodes []*Node, class string) int {
	count := 0
	for _, n := range nodes {
		if n.TakesData() && v.node(n) == class {
			count++
		}
	}
	return count
}

// target is how the data of a write of class and size bytes is to be kept.
func (v *classView) target(nodes []*Node, class string, size int64) dataTarget {
	p := v.protection[class]
	if !p.Parity {
		return targetOf(p, erasure.Layout{}, false, size)
	}
	layout, ok := v.layout(nodes, class)
	return targetOf(p, layout, ok, size)
}

// requiredNodes is the nodes taking data the most demanding class needs: a
// node holds the copies of any class when its own has too few nodes.
func (v *classView) requiredNodes() int {
	required := 0
	for _, p := range v.protection {
		required = max(required, p.requiredNodes())
	}
	return required
}

// redundant reports whether every class keeps more than one copy's worth.
func (v *classView) redundant() bool {
	for _, p := range v.protection {
		if !p.redundant() {
			return false
		}
	}
	return true
}

// sortForClass orders nodes for a write of class: those in the class first,
// each keeping the order it had.
func (v *classView) sortForClass(nodes []*Node, class string) {
	slices.SortStableFunc(nodes, func(a, b *Node) int {
		return cmp.Compare(boolRank(v.node(a) != class), boolRank(v.node(b) != class))
	})
}

// placementShape is what the placement of the writes follows: the
// protection and layout of each class and, with classes beyond STANDARD, the
// nodes in each. It is empty when there is nothing to follow: no class but
// STANDARD, kept in whole copies. settled is false while a class with parity
// has a node that holds data and does not take it yet.
func (v *classView) placementShape(nodes []*Node) (shape string, settled bool) {
	settled = true
	parity := false
	var parts []string
	for _, class := range v.names() {
		p := v.protection[class]
		part := fmt.Sprintf("%s:%d:%t", class, p.Factor, p.Parity)
		if p.Parity {
			parity = true
			layout, ok := v.layout(nodes, class)
			if ok {
				part += fmt.Sprintf(":%d+%d", layout.Data, layout.Parity)
				if v.takers(nodes, class) < layout.Width() {
					settled = false
				}
			}
		}
		if v.defined() {
			var ids []string
			for _, n := range nodes {
				if n.HealthStatus != HealthStatusDead && !n.Drained && v.node(n) == class {
					ids = append(ids, n.ID)
				}
			}
			slices.Sort(ids)
			part += ":" + strings.Join(ids, ",")
		}
		parts = append(parts, part)
	}
	if !parity && !v.defined() {
		return "", true
	}
	return strings.Join(parts, ";"), settled
}

// StorageClassState is how a class keeps its data now: its protection, the
// layout of a write with parity, its nodes and the room it has.
type StorageClassState struct {
	Name   string `json:"name"`
	Factor int    `json:"factor"`
	Parity bool   `json:"parity"`
	// DataShards and ParityShards are the layout of a write with parity;
	// 0 when the class has too few nodes for shards.
	DataShards   int `json:"data_shards"`
	ParityShards int `json:"parity_shards"`
	// Nodes are the nodes of the class that hold data, neither dead nor
	// drained; Takers those that take data now.
	Nodes         int `json:"node_count"`
	Takers        int `json:"takers"`
	RequiredNodes int `json:"required_nodes"`
	// TotalBytes is the disk of its nodes, UsableBytes the data it has room
	// for as it keeps it.
	TotalBytes        int64 `json:"total_bytes"`
	UsableBytes       int64 `json:"usable_bytes"`
	ToleratedFailures int   `json:"tolerated_failures"`
	// BelowProtection is set when fewer nodes of the class take data than
	// it spreads a write over: its writes are kept on nodes of other classes
	// as well.
	BelowProtection bool `json:"below_protection"`
}

// StorageClassStatus is how each class keeps its data now, STANDARD first.
func (m *Manager) StorageClassStatus(ctx context.Context) ([]StorageClassState, error) {
	view, err := m.classView(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]StorageClassState, 0, len(view.protection))
	for _, class := range view.names() {
		p := view.protection[class]
		st := StorageClassState{Name: class, Factor: p.Factor, Parity: p.Parity, RequiredNodes: p.requiredNodes()}
		var room []int64
		for _, n := range nodes {
			if n.HealthStatus != HealthStatusDead && !n.Drained && view.node(n) == class {
				st.Nodes++
				st.TotalBytes += n.CapacityTotal
				room = append(room, n.CapacityTotal)
			}
		}
		st.Takers = view.takers(nodes, class)
		target := view.target(nodes, class, 1)
		needed := st.RequiredNodes
		if target.sharded() {
			st.DataShards, st.ParityShards = target.layout.Data, target.layout.Parity
			needed = max(needed, target.layout.Width())
		}
		if len(room) > 0 {
			st.UsableBytes = target.space(room)
		}
		st.ToleratedFailures = target.tolerance()
		st.BelowProtection = st.Takers < needed
		states = append(states, st)
	}
	return states, nil
}

// PlacementSettings is how every class keeps its data, as text: it changes
// with the protection of a class and with the classes kept.
func (m *Manager) PlacementSettings(ctx context.Context) string {
	view, err := m.classView(ctx)
	if err != nil {
		return ""
	}
	parts := make([]string, 0, len(view.protection))
	for _, class := range view.names() {
		p := view.protection[class]
		parts = append(parts, fmt.Sprintf("%s:%d:%t", class, p.Factor, p.Parity))
	}
	return strings.Join(parts, ";")
}

// NodeStorageClass is the class node n keeps the data of: its own when the
// cluster keeps it, STANDARD otherwise.
func (m *Manager) NodeStorageClass(ctx context.Context, n *Node) string {
	view, err := m.classView(ctx)
	if err != nil {
		return storageclass.Standard
	}
	return view.node(n)
}
