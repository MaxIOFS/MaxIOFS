package notifications

import (
	"net/http"

	"github.com/maxiofs/maxiofs/internal/metadata"
)

// newManagerForTesting creates a Manager with no SSRF restrictions.
// Only for use inside test files in this package; never call from production code.
func newManagerForTesting(store metadata.RawKVStore) *Manager {
	return &Manager{
		kvStore:              store,
		httpClient:           &http.Client{Timeout: webhookTimeout},
		configCache:          make(map[string]*NotificationConfiguration),
		bypassSSRFValidation: true,
	}
}
