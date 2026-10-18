package object

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
)

// IntegrityStatus represents the result of an integrity check
type IntegrityStatus string

const (
	IntegrityOK        IntegrityStatus = "ok"
	IntegrityCorrupted IntegrityStatus = "corrupted"
	IntegrityMissing   IntegrityStatus = "missing"
	IntegritySkipped   IntegrityStatus = "skipped"
	IntegrityError     IntegrityStatus = "error"
)

// IntegrityResult is the per-object result of an integrity check
type IntegrityResult struct {
	Key          string          `json:"key"`
	Status       IntegrityStatus `json:"status"`
	StoredETag   string          `json:"storedETag,omitempty"`
	ComputedETag string          `json:"computedETag,omitempty"`
	ExpectedSize int64           `json:"expectedSize,omitempty"`
	ActualSize   int64           `json:"actualSize,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// BucketIntegrityReport summarises an integrity check over a bucket (or a page of it)
type BucketIntegrityReport struct {
	Bucket     string             `json:"bucket"`
	Duration   string             `json:"duration"`
	Checked    int                `json:"checked"`
	OK         int                `json:"ok"`
	Corrupted  int                `json:"corrupted"`
	Skipped    int                `json:"skipped"`
	Errors     int                `json:"errors"`
	Issues     []*IntegrityResult `json:"issues,omitempty"`
	NextMarker string             `json:"nextMarker,omitempty"`
}

// VerifyObjectIntegrity reads the stored object, recomputes the MD5 of its
// content (after transparent decryption) and compares it to the stored ETag.
func (om *objectManager) VerifyObjectIntegrity(ctx context.Context, bucket, key string) (*IntegrityResult, error) {
	// Folder markers (keys ending with "/") are virtual directory placeholders.
	// They have no content file on storage, so there is nothing to verify.
	if strings.HasSuffix(key, "/") {
		return &IntegrityResult{
			Key:    key,
			Status: IntegritySkipped,
			Reason: "folder marker: not a data object",
		}, nil
	}

	// Fetch metadata to get the stored ETag and expected size
	meta, err := om.metadataStore.GetObject(ctx, bucket, key)
	if err != nil {
		return &IntegrityResult{
			Key:    key,
			Status: IntegrityError,
			Error:  fmt.Sprintf("metadata lookup failed: %v", err),
		}, nil
	}

	storedETag := meta.ETag

	// Delete markers have an empty ETag — they are tombstones, not real objects.
	if storedETag == "" {
		return &IntegrityResult{
			Key:    key,
			Status: IntegritySkipped,
			Reason: "delete marker: not a data object",
		}, nil
	}

	// Of data cut in shards this node checks the shard it holds, against the
	// digest it was stored with.
	if index, ok := om.shardHere(meta); ok {
		meta.Bucket, meta.Key = bucket, key
		return om.verifyShard(ctx, meta, index), nil
	}
	if _, sharded := ErasureLayout(meta); sharded {
		return &IntegrityResult{
			Key:        key,
			Status:     IntegritySkipped,
			StoredETag: storedETag,
			Reason:     "data cut in shards held by other nodes of the cluster",
		}, nil
	}

	// Multipart ETags have the form "<md5>-<partCount>"; we cannot verify them
	// with a simple MD5 of the content, so we skip them.
	if strings.Contains(storedETag, "-") {
		return &IntegrityResult{
			Key:        key,
			Status:     IntegritySkipped,
			StoredETag: storedETag,
			Reason:     "multipart object: ETag is composite, skipping MD5 verification",
		}, nil
	}

	// GetObject handles decryption transparently
	_, reader, err := om.GetObject(ctx, bucket, key)
	var elsewhere *DataElsewhereError
	if errors.As(err, &elsewhere) {
		return &IntegrityResult{
			Key:        key,
			Status:     IntegritySkipped,
			StoredETag: storedETag,
			Reason:     "data held by other nodes of the cluster",
		}, nil
	}
	if err != nil {
		// Distinguish a missing file from other errors
		errStr := err.Error()
		if strings.Contains(errStr, "not found") ||
			strings.Contains(errStr, "no such file") ||
			strings.Contains(errStr, "does not exist") {
			return &IntegrityResult{
				Key:          key,
				Status:       IntegrityMissing,
				StoredETag:   storedETag,
				ExpectedSize: meta.Size,
				Reason:       "object data not found on storage",
			}, nil
		}
		return &IntegrityResult{
			Key:    key,
			Status: IntegrityError,
			Error:  fmt.Sprintf("failed to open object: %v", err),
		}, nil
	}
	defer reader.Close()

	// Hash the full content
	hasher := md5.New()
	actualSize, err := io.Copy(hasher, reader)
	if err != nil {
		return &IntegrityResult{
			Key:    key,
			Status: IntegrityError,
			Error:  fmt.Sprintf("failed to read object data: %v", err),
		}, nil
	}

	computedETag := fmt.Sprintf("%x", hasher.Sum(nil))

	// Compare ETag and size
	if computedETag != storedETag || actualSize != meta.Size {
		return &IntegrityResult{
			Key:          key,
			Status:       IntegrityCorrupted,
			StoredETag:   storedETag,
			ComputedETag: computedETag,
			ExpectedSize: meta.Size,
			ActualSize:   actualSize,
			Reason:       "ETag or size mismatch",
		}, nil
	}

	return &IntegrityResult{
		Key:          key,
		Status:       IntegrityOK,
		StoredETag:   storedETag,
		ComputedETag: computedETag,
		ExpectedSize: meta.Size,
		ActualSize:   actualSize,
	}, nil
}

// verifyShard reads back shard index of the write entry is, held here: its
// bytes must decrypt and match the size and digest they were stored with.
func (om *objectManager) verifyShard(ctx context.Context, entry *metadata.ObjectMetadata, index int) *IntegrityResult {
	info := ShardOf(fromMetadataObject(entry), index, entry.LocationsGen)
	result := &IntegrityResult{Key: entry.Key, StoredETag: entry.ETag, ExpectedSize: info.Layout.ShardSize(entry.Size)}
	sidecar, err := om.storage.GetMetadata(ctx, om.shardRef(info))
	if err != nil {
		result.Status, result.Reason = IntegrityMissing, "the shard this node holds is not on its disk"
		return result
	}
	reader, err := om.OpenShard(ctx, info, 0)
	if err != nil {
		result.Status, result.Reason = IntegrityMissing, "the shard on this node's disk is of another write"
		return result
	}
	defer reader.Close()
	hasher := md5.New()
	result.ActualSize, err = io.Copy(hasher, reader)
	result.ComputedETag = fmt.Sprintf("%x", hasher.Sum(nil))
	switch {
	case err != nil:
		result.Status, result.Reason = IntegrityCorrupted, fmt.Sprintf("the shard held here does not read back: %v", err)
	case result.ActualSize != result.ExpectedSize || result.ComputedETag != sidecar["original-etag"]:
		result.Status, result.Reason = IntegrityCorrupted, "the shard held here does not match its digest"
	default:
		result.Status, result.Reason = IntegrityOK, "the shard held here was verified"
		return result
	}
	// A shard that is not the one stored goes: the cluster makes it again
	// from the other shards.
	reader.Close()
	if err := om.DeleteShard(ctx, info); err != nil {
		result.Reason += "; it could not be removed: " + err.Error()
	} else {
		result.Reason += "; removed, the cluster makes it again"
	}
	return result
}

// VerifyBucketIntegrity verifies a page of objects in a bucket.
// prefix, marker and maxKeys provide S3-compatible pagination.
func (om *objectManager) VerifyBucketIntegrity(ctx context.Context, bucket, prefix, marker string, maxKeys int) (*BucketIntegrityReport, error) {
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	start := time.Now()

	objs, nextMarker, err := om.metadataStore.ListObjects(ctx, bucket, prefix, marker, maxKeys)
	if err != nil {
		return nil, fmt.Errorf("listing objects failed: %w", err)
	}

	report := &BucketIntegrityReport{
		Bucket:     bucket,
		NextMarker: nextMarker,
	}

	for _, obj := range objs {
		result, err := om.VerifyObjectIntegrity(ctx, bucket, obj.Key)
		if err != nil {
			// VerifyObjectIntegrity always returns a non-nil result with an error
			// embedded; a returned Go error would be unexpected.
			report.Errors++
			report.Issues = append(report.Issues, &IntegrityResult{
				Key:    obj.Key,
				Status: IntegrityError,
				Error:  err.Error(),
			})
			report.Checked++
			continue
		}

		report.Checked++
		switch result.Status {
		case IntegrityOK:
			report.OK++
		case IntegritySkipped:
			report.Skipped++
		case IntegrityCorrupted, IntegrityMissing:
			report.Corrupted++
			report.Issues = append(report.Issues, result)
		case IntegrityError:
			report.Errors++
			report.Issues = append(report.Issues, result)
		}
	}

	report.Duration = time.Since(start).String()
	return report, nil
}
