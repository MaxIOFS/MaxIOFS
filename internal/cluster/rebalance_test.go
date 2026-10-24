package cluster

import (
	"fmt"
	"testing"

	"github.com/maxiofs/maxiofs/internal/storageclass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAClassWithinTheThresholdMovesNothing(t *testing.T) {
	assert.Nil(t, balanceOf([]nodeFill{
		{id: "a", held: 400, free: 600, takes: true},
		{id: "b", held: 480, free: 520, takes: true},
		{id: "c", held: 320, free: 680, takes: true},
	}))
}

func TestANodeFarFromItsClassIsGivenItsShare(t *testing.T) {
	b := balanceOf([]nodeFill{
		{id: "a", held: 400, free: 600, takes: true},
		{id: "b", held: 400, free: 600, takes: true},
		{id: "c", held: 400, free: 600, takes: true},
		{id: "d", held: 0, free: 1000, takes: true},
	})
	require.NotNil(t, b)
	for _, id := range []string{"a", "b", "c"} {
		assert.InDelta(t, 100, b.excess[id], 1e-9)
		assert.InDelta(t, 0.25, b.share[id], 1e-9)
	}
	assert.InDelta(t, 300, b.deficit["d"], 1e-9)
	assert.Len(t, b.deficit, 1)
}

func TestTheFillOfEachNodeIsItsShareOfItsOwnRoom(t *testing.T) {
	b := balanceOf([]nodeFill{
		{id: "small", held: 500, free: 500, takes: true},
		{id: "large", held: 500, free: 3500, takes: true},
	})
	require.NotNil(t, b)
	assert.InDelta(t, 300, b.excess["small"], 1e-9)
	assert.InDelta(t, 300, b.deficit["large"], 1e-9)
}

func TestANodeThatTakesNoDataGivesAndTakesNothing(t *testing.T) {
	b := balanceOf([]nodeFill{
		{id: "full", held: 800, free: 200, takes: true},
		{id: "empty", held: 100, free: 900, takes: true},
		{id: "pressed", held: 100, free: 900, takes: false},
	})
	require.NotNil(t, b)
	assert.NotContains(t, b.deficit, "pressed")
	assert.InDelta(t, b.deficit["empty"], b.excess["full"], 1e-9, "no more is given than is taken")
}

func TestNothingMovesWithoutANodeToTakeIt(t *testing.T) {
	assert.Nil(t, balanceOf([]nodeFill{
		{id: "full", held: 900, free: 100, takes: true},
		{id: "pressed", held: 0, free: 1000, takes: false},
	}))
}

func TestTheMovesOfManyWritesAddUpToThePlan(t *testing.T) {
	const writes = 20000
	tests := []struct {
		name    string
		fills   []nodeFill
		holders []string
	}{
		{"a fourth node with a factor of 3", []nodeFill{
			{id: "a", held: writes, free: writes, takes: true},
			{id: "b", held: writes, free: writes, takes: true},
			{id: "c", held: writes, free: writes, takes: true},
			{id: "d", held: 0, free: 2 * writes, takes: true},
		}, []string{"a", "b", "c"}},
		{"two nodes of different rooms with a factor of 2", []nodeFill{
			{id: "a", held: writes, free: writes, takes: true},
			{id: "b", held: writes, free: writes, takes: true},
			{id: "c", held: 0, free: writes, takes: true},
			{id: "d", held: 0, free: 3 * writes, takes: true},
		}, []string{"a", "b"}},
		{"three empty nodes with a factor of 3", []nodeFill{
			{id: "a", held: writes, free: writes, takes: true},
			{id: "b", held: writes, free: writes, takes: true},
			{id: "c", held: writes, free: writes, takes: true},
			{id: "d", held: 0, free: 2 * writes, takes: true},
			{id: "e", held: 0, free: 2 * writes, takes: true},
			{id: "f", held: 0, free: 2 * writes, takes: true},
		}, []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := balanceOf(tt.fills)
			require.NotNil(t, b)
			var receivers []*Node
			for _, f := range tt.fills {
				receivers = append(receivers, &Node{ID: f.id})
			}
			gave, took := map[string]float64{}, map[string]float64{}
			for i := range writes {
				w := writeID("bucket", fmt.Sprintf("key-%d", i), "")
				moves := b.moves(w, tt.holders, 1, receivers)
				seen := map[string]bool{}
				for _, m := range moves {
					assert.Contains(t, tt.holders, m.from)
					assert.NotContains(t, tt.holders, m.to.ID)
					assert.False(t, seen[m.to.ID], "one copy per node")
					seen[m.to.ID] = true
					gave[m.from]++
					took[m.to.ID]++
				}
				assert.Equal(t, moves, b.moves(w, tt.holders, 1, receivers), "the same draw every time")
			}
			for id, excess := range b.excess {
				assert.InEpsilon(t, excess, gave[id], 0.05, "given by %s", id)
			}
			for id, deficit := range b.deficit {
				assert.InEpsilon(t, deficit, took[id], 0.12, "taken by %s", id)
			}
		})
	}
}

func TestAClassIsSpreadOnlyWithEveryNodeInServiceAndItsRoomKnown(t *testing.T) {
	view := &classView{protection: map[string]Protection{
		storageclass.Standard: {Factor: 2},
		"GLACIER":             {Factor: 2},
	}}
	node := func(id, class, status string) *Node {
		return &Node{ID: id, StorageClass: class, HealthStatus: status, CapacityTotal: 100}
	}
	healthy := []*Node{
		node("a", "", HealthStatusHealthy), node("b", "", HealthStatusStoragePressure),
		node("c", "GLACIER", HealthStatusHealthy), node("d", "GLACIER", HealthStatusDegraded),
		node("gone", "", HealthStatusDead),
	}
	classes := classesToBalance(view, healthy)
	assert.Len(t, classes[storageclass.Standard], 2, "a dead node is not counted")
	assert.Len(t, classes["GLACIER"], 2)

	tests := []struct {
		name   string
		change func(n *Node)
	}{
		{"a node out of service", func(n *Node) { n.HealthStatus = HealthStatusUnavailable }},
		{"a drained node", func(n *Node) { n.Drained = true }},
		{"a node of unknown room", func(n *Node) { n.CapacityTotal = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := make([]*Node, len(healthy))
			for i, n := range healthy {
				copied := *n
				nodes[i] = &copied
			}
			tt.change(nodes[0])
			classes := classesToBalance(view, nodes)
			assert.NotContains(t, classes, storageclass.Standard)
			assert.Len(t, classes["GLACIER"], 2, "another class is spread")
		})
	}
}

func TestAWriteLargerThanWhatANodeGivesOrTakesStays(t *testing.T) {
	b := balanceOf([]nodeFill{
		{id: "a", held: 1000, free: 1000, takes: true},
		{id: "b", held: 0, free: 2000, takes: true},
	})
	require.NotNil(t, b)
	receivers := []*Node{{ID: "a"}, {ID: "b"}}
	for i := range 100 {
		assert.Empty(t, b.moves(writeID("bucket", fmt.Sprintf("key-%d", i), ""), []string{"a"}, 600, receivers))
	}
}
