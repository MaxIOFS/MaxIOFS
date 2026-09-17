package object

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMultipartLocksAllowIndependentParts(t *testing.T) {
	om := &objectManager{}
	first := om.lockUploadPart("upload", 1)
	second := make(chan struct{})
	go func() { defer close(second); defer om.lockUploadPart("upload", 2)() }()
	progress := false
	select {
	case <-second:
		progress = true
	case <-time.After(time.Second):
	}
	first()
	if !progress {
		<-second
	}
	require.True(t, progress, "different parts must progress concurrently")
	require.Empty(t, om.uploads)
}

func TestMultipartLocksExcludeConflictingOperations(t *testing.T) {
	for _, op := range []string{"same part", "complete or abort"} {
		t.Run(op, func(t *testing.T) {
			om := &objectManager{}
			first := om.lockUploadPart("upload", 1)
			entered := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				close(entered)
				if op == "same part" {
					defer om.lockUploadPart("upload", 1)()
				} else {
					defer om.lockUpload("upload")()
				}
			}()
			<-entered
			early := false
			select {
			case <-done:
				early = true
			case <-time.After(100 * time.Millisecond):
			}
			first()
			if !early {
				<-done
			}
			require.False(t, early)
			require.Empty(t, om.uploads)
		})
	}
}
