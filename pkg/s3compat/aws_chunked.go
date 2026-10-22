package s3compat

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/sirupsen/logrus"
)

const maxAwsChunkedChunkSize = 64 * 1024 * 1024

// maxAwsChunkedTrailerLines bounds the lines read after the last chunk.
const maxAwsChunkedTrailerLines = 64

var ErrInvalidAwsChunkedSize = errors.New("invalid aws-chunked chunk size")

// AwsChunkedReader decodes AWS chunked encoding format
// Format: {chunk-size-hex}\r\n{chunk-data}\r\n...0\r\n{trailers}\r\n
type AwsChunkedReader struct {
	reader  *bufio.Reader
	buffer  bytes.Buffer
	eof     bool
	decoded int64
	// trailers takes the checksums the body ends with.
	trailers http.Header
	// signing checks each chunk, and the trailers, of a body sent in signed
	// chunks.
	signing *auth.ChunkSigning
}

// isAwsChunked reports a body its headers say is aws-chunked.
func isAwsChunked(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") ||
		r.Header.Get("X-Amz-Decoded-Content-Length") != "" ||
		strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-")
}

// VerifyingChunks checks each chunk, and the trailers, against the signatures
// signing chains: a chunk that does not match ends the body with
// auth.ErrChunkSignatureMismatch before its bytes are given.
func (r *AwsChunkedReader) VerifyingChunks(signing *auth.ChunkSigning) *AwsChunkedReader {
	r.signing = signing
	return r
}

// ChecksumTrailersInto makes the checksums the body ends with headers of h,
// once the body is read.
func (r *AwsChunkedReader) ChecksumTrailersInto(h http.Header) *AwsChunkedReader {
	r.trailers = h
	return r
}

// NewAwsChunkedReader creates a new AWS chunked encoding reader
func NewAwsChunkedReader(r io.Reader) *AwsChunkedReader {
	return &AwsChunkedReader{
		reader: bufio.NewReader(r),
	}
}

// Read implements io.Reader, decoding AWS chunked format
func (r *AwsChunkedReader) Read(p []byte) (n int, err error) {
	if r.eof && r.buffer.Len() == 0 {
		return 0, io.EOF
	}

	// If we have buffered data, return it first
	if r.buffer.Len() > 0 {
		return r.buffer.Read(p)
	}

	// Read next chunk
	if err := r.readNextChunk(); err != nil {
		if err == io.EOF {
			r.eof = true
		}
		if r.buffer.Len() > 0 {
			return r.buffer.Read(p)
		}
		return 0, err
	}

	return r.buffer.Read(p)
}

// readNextChunk reads and decodes the next chunk from aws-chunked format
func (r *AwsChunkedReader) readNextChunk() error {
	// Read chunk size line (hex size + \r\n)
	sizeLine, err := r.reader.ReadString('\n')
	if err != nil {
		return err
	}

	sizeLine = strings.TrimSpace(sizeLine)

	// Format: {hex-size};chunk-signature={signature}
	signature := ""
	if idx := strings.Index(sizeLine, ";"); idx != -1 {
		signature, _ = strings.CutPrefix(sizeLine[idx+1:], "chunk-signature=")
		sizeLine = sizeLine[:idx]
	}

	// Parse chunk size (in hexadecimal)
	chunkSize, err := strconv.ParseInt(sizeLine, 16, 64)
	if err != nil {
		logrus.WithError(err).WithField("size_line", sizeLine).Error("Failed to parse chunk size")
		return fmt.Errorf("%w: %s", ErrInvalidAwsChunkedSize, sizeLine)
	}
	if chunkSize < 0 || chunkSize > maxAwsChunkedChunkSize {
		logrus.WithFields(logrus.Fields{
			"size_line":      sizeLine,
			"chunk_size_dec": chunkSize,
		}).Warn("AWS chunked: rejected unsafe chunk size")
		return fmt.Errorf("%w: %s", ErrInvalidAwsChunkedSize, sizeLine)
	}

	logrus.WithFields(logrus.Fields{
		"chunk_size_hex": sizeLine,
		"chunk_size_dec": chunkSize,
		"total_decoded":  r.decoded,
	}).Debug("AWS chunked: read chunk header")

	// If chunk size is 0, we've reached the end
	if chunkSize == 0 {
		if r.signing != nil {
			if err := r.signing.VerifyChunk(signature, nil); err != nil {
				return fmt.Errorf("the last chunk: %w", err)
			}
		}
		// The trailers end the body. An empty line may part them from their
		// signature, as minio-go writes them, so they are read to its end.
		type trailer struct{ name, value string }
		var trailers []trailer
		trailerSignature := ""
		for lines := 0; ; lines++ {
			if lines > maxAwsChunkedTrailerLines {
				return fmt.Errorf("%w: more than %d trailer lines", ErrInvalidAwsChunkedSize, maxAwsChunkedTrailerLines)
			}
			trailerLine, err := r.reader.ReadString('\n')
			if err != nil && err != io.EOF {
				return err
			}

			trailerLine = strings.TrimSpace(trailerLine)
			if trailerLine == "" {
				if err == io.EOF {
					break
				}
				continue
			}
			if name, value, ok := strings.Cut(trailerLine, ":"); ok {
				name, value = strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(value)
				if name == "x-amz-trailer-signature" {
					trailerSignature = value
				} else {
					trailers = append(trailers, trailer{name, value})
				}
			}

			logrus.WithField("trailer", trailerLine).Debug("AWS chunked: read trailer")

			// We've hit EOF or end of trailers
			if err == io.EOF {
				break
			}
		}
		if r.signing != nil && len(trailers) > 0 {
			var canonical strings.Builder
			for _, t := range trailers {
				canonical.WriteString(t.name + ":" + t.value + "\n")
			}
			if err := r.signing.VerifyTrailer(trailerSignature, []byte(canonical.String())); err != nil {
				return fmt.Errorf("the trailers: %w", err)
			}
		}
		for _, t := range trailers {
			if r.trailers != nil && strings.HasPrefix(t.name, "x-amz-checksum-") {
				r.trailers.Set(t.name, t.value)
			}
		}
		return io.EOF
	}

	// Read chunk data
	chunkData := make([]byte, chunkSize)
	_, err = io.ReadFull(r.reader, chunkData)
	if err != nil {
		return fmt.Errorf("failed to read chunk data: %w", err)
	}
	if r.signing != nil {
		if err := r.signing.VerifyChunk(signature, chunkData); err != nil {
			return fmt.Errorf("a chunk of %d bytes: %w", chunkSize, err)
		}
	}

	// Write decoded chunk to buffer
	r.buffer.Write(chunkData)
	r.decoded += chunkSize

	// Read trailing \r\n after chunk data
	trailing, err := r.reader.ReadString('\n')
	if err != nil {
		return err
	}

	if strings.TrimSpace(trailing) != "" {
		logrus.WithField("trailing", trailing).Warn("AWS chunked: unexpected data after chunk")
	}

	return nil
}

// Close implements io.Closer
func (r *AwsChunkedReader) Close() error {
	return nil
}
