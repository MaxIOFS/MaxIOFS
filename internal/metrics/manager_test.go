package metrics

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/config"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestManager(t *testing.T) *metricsManager {
	t.Helper()
	manager, ok := NewManagerWithStore(config.MetricsConfig{Enable: true, Interval: 10}, "", nil).(*metricsManager)
	require.True(t, ok)
	return manager
}

// sample is the value of the series of name whose labels are exactly labels:
// a counter's or gauge's value, a histogram's count. ok is false when there
// is no such series.
func sample(t *testing.T, m *metricsManager, name string, labels map[string]string) (value float64, ok bool) {
	t.Helper()
	families, err := m.registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if !sameLabels(metric.GetLabel(), labels) {
				continue
			}
			switch {
			case metric.Counter != nil:
				return metric.GetCounter().GetValue(), true
			case metric.Gauge != nil:
				return metric.GetGauge().GetValue(), true
			case metric.Histogram != nil:
				return float64(metric.GetHistogram().GetSampleCount()), true
			}
		}
	}
	return 0, false
}

func sameLabels(got []*dto.LabelPair, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, pair := range got {
		if v, ok := want[pair.GetName()]; !ok || v != pair.GetValue() {
			return false
		}
	}
	return true
}

// labelValues is every label value of every series of name.
func labelValues(t *testing.T, m *metricsManager, name string) []string {
	t.Helper()
	families, err := m.registry.Gather()
	require.NoError(t, err)
	var values []string
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, pair := range metric.GetLabel() {
				values = append(values, pair.GetValue())
			}
		}
	}
	return values
}

// s3Router is a router with named routes, as the S3 API registers them, behind
// the metrics middleware.
func s3Router(m *metricsManager, handler http.HandlerFunc) *mux.Router {
	router := mux.NewRouter()
	router.Use(m.Middleware())
	bucket := router.PathPrefix("/{bucket}").Subrouter()
	bucket.HandleFunc("", handler).Methods("GET").Name("ListObjects")
	object := bucket.PathPrefix("/{object:.+}").Subrouter()
	object.HandleFunc("", handler).Methods("GET", "HEAD").Name("GetObject")
	return router
}

func serve(router http.Handler, method, target string, signed bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if signed {
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20261013/us-east-1/s3/aws4_request")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestNewManager(t *testing.T) {
	manager := NewManagerWithStore(config.MetricsConfig{Enable: true, Path: "/metrics", Interval: 10}, "", nil)
	require.NotNil(t, manager)
	assert.False(t, manager.IsHealthy(), "not started")
}

func TestNewManager_Disabled(t *testing.T) {
	manager := NewManagerWithStore(config.MetricsConfig{Enable: false}, "", nil)
	_, ok := manager.(*noopManager)
	assert.True(t, ok, "disabled manager should be noopManager")
}

// Requests are counted by the S3 operation they are, not by their path: each
// object key would be a new series.
func TestS3RequestsAreCountedByOperationNotPath(t *testing.T) {
	m := newTestManager(t)
	router := s3Router(m, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "data") })
	for _, key := range []string{"photos/one.jpg", "photos/two.jpg", "three"} {
		require.Equal(t, http.StatusOK, serve(router, "GET", "/media/"+key, true).Code)
	}
	require.Equal(t, http.StatusOK, serve(router, "GET", "/media", true).Code)

	count, ok := sample(t, m, "maxiofs_http_requests_total", map[string]string{"method": "GET", "operation": "GetObject", "status": "200"})
	require.True(t, ok)
	assert.EqualValues(t, 3, count)
	count, _ = sample(t, m, "maxiofs_s3_operations_total", map[string]string{"operation": "GetObject", "bucket": "media", "status": "success"})
	assert.EqualValues(t, 3, count)
	count, _ = sample(t, m, "maxiofs_s3_operation_duration_seconds", map[string]string{"operation": "ListObjects"})
	assert.EqualValues(t, 1, count)
	count, _ = sample(t, m, "maxiofs_http_response_size_bytes", map[string]string{"method": "GET", "operation": "GetObject"})
	assert.EqualValues(t, 3, count)
	for _, name := range []string{"maxiofs_http_requests_total", "maxiofs_http_request_duration_seconds", "maxiofs_s3_operations_total", "maxiofs_s3_operation_duration_seconds"} {
		for _, value := range labelValues(t, m, name) {
			assert.NotContains(t, value, "photos", "%s carries a key", name)
		}
	}
}

// A request that matched no named route is counted as Unknown.
func TestARequestWithoutARouteNameIsUnknown(t *testing.T) {
	m := newTestManager(t)
	router := mux.NewRouter()
	router.Use(m.Middleware())
	router.HandleFunc("/{bucket}", func(w http.ResponseWriter, r *http.Request) {}).Methods("PUT")
	serve(router, "PUT", "/made", true)
	_, ok := sample(t, m, "maxiofs_http_requests_total", map[string]string{"method": "PUT", "operation": "Unknown", "status": "200"})
	assert.True(t, ok)
}

// A refusal (4xx), such as a backup tool's probe for a key it does not have,
// is counted apart from an error (5xx), each by its S3 error code; a HEAD
// answer has no body and is counted by its status.
func TestS3RefusalsAreNotErrors(t *testing.T) {
	m := newTestManager(t)
	status, code := http.StatusNotFound, "NoSuchKey"
	router := s3Router(m, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>`+code+`</Code><Message>m</Message></Error>`)
		}
	})
	serve(router, "GET", "/media/missing", true)
	serve(router, "HEAD", "/media/missing", true)
	status, code = http.StatusInternalServerError, "InternalError"
	serve(router, "GET", "/media/broken", true)

	for _, c := range []struct {
		name   string
		labels map[string]string
		want   float64
	}{
		{"maxiofs_s3_refused_total", map[string]string{"operation": "GetObject", "bucket": "media", "error_type": "NoSuchKey"}, 1},
		{"maxiofs_s3_refused_total", map[string]string{"operation": "GetObject", "bucket": "media", "error_type": "404"}, 1},
		{"maxiofs_s3_errors_total", map[string]string{"operation": "GetObject", "bucket": "media", "error_type": "InternalError"}, 1},
		{"maxiofs_s3_operations_total", map[string]string{"operation": "GetObject", "bucket": "media", "status": "refused"}, 2},
		{"maxiofs_s3_operations_total", map[string]string{"operation": "GetObject", "bucket": "media", "status": "error"}, 1},
		{"maxiofs_http_requests_total", map[string]string{"method": "HEAD", "operation": "GetObject", "status": "404"}, 1},
	} {
		count, _ := sample(t, m, c.name, c.labels)
		assert.EqualValues(t, c.want, count, "%s%v", c.name, c.labels)
	}
	_, ok := sample(t, m, "maxiofs_s3_errors_total", map[string]string{"operation": "GetObject", "bucket": "media", "error_type": "NoSuchKey"})
	assert.False(t, ok, "a refusal is no error")
}

// The counters kept from before refusals were counted apart are restored but
// their errors: that total mixed refusals in and cannot be split. The ones
// kept since are restored whole.
func TestAKeptErrorTotalThatMixedRefusalsInIsNotRestored(t *testing.T) {
	store := createTestPebbleStore(t)
	before := `{"total_requests":100,"total_errors":40,"total_latency_ms":500,"latency_count":100,"server_start_time":"2026-01-01T00:00:00Z"}`
	require.NoError(t, store.PutRaw(context.Background(), "metrics:persisted_state", []byte(before)))
	restored := func() map[string]interface{} {
		t.Helper()
		m, ok := NewManagerWithStore(config.MetricsConfig{Enable: true, Interval: 10}, "", store).(*metricsManager)
		require.True(t, ok)
		snapshot, err := m.GetS3MetricsSnapshot()
		require.NoError(t, err)
		snapshot["manager"] = m
		return snapshot
	}

	snapshot := restored()
	assert.EqualValues(t, 100, snapshot["totalRequests"])
	assert.EqualValues(t, 0, snapshot["totalErrors"])

	m := snapshot["manager"].(*metricsManager)
	atomic.StoreUint64(&m.totalErrors, 3)
	atomic.StoreUint64(&m.totalRefused, 7)
	require.NoError(t, m.persistCounters())
	snapshot = restored()
	assert.EqualValues(t, 3, snapshot["totalErrors"])
	assert.EqualValues(t, 7, snapshot["totalRefused"])
}

// A refused request does not name its bucket: anyone can send any name, and
// each would be a new series.
func TestARefusedRequestDoesNotNameItsBucket(t *testing.T) {
	m := newTestManager(t)
	status, code := http.StatusForbidden, "AccessDenied"
	router := s3Router(m, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "<Error><Code>"+code+"</Code></Error>")
	})
	serve(router, "GET", "/guessed-name-1/key", false)
	serve(router, "GET", "/guessed-name-2/key", true)
	code = "NoSuchBucket"
	serve(router, "GET", "/guessed-name-3", true)
	status, code = http.StatusInternalServerError, "InternalError"
	serve(router, "GET", "/guessed-name-4/key", false)
	serve(router, "GET", "/existing/key", true)

	for _, family := range []string{"maxiofs_s3_errors_total", "maxiofs_s3_refused_total"} {
		for _, value := range labelValues(t, m, family) {
			assert.NotContains(t, value, "guessed", family)
		}
	}
	count, _ := sample(t, m, "maxiofs_s3_errors_total", map[string]string{"operation": "GetObject", "bucket": "existing", "error_type": "InternalError"})
	assert.EqualValues(t, 1, count, "a signed request that failed past authentication names its bucket")
	count, _ = sample(t, m, "maxiofs_s3_refused_total", map[string]string{"operation": "GetObject", "bucket": "", "error_type": "AccessDenied"})
	assert.EqualValues(t, 2, count)
}

// A scrape reads every bucket's usage; a bucket that is gone leaves the
// gauges.
func TestAScrapeReadsBucketUsage(t *testing.T) {
	m := newTestManager(t)
	usage := []BucketUsage{{Name: "kept", Objects: 3, Bytes: 300}, {Name: "deleted", Objects: 1, Bytes: 10}}
	m.SetBucketUsageProvider(func() ([]BucketUsage, error) { return usage, nil })
	scrape := func() string {
		w := httptest.NewRecorder()
		m.GetMetricsHandler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
		require.Equal(t, http.StatusOK, w.Code)
		return w.Body.String()
	}

	body := scrape()
	assert.Contains(t, body, `maxiofs_bucket_bytes_total{bucket="kept"} 300`)
	assert.Contains(t, body, `maxiofs_bucket_objects_total{bucket="deleted"} 1`)

	usage = usage[:1]
	body = scrape()
	assert.Contains(t, body, `maxiofs_bucket_objects_total{bucket="kept"} 3`)
	assert.NotContains(t, body, `bucket="deleted"`)

	// A failed read keeps the last values rather than emptying them.
	m.SetBucketUsageProvider(func() ([]BucketUsage, error) { return nil, errors.New("store closed") })
	assert.Contains(t, scrape(), `maxiofs_bucket_bytes_total{bucket="kept"} 300`)
}

func TestAScrapeReadsTheCache(t *testing.T) {
	m := newTestManager(t)
	m.SetCacheStatsProvider(func() (int64, int64, int64) { return 75, 25, 8 << 20 })
	m.GetMetricsHandler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/metrics", nil))
	rate, _ := sample(t, m, "maxiofs_cache_hit_rate", map[string]string{})
	assert.InDelta(t, 0.75, rate, 0.0001)
	size, _ := sample(t, m, "maxiofs_cache_size_bytes", map[string]string{})
	assert.EqualValues(t, 8<<20, size)
}

// Each update replaces the previous counts: a bucket that holds no retained
// object any more leaves the gauge.
func TestRetentionCountsReplaceThePreviousOnes(t *testing.T) {
	m := newTestManager(t)
	m.UpdateRetentionMetrics(map[string]RetentionCount{"vault": {Governance: 2, Compliance: 5}, "old": {Governance: 1}})
	count, _ := sample(t, m, "maxiofs_object_lock_retention_objects_total", map[string]string{"bucket": "vault", "mode": "compliance"})
	assert.EqualValues(t, 5, count)

	m.UpdateRetentionMetrics(map[string]RetentionCount{"vault": {Governance: 3}})
	_, ok := sample(t, m, "maxiofs_object_lock_retention_objects_total", map[string]string{"bucket": "old", "mode": "governance"})
	assert.False(t, ok)
	count, _ = sample(t, m, "maxiofs_object_lock_retention_objects_total", map[string]string{"bucket": "vault", "mode": "governance"})
	assert.EqualValues(t, 3, count)
}

func TestAuthAttemptsCountFailuresByReason(t *testing.T) {
	m := newTestManager(t)
	m.RecordAuthAttempt("s3_sigv4", true, "")
	m.RecordAuthAttempt("s3_sigv4", false, "SignatureDoesNotMatch")
	m.RecordAuthAttempt("console_password", false, "invalid_credentials")

	count, _ := sample(t, m, "maxiofs_auth_attempts_total", map[string]string{"method": "s3_sigv4", "status": "success"})
	assert.EqualValues(t, 1, count)
	count, _ = sample(t, m, "maxiofs_auth_attempts_total", map[string]string{"method": "s3_sigv4", "status": "failure"})
	assert.EqualValues(t, 1, count)
	count, _ = sample(t, m, "maxiofs_auth_failures_total", map[string]string{"method": "console_password", "reason": "invalid_credentials"})
	assert.EqualValues(t, 1, count)
	_, ok := sample(t, m, "maxiofs_auth_failures_total", map[string]string{"method": "s3_sigv4", "reason": ""})
	assert.False(t, ok, "a success is no failure")
}

func TestOperationRecorders(t *testing.T) {
	m := newTestManager(t)
	m.RecordStorageOperation("Get", StorageNotFound, time.Millisecond)
	m.RecordObjectOperation("PutObject", "media", 4096, time.Millisecond)
	m.RecordBucketOperation("CreateBucket", "media", true)
	m.RecordObjectLockOperation("PutObjectRetention", "media", false)
	m.RecordBackgroundTask("lifecycle", time.Second, true)
	m.RecordSystemEvent("disk_warning")

	for _, c := range []struct {
		name   string
		labels map[string]string
	}{
		{"maxiofs_storage_operations_total", map[string]string{"operation": "Get", "status": "not_found"}},
		{"maxiofs_storage_operation_duration_seconds", map[string]string{"operation": "Get"}},
		{"maxiofs_object_operations_total", map[string]string{"operation": "PutObject", "bucket": "media"}},
		{"maxiofs_object_operation_duration_seconds", map[string]string{"operation": "PutObject"}},
		{"maxiofs_object_size_bytes", map[string]string{"operation": "PutObject"}},
		{"maxiofs_bucket_operations_total", map[string]string{"operation": "CreateBucket", "bucket": "media", "status": "success"}},
		{"maxiofs_object_lock_operations_total", map[string]string{"operation": "PutObjectRetention", "bucket": "media", "status": "failure"}},
		{"maxiofs_background_tasks_total", map[string]string{"task_type": "lifecycle", "status": "success"}},
		{"maxiofs_background_task_duration_seconds", map[string]string{"task_type": "lifecycle"}},
		{"maxiofs_system_events_total", map[string]string{"event_type": "disk_warning"}},
	} {
		count, ok := sample(t, m, c.name, c.labels)
		assert.True(t, ok, c.name)
		assert.EqualValues(t, 1, count, c.name)
	}
}

func TestGetMetricsSnapshot(t *testing.T) {
	snapshot, err := newTestManager(t).GetMetricsSnapshot()
	require.NoError(t, err)
	assert.Contains(t, snapshot, "timestamp")
	assert.Contains(t, snapshot, "namespace")
}

// The console's S3 totals count what the middleware sees.
func TestGetS3MetricsSnapshot(t *testing.T) {
	m := newTestManager(t)
	router := s3Router(m, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "missing"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "broken"):
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	serve(router, "GET", "/media/key", true)
	serve(router, "GET", "/media/missing", true)
	serve(router, "GET", "/media/broken", true)

	snapshot, err := m.GetS3MetricsSnapshot()
	require.NoError(t, err)
	assert.EqualValues(t, 3, snapshot["totalRequests"])
	assert.EqualValues(t, 1, snapshot["totalErrors"], "a refusal (4xx) is no error")
	assert.EqualValues(t, 1, snapshot["totalRefused"])
	assert.Contains(t, snapshot, "avgLatency")
	assert.Contains(t, snapshot, "requestsPerSec")
}

func TestIsHealthy(t *testing.T) {
	manager := newTestManager(t)
	assert.False(t, manager.IsHealthy())
	require.NoError(t, manager.Start(context.Background()))
	assert.True(t, manager.IsHealthy())
	manager.Stop()
	assert.False(t, manager.IsHealthy())
}

func TestStartStop(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()
	require.NoError(t, manager.Start(ctx))
	assert.Error(t, manager.Start(ctx), "already started")
	manager.Stop()
	// Stopping again is a no-op: two shutdown paths can reach it.
	assert.NotPanics(t, func() { manager.Stop() })
}

func TestReset(t *testing.T) {
	assert.NoError(t, newTestManager(t).Reset())
}

func TestNoopManager(t *testing.T) {
	noop := &noopManager{}
	noop.RecordStorageOperation("Get", StorageSuccess, 0)
	noop.RecordObjectOperation("PutObject", "bucket", 0, 0)
	noop.RecordAuthAttempt("s3_sigv4", true, "")
	noop.RecordSystemEvent("event")
	noop.RecordBucketOperation("CreateBucket", "bucket", true)
	noop.RecordObjectLockOperation("PutObjectRetention", "bucket", true)
	noop.UpdateRetentionMetrics(nil)
	noop.RecordBackgroundTask("task", 0, true)

	assert.NotNil(t, noop.GetMetricsHandler())
	assert.True(t, noop.IsHealthy())
	assert.NoError(t, noop.Reset())
	assert.NoError(t, noop.Start(context.Background()))
	noop.Stop()

	_, err := noop.GetMetricsSnapshot()
	assert.Error(t, err)
	snapshot, err := noop.GetS3MetricsSnapshot()
	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	_, err = noop.GetHistoricalMetrics("system", time.Now(), time.Now())
	assert.Error(t, err)
	_, err = noop.GetHistoryStats()
	assert.Error(t, err)
	assert.NotNil(t, noop.Middleware())
}
