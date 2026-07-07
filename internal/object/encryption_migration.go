package object

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/maxiofs/maxiofs/pkg/encryption"
	"github.com/sirupsen/logrus"
)

// Plaintext → envelope conversion used by the background encryption worker.
//
// Safety model (this rewrites production object data, so every step protects
// the original):
//   - The plaintext is staged to a temp file first; the original is only
//     replaced by storage.Put's atomic temp+rename, so a failure at any point
//     leaves the original untouched.
//   - The per-key shard lock is held for the whole conversion, serialising
//     against the metadata section of concurrent PutObject calls, and the
//     sidecar is re-checked under the lock.
//   - After the rewrite the object is read back, decrypted and its MD5
//     compared with the staged plaintext. On mismatch: if the sidecar carries
//     a different wrapped DEK than the one we wrote, a concurrent client
//     overwrite won the rename race and the object is left alone; otherwise
//     the staged plaintext is restored.

// EncryptExistingObject converts a stored plaintext object (and all its
// stored versions) to envelope encryption. Objects already encrypted, folder
// markers and delete markers are skipped. Returns how many stored files were
// converted and how many were skipped.
func (om *objectManager) EncryptExistingObject(ctx context.Context, bucket, key string) (converted, skipped int, err error) {
	// Folder markers carry no data and are intentionally stored unencrypted.
	if strings.HasSuffix(key, "/") {
		return 0, 1, nil
	}

	// Collect every physical path this key owns: the current object plus all
	// stored versions (each version is its own file + sidecar).
	paths := make(map[string]struct{})

	metaObj, metaErr := om.metadataStore.GetObject(ctx, bucket, key)
	if metaErr == nil && metaObj != nil && !isMetadataDeleteMarker(metaObj) {
		if metaObj.VersionID != "" {
			paths[om.getVersionedObjectPath(bucket, key, metaObj.VersionID)] = struct{}{}
		} else {
			paths[om.getObjectPath(bucket, key)] = struct{}{}
		}
	} else if metaErr != nil {
		// No metadata entry — fall back to the plain path (sidecar-only objects).
		paths[om.getObjectPath(bucket, key)] = struct{}{}
	}

	if versions, vErr := om.metadataStore.GetObjectVersions(ctx, bucket, key); vErr == nil {
		for _, v := range versions {
			if v.VersionID == "" {
				continue
			}
			paths[om.getVersionedObjectPath(bucket, key, v.VersionID)] = struct{}{}
		}
	}

	for path := range paths {
		didConvert, cErr := om.convertPathToEnvelope(ctx, bucket, key, path)
		if cErr != nil {
			return converted, skipped, cErr
		}
		if didConvert {
			converted++
		} else {
			skipped++
		}
	}
	return converted, skipped, nil
}

// convertPathToEnvelope converts one stored file to envelope encryption.
// Returns (false, nil) when there is nothing to do (missing file, already
// encrypted, directory marker).
func (om *objectManager) convertPathToEnvelope(ctx context.Context, bucket, key, path string) (bool, error) {
	// Cheap pre-checks without the lock.
	exists, err := om.storage.Exists(ctx, path)
	if err != nil || !exists {
		return false, nil
	}
	meta, err := om.storage.GetMetadata(ctx, path)
	if err != nil {
		return false, nil
	}
	if meta["encrypted"] == "true" || meta["content-type"] == "application/x-directory" {
		return false, nil
	}

	// Serialise against concurrent writers to the same key for the whole
	// conversion (stage → rewrite → verify).
	defer om.lockKey(bucket, key)()

	// Re-check under the lock — a client PUT may have replaced the object
	// (new writes are always envelope-encrypted).
	meta, err = om.storage.GetMetadata(ctx, path)
	if err != nil {
		return false, nil
	}
	if meta["encrypted"] == "true" {
		return false, nil
	}

	// Stage the plaintext to a temp file, hashing as we copy. The staged copy
	// doubles as the restore source if verification fails.
	reader, _, err := om.storage.Get(ctx, path)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to open object for encryption: %w", err)
	}

	tempFile, err := os.CreateTemp(om.config.Root, "maxiofs-encmigrate-*")
	if err != nil {
		reader.Close()
		return false, fmt.Errorf("failed to create staging file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	hasher := md5.New()
	stagedSize, err := io.Copy(io.MultiWriter(tempFile, hasher), reader)
	reader.Close()
	tempFile.Close()
	if err != nil {
		return false, fmt.Errorf("failed to stage plaintext: %w", err)
	}
	stagedMD5 := hex.EncodeToString(hasher.Sum(nil))

	// Preserve the stored ETag (may be a multipart "<md5>-<N>" value); size is
	// the real staged byte count.
	originalETag := meta["etag"]
	if originalETag == "" {
		originalETag = stagedMD5
	}

	// Copy the existing sidecar entries so content-type / user metadata survive.
	metaCopy := make(map[string]string, len(meta)+8)
	for k, v := range meta {
		metaCopy[k] = v
	}

	if err := om.storeEncryptedObject(ctx, path, tempPath, metaCopy, stagedSize, originalETag); err != nil {
		return false, fmt.Errorf("failed to rewrite object encrypted: %w", err)
	}
	wroteDEK := metaCopy["wrapped-dek"]

	// Verify: read the final file back, decrypt, compare MD5 with the staged
	// plaintext.
	if verifyErr := om.verifyConvertedObject(ctx, path, stagedMD5); verifyErr != nil {
		// Did a concurrent client overwrite win the rename race? Then the
		// object on disk is theirs (valid, envelope) — leave it alone.
		if cur, mErr := om.storage.GetMetadata(ctx, path); mErr == nil && cur["wrapped-dek"] != "" && cur["wrapped-dek"] != wroteDEK {
			logrus.WithFields(logrus.Fields{"bucket": bucket, "key": key}).
				Info("Encryption migration: object was overwritten concurrently, leaving client version")
			return false, nil
		}

		// Real failure — restore the staged plaintext with the original sidecar.
		restoreFile, rErr := os.Open(tempPath)
		if rErr == nil {
			restoreMeta := make(map[string]string, len(meta))
			for k, v := range meta {
				restoreMeta[k] = v
			}
			rErr = om.storage.Put(ctx, path, restoreFile, restoreMeta)
			restoreFile.Close()
		}
		if rErr != nil {
			logrus.WithError(rErr).WithFields(logrus.Fields{"bucket": bucket, "key": key}).
				Error("Encryption migration: verification failed AND restore failed")
			return false, fmt.Errorf("verification failed (%v) and restore failed: %w", verifyErr, rErr)
		}
		return false, fmt.Errorf("verification failed, plaintext restored: %w", verifyErr)
	}

	return true, nil
}

// decryptMetaFor builds the stream-decryption metadata from a sidecar (same
// algorithm routing as GetObject: unmarked objects are legacy AES-CTR).
func decryptMetaFor(storageMetadata map[string]string) *encryption.EncryptionMetadata {
	alg := storageMetadata["x-amz-server-side-encryption-algorithm"]
	if alg == "" {
		alg = "AES-256-CTR"
	}
	return &encryption.EncryptionMetadata{Algorithm: alg}
}

// verifyConvertedObject decrypts the object at path and compares the
// plaintext MD5 with the expected value.
func (om *objectManager) verifyConvertedObject(ctx context.Context, path, expectedMD5 string) error {
	reader, meta, err := om.storage.Get(ctx, path)
	if err != nil {
		return fmt.Errorf("readback failed: %w", err)
	}
	defer reader.Close()

	if meta["encrypted"] != "true" {
		return fmt.Errorf("object is not marked encrypted after rewrite")
	}
	decryptKey, err := om.decryptionKeyFor(meta)
	if err != nil {
		return fmt.Errorf("failed to resolve decryption key: %w", err)
	}

	hasher := md5.New()
	if err := om.encryptor.DecryptStream(reader, hasher, decryptKey, decryptMetaFor(meta)); err != nil {
		return fmt.Errorf("decryption failed: %w", err)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expectedMD5 {
		return fmt.Errorf("plaintext MD5 mismatch after conversion: expected %s got %s", expectedMD5, got)
	}
	return nil
}
