package s3compat

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
)

// A write refused at the limit and one that ran out of disk answer the same.
func TestAFullDiskAnswers507(t *testing.T) {
	for name, err := range map[string]error{
		"refused": object.ErrDiskFull,
		"ran out": fmt.Errorf("failed to store object: %w", syscall.ENOSPC),
	} {
		t.Run(name, func(t *testing.T) {
			h := &Handler{}
			rr := httptest.NewRecorder()
			h.writeObjectWriteError(rr, httptest.NewRequest(http.MethodPut, "/b/k", nil), err, "b", "k")

			assert.Equal(t, http.StatusInsufficientStorage, rr.Code)
			assert.Contains(t, rr.Body.String(), "<Code>InsufficientStorage</Code>")
			assert.Equal(t, "InsufficientStorage", completionErrorCode(err))
		})
	}
}
