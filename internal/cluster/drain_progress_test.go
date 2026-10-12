package cluster

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The time left is the pace so far applied to what is left, unknown while
// nothing has moved.
func TestTheTimeLeftOfADrain(t *testing.T) {
	assert.Zero(t, eta(0, 100, time.Minute), "nothing moved")
	assert.Zero(t, eta(100, 0, time.Minute), "nothing left")
	assert.EqualValues(t, 181, eta(25, 75, time.Minute), "a quarter in a minute leaves three")

	var d drainProgress
	assert.Nil(t, d.snapshot())
	d.begin(4, 400)
	d.moved(100)
	p := d.snapshot()
	require.NotNil(t, p)
	assert.EqualValues(t, 3, p.RemainingObjects)
	assert.EqualValues(t, 300, p.RemainingBytes)
	assert.Positive(t, p.EtaSeconds)
	d.settle(5, 500)
	p = d.snapshot()
	assert.EqualValues(t, 5, p.TotalObjects, "a pass that finds more raises the total")
	assert.EqualValues(t, 500, p.TotalBytes)
	assert.Zero(t, p.EtaSeconds, "nothing moved of the new total")
	d.reset()
	assert.Nil(t, d.snapshot())
}
