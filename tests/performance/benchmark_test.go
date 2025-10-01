package performance

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
)

// setupBenchmark creates managers for benchmarking
func setupBenchmark(b *testing.B) (bucket.Manager, object.Manager, string, func()) {
	tempDir, err := os.MkdirTemp("", "maxiofs-bench-*")
	if err != nil {
		b.Fatal(err)
	}

	storageBackend, err := storage.NewFilesystemBackend(storage.Config{Root: tempDir})
	if err != nil {
		b.Fatal(err)
	}

	bucketMgr := bucket.NewManager(storageBackend)
	objectMgr := object.NewManager(storageBackend, config.StorageConfig{})

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return bucketMgr, objectMgr, tempDir, cleanup
}

// BenchmarkBucketOperations benchmarks bucket CRUD operations
func BenchmarkBucketOperations(b *testing.B) {
	bucketMgr, _, _, cleanup := setupBenchmark(b)
	defer cleanup()

	ctx := context.Background()

	b.Run("CreateBucket", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			bucketName := fmt.Sprintf("bench-bucket-%d-%d", b.N, i)
			if err := bucketMgr.CreateBucket(ctx, bucketName); err != nil {
				// Skip if bucket already exists (race condition in benchmarks)
				if err.Error() != "bucket already exists" {
					b.Fatal(err)
				}
			}
		}
	})

	// Create a bucket for subsequent tests
	testBucket := "test-bucket"
	if err := bucketMgr.CreateBucket(ctx, testBucket); err != nil {
		b.Fatal(err)
	}

	b.Run("BucketExists", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := bucketMgr.BucketExists(ctx, testBucket)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("GetBucketInfo", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := bucketMgr.GetBucketInfo(ctx, testBucket)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ListBuckets", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := bucketMgr.ListBuckets(ctx)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkObjectOperations benchmarks object CRUD operations
func BenchmarkObjectOperations(b *testing.B) {
	bucketMgr, objectMgr, _, cleanup := setupBenchmark(b)
	defer cleanup()

	ctx := context.Background()
	bucketName := "bench-objects"

	if err := bucketMgr.CreateBucket(ctx, bucketName); err != nil {
		b.Fatal(err)
	}

	// Small object (1KB)
	smallData := make([]byte, 1024)
	rand.Read(smallData)

	b.Run("PutObject_1KB", func(b *testing.B) {
		b.SetBytes(1024)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			objectKey := fmt.Sprintf("object-%d", i)
			_, err := objectMgr.PutObject(ctx, bucketName, objectKey, bytes.NewReader(smallData), http.Header{})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	// Create object for read benchmarks
	testObject := "test-object"
	_, err := objectMgr.PutObject(ctx, bucketName, testObject, bytes.NewReader(smallData), http.Header{})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("GetObject_1KB", func(b *testing.B) {
		b.SetBytes(1024)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, reader, err := objectMgr.GetObject(ctx, bucketName, testObject)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, reader)
			reader.Close()
		}
	})

	b.Run("GetObjectMetadata", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := objectMgr.GetObjectMetadata(ctx, bucketName, testObject)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	// Add multiple objects for listing
	for i := 0; i < 100; i++ {
		objectKey := fmt.Sprintf("list-object-%d", i)
		_, err := objectMgr.PutObject(ctx, bucketName, objectKey, bytes.NewReader(smallData), http.Header{})
		if err != nil {
			b.Fatal(err)
		}
	}

	b.Run("ListObjects_100", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := objectMgr.ListObjects(ctx, bucketName, "", "", "", 1000)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkLargeFileOperations benchmarks operations with large files
func BenchmarkLargeFileOperations(b *testing.B) {
	bucketMgr, objectMgr, _, cleanup := setupBenchmark(b)
	defer cleanup()

	ctx := context.Background()
	bucketName := "bench-large"

	if err := bucketMgr.CreateBucket(ctx, bucketName); err != nil {
		b.Fatal(err)
	}

	sizes := []struct {
		name string
		size int64
	}{
		{"1MB", 1 * 1024 * 1024},
		{"10MB", 10 * 1024 * 1024},
		{"100MB", 100 * 1024 * 1024},
	}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("PutObject_%s", size.name), func(b *testing.B) {
			data := make([]byte, size.size)
			rand.Read(data)

			b.SetBytes(size.size)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				objectKey := fmt.Sprintf("large-object-%s-%d", size.name, i)
				_, err := objectMgr.PutObject(ctx, bucketName, objectKey, bytes.NewReader(data), http.Header{})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}

	// Create large object for read benchmarks
	largeData := make([]byte, 10*1024*1024) // 10MB
	rand.Read(largeData)
	testLargeObject := "test-large-object"
	_, err := objectMgr.PutObject(ctx, bucketName, testLargeObject, bytes.NewReader(largeData), http.Header{})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("GetObject_10MB", func(b *testing.B) {
		b.SetBytes(10 * 1024 * 1024)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, reader, err := objectMgr.GetObject(ctx, bucketName, testLargeObject)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, reader)
			reader.Close()
		}
	})
}

// BenchmarkMultipartUpload benchmarks multipart upload operations
func BenchmarkMultipartUpload(b *testing.B) {
	bucketMgr, objectMgr, _, cleanup := setupBenchmark(b)
	defer cleanup()

	ctx := context.Background()
	bucketName := "bench-multipart"

	if err := bucketMgr.CreateBucket(ctx, bucketName); err != nil {
		b.Fatal(err)
	}

	// 5MB parts (minimum size for multipart)
	partData := make([]byte, 5*1024*1024)
	rand.Read(partData)

	b.Run("CreateMultipartUpload", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			objectKey := fmt.Sprintf("multipart-object-%d", i)
			_, err := objectMgr.CreateMultipartUpload(ctx, bucketName, objectKey, http.Header{})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	// Create upload for parts benchmark
	upload, err := objectMgr.CreateMultipartUpload(ctx, bucketName, "test-multipart", http.Header{})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("UploadPart_5MB", func(b *testing.B) {
		b.SetBytes(5 * 1024 * 1024)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := objectMgr.UploadPart(ctx, upload.UploadID, i+1, bytes.NewReader(partData))
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkConcurrentOperations benchmarks concurrent access patterns
func BenchmarkConcurrentOperations(b *testing.B) {
	bucketMgr, objectMgr, _, cleanup := setupBenchmark(b)
	defer cleanup()

	ctx := context.Background()
	bucketName := "bench-concurrent"

	if err := bucketMgr.CreateBucket(ctx, bucketName); err != nil {
		b.Fatal(err)
	}

	data := make([]byte, 1024)
	rand.Read(data)

	b.Run("ConcurrentWrites", func(b *testing.B) {
		b.SetBytes(1024)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				objectKey := fmt.Sprintf("concurrent-object-%d-%d", b.N, i)
				_, err := objectMgr.PutObject(ctx, bucketName, objectKey, bytes.NewReader(data), http.Header{})
				if err != nil {
					// Log error but don't fail on concurrent race conditions
					b.Logf("Concurrent write error: %v", err)
				}
				i++
			}
		})
	})

	// Create object for concurrent reads
	testObject := "concurrent-read-object"
	_, err := objectMgr.PutObject(ctx, bucketName, testObject, bytes.NewReader(data), http.Header{})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("ConcurrentReads", func(b *testing.B) {
		b.SetBytes(1024)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, reader, err := objectMgr.GetObject(ctx, bucketName, testObject)
				if err != nil {
					b.Fatal(err)
				}
				io.Copy(io.Discard, reader)
				reader.Close()
			}
		})
	})
}

// BenchmarkMemoryAllocation measures memory allocations
func BenchmarkMemoryAllocation(b *testing.B) {
	bucketMgr, objectMgr, _, cleanup := setupBenchmark(b)
	defer cleanup()

	ctx := context.Background()
	bucketName := "bench-memory"

	if err := bucketMgr.CreateBucket(ctx, bucketName); err != nil {
		b.Fatal(err)
	}

	data := make([]byte, 1024)
	rand.Read(data)

	b.Run("PutObject_Allocations", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			objectKey := fmt.Sprintf("alloc-object-%d", i)
			_, err := objectMgr.PutObject(ctx, bucketName, objectKey, bytes.NewReader(data), http.Header{})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	// Create object for read allocations test
	testObject := "alloc-test-object"
	_, err := objectMgr.PutObject(ctx, bucketName, testObject, bytes.NewReader(data), http.Header{})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("GetObject_Allocations", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, reader, err := objectMgr.GetObject(ctx, bucketName, testObject)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, reader)
			reader.Close()
		}
	})
}
