package erasure

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The layout gives one parity shard up to four nodes and two from five, with
// the other nodes as data shards up to MaxData; fewer than three nodes have
// none.
func TestTheLayoutOfAClusterOfNodes(t *testing.T) {
	for _, nodes := range []int{0, 1, 2} {
		_, ok := For(nodes)
		assert.False(t, ok, "%d nodes", nodes)
	}
	cases := map[int][2]int{3: {2, 1}, 4: {3, 1}, 5: {3, 2}, 6: {4, 2}, 10: {8, 2}, 16: {8, 2}}
	for nodes, want := range cases {
		l, ok := For(nodes)
		require.True(t, ok, "%d nodes", nodes)
		assert.Equal(t, want, [2]int{l.Data, l.Parity}, "%d nodes", nodes)
		assert.Equal(t, DefaultBlock, l.Block)
		assert.LessOrEqual(t, l.Width(), nodes)
	}
}

// Each shard holds a Block of each full stripe and its part of the last one.
func TestTheSizeOfAShard(t *testing.T) {
	l := Layout{Data: 2, Parity: 1, Block: 4}
	for size, want := range map[int64]int64{0: 0, 1: 1, 2: 1, 3: 2, 8: 4, 9: 5, 16: 8, 17: 9} {
		assert.Equal(t, want, l.ShardSize(size), "size %d", size)
	}
}

// shardSet is the shards of one object held in memory, some of them missing.
type shardSet struct {
	shards  [][]byte
	missing map[int]bool
	// failAfter makes a shard's reader fail after so many bytes.
	failAfter map[int]int
	opened    atomic.Int32
}

func encodeAll(t *testing.T, l Layout, data []byte) *shardSet {
	t.Helper()
	bufs := make([]*bytes.Buffer, l.Width())
	dst := make([]io.Writer, l.Width())
	for i := range bufs {
		bufs[i] = &bytes.Buffer{}
		dst[i] = bufs[i]
	}
	failed, err := Encode(l, bytes.NewReader(data), int64(len(data)), dst)
	require.NoError(t, err)
	for i, err := range failed {
		require.NoError(t, err, "shard %d", i)
	}
	set := &shardSet{missing: map[int]bool{}, failAfter: map[int]int{}}
	for i, b := range bufs {
		require.EqualValues(t, l.ShardSize(int64(len(data))), b.Len(), "shard %d", i)
		set.shards = append(set.shards, b.Bytes())
	}
	return set
}

type failingReader struct {
	r    io.Reader
	left int
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.left <= 0 {
		return 0, errors.New("the node went away")
	}
	if len(p) > f.left {
		p = p[:f.left]
	}
	n, err := f.r.Read(p)
	f.left -= n
	return n, err
}

func (s *shardSet) open(index int, offset int64) (io.ReadCloser, error) {
	s.opened.Add(1)
	if s.missing[index] {
		return nil, fmt.Errorf("shard %d is not there", index)
	}
	var r io.Reader = bytes.NewReader(s.shards[index][offset:])
	if left, ok := s.failAfter[index]; ok {
		r = &failingReader{r: r, left: left}
	}
	return io.NopCloser(r), nil
}

func readAll(t *testing.T, l Layout, size int64, s *shardSet) ([]byte, error) {
	t.Helper()
	r := NewReader(l, size, s.open)
	defer r.Close()
	return io.ReadAll(r)
}

func randomBytes(n int) []byte {
	data := make([]byte, n)
	rand.New(rand.NewSource(int64(n))).Read(data)
	return data
}

// An object is read back whole from its shards with any parity's worth of
// them missing, whatever its size against the stripes.
func TestAnObjectIsReadBackWithAnyShardsMissing(t *testing.T) {
	for _, l := range []Layout{{Data: 2, Parity: 1, Block: 16}, {Data: 3, Parity: 2, Block: 16}} {
		for _, size := range []int{1, 15, 16, 31, 32, 33, 47, 48, 49, 100, 1000} {
			data := randomBytes(size)
			set := encodeAll(t, l, data)
			got, err := readAll(t, l, int64(size), set)
			require.NoError(t, err)
			require.Equal(t, data, got, "%+v size %d, every shard", l, size)

			for a := 0; a < l.Width(); a++ {
				for b := a; b < l.Width(); b++ {
					if b != a && l.Parity < 2 {
						continue
					}
					set.missing = map[int]bool{a: true, b: true}
					got, err := readAll(t, l, int64(size), set)
					require.NoError(t, err, "%+v size %d without %d and %d", l, size, a, b)
					require.Equal(t, data, got, "%+v size %d without %d and %d", l, size, a, b)
				}
			}
		}
	}
}

// With fewer shards than its data shards, the object cannot be read.
func TestAnObjectWithTooFewShardsIsNotRead(t *testing.T) {
	l := Layout{Data: 2, Parity: 1, Block: 16}
	data := randomBytes(40)
	set := encodeAll(t, l, data)
	set.missing = map[int]bool{0: true, 2: true}
	_, err := readAll(t, l, int64(len(data)), set)
	assert.ErrorIs(t, err, ErrTooFewShards)
}

// A shard that fails while it is read is replaced by a parity shard from
// the stripe it failed in.
func TestAShardThatFailsWhileReadIsReplaced(t *testing.T) {
	l := Layout{Data: 2, Parity: 1, Block: 16}
	data := randomBytes(200)
	set := encodeAll(t, l, data)
	set.failAfter[1] = 40
	got, err := readAll(t, l, int64(len(data)), set)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

// A read from any offset, after a Seek, gives the object's bytes from there.
func TestARangeIsReadFromItsStripe(t *testing.T) {
	l := Layout{Data: 3, Parity: 2, Block: 16}
	data := randomBytes(500)
	set := encodeAll(t, l, data)
	set.missing = map[int]bool{1: true}
	for _, from := range []int64{0, 1, 15, 16, 47, 48, 49, 300, 499} {
		r := NewReader(l, int64(len(data)), set.open)
		at, err := r.Seek(from, io.SeekStart)
		require.NoError(t, err)
		require.Equal(t, from, at)
		part := make([]byte, 30)
		n, err := io.ReadFull(r, part)
		if from+30 > int64(len(data)) {
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		} else {
			require.NoError(t, err)
		}
		assert.Equal(t, data[from:from+int64(n)], part[:n], "from %d", from)
		require.NoError(t, r.Close())
	}

	r := NewReader(l, int64(len(data)), set.open)
	defer r.Close()
	end, err := r.Seek(-10, io.SeekEnd)
	require.NoError(t, err)
	assert.EqualValues(t, 490, end)
	rest, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, data[490:], rest)
	_, err = r.Seek(-1, io.SeekStart)
	assert.Error(t, err)
}

// A sequential read keeps each shard open from one stripe to the next.
func TestASequentialReadOpensEachShardOnce(t *testing.T) {
	l := Layout{Data: 2, Parity: 1, Block: 16}
	data := randomBytes(1000)
	set := encodeAll(t, l, data)
	got, err := readAll(t, l, int64(len(data)), set)
	require.NoError(t, err)
	assert.Equal(t, data, got)
	assert.EqualValues(t, l.Data, set.opened.Load())
}

// A missing shard is made again, byte for byte, from the others.
func TestAShardIsRebuiltFromTheOthers(t *testing.T) {
	l := Layout{Data: 3, Parity: 2, Block: 16}
	data := randomBytes(333)
	set := encodeAll(t, l, data)
	original := [][]byte{set.shards[0], set.shards[4]}
	set.missing = map[int]bool{0: true, 4: true}
	first, last := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, Rebuild(l, int64(len(data)), set.open, map[int]io.Writer{0: first, 4: last}))
	assert.Equal(t, original[0], first.Bytes())
	assert.Equal(t, original[1], last.Bytes())

	set.missing = map[int]bool{0: true, 1: true, 4: true}
	assert.ErrorIs(t, Rebuild(l, int64(len(data)), set.open, map[int]io.Writer{0: &bytes.Buffer{}}), ErrTooFewShards)
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("the node went away") }

// A shard whose destination fails is not written again; the others are
// written whole, and enough of them read the object back.
func TestEncodingGoesOnWhenADestinationFails(t *testing.T) {
	l := Layout{Data: 2, Parity: 1, Block: 16}
	data := randomBytes(100)
	bufs := []*bytes.Buffer{{}, {}}
	dst := []io.Writer{bufs[0], brokenWriter{}, bufs[1]}
	failed, err := Encode(l, bytes.NewReader(data), int64(len(data)), dst)
	require.NoError(t, err)
	assert.NoError(t, failed[0])
	assert.Error(t, failed[1])
	assert.NoError(t, failed[2])

	set := &shardSet{shards: [][]byte{bufs[0].Bytes(), nil, bufs[1].Bytes()}, missing: map[int]bool{1: true}, failAfter: map[int]int{}}
	got, err := readAll(t, l, int64(len(data)), set)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

// An object shorter than its size fails to encode.
func TestEncodingFailsOnAShortObject(t *testing.T) {
	l := Layout{Data: 2, Parity: 1, Block: 16}
	_, err := Encode(l, bytes.NewReader(randomBytes(10)), 20, []io.Writer{io.Discard, io.Discard, io.Discard})
	assert.Error(t, err)
}

// The default block reads back an object of several stripes.
func TestTheDefaultBlock(t *testing.T) {
	l, ok := For(3)
	require.True(t, ok)
	data := randomBytes(3*DefaultBlock + 12345)
	set := encodeAll(t, l, data)
	set.missing = map[int]bool{0: true}
	got, err := readAll(t, l, int64(len(data)), set)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, got))
}

// A Seek to another stripe of a reader already reading opens the shards
// again from there.
func TestASeekWhileReadingReopensTheShards(t *testing.T) {
	l := Layout{Data: 2, Parity: 1, Block: 16}
	data := randomBytes(400)
	set := encodeAll(t, l, data)
	r := NewReader(l, int64(len(data)), set.open)
	defer r.Close()
	head := make([]byte, 10)
	_, err := io.ReadFull(r, head)
	require.NoError(t, err)
	require.Equal(t, data[:10], head)
	for _, to := range []int64{300, 40, 41} {
		_, err = r.Seek(to, io.SeekStart)
		require.NoError(t, err)
		part := make([]byte, 30)
		_, err = io.ReadFull(r, part)
		require.NoError(t, err)
		assert.Equal(t, data[to:to+30], part, "from %d", to)
	}
}

// flakyWriter fails its first write and counts the bytes written after.
type flakyWriter struct {
	failed bool
	after  int
}

func (f *flakyWriter) Write(p []byte) (int, error) {
	if !f.failed {
		f.failed = true
		return 0, errors.New("the node went away")
	}
	f.after += len(p)
	return len(p), nil
}

// A destination that failed is not written again, even when it would take
// the next stripes.
func TestAFailedDestinationIsWrittenNoMore(t *testing.T) {
	l := Layout{Data: 2, Parity: 1, Block: 16}
	flaky := &flakyWriter{}
	failed, err := Encode(l, bytes.NewReader(randomBytes(200)), 200, []io.Writer{io.Discard, flaky, io.Discard})
	require.NoError(t, err)
	assert.Error(t, failed[1])
	assert.Zero(t, flaky.after)
}

// A shard made again is made from the others only: the stale bytes held for
// it are not read.
func TestARebuildDoesNotReadTheShardItMakes(t *testing.T) {
	l := Layout{Data: 2, Parity: 1, Block: 16}
	data := randomBytes(150)
	set := encodeAll(t, l, data)
	original := set.shards[0]
	set.shards[0] = bytes.Repeat([]byte{0xFF}, len(original))
	rebuilt := &bytes.Buffer{}
	require.NoError(t, Rebuild(l, int64(len(data)), set.open, map[int]io.Writer{0: rebuilt}))
	assert.Equal(t, original, rebuilt.Bytes())
}

// Ready reads the first stripe ahead: with too few shards it fails before a
// byte is taken, and the read that follows opens no shard again.
func TestReadyReadsTheFirstStripe(t *testing.T) {
	l := Layout{Data: 2, Parity: 1, Block: 16}
	data := randomBytes(100)
	set := encodeAll(t, l, data)
	r := NewReader(l, int64(len(data)), set.open)
	require.NoError(t, r.Ready())
	opened := set.opened.Load()
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, data, got)
	assert.Equal(t, opened, set.opened.Load())
	require.NoError(t, r.Close())

	set.missing = map[int]bool{0: true, 1: true}
	assert.ErrorIs(t, NewReader(l, int64(len(data)), set.open).Ready(), ErrTooFewShards)
	assert.NoError(t, NewReader(l, 0, set.open).Ready(), "an empty object has nothing to read")
}
