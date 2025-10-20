package s3compat

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

// AwsChunkedReader decodes AWS chunked encoding format
// Format: {chunk-size-hex}\r\n{chunk-data}\r\n...0\r\n{trailers}\r\n
type AwsChunkedReader struct {
	reader  *bufio.Reader
	buffer  bytes.Buffer
	eof     bool
	decoded int64
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

	// Strip chunk-signature if present (warp/MinIO-Go format)
	// Format: {hex-size};chunk-signature={signature}
	if idx := strings.Index(sizeLine, ";"); idx != -1 {
		sizeLine = sizeLine[:idx]
	}

	// Parse chunk size (in hexadecimal)
	chunkSize, err := strconv.ParseInt(sizeLine, 16, 64)
	if err != nil {
		logrus.WithError(err).WithField("size_line", sizeLine).Error("Failed to parse chunk size")
		return fmt.Errorf("invalid chunk size: %s", sizeLine)
	}

	logrus.WithFields(logrus.Fields{
		"chunk_size_hex": sizeLine,
		"chunk_size_dec": chunkSize,
		"total_decoded":  r.decoded,
	}).Debug("AWS chunked: read chunk header")

	// If chunk size is 0, we've reached the end
	if chunkSize == 0 {
		// Read trailers until we hit empty line
		for {
			trailerLine, err := r.reader.ReadString('\n')
			if err != nil && err != io.EOF {
				return err
			}

			trailerLine = strings.TrimSpace(trailerLine)
			if trailerLine == "" {
				// Empty line signals end of trailers
				break
			}

			logrus.WithField("trailer", trailerLine).Debug("AWS chunked: read trailer")

			// We've hit EOF or end of trailers
			if err == io.EOF {
				break
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
