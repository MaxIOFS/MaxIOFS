package logging

import (
	"net/http"
	"time"
)

// newHTTPOutputForTesting creates an HTTPOutput with a plain http.Client that
func newHTTPOutputForTesting(rawURL, authToken string, batchSize int, flushInterval time.Duration) *HTTPOutput {
	output := &HTTPOutput{
		url:           rawURL,
		authToken:     authToken,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		client:        &http.Client{Timeout: 10 * time.Second},
		buffer:        make([]*LogEntry, 0, batchSize),
		stopChan:      make(chan struct{}),
	}

	output.wg.Add(1)
	go output.flusher()

	return output
}
