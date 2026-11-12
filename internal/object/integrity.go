package object

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"strconv"
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
	VersionID    string          `json:"versionId,omitempty"`
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

// VerifyObjectIntegrity verifies the current version of an object.
func (om *objectManager) VerifyObjectIntegrity(ctx context.Context, bucket, key string) (*IntegrityResult, error) {
	return om.verifyObjectVersion(ctx, bucket, key), nil
}

// verifyObjectVersion reads back a version of an object, the current one without
// versionID, and compares its content (after transparent decryption) to its
// stored ETag and size.
func (om *objectManager) verifyObjectVersion(ctx context.Context, bucket, key string, versionID ...string) *IntegrityResult {
	// Folder markers (keys ending with "/") are virtual directory placeholders.
	// They have no content file on storage, so there is nothing to verify.
	if strings.HasSuffix(key, "/") {
		return &IntegrityResult{
			Key:    key,
			Status: IntegritySkipped,
			Reason: "folder marker: not a data object",
		}
	}

	// Fetch metadata to get the stored ETag and expected size
	meta, err := om.metadataStore.GetObject(ctx, bucket, key, versionID...)
	if err != nil {
		return &IntegrityResult{
			Key:    key,
			Status: IntegrityError,
			Error:  fmt.Sprintf("metadata lookup failed: %v", err),
		}
	}

	storedETag := meta.ETag

	// Delete markers have an empty ETag — they are tombstones, not real objects.
	if storedETag == "" {
		return &IntegrityResult{
			Key:    key,
			Status: IntegritySkipped,
			Reason: "delete marker: not a data object",
		}
	}

	// Of data cut in shards this node checks the shard it holds, against the
	// digest it was stored with.
	if index, ok := om.shardHere(meta); ok {
		meta.Bucket, meta.Key = bucket, key
		return om.verifyShard(ctx, meta, index)
	}
	if _, sharded := ErasureLayout(meta); sharded {
		return &IntegrityResult{
			Key:        key,
			Status:     IntegritySkipped,
			StoredETag: storedETag,
			Reason:     "data cut in shards held by other nodes of the cluster",
		}
	}

	// The ETag of an object encrypted with a customer key is the MD5 of the
	// bytes stored: they are checked without the key. One written in parts has a
	// composite ETag, and the content is not readable here to recompute it.
	if meta.CustomerDEK != "" {
		if strings.Contains(storedETag, "-") {
			return &IntegrityResult{
				Key:        key,
				Status:     IntegritySkipped,
				StoredETag: storedETag,
				Reason:     "written in parts with a customer key: it cannot be read here",
			}
		}
		return om.verifyStoredBytes(ctx, bucket, key, meta)
	}

	// GetObject handles decryption transparently
	_, reader, err := om.GetObject(ctx, bucket, key, versionID...)
	var elsewhere *DataElsewhereError
	if errors.As(err, &elsewhere) {
		return &IntegrityResult{
			Key:        key,
			Status:     IntegritySkipped,
			StoredETag: storedETag,
			Reason:     "data held by other nodes of the cluster",
		}
	}
	if errors.Is(err, ErrObjectUnreadable) {
		return &IntegrityResult{
			Key:          key,
			Status:       IntegrityCorrupted,
			StoredETag:   storedETag,
			ExpectedSize: meta.Size,
			Reason:       err.Error(),
		}
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
			}
		}
		return &IntegrityResult{
			Key:    key,
			Status: IntegrityError,
			Error:  fmt.Sprintf("failed to open object: %v", err),
		}
	}
	defer reader.Close()

	computedETag, actualSize, err := contentETag(reader, meta.Parts)
	if err != nil {
		if ctx.Err() != nil {
			return &IntegrityResult{Key: key, Status: IntegrityError, Error: fmt.Sprintf("failed to read object data: %v", err)}
		}
		// Data at rest is encrypted and authenticated: a byte changed on disk
		// fails the read, it never reaches the digest.
		return &IntegrityResult{
			Key:          key,
			Status:       IntegrityCorrupted,
			StoredETag:   storedETag,
			ExpectedSize: meta.Size,
			ActualSize:   actualSize,
			Reason:       fmt.Sprintf("the data does not read back: %v", err),
		}
	}

	// An object written in parts before their sizes were kept has a composite
	// ETag nothing can recompute: its content read back authenticated, and its
	// size is what counts.
	sameContent := computedETag == storedETag
	if strings.Contains(storedETag, "-") && len(meta.Parts) == 0 {
		sameContent = true
		computedETag = storedETag
	}

	// Compare ETag and size
	if !sameContent || actualSize != meta.Size {
		return &IntegrityResult{
			Key:          key,
			Status:       IntegrityCorrupted,
			StoredETag:   storedETag,
			ComputedETag: computedETag,
			ExpectedSize: meta.Size,
			ActualSize:   actualSize,
			Reason:       "ETag or size mismatch",
		}
	}

	return &IntegrityResult{
		Key:          key,
		Status:       IntegrityOK,
		StoredETag:   storedETag,
		ComputedETag: computedETag,
		ExpectedSize: meta.Size,
		ActualSize:   actualSize,
	}
}

// contentETag reads an object's content and gives its ETag as S3 computes it and
// its size: the MD5 of the content or, for an object written in parts, the MD5 of
// the MD5s of its parts with their count.
func contentETag(r io.Reader, parts []metadata.PartRun) (string, int64, error) {
	if len(parts) == 0 {
		hasher := md5.New()
		size, err := io.Copy(hasher, r)
		return fmt.Sprintf("%x", hasher.Sum(nil)), size, err
	}
	composite := md5.New()
	var size int64
	count := 0
	for _, run := range parts {
		for i := 0; i < run.Count; i++ {
			part := md5.New()
			n, err := io.CopyN(part, r, run.Size)
			size += n
			if err != nil && err != io.EOF {
				return "", size, err
			}
			composite.Write(part.Sum(nil))
			count++
		}
	}
	rest, err := io.Copy(io.Discard, r)
	return fmt.Sprintf("%x-%d", composite.Sum(nil), count), size + rest, err
}

// verifyShard reads back shard index of the write entry is, held here: its
// bytes must decrypt and match the size and digest they were stored with.
func (om *objectManager) verifyShard(ctx context.Context, entry *metadata.ObjectMetadata, index int) *IntegrityResult {
	info := ShardOf(fromMetadataObject(entry), index, entry.LocationsGen)
	result := &IntegrityResult{Key: entry.Key, StoredETag: entry.ETag, ExpectedSize: info.Layout.ShardSize(info.CutSize())}
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

// VerifyBucketIntegrity verifies a page of the versions of a bucket: every
// version, as one that is not the latest is what a restore may need. prefix,
// marker and maxKeys page through them; NextMarker is opaque.
func (om *objectManager) VerifyBucketIntegrity(ctx context.Context, bucket, prefix, marker string, maxKeys int) (*BucketIntegrityReport, error) {
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	start := time.Now()

	// A version ID has no slash, a key may.
	versionMarker, keyMarker, _ := strings.Cut(marker, "/")
	page, err := om.metadataStore.ListObjectVersionsPage(ctx, bucket, prefix, "", keyMarker, versionMarker, maxKeys)
	if err != nil {
		return nil, fmt.Errorf("listing objects failed: %w", err)
	}

	report := &BucketIntegrityReport{Bucket: bucket}
	if page.IsTruncated {
		report.NextMarker = page.NextVersionIDMarker + "/" + page.NextKeyMarker
	}

	for _, version := range page.Versions {
		// A scan of millions of versions must not outlive a stop.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var pinned []string
		if version.VersionID != "" {
			pinned = []string{version.VersionID}
		}
		result := om.verifyObjectVersion(ctx, bucket, version.Key, pinned...)
		result.VersionID = version.VersionID

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

// verifyStoredBytes checks the bytes this node stores of the write entry is
// against its ETag, the MD5 of those bytes, and their size against the
// sidecar's.
func (om *objectManager) verifyStoredBytes(ctx context.Context, bucket, key string, entry *metadata.ObjectMetadata) *IntegrityResult {
	result := &IntegrityResult{Key: key, StoredETag: entry.ETag}
	if !om.holdsData(entry) {
		result.Status, result.Reason = IntegritySkipped, "data held by other nodes of the cluster"
		return result
	}
	reader, sidecar, _, err := om.GetObjectRaw(ctx, bucket, key, entry.VersionID)
	if err != nil {
		result.Status, result.Reason = IntegrityMissing, "object data not found on storage"
		return result
	}
	defer reader.Close()
	result.ExpectedSize, _ = strconv.ParseInt(sidecar["size"], 10, 64)
	hasher := md5.New()
	result.ActualSize, err = io.Copy(hasher, reader)
	if err != nil {
		result.Status, result.Error = IntegrityError, fmt.Sprintf("failed to read object data: %v", err)
		return result
	}
	result.ComputedETag = fmt.Sprintf("%x", hasher.Sum(nil))
	if result.ComputedETag != entry.ETag || result.ActualSize != result.ExpectedSize {
		result.Status, result.Reason = IntegrityCorrupted, "ETag or size mismatch"
		return result
	}
	result.Status = IntegrityOK
	return result
}
