package object

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"hash/crc64"
	"io"
	"net/http"
	"strings"
)

var (
	// ErrBadDigest: the bytes of a write are not those its digest names.
	ErrBadDigest = errors.New("BadDigest")
	// ErrInvalidDigest: a Content-MD5 that is no MD5 digest.
	ErrInvalidDigest = errors.New("InvalidDigest")
	// ErrUnknownChecksum: a checksum algorithm S3 does not have.
	ErrUnknownChecksum = errors.New("unknown checksum algorithm")
)

// checksumAlgorithms are the checksums a write can carry, by the name that
// follows x-amz-checksum- in their header.
var checksumAlgorithms = []string{"CRC32", "CRC32C", "CRC64NVME", "SHA1", "SHA256"}

// crc64NVME is the table of CRC-64/NVME, reflected.
var crc64NVME = crc64.MakeTable(0x9a6c9329ac4bc9b5)

// NewChecksumHasher returns the hash of a checksum algorithm, nil for one S3
// does not have.
func NewChecksumHasher(algorithm string) hash.Hash {
	switch algorithm {
	case "CRC32":
		return crc32.NewIEEE()
	case "CRC32C":
		return crc32.New(crc32.MakeTable(crc32.Castagnoli))
	case "CRC64NVME":
		return crc64.New(crc64NVME)
	case "SHA1":
		return sha1.New()
	case "SHA256":
		return sha256.New()
	}
	return nil
}

// ChecksumHeader is the header that carries the checksum of an algorithm.
func ChecksumHeader(algorithm string) string {
	return "x-amz-checksum-" + strings.ToLower(algorithm)
}

// ChecksumAlgorithm is the checksum a write carries: named by
// x-amz-checksum-algorithm or x-amz-sdk-checksum-algorithm, given in its
// x-amz-checksum-* header, or announced in x-amz-trailer. "" when none.
func ChecksumAlgorithm(headers http.Header) string {
	for _, name := range []string{"x-amz-checksum-algorithm", "x-amz-sdk-checksum-algorithm"} {
		if v := strings.ToUpper(strings.TrimSpace(headers.Get(name))); v != "" {
			return v
		}
	}
	trailer := strings.ToLower(headers.Get("x-amz-trailer"))
	for _, algorithm := range checksumAlgorithms {
		name := ChecksumHeader(algorithm)
		if headers.Get(name) != "" || strings.Contains(trailer, name) {
			return algorithm
		}
	}
	return ""
}

// checkContentMD5 compares the MD5 of the bytes of a write with its
// Content-MD5, when it has one.
func checkContentMD5(headers http.Header, sum []byte) error {
	value := headers.Get("Content-MD5")
	if value == "" {
		return nil
	}
	want, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(want) != md5.Size {
		return fmt.Errorf("%w: the Content-MD5 you specified is not valid", ErrInvalidDigest)
	}
	if !bytes.Equal(want, sum) {
		return fmt.Errorf("%w: the Content-MD5 you specified did not match what was received", ErrBadDigest)
	}
	return nil
}

// checkChecksum compares the checksum of the bytes of a write with the one it
// carries, when it carries one. The value is read once the bytes are, so a
// checksum sent in a trailer counts.
func checkChecksum(headers http.Header, algorithm string, sum []byte) error {
	value := headers.Get(ChecksumHeader(algorithm))
	if value != "" && value != base64.StdEncoding.EncodeToString(sum) {
		return fmt.Errorf("%w: the %s you specified did not match the calculated checksum", ErrBadDigest, algorithm)
	}
	return nil
}

// digestCheckingReader fails the last Read of a body whose bytes are not
// those its Content-MD5 or checksum names: the write it feeds stores nothing.
type digestCheckingReader struct {
	inner     io.Reader
	headers   http.Header
	md5       hash.Hash
	algorithm string
	checksum  hash.Hash
	checked   bool
}

// NewDigestCheckingReader checks the bytes of r against the Content-MD5 and
// the checksum headers names.
func NewDigestCheckingReader(r io.Reader, headers http.Header) (io.Reader, error) {
	d := &digestCheckingReader{inner: r, headers: headers}
	if headers.Get("Content-MD5") != "" {
		d.md5 = md5.New()
	}
	if algorithm := ChecksumAlgorithm(headers); algorithm != "" {
		if d.checksum = NewChecksumHasher(algorithm); d.checksum == nil {
			return nil, fmt.Errorf("%w: %s", ErrUnknownChecksum, algorithm)
		}
		d.algorithm = algorithm
	}
	if d.md5 == nil && d.checksum == nil {
		return r, nil
	}
	return d, nil
}

func (d *digestCheckingReader) Read(p []byte) (int, error) {
	n, err := d.inner.Read(p)
	if n > 0 {
		if d.md5 != nil {
			d.md5.Write(p[:n])
		}
		if d.checksum != nil {
			d.checksum.Write(p[:n])
		}
	}
	if err == io.EOF && !d.checked {
		d.checked = true
		if d.md5 != nil {
			if bad := checkContentMD5(d.headers, d.md5.Sum(nil)); bad != nil {
				return n, bad
			}
		}
		if d.checksum != nil {
			if bad := checkChecksum(d.headers, d.algorithm, d.checksum.Sum(nil)); bad != nil {
				return n, bad
			}
		}
	}
	return n, err
}
