package encryption

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type cutShort struct{ r io.Reader }

func (c *cutShort) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

// A source that fails, as a body cut short does with io.ErrUnexpectedEOF,
// fails the encryption with its error; one that ends is encrypted whole, in
// chunks as before.
func TestAStreamCutShortIsNotEncryptedAsWhole(t *testing.T) {
	enc := NewAESGCMEncryptor(DefaultEncryptionConfig())
	key, err := enc.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("chunk"), 30_000) // more than two chunks

	_, err = enc.EncryptStream(&cutShort{r: bytes.NewReader(data)}, io.Discard, key)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("a source cut short encrypted with %v", err)
	}

	var sealed bytes.Buffer
	meta, err := enc.EncryptStream(io.MultiReader(bytes.NewReader(data[:70_000]), bytes.NewReader(data[70_000:])), &sealed, key)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Size != int64(len(data)) {
		t.Fatalf("encrypted %d of %d bytes", meta.Size, len(data))
	}
	if want := 12 + len(data) + 3*20; sealed.Len() != want {
		t.Fatalf("stored as %d bytes, not the %d of three chunks", sealed.Len(), want)
	}
	var plain bytes.Buffer
	if err := enc.DecryptStream(&sealed, &plain, key, meta); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain.Bytes(), data) {
		t.Fatal("what is decrypted is not what was encrypted")
	}
}
