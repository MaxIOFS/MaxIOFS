package audit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Each retention cleanup is told to the task observer.
func TestARetentionCleanupIsObserved(t *testing.T) {
	m, cleanup := setupTestDB(t)
	defer cleanup()
	var runs []string
	m.SetTaskObserver(func(task string, _ time.Duration, failed bool) { runs = append(runs, fmt.Sprintf("%s:%v", task, failed)) })

	m.retentionCleanupWithSettings(context.Background(), 30)

	assert.Equal(t, []string{"audit_retention:false"}, runs)
}
