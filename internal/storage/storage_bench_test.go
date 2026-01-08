package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
)

// BenchmarkPut_10KB benchmarks writing 10KB object
func BenchmarkPut_10KB(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	data := bytes.Repeat([]byte("a"), 10*1024)
	ctx := context.Background()

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := backend.Put(ctx, "bench-10kb", bytes.NewReader(data), nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPut_1MB benchmarks writing 1MB object
func BenchmarkPut_1MB(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	data := bytes.Repeat([]byte("a"), 1024*1024)
	ctx := context.Background()

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := backend.Put(ctx, "bench-1mb", bytes.NewReader(data), nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPut_10MB benchmarks writing 10MB object
func BenchmarkPut_10MB(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	data := bytes.Repeat([]byte("a"), 10*1024*1024)
	ctx := context.Background()

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := backend.Put(ctx, "bench-10mb", bytes.NewReader(data), nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGet_10KB benchmarks reading 10KB object
func BenchmarkGet_10KB(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	// Setup: write test object
	data := bytes.Repeat([]byte("a"), 10*1024)
	ctx := context.Background()
	err := backend.Put(ctx, "bench-10kb", bytes.NewReader(data), nil)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader, _, err := backend.Get(ctx, "bench-10kb")
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, reader)
		reader.Close()
	}
}

// BenchmarkGet_1MB benchmarks reading 1MB object
func BenchmarkGet_1MB(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	// Setup: write test object
	data := bytes.Repeat([]byte("a"), 1024*1024)
	ctx := context.Background()
	err := backend.Put(ctx, "bench-1mb", bytes.NewReader(data), nil)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader, _, err := backend.Get(ctx, "bench-1mb")
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, reader)
		reader.Close()
	}
}

// BenchmarkGet_10MB benchmarks reading 10MB object
func BenchmarkGet_10MB(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	// Setup: write test object
	data := bytes.Repeat([]byte("a"), 10*1024*1024)
	ctx := context.Background()
	err := backend.Put(ctx, "bench-10mb", bytes.NewReader(data), nil)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader, _, err := backend.Get(ctx, "bench-10mb")
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, reader)
		reader.Close()
	}
}

// BenchmarkDelete benchmarks deleting objects
func BenchmarkDelete(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	data := bytes.Repeat([]byte("a"), 10*1024)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Setup: create object to delete
		err := backend.Put(ctx, "bench-delete", bytes.NewReader(data), nil)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		// Benchmark: delete object
		err = backend.Delete(ctx, "bench-delete")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExists benchmarks checking object existence
func BenchmarkExists(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	// Setup: create test object
	data := bytes.Repeat([]byte("a"), 10*1024)
	ctx := context.Background()
	err := backend.Put(ctx, "bench-exists", bytes.NewReader(data), nil)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := backend.Exists(ctx, "bench-exists")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkList benchmarks listing objects
func BenchmarkList(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	// Setup: create 100 test objects
	data := bytes.Repeat([]byte("a"), 1024)
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		err := backend.Put(ctx, "bench-list-"+string(rune('0'+i/10))+string(rune('0'+i%10)), bytes.NewReader(data), nil)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := backend.List(ctx, "bench-list-", false)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetMetadata benchmarks getting object metadata
func BenchmarkGetMetadata(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	// Setup: create test object with metadata
	data := bytes.Repeat([]byte("a"), 10*1024)
	ctx := context.Background()
	metadata := map[string]string{
		"Content-Type": "application/octet-stream",
		"test-key":     "test-value",
	}
	err := backend.Put(ctx, "bench-metadata", bytes.NewReader(data), metadata)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := backend.GetMetadata(ctx, "bench-metadata")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConcurrentPuts benchmarks concurrent writes to different objects
func BenchmarkConcurrentPuts(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	data := bytes.Repeat([]byte("a"), 10*1024)
	ctx := context.Background()

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := "bench-concurrent/" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26))
			err := backend.Put(ctx, path, bytes.NewReader(data), nil)
			if err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

// BenchmarkConcurrentGets benchmarks concurrent reads from same object
func BenchmarkConcurrentGets(b *testing.B) {
	backend, cleanup := setupBenchBackend(b)
	defer cleanup()

	// Setup: write test object
	data := bytes.Repeat([]byte("a"), 10*1024)
	ctx := context.Background()
	err := backend.Put(ctx, "bench-concurrent-get", bytes.NewReader(data), nil)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			reader, _, err := backend.Get(ctx, "bench-concurrent-get")
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, reader)
			reader.Close()
		}
	})
}

// setupBenchBackend creates a temporary filesystem backend for benchmarking
func setupBenchBackend(b *testing.B) (Backend, func()) {
	tmpDir := b.TempDir()

	config := Config{
		Root: tmpDir,
	}

	backend, err := NewFilesystemBackend(config)
	if err != nil {
		b.Fatal(err)
	}

	cleanup := func() {
		backend.Close()
	}

	return backend, cleanup
}
