package encryption

import (
	"bytes"
	"encoding/binary"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A damaged chunk length is refused as damage and never allocated: a length no
// chunk is written with would otherwise make a read take that much memory.
func TestADamagedChunkLengthIsNotAllocated(t *testing.T) {
	encryptor := NewAESGCMEncryptor(DefaultEncryptionConfig())
	key, err := encryptor.GenerateKey()
	require.NoError(t, err)
	var encrypted bytes.Buffer
	metadata, err := encryptor.EncryptStream(strings.NewReader(strings.Repeat("a", 100)), &encrypted, key)
	require.NoError(t, err)
	data := encrypted.Bytes()
	binary.BigEndian.PutUint32(data[12:16], 512<<20) // after the 12-byte nonce

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	err = encryptor.DecryptStream(bytes.NewReader(data), io.Discard, key, metadata)
	runtime.ReadMemStats(&after)

	assert.ErrorContains(t, err, "more than a chunk is written with")
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(16<<20), "the length was allocated")
}
