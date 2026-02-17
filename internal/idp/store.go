package idp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Store handles SQLite persistence for identity providers and group mappings
type Store struct {
	db *sql.DB
}

// NewStore creates a new IDP store using the given database connection
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateProvider inserts a new identity provider
func (s *Store) CreateProvider(idp *IdentityProvider) error {
	configJSON, err := json.Marshal(idp.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO identity_providers (id, name, type, tenant_id, status, config, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, idp.ID, idp.Name, idp.Type, nullStr(idp.TenantID), idp.Status, string(configJSON),
		idp.CreatedBy, idp.CreatedAt, idp.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	return nil
}

// GetProvider retrieves a single identity provider by ID
func (s *Store) GetProvider(id string) (*IdentityProvider, error) {
	var idp IdentityProvider
	var configJSON string
	var tenantID sql.NullString

	err := s.db.QueryRow(`
		SELECT id, name, type, tenant_id, status, config, created_by, created_at, updated_at
		FROM identity_providers
		WHERE id = ?
	`, id).Scan(&idp.ID, &idp.Name, &idp.Type, &tenantID, &idp.Status, &configJSON,
		&idp.CreatedBy, &idp.CreatedAt, &idp.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("identity provider not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}

	if tenantID.Valid {
		idp.TenantID = tenantID.String
	}

	if err := json.Unmarshal([]byte(configJSON), &idp.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &idp, nil
}

// UpdateProvider updates an existing identity provider
func (s *Store) UpdateProvider(idp *IdentityProvider) error {
	configJSON, err := json.Marshal(idp.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	result, err := s.db.Exec(`
		UPDATE identity_providers
		SET name = ?, type = ?, tenant_id = ?, status = ?, config = ?, updated_at = ?
		WHERE id = ?
	`, idp.Name, idp.Type, nullStr(idp.TenantID), idp.Status, string(configJSON),
		idp.UpdatedAt, idp.ID)
	if err != nil {
		return fmt.Errorf("failed to update provider: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("identity provider not found: %s", idp.ID)
	}

	return nil
}

// DeleteProvider removes an identity provider (cascade deletes group mappings)
func (s *Store) DeleteProvider(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete group mappings first
	if _, err := tx.Exec(`DELETE FROM idp_group_mappings WHERE provider_id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete group mappings: %w", err)
	}

	// Delete provider
	result, err := tx.Exec(`DELETE FROM identity_providers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete provider: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("identity provider not found: %s", id)
	}

	return tx.Commit()
}

// ListProviders lists identity providers, optionally filtered by tenant
func (s *Store) ListProviders(tenantID string) ([]*IdentityProvider, error) {
	var rows *sql.Rows
	var err error

	if tenantID == "" {
		// Global admin: see all
		rows, err = s.db.Query(`
			SELECT id, name, type, tenant_id, status, config, created_by, created_at, updated_at
			FROM identity_providers
			ORDER BY created_at DESC
		`)
	} else {
		// Tenant admin: see only own tenant's providers
		rows, err = s.db.Query(`
			SELECT id, name, type, tenant_id, status, config, created_by, created_at, updated_at
			FROM identity_providers
			WHERE tenant_id = ?
			ORDER BY created_at DESC
		`, tenantID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}
	defer rows.Close()

	var providers []*IdentityProvider
	for rows.Next() {
		var idp IdentityProvider
		var configJSON string
		var tid sql.NullString

		if err := rows.Scan(&idp.ID, &idp.Name, &idp.Type, &tid, &idp.Status, &configJSON,
			&idp.CreatedBy, &idp.CreatedAt, &idp.UpdatedAt); err != nil {
			return nil, err
		}

		if tid.Valid {
			idp.TenantID = tid.String
		}

		if err := json.Unmarshal([]byte(configJSON), &idp.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config for %s: %w", idp.ID, err)
		}

		providers = append(providers, &idp)
	}

	return providers, rows.Err()
}

// ListActiveOAuthProviders returns active OAuth2 providers for the login page
func (s *Store) ListActiveOAuthProviders() ([]*IdentityProvider, error) {
	rows, err := s.db.Query(`
		SELECT id, name, type, tenant_id, status, config, created_by, created_at, updated_at
		FROM identity_providers
		WHERE type = 'oauth2' AND status = 'active'
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list OAuth providers: %w", err)
	}
	defer rows.Close()

	var providers []*IdentityProvider
	for rows.Next() {
		var idp IdentityProvider
		var configJSON string
		var tid sql.NullString

		if err := rows.Scan(&idp.ID, &idp.Name, &idp.Type, &tid, &idp.Status, &configJSON,
			&idp.CreatedBy, &idp.CreatedAt, &idp.UpdatedAt); err != nil {
			return nil, err
		}

		if tid.Valid {
			idp.TenantID = tid.String
		}

		if err := json.Unmarshal([]byte(configJSON), &idp.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config for %s: %w", idp.ID, err)
		}

		providers = append(providers, &idp)
	}

	return providers, rows.Err()
}

// CreateGroupMapping inserts a new group mapping
func (s *Store) CreateGroupMapping(mapping *GroupMapping) error {
	_, err := s.db.Exec(`
		INSERT INTO idp_group_mappings (id, provider_id, external_group, external_group_name, role, tenant_id, auto_sync, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, mapping.ID, mapping.ProviderID, mapping.ExternalGroup, mapping.ExternalGroupName,
		mapping.Role, nullStr(mapping.TenantID), mapping.AutoSync, mapping.CreatedAt, mapping.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create group mapping: %w", err)
	}

	return nil
}

// GetGroupMapping retrieves a single group mapping by ID
func (s *Store) GetGroupMapping(id string) (*GroupMapping, error) {
	var m GroupMapping
	var tenantID sql.NullString
	var lastSynced sql.NullInt64

	err := s.db.QueryRow(`
		SELECT id, provider_id, external_group, external_group_name, role, tenant_id, auto_sync, last_synced_at, created_at, updated_at
		FROM idp_group_mappings
		WHERE id = ?
	`, id).Scan(&m.ID, &m.ProviderID, &m.ExternalGroup, &m.ExternalGroupName,
		&m.Role, &tenantID, &m.AutoSync, &lastSynced, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("group mapping not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get group mapping: %w", err)
	}

	if tenantID.Valid {
		m.TenantID = tenantID.String
	}
	if lastSynced.Valid {
		m.LastSyncedAt = lastSynced.Int64
	}

	return &m, nil
}

// UpdateGroupMapping updates an existing group mapping
func (s *Store) UpdateGroupMapping(mapping *GroupMapping) error {
	result, err := s.db.Exec(`
		UPDATE idp_group_mappings
		SET external_group = ?, external_group_name = ?, role = ?, tenant_id = ?, auto_sync = ?, updated_at = ?
		WHERE id = ?
	`, mapping.ExternalGroup, mapping.ExternalGroupName, mapping.Role,
		nullStr(mapping.TenantID), mapping.AutoSync, mapping.UpdatedAt, mapping.ID)
	if err != nil {
		return fmt.Errorf("failed to update group mapping: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("group mapping not found: %s", mapping.ID)
	}

	return nil
}

// DeleteGroupMapping removes a group mapping
func (s *Store) DeleteGroupMapping(id string) error {
	result, err := s.db.Exec(`DELETE FROM idp_group_mappings WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete group mapping: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("group mapping not found: %s", id)
	}

	return nil
}

// ListGroupMappings lists group mappings for a provider
func (s *Store) ListGroupMappings(providerID string) ([]*GroupMapping, error) {
	rows, err := s.db.Query(`
		SELECT id, provider_id, external_group, external_group_name, role, tenant_id, auto_sync, last_synced_at, created_at, updated_at
		FROM idp_group_mappings
		WHERE provider_id = ?
		ORDER BY created_at DESC
	`, providerID)
	if err != nil {
		return nil, fmt.Errorf("failed to list group mappings: %w", err)
	}
	defer rows.Close()

	var mappings []*GroupMapping
	for rows.Next() {
		var m GroupMapping
		var tenantID sql.NullString
		var lastSynced sql.NullInt64

		if err := rows.Scan(&m.ID, &m.ProviderID, &m.ExternalGroup, &m.ExternalGroupName,
			&m.Role, &tenantID, &m.AutoSync, &lastSynced, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}

		if tenantID.Valid {
			m.TenantID = tenantID.String
		}
		if lastSynced.Valid {
			m.LastSyncedAt = lastSynced.Int64
		}

		mappings = append(mappings, &m)
	}

	return mappings, rows.Err()
}

// UpdateGroupMappingSyncTime updates the last_synced_at timestamp
func (s *Store) UpdateGroupMappingSyncTime(id string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`
		UPDATE idp_group_mappings SET last_synced_at = ?, updated_at = ? WHERE id = ?
	`, now, now, id)
	return err
}

// CountUsersWithProvider returns the number of users linked to a specific provider
func (s *Store) CountUsersWithProvider(providerID string, providerType string) (int, error) {
	var count int
	authProviderPrefix := providerType + ":" + providerID
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM users WHERE auth_provider = ?
	`, authProviderPrefix).Scan(&count)
	return count, err
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
