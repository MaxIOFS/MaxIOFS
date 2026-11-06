package object

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// At the limit no object, part or completion gets in, and what frees space and
// what reads still works. Writes come back with the space.
func TestWritesStopWhenADiskIsFull(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "full")
	defer cleanup()
	ctx := context.Background()
	version := put(t, om, "full", "kept", "body")
	upload, err := om.CreateMultipartUpload(ctx, "full", "parts", http.Header{})
	require.NoError(t, err)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("part"))
	require.NoError(t, err)

	om.diskUsed = func(string) (float64, error) { return MaxDiskUsedPercent, nil }

	_, err = om.PutObject(ctx, "full", "new", strings.NewReader("x"), http.Header{})
	assert.ErrorIs(t, err, ErrDiskFull)
	_, err = om.UploadPart(ctx, upload.UploadID, 2, strings.NewReader("x"))
	assert.ErrorIs(t, err, ErrDiskFull)
	_, err = om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*part})
	assert.ErrorIs(t, err, ErrDiskFull)

	_, reader, err := om.GetObject(ctx, "full", "kept", version)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	_, err = om.DeleteObject(ctx, "full", "kept", false, version)
	require.NoError(t, err)

	om.diskUsed = func(string) (float64, error) { return MaxDiskUsedPercent - 0.1, nil }
	_, err = om.PutObject(ctx, "full", "new", strings.NewReader("x"), http.Header{})
	assert.NoError(t, err)
}

// Pebble and SQLite may sit on another disk than the objects.
func TestAFullMetadataDiskStopsWrites(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "full")
	defer cleanup()
	om.dataDir = "metadata-disk"
	om.diskUsed = func(path string) (float64, error) {
		if path == om.dataDir {
			return 100, nil
		}
		return 10, nil
	}

	_, err := om.PutObject(context.Background(), "full", "new", strings.NewReader("x"), http.Header{})
	assert.ErrorIs(t, err, ErrDiskFull)
}

func TestADiskThatCannotBeReadRefusesNothing(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "full")
	defer cleanup()
	om.diskUsed = func(string) (float64, error) { return 0, errors.New("no such disk") }

	_, err := om.PutObject(context.Background(), "full", "new", strings.NewReader("x"), http.Header{})
	assert.NoError(t, err)
}

// Writes in flight can pass the limit: a write that runs out of disk is the same
// answer.
func TestAWriteThatRanOutOfDiskIsADiskFull(t *testing.T) {
	cause := &os.PathError{Op: "write", Path: "object", Err: syscall.ENOSPC}
	err := fmt.Errorf("failed to store object: %w", storage.NewErrorWithCause("WriteData", "Failed to write data", cause))

	assert.True(t, IsDiskFull(err))
	assert.True(t, IsDiskFull(ErrDiskFull))
	assert.False(t, IsDiskFull(errors.New("something else")))
}

func TestWritableBytesLeaveOutWhatStaysFree(t *testing.T) {
	assert.Equal(t, uint64(5), WritableBytes(990, 10))
	assert.Equal(t, uint64(0), WritableBytes(995, 5))
	assert.Equal(t, uint64(0), WritableBytes(1000, 0))
}
