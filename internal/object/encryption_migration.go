package object

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/maxiofs/maxiofs/pkg/encryption"
	"github.com/sirupsen/logrus"
)

// EncryptExistingObject converts a stored plaintext object (and all its
func (om *objectManager) EncryptExistingObject(ctx context.Context, bucket, key string) (converted, skipped int, err error) {
	// Folder markers carry no data and are intentionally stored unencrypted.
	if strings.HasSuffix(key, "/") {
		return 0, 1, nil
	}

	// Collect every physical path this key owns: the current object plus all
	// stored versions (each version is its own file + sidecar).
	refs := make(map[storage.ObjectRef]struct{})

	metaObj, metaErr := om.metadataStore.GetObject(ctx, bucket, key)
	if metaErr == nil && metaObj != nil && !isMetadataDeleteMarker(metaObj) {
		if metaObj.VersionID != "" {
			refs[om.versionRef(bucket, key, metaObj.VersionID)] = struct{}{}
		} else {
			refs[om.objectRef(bucket, key)] = struct{}{}
		}
	} else if metaErr != nil {
		// No metadata entry — fall back to the plain path (sidecar-only objects).
		refs[om.objectRef(bucket, key)] = struct{}{}
	}

	if versions, vErr := om.metadataStore.GetObjectVersions(ctx, bucket, key); vErr == nil {
		for _, v := range versions {
			if v.VersionID == "" {
				continue
			}
			refs[om.versionRef(bucket, key, v.VersionID)] = struct{}{}
		}
	}

	for ref := range refs {
		didConvert, cErr := om.convertPathToEnvelope(ctx, bucket, key, ref)
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
func (om *objectManager) convertPathToEnvelope(ctx context.Context, bucket, key string, ref storage.ObjectRef) (converted bool, resultErr error) {
	// Cheap pre-checks without the lock.
	exists, err := om.storage.Exists(ctx, ref)
	if err != nil || !exists {
		return false, nil
	}
	meta, err := om.storage.GetMetadata(ctx, ref)
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
	meta, err = om.storage.GetMetadata(ctx, ref)
	if err != nil {
		return false, nil
	}
	switch om.migrationActionFor(meta) {
	case migrationSkip:
		return false, nil
	case migrationRewrap:
		// Envelope wrapped with an old KEK version: only the wrapped DEK
		// changes — object data is never touched.
		return om.rewrapPathDEK(ctx, bucket, key, ref, meta)
	}
	// migrationEncrypt: plaintext or legacy direct-encrypted → full envelope
	// rewrite below.
	isLegacyEncrypted := meta["encrypted"] == "true"

	reader, _, err := om.storage.Get(ctx, ref)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to open object for encryption: %w", err)
	}

	prefix := rollback.ObjectPrefix
	if isLegacyEncrypted {
		prefix = "maxiofs-encmigrate-"
	}
	tempFile, err := os.CreateTemp(om.config.Root, prefix+"*")
	if err != nil {
		reader.Close()
		return false, fmt.Errorf("failed to create staging file: %w", err)
	}
	tempPath := tempFile.Name()
	restorePath := tempPath
	keepBackup := false
	defer func() {
		if !keepBackup || tempPath != restorePath {
			os.Remove(tempPath)
		}
	}()
	defer tempFile.Close()
	hasher := md5.New()
	var stagedSize int64

	if isLegacyEncrypted {
		rawFile, rErr := os.CreateTemp(om.config.Root, rollback.ObjectPrefix+"*")
		if rErr != nil {
			reader.Close()
			return false, fmt.Errorf("failed to create raw staging file: %w", rErr)
		}
		rawPath := rawFile.Name()
		defer func() {
			if !keepBackup {
				os.Remove(rawPath)
			}
		}()
		defer rawFile.Close()
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
		dErr = errors.Join(dErr, rawFile.Sync(), rawFile.Close(), tempFile.Close())
		if dErr != nil {
			return false, fmt.Errorf("failed to decrypt legacy object for conversion: %w", dErr)
		}
	} else {
		stagedSize, err = io.Copy(io.MultiWriter(tempFile, hasher), reader)
		reader.Close()
		err = errors.Join(err, tempFile.Sync(), tempFile.Close())
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
	committed, err := om.metadataStore.GetObject(ctx, bucket, key, ref.VersionID)
	if err != nil && !errors.Is(err, metadata.ErrObjectNotFound) && !errors.Is(err, metadata.ErrVersionNotFound) {
		return false, err
	}
	manifestPath := restorePath + rollback.ManifestSuffix
	if err := writeObjectBackupManifest(manifestPath, ref, meta, committedObject(committed)); err != nil {
		os.Remove(manifestPath)
		return false, err
	}
	defer func() {
		if !keepBackup {
			os.Remove(manifestPath)
		}
	}()
	verified := false
	defer func() {
		if verified {
			return
		}
		keepBackup = true
		backup, err := os.Open(restorePath)
		if err == nil {
			err = om.storage.Put(context.WithoutCancel(ctx), ref, backup, meta)
			backup.Close()
		}
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("restore failed; backup retained at %s: %w", restorePath, err))
			return
		}
		keepBackup = false
	}()

	if err := om.storeEncryptedObject(ctx, ref, tempPath, metaCopy, stagedSize, originalETag); err != nil {
		return false, fmt.Errorf("failed to rewrite object encrypted: %w", err)
	}
	if err := om.verifyConvertedObject(ctx, ref, stagedMD5); err != nil {
		return false, fmt.Errorf("verification failed: %w", err)
	}
	verified = true

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
func (om *objectManager) rewrapPathDEK(ctx context.Context, bucket, key string, ref storage.ObjectRef, meta map[string]string) (bool, error) {
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

	if err := om.storage.SetMetadata(ctx, ref, metaCopy); err != nil {
		return false, fmt.Errorf("failed to update sidecar with re-wrapped DEK: %w", err)
	}

	// Verify: re-read the sidecar and unwrap with the current KEK — the DEK
	// must be byte-identical (the data was never touched).
	verifyMeta, err := om.storage.GetMetadata(ctx, ref)
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
func (om *objectManager) verifyConvertedObject(ctx context.Context, ref storage.ObjectRef, expectedMD5 string) error {
	reader, meta, err := om.storage.Get(ctx, ref)
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
