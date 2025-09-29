package compression

import (
	"bytes"
	"strings"
	"testing"
)

func TestGzipCompression(t *testing.T) {
	compressor := NewGzipCompressor(DefaultCompressionConfig())

	// Test data (repeating pattern compresses well)
	originalData := []byte(strings.Repeat("Hello, World! This is a test message. ", 100))

	// Test compression
	compressed, err := compressor.Compress(originalData)
	if err != nil {
		t.Fatalf("Failed to compress data: %v", err)
	}

	if compressed.Algorithm != "gzip" {
		t.Errorf("Expected algorithm gzip, got %s", compressed.Algorithm)
	}

	if compressed.Metadata.OriginalSize != int64(len(originalData)) {
		t.Errorf("Expected original size %d, got %d", len(originalData), compressed.Metadata.OriginalSize)
	}

	if compressed.Metadata.CompressedSize >= compressed.Metadata.OriginalSize {
		t.Error("Compressed size should be smaller than original")
	}

	// Test decompression
	decompressed, err := compressor.Decompress(compressed)
	if err != nil {
		t.Fatalf("Failed to decompress data: %v", err)
	}

	if !bytes.Equal(originalData, decompressed) {
		t.Error("Decompressed data doesn't match original")
	}
}

func TestCompressionService(t *testing.T) {
	service := NewCompressionService(DefaultCompressionConfig())

	// Test compressible content
	textData := []byte(strings.Repeat("This is a text file with repeated content. ", 50))
	compressed, err := service.CompressData(textData, "text/plain")
	if err != nil {
		t.Fatalf("Failed to compress text data: %v", err)
	}

	if compressed.Algorithm == "none" {
		t.Error("Text data should be compressed")
	}

	// Test decompression
	decompressed, err := service.DecompressData(compressed)
	if err != nil {
		t.Fatalf("Failed to decompress data: %v", err)
	}

	if !bytes.Equal(textData, decompressed) {
		t.Error("Decompressed data doesn't match original")
	}
}

func TestShouldCompress(t *testing.T) {
	compressor := NewGzipCompressor(DefaultCompressionConfig())

	testCases := []struct {
		contentType string
		size        int64
		expected    bool
		description string
	}{
		{"text/plain", 2048, true, "text content above minimum size"},
		{"text/html", 2048, true, "HTML content above minimum size"},
		{"application/json", 2048, true, "JSON content above minimum size"},
		{"image/jpeg", 2048, false, "JPEG image should not be compressed"},
		{"image/png", 2048, false, "PNG image should not be compressed"},
		{"application/zip", 2048, false, "ZIP file should not be compressed"},
		{"text/plain", 512, false, "text content below minimum size"},
		{"text/plain", 200*1024*1024, false, "content above maximum size"},
	}

	for _, tc := range testCases {
		result := compressor.ShouldCompress(tc.contentType, tc.size)
		if result != tc.expected {
			t.Errorf("%s: expected %v, got %v", tc.description, tc.expected, result)
		}
	}
}

func TestCompressionDetection(t *testing.T) {
	compressor := NewGzipCompressor(DefaultCompressionConfig())

	// Test gzip detection
	gzipData := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00}
	algorithm, isCompressed := compressor.DetectCompression(gzipData)
	if !isCompressed || algorithm != "gzip" {
		t.Errorf("Failed to detect gzip compression: algorithm=%s, compressed=%v", algorithm, isCompressed)
	}

	// Test non-compressed data
	plainData := []byte("This is plain text data")
	algorithm, isCompressed = compressor.DetectCompression(plainData)
	if isCompressed {
		t.Errorf("Plain data detected as compressed: algorithm=%s", algorithm)
	}

	// Test ZIP detection
	zipData := []byte{0x50, 0x4b, 0x03, 0x04}
	algorithm, isCompressed = compressor.DetectCompression(zipData)
	if !isCompressed || algorithm != "zip" {
		t.Errorf("Failed to detect ZIP compression: algorithm=%s, compressed=%v", algorithm, isCompressed)
	}
}

func TestStreamCompression(t *testing.T) {
	compressor := NewGzipCompressor(DefaultCompressionConfig())

	// Test data
	originalData := strings.Repeat("Stream compression test data. ", 100)
	src := strings.NewReader(originalData)
	var compressed bytes.Buffer

	// Test stream compression
	metadata, err := compressor.CompressStream(src, &compressed)
	if err != nil {
		t.Fatalf("Failed to compress stream: %v", err)
	}

	if metadata.OriginalSize != int64(len(originalData)) {
		t.Errorf("Expected original size %d, got %d", len(originalData), metadata.OriginalSize)
	}

	// Test stream decompression
	var decompressed bytes.Buffer
	err = compressor.DecompressStream(&compressed, &decompressed, metadata)
	if err != nil {
		t.Fatalf("Failed to decompress stream: %v", err)
	}

	if decompressed.String() != originalData {
		t.Error("Decompressed stream doesn't match original")
	}
}

func TestNoopCompressor(t *testing.T) {
	compressor := NewNoopCompressor(nil)

	testData := []byte("Test data for noop compressor")

	// Test compression (should return data as-is)
	compressed, err := compressor.Compress(testData)
	if err != nil {
		t.Fatalf("Noop compress failed: %v", err)
	}

	if compressed.Algorithm != "none" {
		t.Errorf("Expected algorithm 'none', got %s", compressed.Algorithm)
	}

	if !bytes.Equal(compressed.Data, testData) {
		t.Error("Noop compressor should return original data")
	}

	// Test decompression
	decompressed, err := compressor.Decompress(compressed)
	if err != nil {
		t.Fatalf("Noop decompress failed: %v", err)
	}

	if !bytes.Equal(decompressed, testData) {
		t.Error("Noop decompressor should return original data")
	}

	// ShouldCompress should always return false
	if compressor.ShouldCompress("text/plain", 1024) {
		t.Error("Noop compressor should never compress")
	}
}

func TestCompressionConfigs(t *testing.T) {
	// Test default config
	defaultConfig := DefaultCompressionConfig()
	if defaultConfig.Algorithm != "gzip" {
		t.Error("Default config should use gzip")
	}

	// Test text config
	textConfig := TextCompressionConfig()
	if textConfig.Level != 9 {
		t.Error("Text config should use maximum compression level")
	}

	// Test fast config
	fastConfig := FastCompressionConfig()
	if fastConfig.Level != 1 {
		t.Error("Fast config should use minimum compression level")
	}
}

func TestMatchContentType(t *testing.T) {
	testCases := []struct {
		contentType string
		pattern     string
		expected    bool
	}{
		{"text/plain", "text/plain", true},
		{"text/html", "text/*", true},
		{"image/jpeg", "image/*", true},
		{"application/json", "text/*", false},
		{"TEXT/PLAIN", "text/plain", true}, // case insensitive
		{"text/plain; charset=utf-8", "text/plain", false}, // exact match only
	}

	for _, tc := range testCases {
		result := matchContentType(tc.contentType, tc.pattern)
		if result != tc.expected {
			t.Errorf("matchContentType(%s, %s): expected %v, got %v",
				tc.contentType, tc.pattern, tc.expected, result)
		}
	}
}