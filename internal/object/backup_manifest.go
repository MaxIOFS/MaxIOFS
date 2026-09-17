package object

import (
	"encoding/json"
	"os"

	"github.com/maxiofs/maxiofs/internal/storage"
)

type objectBackupManifest struct {
	Ref      storage.ObjectRef `json:"ref"`
	Metadata map[string]string `json:"metadata"`
}

func writeObjectBackupManifest(path string, ref storage.ObjectRef, metadata map[string]string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(objectBackupManifest{Ref: ref, Metadata: metadata}); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}
