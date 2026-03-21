package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// AccessLogEntry represents a single S3 request to be logged to the target bucket.
type AccessLogEntry struct {
	Timestamp   time.Time
	BucketName  string
	TenantID    string
	ObjectKey   string
	Operation   string
	RemoteIP    string
	UserAgent   string
	Requester   string
	RequestID   string
	HTTPStatus  int
	BytesSent   int64
}

// BucketAccessLogger asynchronously delivers S3 server access logs to the configured
// target bucket in AWS S3 access log format.
type BucketAccessLogger struct {
	bucketManager bucket.Manager
	objectManager object.Manager
	entries       chan AccessLogEntry
	done          chan struct{}
}

// NewBucketAccessLogger creates and starts the background log-delivery goroutine.
func NewBucketAccessLogger(bm bucket.Manager, om object.Manager) *BucketAccessLogger {
	l := &BucketAccessLogger{
		bucketManager: bm,
		objectManager: om,
		entries:       make(chan AccessLogEntry, 1000),
		done:          make(chan struct{}),
	}
	go l.run()
	return l
}

// Log queues an access log entry for async delivery. Drops silently if the buffer is full.
func (l *BucketAccessLogger) Log(entry AccessLogEntry) {
	select {
	case l.entries <- entry:
	default:
		// Buffer full — drop rather than block the request path
	}
}

// Stop flushes pending entries and stops the background goroutine.
func (l *BucketAccessLogger) Stop() {
	close(l.done)
}

func (l *BucketAccessLogger) run() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	var buffer []AccessLogEntry
	flush := func() {
		if len(buffer) == 0 {
			return
		}
		l.flushEntries(buffer)
		buffer = buffer[:0]
	}

	for {
		select {
		case entry := <-l.entries:
			buffer = append(buffer, entry)
			if len(buffer) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-l.done:
			// Drain remaining queued entries before exiting
			for {
				select {
				case entry := <-l.entries:
					buffer = append(buffer, entry)
				default:
					flush()
					return
				}
			}
		}
	}
}

// flushEntries groups entries by source bucket, looks up the logging target for each,
// formats lines in AWS S3 access log format, and writes a single object per bucket.
func (l *BucketAccessLogger) flushEntries(entries []AccessLogEntry) {
	// Group by (tenantID, bucketName)
	type bucketKey struct{ tenantID, bucket string }
	grouped := make(map[bucketKey][]AccessLogEntry)
	for _, e := range entries {
		k := bucketKey{e.TenantID, e.BucketName}
		grouped[k] = append(grouped[k], e)
	}

	ctx := context.Background()
	for k, bEntries := range grouped {
		cfg, err := l.bucketManager.GetLogging(ctx, k.tenantID, k.bucket)
		if err != nil || cfg == nil || cfg.TargetBucket == "" {
			continue // No logging configured for this bucket
		}

		var lines strings.Builder
		for _, e := range bEntries {
			requester := e.Requester
			if requester == "" {
				requester = "-"
			}
			objectKey := e.ObjectKey
			if objectKey == "" {
				objectKey = "-"
			}
			// AWS S3 access log format (simplified — no owner/signature fields)
			lines.WriteString(fmt.Sprintf(
				"%s %s [%s] %s %s %s %s %s - %d - %d - \"-\" \"%s\" -\n",
				k.bucket,
				k.bucket,
				e.Timestamp.UTC().Format("02/Jan/2006:15:04:05 +0000"),
				e.RemoteIP,
				requester,
				e.RequestID,
				e.Operation,
				objectKey,
				e.HTTPStatus,
				e.BytesSent,
				e.UserAgent,
			))
		}

		logKey := fmt.Sprintf("%s%s-%s",
			cfg.TargetPrefix,
			time.Now().UTC().Format("2006-01-02-15-04-05"),
			fmt.Sprintf("%d", time.Now().UnixNano()),
		)

		targetBucketPath := cfg.TargetBucket
		content := strings.NewReader(lines.String())
		hdrs := make(http.Header)
		hdrs.Set("Content-Type", "text/plain")
		hdrs.Set("Content-Length", fmt.Sprintf("%d", lines.Len()))
		if _, err := l.objectManager.PutObject(ctx, targetBucketPath, logKey, content, hdrs); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"sourceBucket": k.bucket,
				"targetBucket": cfg.TargetBucket,
				"logKey":       logKey,
			}).Warn("BucketAccessLogger: failed to write access log object")
		}
	}
}
