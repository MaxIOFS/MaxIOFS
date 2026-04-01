package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
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
	wg            sync.WaitGroup
}

// NewBucketAccessLogger creates and starts the background log-delivery goroutine.
func NewBucketAccessLogger(bm bucket.Manager, om object.Manager) *BucketAccessLogger {
	l := &BucketAccessLogger{
		bucketManager: bm,
		objectManager: om,
		entries:       make(chan AccessLogEntry, 1000),
		done:          make(chan struct{}),
	}
	l.wg.Add(1)
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

// Stop signals the background goroutine to flush pending entries and exit,
// then waits for it to finish before returning. This ensures no writes happen
// to the metadata store after Stop() returns, so Pebble can be safely closed.
func (l *BucketAccessLogger) Stop() {
	close(l.done)
	l.wg.Wait()
}

func (l *BucketAccessLogger) run() {
	defer l.wg.Done()
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

// ============================================================================
// S3 Access Logging Middleware
// ============================================================================

// captureResponseWriter wraps http.ResponseWriter to capture status code and
// bytes written so the access logger can record them without interfering with
// the normal response path. It forwards Flush() so streaming handlers work.
type captureResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	bytes       int64
	wroteHeader bool
}

func (c *captureResponseWriter) WriteHeader(code int) {
	if !c.wroteHeader {
		c.statusCode = code
		c.wroteHeader = true
		c.ResponseWriter.WriteHeader(code)
	}
}

func (c *captureResponseWriter) Write(b []byte) (int, error) {
	if !c.wroteHeader {
		c.statusCode = http.StatusOK
		c.wroteHeader = true
	}
	n, err := c.ResponseWriter.Write(b)
	c.bytes += int64(n)
	return n, err
}

func (c *captureResponseWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// s3AccessLoggingMiddleware returns a gorilla/mux middleware that logs every
// S3 API request to the bucket's configured access-log target (if any).
// It must be registered after the auth middleware so the user is in context.
func (s *Server) s3AccessLoggingMiddleware() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			crw := &captureResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(crw, r)

			// Extract bucket from the first path segment.
			p := strings.TrimPrefix(r.URL.Path, "/")
			if p == "" || p == "health" || p == "ready" {
				return
			}
			parts := strings.SplitN(p, "/", 2)
			bucketName := parts[0]
			if bucketName == "" {
				return
			}
			var objectKey string
			if len(parts) > 1 {
				objectKey = parts[1]
			}

			// Auth info from context (available only after auth middleware ran).
			tenantID := ""
			requester := "-"
			if user, ok := auth.GetUserFromContext(r.Context()); ok {
				requester = user.Username
				tenantID = user.TenantID
			}

			// Remote IP (strip port).
			remoteIP := r.RemoteAddr
			if ip, _, err := net.SplitHostPort(remoteIP); err == nil {
				remoteIP = ip
			}

			requestID := w.Header().Get("x-amz-request-id")

			s.accessLogger.Log(AccessLogEntry{
				Timestamp:  start,
				BucketName: bucketName,
				TenantID:   tenantID,
				ObjectKey:  objectKey,
				Operation:  inferS3Operation(r, objectKey != ""),
				RemoteIP:   remoteIP,
				UserAgent:  r.UserAgent(),
				Requester:  requester,
				RequestID:  requestID,
				HTTPStatus: crw.statusCode,
				BytesSent:  crw.bytes,
			})
		})
	}
}

// inferS3Operation maps an HTTP method + query string to an AWS S3 access log
// operation name (e.g. REST.GET.OBJECT, REST.PUT.BUCKET).
func inferS3Operation(r *http.Request, isObject bool) string {
	method := r.Method
	q := r.URL.RawQuery

	if isObject {
		switch method {
		case http.MethodGet:
			if strings.Contains(q, "select") {
				return "REST.SELECT.OBJECT"
			}
			return "REST.GET.OBJECT"
		case http.MethodHead:
			return "REST.HEAD.OBJECT"
		case http.MethodPut:
			if strings.Contains(q, "uploadId") {
				return "REST.PUT.PART"
			}
			if r.Header.Get("x-amz-copy-source") != "" {
				return "REST.COPY.OBJECT"
			}
			return "REST.PUT.OBJECT"
		case http.MethodDelete:
			return "REST.DELETE.OBJECT"
		case http.MethodPost:
			if strings.Contains(q, "restore") {
				return "REST.POST.OBJECT.RESTORE"
			}
			if strings.Contains(q, "uploads") {
				return "REST.INIT.UPLOAD"
			}
			if strings.Contains(q, "uploadId") {
				return "REST.COMPLETE.UPLOAD"
			}
			return "REST.POST.OBJECT"
		}
	} else {
		switch method {
		case http.MethodGet:
			if strings.Contains(q, "uploads") {
				return "REST.GET.BUCKET.LISTMULTIPARTUPLOADS"
			}
			if strings.Contains(q, "versions") {
				return "REST.GET.BUCKET.VERSIONS"
			}
			return "REST.GET.BUCKET"
		case http.MethodHead:
			return "REST.HEAD.BUCKET"
		case http.MethodPut:
			return "REST.PUT.BUCKET"
		case http.MethodDelete:
			return "REST.DELETE.BUCKET"
		case http.MethodPost:
			if strings.Contains(q, "delete") {
				return "REST.POST.BUCKET.DELETE"
			}
			return "REST.POST.BUCKET"
		}
	}
	return method
}
