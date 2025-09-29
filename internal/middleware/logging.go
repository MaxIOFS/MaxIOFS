package middleware

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// LoggingConfig holds logging middleware configuration
type LoggingConfig struct {
	// Logger is the custom logger to use. If nil, standard log package is used
	Logger Logger
	// LogFormat specifies the log format: "common", "combined", "json", or "custom"
	LogFormat string
	// CustomFormatter is used when LogFormat is "custom"
	CustomFormatter func(LogEntry) string
	// SkipPaths contains paths that should not be logged
	SkipPaths []string
	// LogBody indicates whether to log request/response bodies (not recommended for production)
	LogBody bool
	// MaxBodySize limits the body size to log (in bytes)
	MaxBodySize int64
}

// Logger interface for custom loggers
type Logger interface {
	Printf(format string, v ...interface{})
	Print(v ...interface{})
}

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp    time.Time
	Method       string
	URL          string
	Proto        string
	Status       int
	Size         int64
	Duration     time.Duration
	RemoteAddr   string
	UserAgent    string
	Referer      string
	RequestID    string
	UserID       string
	RequestBody  string
	ResponseBody string
}

// responseWriterWrapper wraps http.ResponseWriter to capture response information
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
	size       int64
	body       []byte
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriterWrapper) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = 200
	}
	size, err := rw.ResponseWriter.Write(b)
	rw.size += int64(size)

	// Capture response body if configured
	if cap(rw.body) > 0 && len(rw.body)+len(b) < cap(rw.body) {
		rw.body = append(rw.body, b...)
	}

	return size, err
}

// DefaultLoggingConfig returns the default logging configuration
func DefaultLoggingConfig() *LoggingConfig {
	return &LoggingConfig{
		LogFormat:   "common",
		SkipPaths:   []string{"/health", "/metrics"},
		LogBody:     false,
		MaxBodySize: 1024, // 1KB
	}
}

// VerboseLoggingConfig returns a verbose logging configuration for debugging
func VerboseLoggingConfig() *LoggingConfig {
	return &LoggingConfig{
		LogFormat:   "json",
		SkipPaths:   []string{},
		LogBody:     true,
		MaxBodySize: 4096, // 4KB
	}
}

// Logging returns a middleware that logs HTTP requests with default configuration
func Logging() func(http.Handler) http.Handler {
	return LoggingWithConfig(DefaultLoggingConfig())
}

// LoggingWithConfig returns a logging middleware with custom configuration
func LoggingWithConfig(config *LoggingConfig) func(http.Handler) http.Handler {
	logger := config.Logger
	if logger == nil {
		logger = log.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if path should be skipped
			for _, skipPath := range config.SkipPaths {
				if r.URL.Path == skipPath {
					next.ServeHTTP(w, r)
					return
				}
			}

			start := time.Now()

			// Capture request body if configured
			var requestBody string
			if config.LogBody && r.ContentLength > 0 && r.ContentLength <= config.MaxBodySize {
				if body := readRequestBody(r, config.MaxBodySize); body != "" {
					requestBody = body
				}
			}

			// Wrap response writer to capture response information
			rw := &responseWriterWrapper{
				ResponseWriter: w,
				statusCode:     0,
				size:           0,
			}

			// Prepare response body capture if configured
			if config.LogBody {
				rw.body = make([]byte, 0, config.MaxBodySize)
			}

			// Process request
			next.ServeHTTP(rw, r)

			// Calculate duration
			duration := time.Since(start)

			// Prepare log entry
			entry := LogEntry{
				Timestamp:   start,
				Method:      r.Method,
				URL:         r.RequestURI,
				Proto:       r.Proto,
				Status:      rw.statusCode,
				Size:        rw.size,
				Duration:    duration,
				RemoteAddr:  getRemoteAddr(r),
				UserAgent:   r.UserAgent(),
				Referer:     r.Referer(),
				RequestID:   getRequestID(r),
				UserID:      getUserID(r),
				RequestBody: requestBody,
			}

			if config.LogBody && len(rw.body) > 0 {
				entry.ResponseBody = string(rw.body)
			}

			// Log the entry
			logMessage := formatLogEntry(entry, config)
			logger.Print(logMessage)
		})
	}
}

// formatLogEntry formats the log entry according to the specified format
func formatLogEntry(entry LogEntry, config *LoggingConfig) string {
	switch config.LogFormat {
	case "common":
		return formatCommonLog(entry)
	case "combined":
		return formatCombinedLog(entry)
	case "json":
		return formatJSONLog(entry)
	case "custom":
		if config.CustomFormatter != nil {
			return config.CustomFormatter(entry)
		}
		return formatCommonLog(entry)
	default:
		return formatCommonLog(entry)
	}
}

// formatCommonLog formats log entry in Common Log Format
func formatCommonLog(entry LogEntry) string {
	return fmt.Sprintf("%s - %s [%s] \"%s %s %s\" %d %d",
		entry.RemoteAddr,
		getLogUser(entry),
		entry.Timestamp.Format("02/Jan/2006:15:04:05 -0700"),
		entry.Method,
		entry.URL,
		entry.Proto,
		entry.Status,
		entry.Size,
	)
}

// formatCombinedLog formats log entry in Combined Log Format
func formatCombinedLog(entry LogEntry) string {
	common := formatCommonLog(entry)
	return fmt.Sprintf("%s \"%s\" \"%s\" %dms",
		common,
		entry.Referer,
		entry.UserAgent,
		entry.Duration.Milliseconds(),
	)
}

// formatJSONLog formats log entry as JSON
func formatJSONLog(entry LogEntry) string {
	json := fmt.Sprintf(
		`{"timestamp":"%s","method":"%s","url":"%s","proto":"%s","status":%d,"size":%d,"duration_ms":%d,"remote_addr":"%s","user_agent":"%s","referer":"%s"`,
		entry.Timestamp.Format(time.RFC3339),
		entry.Method,
		entry.URL,
		entry.Proto,
		entry.Status,
		entry.Size,
		entry.Duration.Milliseconds(),
		entry.RemoteAddr,
		entry.UserAgent,
		entry.Referer,
	)

	if entry.RequestID != "" {
		json += fmt.Sprintf(`,"request_id":"%s"`, entry.RequestID)
	}

	if entry.UserID != "" {
		json += fmt.Sprintf(`,"user_id":"%s"`, entry.UserID)
	}

	if entry.RequestBody != "" {
		json += fmt.Sprintf(`,"request_body":"%s"`, escapeJSON(entry.RequestBody))
	}

	if entry.ResponseBody != "" {
		json += fmt.Sprintf(`,"response_body":"%s"`, escapeJSON(entry.ResponseBody))
	}

	json += "}"
	return json
}

// Helper functions

func readRequestBody(r *http.Request, maxSize int64) string {
	// This is a simplified implementation
	// In production, you'd want to handle this more carefully
	// to avoid consuming the body that the handler needs
	return "" // TODO: Implement safe body reading
}

func getRemoteAddr(r *http.Request) string {
	// Check for forwarded headers
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	return r.RemoteAddr
}

func getRequestID(r *http.Request) string {
	// Check common request ID headers
	if rid := r.Header.Get("X-Request-ID"); rid != "" {
		return rid
	}
	if rid := r.Header.Get("X-Trace-ID"); rid != "" {
		return rid
	}
	return ""
}

func getUserID(r *http.Request) string {
	// This would typically extract user ID from context or JWT token
	// For MVP, return empty string
	return ""
}

func getLogUser(entry LogEntry) string {
	if entry.UserID != "" {
		return entry.UserID
	}
	return "-"
}

func escapeJSON(s string) string {
	// Simple JSON escaping for basic characters
	result := ""
	for _, char := range s {
		switch char {
		case '"':
			result += `\"`
		case '\\':
			result += `\\`
		case '\n':
			result += `\n`
		case '\r':
			result += `\r`
		case '\t':
			result += `\t`
		default:
			result += string(char)
		}
	}
	return result
}

// S3LoggingMiddleware returns a logging middleware specifically configured for S3 operations
func S3LoggingMiddleware() func(http.Handler) http.Handler {
	config := &LoggingConfig{
		LogFormat: "custom",
		SkipPaths: []string{"/health"},
		LogBody:   false, // S3 bodies can be large, so disable by default
	}

	// Custom formatter for S3 operations
	config.CustomFormatter = func(entry LogEntry) string {
		return fmt.Sprintf(
			`[S3] %s %s %s - %d %dB %dms - %s`,
			entry.Timestamp.Format("2006-01-02 15:04:05"),
			entry.Method,
			entry.URL,
			entry.Status,
			entry.Size,
			entry.Duration.Milliseconds(),
			entry.RemoteAddr,
		)
	}

	return LoggingWithConfig(config)
}