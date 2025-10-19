package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// TenantManager defines the interface for tenant management
type TenantManager interface {
	CreateTenant(ctx context.Context, tenant *Tenant) error
	GetTenant(ctx context.Context, tenantID string) (*Tenant, error)
	GetTenantByName(ctx context.Context, name string) (*Tenant, error)
	ListTenants(ctx context.Context) ([]*Tenant, error)
	UpdateTenant(ctx context.Context, tenant *Tenant) error
	DeleteTenant(ctx context.Context, tenantID string) error
	ListTenantUsers(ctx context.Context, tenantID string) ([]*User, error)
}

// CreateTenant creates a new tenant
func (s *SQLiteStore) CreateTenant(tenant *Tenant) error {
	// Serialize metadata
	metadataJSON, _ := json.Marshal(tenant.Metadata)

	// Set default quota values if not specified
	if tenant.MaxAccessKeys == 0 {
		tenant.MaxAccessKeys = 10
	}
	if tenant.MaxStorageBytes == 0 {
		tenant.MaxStorageBytes = 107374182400 // 100GB
	}
	if tenant.MaxBuckets == 0 {
		tenant.MaxBuckets = 100
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO tenants (id, name, display_name, description, status, max_access_keys, max_storage_bytes, current_storage_bytes, max_buckets, current_buckets, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, tenant.ID, tenant.Name, tenant.DisplayName, tenant.Description, tenant.Status,
		tenant.MaxAccessKeys, tenant.MaxStorageBytes, tenant.CurrentStorageBytes, tenant.MaxBuckets, tenant.CurrentBuckets,
		string(metadataJSON), tenant.CreatedAt, tenant.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create tenant: %w", err)
	}

	return tx.Commit()
}

// GetTenant retrieves a tenant by ID
func (s *SQLiteStore) GetTenant(tenantID string) (*Tenant, error) {
	var tenant Tenant
	var metadataJSON string

	err := s.db.QueryRow(`
		SELECT id, name, display_name, description, status, max_access_keys, max_storage_bytes, current_storage_bytes, max_buckets, current_buckets, metadata, created_at, updated_at
		FROM tenants
		WHERE id = ? AND status != 'deleted'
	`, tenantID).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.DisplayName,
		&tenant.Description,
		&tenant.Status,
		&tenant.MaxAccessKeys,
		&tenant.MaxStorageBytes,
		&tenant.CurrentStorageBytes,
		&tenant.MaxBuckets,
		&tenant.CurrentBuckets,
		&metadataJSON,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound // Reuse error type
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	// Deserialize metadata
	if metadataJSON != "" {
		json.Unmarshal([]byte(metadataJSON), &tenant.Metadata)
	}

	// Calculate CurrentAccessKeys in real-time
	count, err := s.CountActiveAccessKeysByTenant(tenantID)
	if err != nil {
		logrus.WithError(err).Warn("Failed to count active access keys for tenant, using 0")
		count = 0
	}
	tenant.CurrentAccessKeys = int64(count)

	return &tenant, nil
}

// GetTenantByName retrieves a tenant by name
func (s *SQLiteStore) GetTenantByName(name string) (*Tenant, error) {
	var tenant Tenant
	var metadataJSON string

	err := s.db.QueryRow(`
		SELECT id, name, display_name, description, status, max_access_keys, max_storage_bytes, current_storage_bytes, max_buckets, current_buckets, metadata, created_at, updated_at
		FROM tenants
		WHERE name = ? AND status != 'deleted'
	`, name).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.DisplayName,
		&tenant.Description,
		&tenant.Status,
		&tenant.MaxAccessKeys,
		&tenant.MaxStorageBytes,
		&tenant.CurrentStorageBytes,
		&tenant.MaxBuckets,
		&tenant.CurrentBuckets,
		&metadataJSON,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant by name: %w", err)
	}

	// Deserialize metadata
	if metadataJSON != "" {
		json.Unmarshal([]byte(metadataJSON), &tenant.Metadata)
	}

	// Calculate CurrentAccessKeys in real-time
	count, err := s.CountActiveAccessKeysByTenant(tenant.ID)
	if err != nil {
		logrus.WithError(err).Warn("Failed to count active access keys for tenant, using 0")
		count = 0
	}
	tenant.CurrentAccessKeys = int64(count)

	return &tenant, nil
}

// ListTenants returns all tenants
func (s *SQLiteStore) ListTenants() ([]*Tenant, error) {
	rows, err := s.db.Query(`
		SELECT id, name, display_name, description, status, max_access_keys, max_storage_bytes, current_storage_bytes, max_buckets, current_buckets, metadata, created_at, updated_at
		FROM tenants
		WHERE status != 'deleted'
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*Tenant
	for rows.Next() {
		var tenant Tenant
		var metadataJSON string

		err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&tenant.DisplayName,
			&tenant.Description,
			&tenant.Status,
			&tenant.MaxAccessKeys,
			&tenant.MaxStorageBytes,
			&tenant.CurrentStorageBytes,
			&tenant.MaxBuckets,
			&tenant.CurrentBuckets,
			&metadataJSON,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
		)
		if err != nil {
			logrus.WithError(err).Error("Failed to scan tenant row")
			continue
		}

		// Deserialize metadata
		if metadataJSON != "" {
			json.Unmarshal([]byte(metadataJSON), &tenant.Metadata)
		}

		// Calculate CurrentAccessKeys in real-time
		count, err := s.CountActiveAccessKeysByTenant(tenant.ID)
		if err != nil {
			logrus.WithError(err).Warn("Failed to count active access keys for tenant, using 0")
			count = 0
		}
		tenant.CurrentAccessKeys = int64(count)

		tenants = append(tenants, &tenant)
	}

	return tenants, nil
}

// UpdateTenant updates an existing tenant
func (s *SQLiteStore) UpdateTenant(tenant *Tenant) error {
	// Serialize metadata
	metadataJSON, _ := json.Marshal(tenant.Metadata)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE tenants
		SET display_name = ?, description = ?, status = ?, max_access_keys = ?, max_storage_bytes = ?, current_storage_bytes = ?, max_buckets = ?, current_buckets = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, tenant.DisplayName, tenant.Description, tenant.Status, tenant.MaxAccessKeys, tenant.MaxStorageBytes, tenant.CurrentStorageBytes, tenant.MaxBuckets, tenant.CurrentBuckets, string(metadataJSON), tenant.UpdatedAt, tenant.ID)

	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}

	return tx.Commit()
}

// DeleteTenant permanently deletes a tenant and all associated users and access keys
func (s *SQLiteStore) DeleteTenant(tenantID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get all users in this tenant
	rows, err := tx.Query(`SELECT id FROM users WHERE tenant_id = ?`, tenantID)
	if err != nil {
		return fmt.Errorf("failed to get tenant users: %w", err)
	}

	var userIDs []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan user ID: %w", err)
		}
		userIDs = append(userIDs, userID)
	}
	rows.Close()

	// Delete all access keys for each user in the tenant
	for _, userID := range userIDs {
		_, err = tx.Exec(`DELETE FROM access_keys WHERE user_id = ?`, userID)
		if err != nil {
			return fmt.Errorf("failed to delete access keys for user %s: %w", userID, err)
		}
	}

	// Delete all users in the tenant
	_, err = tx.Exec(`DELETE FROM users WHERE tenant_id = ?`, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete tenant users: %w", err)
	}

	// Delete tenant
	_, err = tx.Exec(`DELETE FROM tenants WHERE id = ?`, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}

	return tx.Commit()
}

// ListTenantUsers returns all users in a tenant
func (s *SQLiteStore) ListTenantUsers(tenantID string) ([]*User, error) {
	rows, err := s.db.Query(`
		SELECT id, username, password_hash, display_name, email, status, tenant_id, roles, policies, metadata, created_at, updated_at
		FROM users
		WHERE tenant_id = ? AND status != 'deleted'
		ORDER BY username
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenant users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var user User
		var rolesJSON, policiesJSON, metadataJSON string
		var tenantID sql.NullString

		err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.Password,
			&user.DisplayName,
			&user.Email,
			&user.Status,
			&tenantID,
			&rolesJSON,
			&policiesJSON,
			&metadataJSON,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			logrus.WithError(err).Error("Failed to scan user row")
			continue
		}

		if tenantID.Valid {
			user.TenantID = tenantID.String
		}

		// Deserialize JSON fields
		if rolesJSON != "" {
			json.Unmarshal([]byte(rolesJSON), &user.Roles)
		}
		if policiesJSON != "" {
			json.Unmarshal([]byte(policiesJSON), &user.Policies)
		}
		if metadataJSON != "" {
			json.Unmarshal([]byte(metadataJSON), &user.Metadata)
		}

		users = append(users, &user)
	}

	return users, nil
}

// GenerateTenantID generates a unique tenant ID
func GenerateTenantID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "tenant-" + hex.EncodeToString(b)
}

// IncrementTenantBucketCount increments the current bucket count for a tenant
func (s *SQLiteStore) IncrementTenantBucketCount(tenantID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE tenants
		SET current_buckets = current_buckets + 1, updated_at = ?
		WHERE id = ?
	`, time.Now().Unix(), tenantID)

	if err != nil {
		return fmt.Errorf("failed to increment bucket count: %w", err)
	}

	return tx.Commit()
}

// DecrementTenantBucketCount decrements the current bucket count for a tenant
func (s *SQLiteStore) DecrementTenantBucketCount(tenantID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE tenants
		SET current_buckets = CASE
			WHEN current_buckets > 0 THEN current_buckets - 1
			ELSE 0
		END, updated_at = ?
		WHERE id = ?
	`, time.Now().Unix(), tenantID)

	if err != nil {
		return fmt.Errorf("failed to decrement bucket count: %w", err)
	}

	return tx.Commit()
}
