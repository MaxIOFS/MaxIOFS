package s3compat

import (
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"io"
	"strings"
)

// maxSelectDecompressed is the most a compressed object may decompress to for
// one query. An uncompressed object loads at most what is stored, which its
// quota counted; a compressed one could expand without end on the node's
// disk. 5 GiB is the largest object a single PUT stores.
var maxSelectDecompressed int64 = 5 << 30

var (
	// errSelectCompression: the object is not compressed as the request says.
	errSelectCompression = errors.New("the object is not compressed as CompressionType says")
	// errSelectTooLarge: the object decompresses to more than a query reads.
	errSelectTooLarge = errors.New("the object decompresses to more than S3 Select reads")
)

// selectCompressionSupported reports whether S3 Select reads a CompressionType.
func selectCompressionSupported(compression string) bool {
	switch strings.ToUpper(compression) {
	case "", "NONE", "GZIP", "BZIP2":
		return true
	}
	return false
}

// selectSource reads the stored object, counting the bytes a query scans and
// keeping the storage's own error apart from the decompressor's.
type selectSource struct {
	r   io.Reader
	n   int64
	err error
}

func (s *selectSource) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	s.n += int64(n)
	if err != nil && err != io.EOF {
		s.err = err
	}
	return n, err
}

// selectDecompressed is what a decompressor gives, held to
// maxSelectDecompressed. failed says why it stopped, when it was the
// decompression and not the storage.
type selectDecompressed struct {
	r      io.Reader
	source *selectSource
	n      int64
	failed error
}

func (d *selectDecompressed) Read(p []byte) (int, error) {
	if d.failed != nil {
		return 0, d.failed
	}
	n, err := d.r.Read(p)
	d.n += int64(n)
	switch {
	case d.n > maxSelectDecompressed:
		d.failed = errSelectTooLarge
		return 0, d.failed
	case err != nil && err != io.EOF && d.source.err == nil:
		d.failed = errSelectCompression
		return n, d.failed
	}
	return n, err
}

// selectInput returns the object's data as the query reads it, decompressed as
// compression says. A GZIP object that does not start as one fails here; the
// rest of a broken stream fails as it is read.
func selectInput(compression string, source *selectSource) (io.Reader, error) {
	switch strings.ToUpper(compression) {
	case "GZIP":
		zr, err := gzip.NewReader(source)
		if err != nil {
			if source.err != nil {
				return nil, source.err
			}
			return nil, errSelectCompression
		}
		return &selectDecompressed{r: zr, source: source}, nil
	case "BZIP2":
		return &selectDecompressed{r: bzip2.NewReader(source), source: source}, nil
	}
	return source, nil
}

// selectInputFailure is why the decompression of input stopped, or nil.
func selectInputFailure(input io.Reader) error {
	if d, ok := input.(*selectDecompressed); ok {
		return d.failed
	}
	return nil
}
