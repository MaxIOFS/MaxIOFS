package object

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// ShardInfo names one shard of a write cut in shards: the write by its
// bucket, key, version, ETag, size and times, the layout, the shard's index,
// and Gen, the number of the placement it was made for.
type ShardInfo struct {
	Bucket       string         `json:"bucket"`
	Key          string         `json:"key"`
	VersionID    string         `json:"version_id,omitempty"`
	Index        int            `json:"index"`
	ETag         string         `json:"etag"`
	Size         int64          `json:"size"`
	LastModified int64          `json:"last_modified"`
	WrittenAt    int64          `json:"written_at,omitempty"`
	Layout       erasure.Layout `json:"layout"`
	Gen          int64          `json:"gen"`
}

// ShardStore keeps the shards this node holds of writes cut in shards.
type ShardStore interface {
	// PutShard stores a shard, its whole bytes read from data, without
	// touching the write's entry.
	PutShard(ctx context.Context, info ShardInfo, data io.Reader) error
	// OpenShard opens from offset the shard this node holds of the write,
	// layout and index info names.
	OpenShard(ctx context.Context, info ShardInfo, offset int64) (io.ReadCloser, error)
	// DeleteShard removes the shard this node holds of the write, layout and
	// index info names.
	DeleteShard(ctx context.Context, info ShardInfo) error
}

// ErasureLayout is the layout of the shards of an entry's data, when it is
// cut in shards.
func ErasureLayout(meta *metadata.ObjectMetadata) (erasure.Layout, bool) {
	if meta == nil || meta.ECData <= 0 {
		return erasure.Layout{}, false
	}
	return erasure.Layout{Data: meta.ECData, Parity: meta.ECParity, Block: meta.ECBlock}, true
}

// Erasure is the layout of the shards of o's data, when it is cut in shards.
func (o *Object) Erasure() (erasure.Layout, bool) {
	if o == nil || o.ECData <= 0 {
		return erasure.Layout{}, false
	}
	return erasure.Layout{Data: o.ECData, Parity: o.ECParity, Block: o.ECBlock}, true
}

// ShardOf names shard index of o's write, made for the placement gen.
func ShardOf(o *Object, index int, gen int64) ShardInfo {
	layout, _ := o.Erasure()
	return ShardInfo{
		Bucket: o.Bucket, Key: o.Key, VersionID: o.VersionID, Index: index,
		ETag: o.ETag, Size: o.Size, LastModified: o.LastModified.Unix(), WrittenAt: o.WrittenAt,
		Layout: layout, Gen: gen,
	}
}

// isWriteOf reports whether s is a shard of the write entry is.
func (s ShardInfo) isWriteOf(entry *metadata.ObjectMetadata) bool {
	return s.ETag == entry.ETag && s.Size == entry.Size && s.LastModified == entry.LastModified.Unix() &&
		s.WrittenAt == entry.WrittenAt && sameVersion(s.VersionID, entry.VersionID)
}

// sameWrite reports whether two entries are of one write.
func sameWrite(a, b *metadata.ObjectMetadata) bool {
	return a != nil && b != nil && sameVersion(a.VersionID, b.VersionID) && a.ETag == b.ETag && a.Size == b.Size &&
		a.LastModified.Unix() == b.LastModified.Unix() && a.WrittenAt == b.WrittenAt
}

// sameShard reports whether s and o name one shard: of one write, cut alike.
func (s ShardInfo) sameShard(o ShardInfo) bool {
	return s.Bucket == o.Bucket && s.Key == o.Key && sameVersion(s.VersionID, o.VersionID) && s.Index == o.Index &&
		s.ETag == o.ETag && s.Size == o.Size && s.LastModified == o.LastModified && s.WrittenAt == o.WrittenAt &&
		s.Layout == o.Layout
}

// Sidecar fields of a shard: the write it is of, its layout and placement.
const (
	shardETagField         = "ec-etag"
	shardSizeField         = "ec-size"
	shardLastModifiedField = "ec-last-modified"
	shardWrittenAtField    = "ec-written-at"
	shardDataField         = "ec-data"
	shardParityField       = "ec-parity"
	shardBlockField        = "ec-block"
	shardGenField          = "ec-gen"
)

func (s ShardInfo) sidecar() map[string]string {
	return map[string]string{
		shardETagField:         s.ETag,
		shardSizeField:         strconv.FormatInt(s.Size, 10),
		shardLastModifiedField: strconv.FormatInt(s.LastModified, 10),
		shardWrittenAtField:    strconv.FormatInt(s.WrittenAt, 10),
		shardDataField:         strconv.Itoa(s.Layout.Data),
		shardParityField:       strconv.Itoa(s.Layout.Parity),
		shardBlockField:        strconv.Itoa(s.Layout.Block),
		shardGenField:          strconv.FormatInt(s.Gen, 10),
	}
}

// shardFromSidecar is the shard a stored shard's sidecar describes.
func shardFromSidecar(sidecar map[string]string, ref storage.ObjectRef) (ShardInfo, bool) {
	index, ok := ref.ShardIndex()
	if !ok || sidecar[shardETagField] == "" {
		return ShardInfo{}, false
	}
	s := ShardInfo{Bucket: ref.Bucket, Key: ref.Key, VersionID: ref.VersionID, Index: index, ETag: sidecar[shardETagField]}
	var errs [7]error
	s.Size, errs[0] = strconv.ParseInt(sidecar[shardSizeField], 10, 64)
	s.LastModified, errs[1] = strconv.ParseInt(sidecar[shardLastModifiedField], 10, 64)
	s.WrittenAt, errs[2] = strconv.ParseInt(sidecar[shardWrittenAtField], 10, 64)
	s.Layout.Data, errs[3] = strconv.Atoi(sidecar[shardDataField])
	s.Layout.Parity, errs[4] = strconv.Atoi(sidecar[shardParityField])
	s.Layout.Block, errs[5] = strconv.Atoi(sidecar[shardBlockField])
	s.Gen, errs[6] = strconv.ParseInt(sidecar[shardGenField], 10, 64)
	return s, errors.Join(errs[:]...) == nil
}

func (om *objectManager) shardRef(s ShardInfo) storage.ObjectRef {
	ref := om.objectRef(s.Bucket, s.Key)
	if s.VersionID != "" {
		ref = om.versionRef(s.Bucket, s.Key, s.VersionID)
	}
	return ref.ShardOf(s.Index)
}

// shardHere is the index of the shard this node holds of an entry's data,
// when the entry cuts it in shards and names this node.
func (om *objectManager) shardHere(meta *metadata.ObjectMetadata) (int, bool) {
	if _, ok := ErasureLayout(meta); !ok || om.localNode == nil {
		return 0, false
	}
	index := slices.Index(meta.Locations, om.localNode())
	return index, index >= 0
}

// PutShard stores shard info.Index of a write. The shard of a write older
// than the one held here without a version is not kept.
func (om *objectManager) PutShard(ctx context.Context, info ShardInfo, data io.Reader) error {
	if err := om.validateObjectName(info.Key); err != nil {
		return err
	}
	if !info.Layout.Valid() || info.Index < 0 || info.Index >= info.Layout.Width() {
		return fmt.Errorf("no shard %d in a layout of %d+%d", info.Index, info.Layout.Data, info.Layout.Parity)
	}
	if info.VersionID != "" {
		if err := validateReplicatedVersionID(info.VersionID); err != nil {
			return err
		}
	}
	if _, err := om.loadBucketMetadata(ctx, info.Bucket); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(om.config.Root, "maxiofs-upload-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	defer tempFile.Close()
	hasher := md5.New()
	n, err := io.Copy(io.MultiWriter(tempFile, hasher), data)
	if err != nil {
		return fmt.Errorf("failed to receive the shard: %w", err)
	}
	if want := info.Layout.ShardSize(info.Size); n != want {
		return fmt.Errorf("a shard of %d bytes is %d bytes long", want, n)
	}
	if err := errors.Join(tempFile.Sync(), tempFile.Close()); err != nil {
		return fmt.Errorf("failed to stage the shard: %w", err)
	}

	defer om.lockKey(info.Bucket, info.Key)()
	if info.VersionID == "" {
		current, err := om.metadataStore.GetObject(ctx, info.Bucket, info.Key)
		if err != nil && !errors.Is(err, metadata.ErrObjectNotFound) {
			return fmt.Errorf("failed to read the current entry: %w", err)
		}
		if laterWriteHere(current, time.Unix(info.LastModified, 0), info.WrittenAt) {
			return nil
		}
	}
	return om.storeEncryptedObject(ctx, om.shardRef(info), tempPath, info.sidecar(), n, hex.EncodeToString(hasher.Sum(nil)))
}

// OpenShard opens from offset the shard this node holds of the write, layout
// and index info names; ErrObjectNotFound when it holds none, or one of
// another write or layout.
func (om *objectManager) OpenShard(ctx context.Context, info ShardInfo, offset int64) (io.ReadCloser, error) {
	ref := om.shardRef(info)
	encrypted, sidecar, err := om.storage.Get(ctx, ref)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open the shard: %w", err)
	}
	if held, ok := shardFromSidecar(sidecar, ref); !ok || !held.sameShard(info) {
		encrypted.Close()
		return nil, ErrObjectNotFound
	}
	plain, err := om.decrypting(ctx, encrypted, sidecar)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, plain, offset); err != nil {
			plain.Close()
			return nil, fmt.Errorf("failed to read the shard to %d: %w", offset, err)
		}
	}
	return plain, nil
}

// DeleteShard removes the shard this node holds of the write, layout and
// index info names.
func (om *objectManager) DeleteShard(ctx context.Context, info ShardInfo) error {
	defer om.lockKey(info.Bucket, info.Key)()
	ref := om.shardRef(info)
	sidecar, err := om.storage.GetMetadata(ctx, ref)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if held, ok := shardFromSidecar(sidecar, ref); !ok || !held.sameShard(info) {
		return nil
	}
	if err := om.storage.Delete(ctx, ref); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
		return err
	}
	return nil
}

// holdsShard reports whether this node has shard index of the write entry is.
func (om *objectManager) holdsShard(ctx context.Context, entry *metadata.ObjectMetadata, index int) (bool, error) {
	ref := om.objectRef(entry.Bucket, entry.Key)
	if entry.VersionID != "" {
		ref = om.versionRef(entry.Bucket, entry.Key, entry.VersionID)
	}
	ref = ref.ShardOf(index)
	sidecar, err := om.storage.GetMetadata(ctx, ref)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	held, ok := shardFromSidecar(sidecar, ref)
	layout, _ := ErasureLayout(entry)
	return ok && held.isWriteOf(entry) && held.Layout == layout, nil
}

// dropStaleShards removes the shards held here of the write of ref that
// entry, the newest entry of it here, does not need: those of an earlier
// write, and those of its own write that its placement does not name here,
// unless made for a newer placement.
func (om *objectManager) dropStaleShards(ctx context.Context, entry *metadata.ObjectMetadata, ref storage.ObjectRef) {
	indexes, err := om.storage.Shards(ctx, ref)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": ref.Bucket, "key": ref.Key}).
			Warn("Failed to list the shards held of an object")
		return
	}
	for _, i := range indexes {
		shard := ref.ShardOf(i)
		sidecar, err := om.storage.GetMetadata(ctx, shard)
		if err != nil {
			continue
		}
		if held, ok := shardFromSidecar(sidecar, shard); ok && om.keepsShard(entry, held) {
			continue
		}
		if err := om.storage.Delete(ctx, shard); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
			logrus.WithError(err).WithFields(logrus.Fields{"bucket": ref.Bucket, "key": ref.Key, "shard": i}).
				Warn("Failed to remove a shard this node no longer holds")
		}
	}
}

// keepsShard reports whether this node keeps a shard it holds, entry being
// its newest entry of the shard's key.
func (om *objectManager) keepsShard(entry *metadata.ObjectMetadata, held ShardInfo) bool {
	if !held.isWriteOf(entry) {
		return CompareWrites(held.LastModified, held.WrittenAt, entry.LastModified.Unix(), entry.WrittenAt) > 0
	}
	if held.Gen > entry.LocationsGen {
		return true
	}
	index, here := om.shardHere(entry)
	layout, _ := ErasureLayout(entry)
	return here && index == held.Index && layout == held.Layout
}

// removeShards removes every shard held here of the write of ref.
func (om *objectManager) removeShards(ctx context.Context, ref storage.ObjectRef) {
	indexes, err := om.storage.Shards(ctx, ref)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": ref.Bucket, "key": ref.Key}).
			Warn("Failed to list the shards of a deleted object")
		return
	}
	for _, i := range indexes {
		if err := om.storage.Delete(ctx, ref.ShardOf(i)); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
			logrus.WithError(err).WithFields(logrus.Fields{"bucket": ref.Bucket, "key": ref.Key, "shard": i}).
				Warn("Failed to remove a shard of a deleted object")
		}
	}
}
