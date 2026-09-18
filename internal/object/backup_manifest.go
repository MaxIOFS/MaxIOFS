package object

import (
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
)

// The manifest shape lives in internal/rollback: the boot pass reads what the
// writer here produces, so both sides must never drift.
type objectBackupManifest = rollback.ObjectManifest

// committedObject is the index identity a retained copy is decided against.
func committedObject(entry *metadata.ObjectMetadata) *rollback.Committed {
	if entry == nil {
		return nil
	}
	return &rollback.Committed{Size: entry.Size, ETag: entry.ETag}
}

func committedPart(row *metadata.PartMetadata) *rollback.Committed {
	if row == nil {
		return nil
	}
	return &rollback.Committed{Size: row.Size, ETag: row.ETag}
}

func writeObjectBackupManifest(path string, ref storage.ObjectRef, metadata map[string]string, committed *rollback.Committed) error {
	return rollback.WriteObjectManifest(path, ref, metadata, committed)
}
