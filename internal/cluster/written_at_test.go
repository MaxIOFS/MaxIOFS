package cluster

import (
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
)

// Two writes of an object are ordered by second, then within one second by
// the time each was written when both carry it.
func TestTwoWritesOfOneSecondAreOrderedByTheirWriteTime(t *testing.T) {
	second := time.Unix(1_700_000_000, 0)
	early, late := second.Add(100*time.Millisecond).UnixNano(), second.Add(900*time.Millisecond).UnixNano()
	assert.Equal(t, -1, compareWrites(second.Unix(), late, second.Unix()+1, early), "the second decides first")
	assert.Equal(t, 1, compareWrites(second.Unix(), late, second.Unix(), early))
	assert.Equal(t, -1, compareWrites(second.Unix(), early, second.Unix(), late))
	assert.Equal(t, 0, compareWrites(second.Unix(), 0, second.Unix(), late), "a write without its time cannot be ordered within the second")

	local := &object.Object{LastModified: second, WrittenAt: late, ETag: "aaaa"}
	_, act := lwwClassify(local, &ChecksumEntry{LastModified: second.Unix(), WrittenAt: early, ETag: "ffff"})
	assert.Equal(t, actPushToPeer, act, "the later write wins, whatever its ETag")
	_, act = lwwClassify(&object.Object{LastModified: second, WrittenAt: early, ETag: "ffff"},
		&ChecksumEntry{LastModified: second.Unix(), WrittenAt: late, ETag: "aaaa"})
	assert.Equal(t, actPullFromPeer, act)
}

// A deletion at a time removes what was written before it: within its
// second, by the time of the write when the object carries it; without it,
// the write counts as made after.
func TestAWriteIsOrderedAgainstADeletionOfItsSecond(t *testing.T) {
	second := time.Unix(1_700_000_000, 0)
	deletion := second.Add(500 * time.Millisecond).UnixNano()
	before, after := second.Add(100*time.Millisecond).UnixNano(), second.Add(900*time.Millisecond).UnixNano()
	assert.False(t, writtenSince(second, before, deletion))
	assert.True(t, writtenSince(second, after, deletion))
	assert.True(t, writtenSince(second, 0, deletion), "no write time: kept")
	assert.False(t, writtenSince(second.Add(-time.Second), after, deletion), "an earlier second is before")
	assert.True(t, writtenSince(second.Add(time.Second), before, deletion), "a later second is after")
}
