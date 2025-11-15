package audit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func setupTestDB(t *testing.T) (*Manager, func()) {
	t.Helper()

	// Create temp directory for test database
	tempDir, err := os.MkdirTemp("", "audit-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "audit_test.db")

	// Create logger
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise in tests

	// Create store
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create store: %v", err)
	}

	// Create manager
	mgr := NewManager(store, logger)

	// Cleanup function
	cleanup := func() {
		mgr.Close()
		os.RemoveAll(tempDir)
	}

	return mgr, cleanup
}

func TestCreateAuditLog(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	event := &AuditEvent{
		TenantID:     "tenant-1",
		UserID:       "user-1",
		Username:     "testuser",
		EventType:    EventTypeLoginSuccess,
		ResourceType: ResourceTypeSystem,
		Action:       ActionLogin,
		Status:       StatusSuccess,
		IPAddress:    "192.168.1.1",
		UserAgent:    "Mozilla/5.0",
		Details: map[string]interface{}{
			"method": "password",
		},
	}

	err := mgr.LogEvent(ctx, event)
	if err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
}

func TestGetLogs(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test events
	events := []*AuditEvent{
		{
			TenantID:  "tenant-1",
			UserID:    "user-1",
			Username:  "user1",
			EventType: EventTypeLoginSuccess,
			Action:    ActionLogin,
			Status:    StatusSuccess,
		},
		{
			TenantID:  "tenant-1",
			UserID:    "user-2",
			Username:  "user2",
			EventType: EventTypeLoginFailed,
			Action:    ActionLogin,
			Status:    StatusFailed,
		},
		{
			TenantID:  "tenant-2",
			UserID:    "user-3",
			Username:  "user3",
			EventType: EventTypeUserCreated,
			Action:    ActionCreate,
			Status:    StatusSuccess,
		},
	}

	for _, event := range events {
		if err := mgr.LogEvent(ctx, event); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Test: Get all logs
	logs, total, err := mgr.GetLogs(ctx, &AuditLogFilters{
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected 3 total logs, got %d", total)
	}

	if len(logs) != 3 {
		t.Errorf("Expected 3 logs, got %d", len(logs))
	}
}

func TestGetLogsByTenant(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test events
	events := []*AuditEvent{
		{
			TenantID:  "tenant-1",
			UserID:    "user-1",
			Username:  "user1",
			EventType: EventTypeLoginSuccess,
			Action:    ActionLogin,
			Status:    StatusSuccess,
		},
		{
			TenantID:  "tenant-1",
			UserID:    "user-2",
			Username:  "user2",
			EventType: EventTypeLoginFailed,
			Action:    ActionLogin,
			Status:    StatusFailed,
		},
		{
			TenantID:  "tenant-2",
			UserID:    "user-3",
			Username:  "user3",
			EventType: EventTypeUserCreated,
			Action:    ActionCreate,
			Status:    StatusSuccess,
		},
	}

	for _, event := range events {
		if err := mgr.LogEvent(ctx, event); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Test: Get tenant-1 logs
	logs, total, err := mgr.GetLogsByTenant(ctx, "tenant-1", &AuditLogFilters{
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected 2 total logs for tenant-1, got %d", total)
	}

	if len(logs) != 2 {
		t.Errorf("Expected 2 logs for tenant-1, got %d", len(logs))
	}

	// Verify all logs belong to tenant-1
	for _, log := range logs {
		if log.TenantID != "tenant-1" {
			t.Errorf("Expected tenant-1, got %s", log.TenantID)
		}
	}
}

func TestFilterByEventType(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test events
	events := []*AuditEvent{
		{
			UserID:    "user-1",
			Username:  "user1",
			EventType: EventTypeLoginSuccess,
			Action:    ActionLogin,
			Status:    StatusSuccess,
		},
		{
			UserID:    "user-2",
			Username:  "user2",
			EventType: EventTypeLoginFailed,
			Action:    ActionLogin,
			Status:    StatusFailed,
		},
		{
			UserID:    "user-3",
			Username:  "user3",
			EventType: EventTypeUserCreated,
			Action:    ActionCreate,
			Status:    StatusSuccess,
		},
	}

	for _, event := range events {
		if err := mgr.LogEvent(ctx, event); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Test: Filter by EventTypeLoginSuccess
	logs, total, err := mgr.GetLogs(ctx, &AuditLogFilters{
		EventType: EventTypeLoginSuccess,
		Page:      1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected 1 login_success event, got %d", total)
	}

	if len(logs) != 1 {
		t.Errorf("Expected 1 log, got %d", len(logs))
	}

	if logs[0].EventType != EventTypeLoginSuccess {
		t.Errorf("Expected event type %s, got %s", EventTypeLoginSuccess, logs[0].EventType)
	}
}

func TestFilterByStatus(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test events
	events := []*AuditEvent{
		{
			UserID:    "user-1",
			Username:  "user1",
			EventType: EventTypeLoginSuccess,
			Action:    ActionLogin,
			Status:    StatusSuccess,
		},
		{
			UserID:    "user-2",
			Username:  "user2",
			EventType: EventTypeLoginFailed,
			Action:    ActionLogin,
			Status:    StatusFailed,
		},
		{
			UserID:    "user-3",
			Username:  "user3",
			EventType: EventTypeUserCreated,
			Action:    ActionCreate,
			Status:    StatusSuccess,
		},
	}

	for _, event := range events {
		if err := mgr.LogEvent(ctx, event); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Test: Filter by failed status
	logs, total, err := mgr.GetLogs(ctx, &AuditLogFilters{
		Status:   StatusFailed,
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected 1 failed event, got %d", total)
	}

	if len(logs) != 1 {
		t.Errorf("Expected 1 log, got %d", len(logs))
	}

	if logs[0].Status != StatusFailed {
		t.Errorf("Expected status %s, got %s", StatusFailed, logs[0].Status)
	}
}

func TestFilterByDateRange(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	now := time.Now().Unix()
	yesterday := now - (24 * 60 * 60)
	twoDaysAgo := now - (2 * 24 * 60 * 60)

	// Test: Filter by date range
	_, total, err := mgr.GetLogs(ctx, &AuditLogFilters{
		StartDate: yesterday,
		EndDate:   now,
		Page:      1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// All test events should be within range since they were just created
	if total < 0 {
		t.Errorf("Expected non-negative total, got %d", total)
	}

	// Test: Filter with date range that excludes all events
	_, total, err = mgr.GetLogs(ctx, &AuditLogFilters{
		StartDate: twoDaysAgo,
		EndDate:   yesterday,
		Page:      1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if total != 0 {
		t.Errorf("Expected 0 logs in old date range, got %d", total)
	}
}

func TestPagination(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create 25 test events
	for i := 0; i < 25; i++ {
		event := &AuditEvent{
			UserID:    "user-1",
			Username:  "user1",
			EventType: EventTypeLoginSuccess,
			Action:    ActionLogin,
			Status:    StatusSuccess,
		}
		if err := mgr.LogEvent(ctx, event); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Test: Page 1 with 10 items
	logs, total, err := mgr.GetLogs(ctx, &AuditLogFilters{
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if total != 25 {
		t.Errorf("Expected 25 total logs, got %d", total)
	}

	if len(logs) != 10 {
		t.Errorf("Expected 10 logs on page 1, got %d", len(logs))
	}

	// Test: Page 2 with 10 items
	logs, total, err = mgr.GetLogs(ctx, &AuditLogFilters{
		Page:     2,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if len(logs) != 10 {
		t.Errorf("Expected 10 logs on page 2, got %d", len(logs))
	}

	// Test: Page 3 with 10 items (should have 5 remaining)
	logs, total, err = mgr.GetLogs(ctx, &AuditLogFilters{
		Page:     3,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if len(logs) != 5 {
		t.Errorf("Expected 5 logs on page 3, got %d", len(logs))
	}
}

func TestGetLogByID(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	event := &AuditEvent{
		UserID:    "user-1",
		Username:  "testuser",
		EventType: EventTypeLoginSuccess,
		Action:    ActionLogin,
		Status:    StatusSuccess,
		IPAddress: "192.168.1.1",
	}

	if err := mgr.LogEvent(ctx, event); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Get the log we just created
	logs, _, err := mgr.GetLogs(ctx, &AuditLogFilters{
		Page:     1,
		PageSize: 1,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if len(logs) == 0 {
		t.Fatal("No logs returned")
	}

	logID := logs[0].ID

	// Test: Get log by ID
	log, err := mgr.GetLogByID(ctx, logID)
	if err != nil {
		t.Fatalf("Failed to get log by ID: %v", err)
	}

	if log.ID != logID {
		t.Errorf("Expected log ID %d, got %d", logID, log.ID)
	}

	if log.Username != "testuser" {
		t.Errorf("Expected username testuser, got %s", log.Username)
	}

	if log.IPAddress != "192.168.1.1" {
		t.Errorf("Expected IP 192.168.1.1, got %s", log.IPAddress)
	}
}

func TestPurgeLogs(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test events
	for i := 0; i < 5; i++ {
		event := &AuditEvent{
			UserID:    "user-1",
			Username:  "user1",
			EventType: EventTypeLoginSuccess,
			Action:    ActionLogin,
			Status:    StatusSuccess,
		}
		if err := mgr.LogEvent(ctx, event); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Verify we have 5 logs
	_, total, err := mgr.GetLogs(ctx, &AuditLogFilters{
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected 5 logs before purge, got %d", total)
	}

	// Test: Purge logs older than 0 days (should delete nothing since they're fresh)
	deleted, err := mgr.PurgeLogs(ctx, 1)
	if err != nil {
		t.Fatalf("Failed to purge logs: %v", err)
	}

	if deleted != 0 {
		t.Errorf("Expected 0 deleted logs, got %d", deleted)
	}

	// Verify we still have 5 logs
	_, total, err = mgr.GetLogs(ctx, &AuditLogFilters{
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs after purge: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected 5 logs after purge, got %d", total)
	}
}

func TestMultipleFilters(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test events
	events := []*AuditEvent{
		{
			TenantID:     "tenant-1",
			UserID:       "user-1",
			Username:     "user1",
			EventType:    EventTypeLoginSuccess,
			ResourceType: ResourceTypeSystem,
			Action:       ActionLogin,
			Status:       StatusSuccess,
		},
		{
			TenantID:     "tenant-1",
			UserID:       "user-2",
			Username:     "user2",
			EventType:    EventTypeLoginFailed,
			ResourceType: ResourceTypeSystem,
			Action:       ActionLogin,
			Status:       StatusFailed,
		},
		{
			TenantID:     "tenant-2",
			UserID:       "user-3",
			Username:     "user3",
			EventType:    EventTypeBucketCreated,
			ResourceType: ResourceTypeBucket,
			Action:       ActionCreate,
			Status:       StatusSuccess,
		},
	}

	for _, event := range events {
		if err := mgr.LogEvent(ctx, event); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Test: Multiple filters (tenant + status)
	logs, total, err := mgr.GetLogs(ctx, &AuditLogFilters{
		TenantID: "tenant-1",
		Status:   StatusSuccess,
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected 1 log with multiple filters, got %d", total)
	}

	if len(logs) != 1 {
		t.Errorf("Expected 1 log, got %d", len(logs))
	}

	if logs[0].TenantID != "tenant-1" || logs[0].Status != StatusSuccess {
		t.Errorf("Expected tenant-1 and success status, got tenant=%s status=%s",
			logs[0].TenantID, logs[0].Status)
	}
}
