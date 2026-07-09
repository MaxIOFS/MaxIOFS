package object

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
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
	if meta["content-type"] == "application/x-directory" {
		return false, nil
	}
	if action := om.migrationActionFor(meta); action == migrationSkip {
		return false, nil
	}

	// Serialise against concurrent writers to the same key for the whole
	// conversion (stage → rewrite → verify).
	defer om.lockKey(bucket, key)()

	// Re-check under the lock — a client PUT may have replaced the object
	// (new writes are always current-KEK envelope).
	meta, err = om.storage.GetMetadata(ctx, path)
	if err != nil {
		return false, nil
	}
	switch om.migrationActionFor(meta) {
	case migrationSkip:
		return false, nil
	case migrationRewrap:
		// Envelope wrapped with an old KEK version: only the wrapped DEK
		// changes — object data is never touched.
		return om.rewrapPathDEK(ctx, bucket, key, path, meta)
	}
	// migrationEncrypt: plaintext or legacy direct-encrypted → full envelope
	// rewrite below.
	isLegacyEncrypted := meta["encrypted"] == "true"

	// Stage the PLAINTEXT to a temp file, hashing as we copy (decrypting on
	// the fly for legacy direct-encrypted objects). For legacy objects the
	// original ciphertext is staged too, as the restore source.
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

	// restorePath is what gets written back if verification fails: the
	// plaintext staging for plaintext objects, or the raw ciphertext copy for
	// legacy ones (restoring plaintext under an encrypted sidecar would
	// corrupt the object).
	restorePath := tempPath
	hasher := md5.New()
	var stagedSize int64

	if isLegacyEncrypted {
		rawFile, rErr := os.CreateTemp(om.config.Root, "maxiofs-encmigrate-raw-*")
		if rErr != nil {
			reader.Close()
			return false, fmt.Errorf("failed to create raw staging file: %w", rErr)
		}
		rawPath := rawFile.Name()
		defer os.Remove(rawPath)
		restorePath = rawPath

		// Tee the ciphertext to the raw staging file while decrypting it into
		// the plaintext staging file.
		decryptKey, kErr := om.decryptionKeyFor(meta)
		if kErr != nil {
			reader.Close()
			rawFile.Close()
			tempFile.Close()
			return false, fmt.Errorf("failed to resolve legacy decryption key: %w", kErr)
		}
		tee := io.TeeReader(reader, rawFile)
		plainWriter := io.MultiWriter(tempFile, hasher)
		dErr := om.encryptor.DecryptStream(tee, &countingWriter{w: plainWriter, n: &stagedSize}, decryptKey, decryptMetaFor(meta))
		reader.Close()
		rawFile.Close()
		tempFile.Close()
		if dErr != nil {
			return false, fmt.Errorf("failed to decrypt legacy object for conversion: %w", dErr)
		}
	} else {
		stagedSize, err = io.Copy(io.MultiWriter(tempFile, hasher), reader)
		reader.Close()
		tempFile.Close()
		if err != nil {
			return false, fmt.Errorf("failed to stage plaintext: %w", err)
		}
	}
	stagedMD5 := hex.EncodeToString(hasher.Sum(nil))

	// Preserve the stored plaintext ETag (may be a multipart "<md5>-<N>"
	// value); legacy sidecars carry it in original-etag.
	originalETag := meta["original-etag"]
	if originalETag == "" {
		originalETag = meta["etag"]
	}
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

		// Real failure — restore the original bytes (plaintext staging, or the
		// raw ciphertext copy for legacy objects) with the original sidecar.
		restoreFile, rErr := os.Open(restorePath)
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
		return false, fmt.Errorf("verification failed, original restored: %w", verifyErr)
	}

	return true, nil
}

// migrationAction classifies what the worker must do with a stored file.
type migrationAction int

const (
	migrationSkip    migrationAction = iota // already current-KEK envelope
	migrationEncrypt                        // plaintext or legacy direct-encrypted → full envelope rewrite
	migrationRewrap                         // envelope with an old KEK version → re-wrap DEK only
)

// migrationActionFor inspects a sidecar and returns the required action.
func (om *objectManager) migrationActionFor(meta map[string]string) migrationAction {
	if meta["encrypted"] != "true" {
		return migrationEncrypt // plaintext
	}
	if meta["wrapped-dek"] == "" {
		return migrationEncrypt // legacy direct-encrypted (no DEK)
	}
	version, err := strconv.Atoi(meta["kek-version"])
	if err != nil {
		return migrationSkip // corrupt marker — leave for the integrity tooling
	}
	_, current := om.kekProvider.CurrentKEK()
	if version == current {
		return migrationSkip
	}
	return migrationRewrap
}

// rewrapPathDEK re-wraps an envelope object's DEK with the current KEK.
// Object data is never rewritten — only the sidecar changes, atomically
// (temp+rename), and either sidecar state decrypts correctly.
// Caller holds the per-key lock and passes the sidecar read under it.
func (om *objectManager) rewrapPathDEK(ctx context.Context, bucket, key, path string, meta map[string]string) (bool, error) {
	dek, err := om.decryptionKeyFor(meta)
	if err != nil {
		return false, fmt.Errorf("failed to unwrap DEK with old KEK: %w", err)
	}

	kekKey, kekVersion := om.kekProvider.CurrentKEK()
	wrapped, err := om.encryptor.Encrypt(dek, kekKey)
	if err != nil {
		return false, fmt.Errorf("failed to re-wrap DEK: %w", err)
	}

	metaCopy := make(map[string]string, len(meta))
	for k, v := range meta {
		metaCopy[k] = v
	}
	metaCopy["wrapped-dek"] = hex.EncodeToString(wrapped.Data)
	metaCopy["wrapped-dek-iv"] = hex.EncodeToString(wrapped.IV)
	metaCopy["kek-version"] = strconv.Itoa(kekVersion)

	if err := om.storage.SetMetadata(ctx, path, metaCopy); err != nil {
		return false, fmt.Errorf("failed to update sidecar with re-wrapped DEK: %w", err)
	}

	// Verify: re-read the sidecar and unwrap with the current KEK — the DEK
	// must be byte-identical (the data was never touched).
	verifyMeta, err := om.storage.GetMetadata(ctx, path)
	if err != nil {
		return false, fmt.Errorf("re-wrap verification read failed: %w", err)
	}
	verifyDEK, err := om.decryptionKeyFor(verifyMeta)
	if err != nil {
		return false, fmt.Errorf("re-wrap verification unwrap failed: %w", err)
	}
	if !bytes.Equal(dek, verifyDEK) {
		return false, fmt.Errorf("re-wrap verification failed: DEK mismatch after sidecar update")
	}

	logrus.WithFields(logrus.Fields{"bucket": bucket, "key": key, "kek_version": kekVersion}).
		Debug("Encryption migration: DEK re-wrapped to current KEK")
	return true, nil
}

// countingWriter counts bytes written through it.
type countingWriter struct {
	w io.Writer
	n *int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	*c.n += int64(n)
	return n, err
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
