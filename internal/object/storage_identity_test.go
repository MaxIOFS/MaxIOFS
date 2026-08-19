package object

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func derivedMeta(size, etag string) map[string]string {
	return map[string]string{
		storage.MetadataGeneratedKey: "true",
		"size":                       size,
		"etag":                       etag,
	}
}

func TestConfirmStorageIdentity(t *testing.T) {
	cases := []struct {
		name     string
		recorded *Object
		derived  map[string]string
		servable bool
	}{
		{
			name:     "a real sidecar is not this rule's business",
			recorded: &Object{Size: 10, ETag: "abc"},
			derived:  map[string]string{"size": "10", "etag": "abc"},
			servable: true,
		},
		{
			name:     "size and digest both match",
			recorded: &Object{Size: 28, ETag: "d41d8cd98f00b204e9800998ecf8427e"},
			derived:  derivedMeta("28", "d41d8cd98f00b204e9800998ecf8427e"),
			servable: true,
		},
		{
			name:     "quoted recorded digest still matches",
			recorded: &Object{Size: 28, ETag: `"d41d8cd98f00b204e9800998ecf8427e"`},
			derived:  derivedMeta("28", "D41D8CD98F00B204E9800998ECF8427E"),
			servable: true,
		},
		{
			name:     "digest differs",
			recorded: &Object{Size: 28, ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			derived:  derivedMeta("28", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			servable: false,
		},
		{
			name:     "size differs",
			recorded: &Object{Size: 28, ETag: "abc"},
			derived:  derivedMeta("60", "abc"),
			servable: false,
		},
		// The ciphertext case: recovery records the ciphertext size and no
		// digest, so a rule that only looks for contradictions finds none.
		{
			name:     "recorded object has no digest",
			recorded: &Object{Size: 60, ETag: ""},
			derived:  derivedMeta("60", "e3d548c1"),
			servable: false,
		},
		{
			name:     "bytes on disk have no digest",
			recorded: &Object{Size: 60, ETag: "abc"},
			derived:  derivedMeta("60", ""),
			servable: false,
		},
		{
			name:     "nothing was recorded at all",
			recorded: nil,
			derived:  derivedMeta("60", "abc"),
			servable: false,
		},
		{
			name:     "multipart digest cannot identify the assembled bytes",
			recorded: &Object{Size: 28, ETag: "abc-3"},
			derived:  derivedMeta("28", "abc"),
			servable: false,
		},
		{
			name:     "size on disk is unreadable",
			recorded: &Object{Size: 28, ETag: "abc"},
			derived:  derivedMeta("", "abc"),
			servable: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verdict := confirmStorageIdentity(tc.recorded, tc.derived)
			assert.Equal(t, tc.servable, verdict.Servable)
			if !tc.servable {
				assert.NotEmpty(t, verdict.Reason, "a refusal must say what it could not establish")
			}
		})
	}
}

// The property that makes this a rule rather than a list of checks: anything
// the function cannot positively establish is refused.
func TestConfirmStorageIdentity_RefusesWhatItCannotProve(t *testing.T) {
	sizes := []string{"", "0", "28", "60", "notanumber"}
	etags := []string{"", "abc", "abc-3"}

	for _, size := range sizes {
		for _, etag := range etags {
			for _, recorded := range []*Object{nil, {Size: 28, ETag: ""}, {Size: 28, ETag: "abc"}, {Size: 28, ETag: "abc-3"}} {
				verdict := confirmStorageIdentity(recorded, derivedMeta(size, etag))
				if !verdict.Servable {
					continue
				}
				// The only way through: a recorded single-part digest that the
				// bytes on disk reproduce, at the same size.
				assert.NotNil(t, recorded)
				assert.Equal(t, "abc", etag)
				assert.Equal(t, "abc", recorded.ETag)
				assert.Equal(t, "28", size)
			}
		}
	}
}

// The scenario the sweep found: the sidecar is lost, recovery records the
// ciphertext's size and no digest, and the object is served as a successful
// download of raw AES-GCM bytes.
func TestGetObject_RefusesCiphertextRecordedWithoutADigest(t *testing.T) {
	ctx := context.Background()
	om, backend, metaStore := setupManagerWithConfigKey(t)

	const bucket, key = "sidecar-loss", "victim.txt"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "u"}))

	_, err := om.PutObject(ctx, bucket, key,
		strings.NewReader("the real plaintext"), http.Header{})
	require.NoError(t, err)

	objectPath := om.getObjectPath(bucket, key)
	onDisk, err := backend.GetMetadata(ctx, objectPath)
	require.NoError(t, err)
	ciphertextSize, err := strconv.ParseInt(onDisk["size"], 10, 64)
	require.NoError(t, err)

	// Lose the sidecar, then record what recovery would: the ciphertext's size
	// and no digest.
	require.NoError(t, os.Remove(filepath.Join(om.config.Root, objectPath+".metadata")))
	stored, err := metaStore.GetObject(ctx, bucket, key)
	require.NoError(t, err)
	stored.Size = ciphertextSize
	stored.ETag = ""
	require.NoError(t, metaStore.PutObject(ctx, stored))

	_, reader, err := om.GetObject(ctx, bucket, key)
	if reader != nil {
		reader.Close()
	}
	require.Error(t, err, "ciphertext that cannot be identified must not be served")
	assert.Contains(t, err.Error(), "unreadable")
}
