package auth

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClusterManager for testing cluster-aware quota checking
type mockClusterManager struct {
	enabled bool
}

func (m *mockClusterManager) IsClusterEnabled() bool {
	return m.enabled
}

// mockQuotaAggregator simulates cross-node storage aggregation
type mockQuotaAggregator struct {
	totalStorage int64
	err          error
}

func (m *mockQuotaAggregator) GetTenantTotalStorage(ctx context.Context, tenantID string) (int64, error) {
	if m.err != nil {
		return 0, m.err
	}
	return m.totalStorage, nil
}

// TestCheckTenantStorageQuota_ClusterMode tests quota checking in cluster mode
func TestCheckTenantStorageQuota_ClusterMode(t *testing.T) {
	// This test verifies that CheckTenantStorageQuota uses QuotaAggregator in cluster mode

	// Create a tenant with 1GB quota
	store, tmpDir := setupTestStore(t)
	defer cleanupTestAuthManager(t, tmpDir)

	tenant := &Tenant{
		ID:                  "tenant1",
		Name:                "Test Tenant",
		MaxStorageBytes:     1024 * 1024 * 1024,        // 1GB quota
		CurrentStorageBytes: 100 * 1024 * 1024,         // 100MB local storage
	}

	err := store.CreateTenant(tenant)
	require.NoError(t, err)

	// Create auth manager
	mgr := &authManager{
		store: store,
	}

	// Test 1: Standalone mode - should use local storage only
	t.Run("Standalone mode uses local storage", func(t *testing.T) {
		mgr.SetClusterManager(&mockClusterManager{enabled: false})
		mgr.SetQuotaAggregator(nil)

		// Try to add 900MB (would fit with local 100MB)
		err := mgr.CheckTenantStorageQuota(context.Background(), "tenant1", 900*1024*1024)
		assert.NoError(t, err, "Should allow 900MB when local storage is 100MB")

		// Try to add 1000MB (would exceed with local 100MB)
		err = mgr.CheckTenantStorageQuota(context.Background(), "tenant1", 1000*1024*1024)
		assert.Error(t, err, "Should reject 1000MB when local storage is 100MB")
		assert.Contains(t, err.Error(), "storage quota exceeded")
	})

	// Test 2: Cluster mode - should use aggregated storage from all nodes
	t.Run("Cluster mode uses aggregated storage", func(t *testing.T) {
		mgr.SetClusterManager(&mockClusterManager{enabled: true})

		// Simulate 3 nodes with 300MB each = 900MB total
		mgr.SetQuotaAggregator(&mockQuotaAggregator{
			totalStorage: 900 * 1024 * 1024, // 900MB across all nodes
		})

		// Try to add 100MB (total would be 1000MB, within 1GB quota)
		err := mgr.CheckTenantStorageQuota(context.Background(), "tenant1", 100*1024*1024)
		assert.NoError(t, err, "Should allow 100MB when cluster storage is 900MB")

		// Try to add 200MB (total would be 1100MB, exceeds 1GB quota)
		err = mgr.CheckTenantStorageQuota(context.Background(), "tenant1", 200*1024*1024)
		assert.Error(t, err, "Should reject 200MB when cluster storage is 900MB")
		assert.Contains(t, err.Error(), "storage quota exceeded")
		// Should show cluster storage (900MB = 943718400 bytes) in error
		assert.Contains(t, err.Error(), "943718400") // 900MB in bytes
	})

	// Test 3: Cluster mode with aggregator failure - should fallback to local
	t.Run("Cluster mode falls back to local on aggregator failure", func(t *testing.T) {
		mgr.SetClusterManager(&mockClusterManager{enabled: true})
		mgr.SetQuotaAggregator(&mockQuotaAggregator{
			err: fmt.Errorf("network error"),
		})

		// Should fallback to local storage (100MB)
		// Try to add 900MB (would fit with local 100MB)
		err := mgr.CheckTenantStorageQuota(context.Background(), "tenant1", 900*1024*1024)
		assert.NoError(t, err, "Should fallback to local and allow 900MB")
	})

	// Test 4: Verify the security vulnerability is fixed
	t.Run("Prevents quota bypass attack in cluster mode", func(t *testing.T) {
		mgr.SetClusterManager(&mockClusterManager{enabled: true})

		// ATTACK SCENARIO:
		// - Tenant has 1GB quota
		// - Each of 3 nodes shows 900MB local storage
		// - Total cluster storage = 2700MB (2.7GB)
		// - Should REJECT any additional uploads

		mgr.SetQuotaAggregator(&mockQuotaAggregator{
			totalStorage: 2700 * 1024 * 1024, // 2.7GB across cluster (exceeds quota)
		})

		// Try to upload even 1 byte more
		err := mgr.CheckTenantStorageQuota(context.Background(), "tenant1", 1)
		assert.Error(t, err, "Should REJECT upload when cluster storage exceeds quota")
		assert.Contains(t, err.Error(), "storage quota exceeded")
		// Should show cluster storage (2700MB = 2831155200 bytes) in error
		assert.Contains(t, err.Error(), "2831155200", "Should show cluster-wide usage in error")

		t.Log("✓ SECURITY FIX VERIFIED: Quota bypass attack prevented in cluster mode")
	})

	// Test 5: Empty tenantID (global admin) - no quota
	t.Run("Global admin has no quota", func(t *testing.T) {
		mgr.SetClusterManager(&mockClusterManager{enabled: true})
		mgr.SetQuotaAggregator(&mockQuotaAggregator{
			totalStorage: 999 * 1024 * 1024 * 1024, // 999GB
		})

		// Global admin (empty tenantID) should never be quota-checked
		err := mgr.CheckTenantStorageQuota(context.Background(), "", 999*1024*1024*1024)
		assert.NoError(t, err, "Global admin should have no quota")
	})

	// Test 6: Tenant with unlimited storage (MaxStorageBytes = 0)
	t.Run("Tenant with unlimited storage", func(t *testing.T) {
		tenant2 := &Tenant{
			ID:                  "tenant2",
			Name:                "Unlimited Tenant",
			MaxStorageBytes:     0, // 0 = unlimited (no quota checking)
			CurrentStorageBytes: 5 * 1024 * 1024 * 1024, // 5GB used
		}
		err := store.CreateTenant(tenant2)
		require.NoError(t, err)

		mgr.SetClusterManager(&mockClusterManager{enabled: true})
		mgr.SetQuotaAggregator(&mockQuotaAggregator{
			totalStorage: 10 * 1024 * 1024 * 1024, // 10GB
		})

		// Should allow any amount when MaxStorageBytes = 0 (unlimited)
		err = mgr.CheckTenantStorageQuota(context.Background(), "tenant2", 999*1024*1024*1024)
		assert.NoError(t, err, "Should allow unlimited when MaxStorageBytes = 0")
	})
}
