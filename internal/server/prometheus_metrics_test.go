package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exportedFamilies are every metric family MaxIOFS declares: each must reach
// Prometheus once the server has done what it counts.
var exportedFamilies = []string{
	"maxiofs_http_requests_total",
	"maxiofs_http_request_duration_seconds",
	"maxiofs_http_request_size_bytes",
	"maxiofs_http_response_size_bytes",
	"maxiofs_s3_operations_total",
	"maxiofs_s3_operation_duration_seconds",
	"maxiofs_s3_errors_total",
	"maxiofs_storage_operations_total",
	"maxiofs_storage_operation_duration_seconds",
	"maxiofs_object_operations_total",
	"maxiofs_object_operation_duration_seconds",
	"maxiofs_object_size_bytes",
	"maxiofs_auth_attempts_total",
	"maxiofs_auth_failures_total",
	"maxiofs_system_cpu_usage_percent",
	"maxiofs_system_memory_usage_percent",
	"maxiofs_system_disk_usage_percent",
	"maxiofs_system_disk_used_bytes",
	"maxiofs_system_disk_total_bytes",
	"maxiofs_system_events_total",
	"maxiofs_bucket_objects_total",
	"maxiofs_bucket_bytes_total",
	"maxiofs_bucket_operations_total",
	"maxiofs_object_lock_operations_total",
	"maxiofs_object_lock_retention_objects_total",
	"maxiofs_background_tasks_total",
	"maxiofs_background_task_duration_seconds",
	"maxiofs_cache_hit_rate",
	"maxiofs_cache_size_bytes",
	"maxiofs_operation_latency_p50_milliseconds",
	"maxiofs_operation_latency_p95_milliseconds",
	"maxiofs_operation_latency_p99_milliseconds",
	"maxiofs_operation_latency_mean_milliseconds",
	"maxiofs_operation_success_rate_percent",
	"maxiofs_operation_count_total",
	"maxiofs_throughput_requests_per_second",
	"maxiofs_throughput_bytes_per_second",
	"maxiofs_throughput_objects_per_second",
}

// seriesValue is the value of the series of the scrape whose name and labels
// are exactly these, and whether there is one.
func seriesValue(scrape, name string, labels map[string]string) (float64, bool) {
	for _, line := range strings.Split(scrape, "\n") {
		if !strings.HasPrefix(line, name+"{") && !strings.HasPrefix(line, name+" ") {
			continue
		}
		head, value, ok := strings.Cut(line, "} ")
		if !ok {
			head, value, _ = strings.Cut(line, " ")
		}
		got := map[string]string{}
		for _, pair := range regexp.MustCompile(`(\w+)="([^"]*)"`).FindAllStringSubmatch(head, -1) {
			got[pair[1]] = pair[2]
		}
		if len(got) != len(labels) {
			continue
		}
		match := true
		for k, v := range labels {
			if got[k] != v {
				match = false
				break
			}
		}
		if match {
			f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			return f, err == nil
		}
	}
	return 0, false
}

// Prometheus gets what the server does: S3 requests by operation and error
// code, storage and object operations, sign-ins, bucket and Object Lock
// changes, retained objects, bucket usage, events, background tasks and the
// metadata cache. No label carries an object key.
func TestPrometheusExportsWhatTheServerDoes(t *testing.T) {
	s := newClusterTestNodeWith(t, func(cfg *config.Config) {
		cfg.Metrics = config.MetricsConfig{Enable: true, Path: "/metrics", Interval: 10}
	})
	ctx := context.Background()
	now := time.Now().Unix()
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "metrics-admin", Username: "metrics-admin",
		Status: auth.UserStatusActive, Roles: []string{auth.RoleAdmin}, CreatedAt: now, UpdatedAt: now}))
	key, err := s.authManager.GenerateAccessKey(ctx, "metrics-admin")
	require.NoError(t, err)
	api := httptest.NewServer(s.httpServer.Handler)
	t.Cleanup(api.Close)
	client := func(secret string) *s3.Client {
		return s3.New(s3.Options{
			BaseEndpoint:               aws.String(api.URL),
			Region:                     "us-east-1",
			UsePathStyle:               true,
			Credentials:                credentials.NewStaticCredentialsProvider(key.AccessKeyID, secret, ""),
			RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
			ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
			RetryMaxAttempts:           1,
		})
	}
	c := client(key.SecretAccessKey)

	_, err = c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("metered")})
	require.NoError(t, err)
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("metered"), Key: aws.String("reports/secret-name.csv"), Body: bytes.NewReader([]byte("a,b,c"))})
	require.NoError(t, err)
	got, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("metered"), Key: aws.String("reports/secret-name.csv")})
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, got.Body)
	got.Body.Close()
	_, err = c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("metered"), Key: aws.String("missing")})
	require.Error(t, err)
	_, err = client("not-the-secret").ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("metered")})
	require.Error(t, err)
	presigned, err := s3.NewPresignClient(c).PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("metered"), Key: aws.String("reports/secret-name.csv")})
	require.NoError(t, err)
	resp, err := http.Get(presigned.URL)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	_, err = c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("locked"), ObjectLockEnabledForBucket: aws.Bool(true)})
	require.NoError(t, err)
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("locked"), Key: aws.String("held"), Body: bytes.NewReader([]byte("x"))})
	require.NoError(t, err)
	_, err = c.PutObjectRetention(ctx, &s3.PutObjectRetentionInput{Bucket: aws.String("locked"), Key: aws.String("held"),
		Retention: &s3types.ObjectLockRetention{Mode: s3types.ObjectLockRetentionModeGovernance, RetainUntilDate: aws.Time(time.Now().Add(time.Hour))}})
	require.NoError(t, err)

	console := httptest.NewServer(s.consoleServer.Handler)
	t.Cleanup(console.Close)
	resp, err = http.Post(console.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"username":"metrics-admin","password":"wrong"}`))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Reads from disk go through the metadata block cache.
	compactor, ok := s.metadataStore.(interface{ Compact(context.Context) error })
	require.True(t, ok)
	require.NoError(t, compactor.Compact(ctx))
	_, err = c.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("metered"), Key: aws.String("reports/secret-name.csv")})
	require.NoError(t, err)

	s.notificationHub.SendNotification(&Notification{Type: "disk_warning"})
	s.runTask(ctx, "stats_reconciler", func() error { return s.reconcileBucketStats(ctx) })

	resp, err = http.Get(api.URL + "/metrics")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	scrape := string(body)

	for _, family := range exportedFamilies {
		assert.Contains(t, scrape, "# TYPE "+family+" ", "%s is not exported", family)
	}
	assert.NotContains(t, scrape, "secret-name", "a label carries an object key")

	for _, c := range []struct {
		name   string
		labels map[string]string
	}{
		{"maxiofs_http_requests_total", map[string]string{"method": "PUT", "operation": "PutObject", "status": "200"}},
		{"maxiofs_http_requests_total", map[string]string{"method": "GET", "operation": "ListObjectsV2", "status": "403"}},
		{"maxiofs_auth_attempts_total", map[string]string{"method": "s3_presigned", "status": "success"}},
		{"maxiofs_s3_operations_total", map[string]string{"operation": "GetObject", "bucket": "metered", "status": "success"}},
		{"maxiofs_s3_errors_total", map[string]string{"operation": "GetObject", "bucket": "metered", "error_type": "NoSuchKey"}},
		{"maxiofs_storage_operations_total", map[string]string{"operation": "Put", "status": "success"}},
		{"maxiofs_object_operations_total", map[string]string{"operation": "PutObject", "bucket": "metered"}},
		{"maxiofs_auth_attempts_total", map[string]string{"method": "s3_sigv4", "status": "success"}},
		{"maxiofs_auth_attempts_total", map[string]string{"method": "s3_sigv4", "status": "failure"}},
		{"maxiofs_auth_failures_total", map[string]string{"method": "console_password", "reason": "invalid_credentials"}},
		{"maxiofs_bucket_operations_total", map[string]string{"operation": "CreateBucket", "bucket": "metered", "status": "success"}},
		{"maxiofs_object_lock_operations_total", map[string]string{"operation": "PutObjectRetention", "bucket": "locked", "status": "success"}},
		{"maxiofs_object_lock_retention_objects_total", map[string]string{"bucket": "locked", "mode": "governance"}},
		{"maxiofs_bucket_objects_total", map[string]string{"bucket": "metered"}},
		{"maxiofs_system_events_total", map[string]string{"event_type": "disk_warning"}},
		{"maxiofs_background_tasks_total", map[string]string{"task_type": "stats_reconciler", "status": "success"}},
	} {
		value, ok := seriesValue(scrape, c.name, c.labels)
		if assert.True(t, ok, "%s%v is not exported", c.name, c.labels) {
			assert.GreaterOrEqual(t, value, 1.0, "%s%v", c.name, c.labels)
		}
	}
	size, ok := seriesValue(scrape, "maxiofs_cache_size_bytes", map[string]string{})
	assert.True(t, ok)
	assert.Greater(t, size, 0.0, "the metadata cache is read")
}
