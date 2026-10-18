// Package erasure cuts an object into data and parity shards with
// Reed-Solomon codes, and reads it back from any of them as many as its data
// shards.
package erasure

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/klauspost/reedsolomon"
)

// DefaultBlock is the bytes of each shard in a full stripe.
const DefaultBlock = 256 << 10

// MaxData is the most data shards of a write: a wider cluster spreads its
// writes over different nodes instead of making wider stripes.
const MaxData = 8

// ErrTooFewShards: fewer shards answered than the data shards of the layout.
var ErrTooFewShards = errors.New("too few shards of the object answered")

// Layout is how an object is cut: Data shards of its bytes, Parity shards
// made from them, Block bytes of each shard per stripe. The last stripe is
// shorter: each shard holds its part of the bytes left, rounded up.
type Layout struct {
	Data   int `json:"data"`
	Parity int `json:"parity"`
	Block  int `json:"block"`
}

// For is the layout of a write in a cluster of nodes that hold data: one
// parity shard up to four nodes, two from five, and data shards for the
// other nodes, MaxData at most. Fewer than three nodes have none.
func For(nodes int) (Layout, bool) {
	parity := 1
	if nodes > 4 {
		parity = 2
	}
	data := min(nodes-parity, MaxData)
	if data < 2 {
		return Layout{}, false
	}
	return Layout{Data: data, Parity: parity, Block: DefaultBlock}, true
}

// Width is the number of shards.
func (l Layout) Width() int { return l.Data + l.Parity }

// Valid reports whether l can cut an object.
func (l Layout) Valid() bool {
	return l.Data >= 1 && l.Parity >= 1 && l.Block > 0 && l.Width() <= 256
}

func (l Layout) stripe() int64 { return int64(l.Data) * int64(l.Block) }

func (l Layout) stripes(size int64) int64 {
	if size <= 0 {
		return 0
	}
	return (size + l.stripe() - 1) / l.stripe()
}

// shardBlock is the bytes of each shard in stripe t of an object of size
// bytes.
func (l Layout) shardBlock(size, t int64) int64 {
	if t < l.stripes(size)-1 {
		return int64(l.Block)
	}
	rest := size - t*l.stripe()
	return (rest + int64(l.Data) - 1) / int64(l.Data)
}

// ShardSize is the size of each shard of an object of size bytes.
func (l Layout) ShardSize(size int64) int64 {
	n := l.stripes(size)
	if n == 0 {
		return 0
	}
	return (n-1)*int64(l.Block) + l.shardBlock(size, n-1)
}

func (l Layout) encoder() (reedsolomon.Encoder, error) {
	if !l.Valid() {
		return nil, fmt.Errorf("invalid erasure layout %d+%d, block %d", l.Data, l.Parity, l.Block)
	}
	return reedsolomon.New(l.Data, l.Parity)
}

// Encode cuts the size bytes read from r into the shards of l, shard i to
// dst[i]; a nil destination is skipped. A destination that fails is written
// no more: the others go on, and its error is reported at its index. Encode
// fails when r does or ends before size bytes.
func Encode(l Layout, r io.Reader, size int64, dst []io.Writer) ([]error, error) {
	if len(dst) != l.Width() {
		return nil, fmt.Errorf("%d destinations for %d shards", len(dst), l.Width())
	}
	enc, err := l.encoder()
	if err != nil {
		return nil, err
	}
	failed := make([]error, len(dst))
	buf := make([]byte, l.Width()*l.Block)
	shards := make([][]byte, l.Width())
	for t := range l.stripes(size) {
		s := l.shardBlock(size, t)
		n := min(l.stripe(), size-t*l.stripe())
		data := buf[:int64(l.Data)*s]
		if _, err := io.ReadFull(r, data[:n]); err != nil {
			return failed, fmt.Errorf("read the object: %w", err)
		}
		clear(data[n:])
		for i := range shards {
			shards[i] = buf[int64(i)*s : int64(i+1)*s]
		}
		if err := enc.Encode(shards); err != nil {
			return failed, err
		}
		var wg sync.WaitGroup
		for i, w := range dst {
			if w == nil || failed[i] != nil {
				continue
			}
			wg.Go(func() {
				if _, err := w.Write(shards[i]); err != nil {
					failed[i] = err
				}
			})
		}
		wg.Wait()
	}
	return failed, nil
}

// Source opens shard index of an object at offset: the shard's bytes from
// there on. It is called for several shards at once.
type Source func(index int, offset int64) (io.ReadCloser, error)

// Reader reads an object from its shards: the data shards and, in place of
// one that fails, a parity shard. Seek moves it; the shards are opened again
// from the stripe a read needs.
type Reader struct {
	layout  Layout
	size    int64
	open    Source
	enc     reedsolomon.Encoder
	encErr  error
	pos     int64
	held    int64
	data    []byte
	bufs    [][]byte
	streams []*shardStream
	failed  []bool
	// errs are the errors of the shards that failed.
	errs []error
}

type shardStream struct {
	body io.ReadCloser
	at   int64
}

// NewReader reads an object of size bytes cut by l, whose shards open opens.
func NewReader(l Layout, size int64, open Source) *Reader {
	enc, err := l.encoder()
	return &Reader{
		layout: l, size: size, open: open, enc: enc, encErr: err, held: -1,
		bufs: make([][]byte, l.Width()), streams: make([]*shardStream, l.Width()),
		failed: make([]bool, l.Width()), errs: make([]error, l.Width()),
	}
}

// Ready reads the stripe at the reader's position: a reader that cannot read
// the object fails before a byte of it is taken.
func (r *Reader) Ready() error {
	if r.encErr != nil {
		return r.encErr
	}
	if r.pos >= r.size {
		return nil
	}
	if t := r.pos / r.layout.stripe(); r.held != t {
		return r.readStripe(t)
	}
	return nil
}

func (r *Reader) Read(p []byte) (int, error) {
	if r.encErr != nil {
		return 0, r.encErr
	}
	if r.pos >= r.size {
		return 0, io.EOF
	}
	t := r.pos / r.layout.stripe()
	if r.held != t {
		if err := r.readStripe(t); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.data[r.pos-t*r.layout.stripe():])
	r.pos += int64(n)
	return n, nil
}

func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	to := offset
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		to += r.pos
	case io.SeekEnd:
		to += r.size
	default:
		return r.pos, fmt.Errorf("invalid whence %d", whence)
	}
	if to < 0 {
		return r.pos, fmt.Errorf("negative position %d", to)
	}
	r.pos = to
	return to, nil
}

func (r *Reader) Close() error {
	var errs error
	for i, s := range r.streams {
		if s != nil {
			errs = errors.Join(errs, s.body.Close())
			r.streams[i] = nil
		}
	}
	return errs
}

// readStripe holds the data of stripe t, made again from parity where a data
// shard failed.
func (r *Reader) readStripe(t int64) error {
	shards, err := r.fetch(t)
	if err != nil {
		return err
	}
	for i := range r.layout.Data {
		if len(shards[i]) == 0 {
			if err := r.enc.ReconstructData(shards); err != nil {
				return fmt.Errorf("rebuild stripe %d: %w", t, err)
			}
			break
		}
	}
	length := min(r.layout.stripe(), r.size-t*r.layout.stripe())
	if r.data == nil {
		r.data = make([]byte, r.layout.stripe())
	}
	r.data = r.data[:0]
	for i := range r.layout.Data {
		r.data = append(r.data, shards[i]...)
	}
	r.data = r.data[:length]
	r.held = t
	return nil
}

// fetch reads stripe t from as many shards as the data shards: those that
// have not failed, in order, and for each that fails the next one. The
// shards not read are empty.
func (r *Reader) fetch(t int64) ([][]byte, error) {
	s := r.layout.shardBlock(r.size, t)
	offset := t * int64(r.layout.Block)
	shards := make([][]byte, r.layout.Width())
	for {
		var chosen []int
		have := 0
		for i := range shards {
			if len(shards[i]) > 0 {
				have++
			}
		}
		for i := range shards {
			if have+len(chosen) == r.layout.Data {
				break
			}
			if !r.failed[i] && len(shards[i]) == 0 {
				chosen = append(chosen, i)
			}
		}
		if have == r.layout.Data {
			return shards, nil
		}
		if have+len(chosen) < r.layout.Data {
			return nil, r.tooFew()
		}
		var wg sync.WaitGroup
		for _, i := range chosen {
			if r.bufs[i] == nil {
				r.bufs[i] = make([]byte, r.layout.Block)
			}
			wg.Go(func() {
				if err := r.readShard(i, offset, r.bufs[i][:s]); err != nil {
					r.failed[i], r.errs[i] = true, err
					return
				}
				shards[i] = r.bufs[i][:s]
			})
		}
		wg.Wait()
		for i := range shards {
			if len(shards[i]) == 0 {
				shards[i] = r.bufs[i][:0]
			}
		}
	}
}

// readShard reads len(b) bytes of shard i at offset into b, opening it there
// unless it is open at offset.
func (r *Reader) readShard(i int, offset int64, b []byte) error {
	s := r.streams[i]
	if s != nil && s.at != offset {
		s.body.Close()
		s, r.streams[i] = nil, nil
	}
	if s == nil {
		body, err := r.open(i, offset)
		if err != nil {
			return err
		}
		s = &shardStream{body: body, at: offset}
		r.streams[i] = s
	}
	if _, err := io.ReadFull(s.body, b); err != nil {
		s.body.Close()
		r.streams[i] = nil
		return fmt.Errorf("shard %d at %d: %w", i, offset, err)
	}
	s.at += int64(len(b))
	return nil
}

func (r *Reader) tooFew() error {
	return fmt.Errorf("%w: %d needed: %w", ErrTooFewShards, r.layout.Data, errors.Join(r.errs...))
}

// Rebuild writes each shard of want, made again from the others, whole to
// want[index].
func Rebuild(l Layout, size int64, open Source, want map[int]io.Writer) error {
	r := NewReader(l, size, open)
	defer r.Close()
	if r.encErr != nil {
		return r.encErr
	}
	for i := range want {
		if i < 0 || i >= l.Width() {
			return fmt.Errorf("no shard %d in a layout of %d", i, l.Width())
		}
		r.failed[i] = true
	}
	for t := range l.stripes(size) {
		shards, err := r.fetch(t)
		if err != nil {
			return err
		}
		if err := r.enc.Reconstruct(shards); err != nil {
			return fmt.Errorf("rebuild stripe %d: %w", t, err)
		}
		for i, w := range want {
			if _, err := w.Write(shards[i]); err != nil {
				return fmt.Errorf("write shard %d: %w", i, err)
			}
		}
	}
	return nil
}
