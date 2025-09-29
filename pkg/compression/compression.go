package compression

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
)

// CompressionConfig holds compression configuration
type CompressionConfig struct {
	// Algorithm specifies the compression algorithm (gzip, none)
	Algorithm string
	// Level specifies the compression level (1-9 for gzip)
	Level int
	// MinSize specifies the minimum size to compress (bytes)
	MinSize int64
	// MaxSize specifies the maximum size to compress (bytes, 0 = no limit)
	MaxSize int64
	// ContentTypes specifies which content types to compress
	ContentTypes []string
	// SkipContentTypes specifies which content types to skip
	SkipContentTypes []string
	// AutoDetect enables automatic compression detection
	AutoDetect bool
}

// CompressionMetadata contains metadata about compressed data
type CompressionMetadata struct {
	Algorithm        string            `json:"algorithm"`
	Level           int               `json:"level"`
	OriginalSize    int64             `json:"original_size"`
	CompressedSize  int64             `json:"compressed_size"`
	CompressionRatio float64          `json:"compression_ratio"`
	ContentType     string            `json:"content_type,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// Compressor defines the interface for compression operations
type Compressor interface {
	// Compress compresses data
	Compress(data []byte) (*CompressedData, error)
	// Decompress decompresses data
	Decompress(compressedData *CompressedData) ([]byte, error)
	// CompressStream compresses data from reader to writer
	CompressStream(src io.Reader, dst io.Writer) (*CompressionMetadata, error)
	// DecompressStream decompresses data from reader to writer
	DecompressStream(src io.Reader, dst io.Writer, metadata *CompressionMetadata) error
	// ShouldCompress determines if data should be compressed based on content type and size
	ShouldCompress(contentType string, size int64) bool
	// DetectCompression detects if data is already compressed
	DetectCompression(data []byte) (string, bool)
}

// CompressedData represents compressed data with metadata
type CompressedData struct {
	Data      []byte               `json:"data"`
	Metadata  *CompressionMetadata `json:"metadata"`
	Algorithm string               `json:"algorithm"`
}

// gzipCompressor implements gzip compression
type gzipCompressor struct {
	config *CompressionConfig
}

// NewGzipCompressor creates a new gzip compressor
func NewGzipCompressor(config *CompressionConfig) Compressor {
	if config == nil {
		config = DefaultCompressionConfig()
	}
	return &gzipCompressor{config: config}
}

// noopCompressor implements no compression (pass-through)
type noopCompressor struct {
	config *CompressionConfig
}

// NewNoopCompressor creates a new no-op compressor
func NewNoopCompressor(config *CompressionConfig) Compressor {
	if config == nil {
		config = &CompressionConfig{Algorithm: "none"}
	}
	return &noopCompressor{config: config}
}

// DefaultCompressionConfig returns default compression configuration
func DefaultCompressionConfig() *CompressionConfig {
	return &CompressionConfig{
		Algorithm: "gzip",
		Level:     6, // Default compression level
		MinSize:   1024, // 1KB minimum
		MaxSize:   100 * 1024 * 1024, // 100MB maximum
		ContentTypes: []string{
			"text/plain",
			"text/html",
			"text/css",
			"text/javascript",
			"application/javascript",
			"application/json",
			"application/xml",
			"text/xml",
			"application/x-yaml",
			"text/yaml",
		},
		SkipContentTypes: []string{
			"image/jpeg",
			"image/png",
			"image/gif",
			"image/webp",
			"video/mp4",
			"video/mpeg",
			"audio/mp3",
			"audio/mpeg",
			"application/zip",
			"application/gzip",
			"application/x-gzip",
			"application/x-tar",
			"application/x-rar",
			"application/x-7z-compressed",
		},
		AutoDetect: true,
	}
}

// TextCompressionConfig returns configuration optimized for text content
func TextCompressionConfig() *CompressionConfig {
	return &CompressionConfig{
		Algorithm: "gzip",
		Level:     9, // Maximum compression for text
		MinSize:   512, // 512 bytes minimum
		MaxSize:   0, // No maximum
		ContentTypes: []string{
			"text/plain",
			"text/html",
			"text/css",
			"text/javascript",
			"application/javascript",
			"application/json",
			"application/xml",
			"text/xml",
		},
		SkipContentTypes: []string{},
		AutoDetect:       true,
	}
}

// FastCompressionConfig returns configuration optimized for speed
func FastCompressionConfig() *CompressionConfig {
	return &CompressionConfig{
		Algorithm: "gzip",
		Level:     1, // Fastest compression
		MinSize:   2048, // 2KB minimum
		MaxSize:   10 * 1024 * 1024, // 10MB maximum
		ContentTypes: []string{
			"text/plain",
			"application/json",
			"text/html",
		},
		SkipContentTypes: []string{
			"image/*",
			"video/*",
			"audio/*",
		},
		AutoDetect: true,
	}
}

// Gzip Compressor Implementation

// Compress compresses data using gzip
func (c *gzipCompressor) Compress(data []byte) (*CompressedData, error) {
	if len(data) == 0 {
		return &CompressedData{
			Data: data,
			Metadata: &CompressionMetadata{
				Algorithm:        "none",
				OriginalSize:     0,
				CompressedSize:   0,
				CompressionRatio: 1.0,
			},
			Algorithm: "none",
		}, nil
	}

	originalSize := int64(len(data))

	// Check if data should be compressed
	if originalSize < c.config.MinSize {
		return c.createUncompressedResult(data), nil
	}

	if c.config.MaxSize > 0 && originalSize > c.config.MaxSize {
		return c.createUncompressedResult(data), nil
	}

	// Check for already compressed data
	if c.config.AutoDetect {
		if _, isCompressed := c.DetectCompression(data); isCompressed {
			return c.createUncompressedResult(data), nil
		}
	}

	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, c.config.Level)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip writer: %w", err)
	}

	if _, err := writer.Write(data); err != nil {
		writer.Close()
		return nil, fmt.Errorf("failed to compress data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	compressedData := buf.Bytes()
	compressedSize := int64(len(compressedData))

	// If compression didn't save space, return original
	if compressedSize >= originalSize {
		return c.createUncompressedResult(data), nil
	}

	compressionRatio := float64(compressedSize) / float64(originalSize)

	metadata := &CompressionMetadata{
		Algorithm:        c.config.Algorithm,
		Level:           c.config.Level,
		OriginalSize:    originalSize,
		CompressedSize:  compressedSize,
		CompressionRatio: compressionRatio,
		Metadata:        make(map[string]string),
	}

	return &CompressedData{
		Data:      compressedData,
		Metadata:  metadata,
		Algorithm: c.config.Algorithm,
	}, nil
}

// Decompress decompresses gzip data
func (c *gzipCompressor) Decompress(compressedData *CompressedData) ([]byte, error) {
	if compressedData.Algorithm == "none" || compressedData.Algorithm == "" {
		return compressedData.Data, nil
	}

	if compressedData.Algorithm != "gzip" {
		return nil, fmt.Errorf("unsupported compression algorithm: %s", compressedData.Algorithm)
	}

	reader, err := gzip.NewReader(bytes.NewReader(compressedData.Data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer reader.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return nil, fmt.Errorf("failed to decompress data: %w", err)
	}

	return buf.Bytes(), nil
}

// CompressStream compresses data from reader to writer
func (c *gzipCompressor) CompressStream(src io.Reader, dst io.Writer) (*CompressionMetadata, error) {
	writer, err := gzip.NewWriterLevel(dst, c.config.Level)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip writer: %w", err)
	}
	defer writer.Close()

	originalSize, err := io.Copy(writer, src)
	if err != nil {
		return nil, fmt.Errorf("failed to compress stream: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	metadata := &CompressionMetadata{
		Algorithm:    c.config.Algorithm,
		Level:       c.config.Level,
		OriginalSize: originalSize,
		Metadata:    make(map[string]string),
	}

	return metadata, nil
}

// DecompressStream decompresses data from reader to writer
func (c *gzipCompressor) DecompressStream(src io.Reader, dst io.Writer, metadata *CompressionMetadata) error {
	if metadata.Algorithm == "none" || metadata.Algorithm == "" {
		_, err := io.Copy(dst, src)
		return err
	}

	if metadata.Algorithm != "gzip" {
		return fmt.Errorf("unsupported compression algorithm: %s", metadata.Algorithm)
	}

	reader, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer reader.Close()

	if _, err := io.Copy(dst, reader); err != nil {
		return fmt.Errorf("failed to decompress stream: %w", err)
	}

	return nil
}

// ShouldCompress determines if content should be compressed
func (c *gzipCompressor) ShouldCompress(contentType string, size int64) bool {
	// Check size constraints
	if size < c.config.MinSize {
		return false
	}

	if c.config.MaxSize > 0 && size > c.config.MaxSize {
		return false
	}

	// Check content type
	contentType = strings.ToLower(contentType)

	// Check skip list first
	for _, skipType := range c.config.SkipContentTypes {
		if matchContentType(contentType, skipType) {
			return false
		}
	}

	// Check allow list
	if len(c.config.ContentTypes) > 0 {
		for _, allowType := range c.config.ContentTypes {
			if matchContentType(contentType, allowType) {
				return true
			}
		}
		return false
	}

	// Default: compress text content
	return strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "xml") ||
		strings.Contains(contentType, "javascript")
}

// DetectCompression detects if data is already compressed
func (c *gzipCompressor) DetectCompression(data []byte) (string, bool) {
	if len(data) < 2 {
		return "", false
	}

	// Check for gzip magic number
	if data[0] == 0x1f && data[1] == 0x8b {
		return "gzip", true
	}

	// Check for other common compression signatures
	if len(data) >= 4 {
		// ZIP
		if data[0] == 0x50 && data[1] == 0x4b && (data[2] == 0x03 || data[2] == 0x05) {
			return "zip", true
		}

		// LZ4
		if data[0] == 0x04 && data[1] == 0x22 && data[2] == 0x4d && data[3] == 0x18 {
			return "lz4", true
		}
	}

	if len(data) >= 6 {
		// 7-Zip
		if bytes.Equal(data[0:6], []byte{0x37, 0x7a, 0xbc, 0xaf, 0x27, 0x1c}) {
			return "7z", true
		}
	}

	return "", false
}

// createUncompressedResult creates a result for uncompressed data
func (c *gzipCompressor) createUncompressedResult(data []byte) *CompressedData {
	return &CompressedData{
		Data: data,
		Metadata: &CompressionMetadata{
			Algorithm:        "none",
			OriginalSize:     int64(len(data)),
			CompressedSize:   int64(len(data)),
			CompressionRatio: 1.0,
			Metadata:        make(map[string]string),
		},
		Algorithm: "none",
	}
}

// NoOp Compressor Implementation (pass-through)

// Compress returns data as-is
func (c *noopCompressor) Compress(data []byte) (*CompressedData, error) {
	return &CompressedData{
		Data: data,
		Metadata: &CompressionMetadata{
			Algorithm:        "none",
			OriginalSize:     int64(len(data)),
			CompressedSize:   int64(len(data)),
			CompressionRatio: 1.0,
			Metadata:        make(map[string]string),
		},
		Algorithm: "none",
	}, nil
}

// Decompress returns data as-is
func (c *noopCompressor) Decompress(compressedData *CompressedData) ([]byte, error) {
	return compressedData.Data, nil
}

// CompressStream copies data without compression
func (c *noopCompressor) CompressStream(src io.Reader, dst io.Writer) (*CompressionMetadata, error) {
	size, err := io.Copy(dst, src)
	if err != nil {
		return nil, err
	}

	return &CompressionMetadata{
		Algorithm:        "none",
		OriginalSize:     size,
		CompressedSize:   size,
		CompressionRatio: 1.0,
		Metadata:        make(map[string]string),
	}, nil
}

// DecompressStream copies data without decompression
func (c *noopCompressor) DecompressStream(src io.Reader, dst io.Writer, metadata *CompressionMetadata) error {
	_, err := io.Copy(dst, src)
	return err
}

// ShouldCompress always returns false
func (c *noopCompressor) ShouldCompress(contentType string, size int64) bool {
	return false
}

// DetectCompression always returns false
func (c *noopCompressor) DetectCompression(data []byte) (string, bool) {
	return "", false
}

// CompressionService combines compressor with automatic detection
type CompressionService struct {
	compressor Compressor
	config     *CompressionConfig
}

// NewCompressionService creates a new compression service
func NewCompressionService(config *CompressionConfig) *CompressionService {
	if config == nil {
		config = DefaultCompressionConfig()
	}

	var compressor Compressor
	switch config.Algorithm {
	case "gzip":
		compressor = NewGzipCompressor(config)
	case "none":
		compressor = NewNoopCompressor(config)
	default:
		compressor = NewGzipCompressor(config)
	}

	return &CompressionService{
		compressor: compressor,
		config:     config,
	}
}

// CompressData compresses data if appropriate
func (cs *CompressionService) CompressData(data []byte, contentType string) (*CompressedData, error) {
	if !cs.compressor.ShouldCompress(contentType, int64(len(data))) {
		// Return data as uncompressed
		return &CompressedData{
			Data: data,
			Metadata: &CompressionMetadata{
				Algorithm:        "none",
				OriginalSize:     int64(len(data)),
				CompressedSize:   int64(len(data)),
				CompressionRatio: 1.0,
				ContentType:     contentType,
				Metadata:        make(map[string]string),
			},
			Algorithm: "none",
		}, nil
	}

	result, err := cs.compressor.Compress(data)
	if err != nil {
		return nil, err
	}

	if result.Metadata != nil {
		result.Metadata.ContentType = contentType
	}

	return result, nil
}

// DecompressData decompresses data
func (cs *CompressionService) DecompressData(compressedData *CompressedData) ([]byte, error) {
	return cs.compressor.Decompress(compressedData)
}

// CompressStream compresses a stream if appropriate
func (cs *CompressionService) CompressStream(src io.Reader, dst io.Writer, contentType string, size int64) (*CompressionMetadata, error) {
	if !cs.compressor.ShouldCompress(contentType, size) {
		// Pass through without compression
		copiedSize, err := io.Copy(dst, src)
		if err != nil {
			return nil, err
		}

		return &CompressionMetadata{
			Algorithm:        "none",
			OriginalSize:     copiedSize,
			CompressedSize:   copiedSize,
			CompressionRatio: 1.0,
			ContentType:     contentType,
			Metadata:        make(map[string]string),
		}, nil
	}

	metadata, err := cs.compressor.CompressStream(src, dst)
	if err != nil {
		return nil, err
	}

	if metadata != nil {
		metadata.ContentType = contentType
	}

	return metadata, nil
}

// DecompressStream decompresses a stream
func (cs *CompressionService) DecompressStream(src io.Reader, dst io.Writer, metadata *CompressionMetadata) error {
	return cs.compressor.DecompressStream(src, dst, metadata)
}

// GetCompressor returns the underlying compressor
func (cs *CompressionService) GetCompressor() Compressor {
	return cs.compressor
}

// Helper functions

// matchContentType checks if a content type matches a pattern (supports wildcards)
func matchContentType(contentType, pattern string) bool {
	contentType = strings.ToLower(contentType)
	pattern = strings.ToLower(pattern)

	if pattern == contentType {
		return true
	}

	// Handle wildcard patterns like "image/*"
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(contentType, prefix+"/")
	}

	return false
}

// IsCompressed checks if data appears to be compressed
func IsCompressed(data []byte) bool {
	compressor := NewGzipCompressor(DefaultCompressionConfig())
	_, compressed := compressor.DetectCompression(data)
	return compressed
}

// GetCompressionRatio calculates compression ratio
func GetCompressionRatio(originalSize, compressedSize int64) float64 {
	if originalSize == 0 {
		return 1.0
	}
	return float64(compressedSize) / float64(originalSize)
}