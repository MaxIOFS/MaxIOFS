package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"
)

// PersistenceManager manages persistent storage of users and access keys
type PersistenceManager struct {
	dataDir string
	mu      sync.RWMutex
}

// NewPersistenceManager creates a new persistence manager
func NewPersistenceManager(dataDir string) (*PersistenceManager, error) {
	// Ensure auth directory exists
	authDir := filepath.Join(dataDir, "auth")
	if err := os.MkdirAll(authDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create auth directory: %w", err)
	}

	return &PersistenceManager{
		dataDir: authDir,
	}, nil
}

// UserData represents persisted user data
type UserData struct {
	Users      map[string]*User      `json:"users"`       // username -> user
	AccessKeys map[string]*AccessKey `json:"access_keys"` // accessKeyID -> key
}

// SaveUsers persists users and access keys to disk
func (pm *PersistenceManager) SaveUsers(consoleUsers map[string]*User, accessKeys map[string]*AccessKey) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	data := UserData{
		Users:      consoleUsers,
		AccessKeys: accessKeys,
	}

	// Marshal to JSON with indentation for readability
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal user data: %w", err)
	}

	// Write to temporary file first
	usersFile := filepath.Join(pm.dataDir, "users.json")
	tempFile := usersFile + ".tmp"

	if err := os.WriteFile(tempFile, jsonData, 0600); err != nil {
		return fmt.Errorf("failed to write users file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, usersFile); err != nil {
		os.Remove(tempFile) // Cleanup on failure
		return fmt.Errorf("failed to rename users file: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"users_count": len(consoleUsers),
		"keys_count":  len(accessKeys),
	}).Debug("Saved users and access keys to disk")

	return nil
}

// LoadUsers loads users and access keys from disk
func (pm *PersistenceManager) LoadUsers() (*UserData, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	usersFile := filepath.Join(pm.dataDir, "users.json")

	// Check if file exists
	if _, err := os.Stat(usersFile); os.IsNotExist(err) {
		logrus.Info("No existing users file found, starting fresh")
		return &UserData{
			Users:      make(map[string]*User),
			AccessKeys: make(map[string]*AccessKey),
		}, nil
	}

	// Read file
	jsonData, err := os.ReadFile(usersFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read users file: %w", err)
	}

	// Unmarshal
	var data UserData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal users data: %w", err)
	}

	// Initialize maps if nil
	if data.Users == nil {
		data.Users = make(map[string]*User)
	}
	if data.AccessKeys == nil {
		data.AccessKeys = make(map[string]*AccessKey)
	}

	logrus.WithFields(logrus.Fields{
		"users_count": len(data.Users),
		"keys_count":  len(data.AccessKeys),
	}).Info("Loaded users and access keys from disk")

	return &data, nil
}

// BackupUsers creates a backup of the users file
func (pm *PersistenceManager) BackupUsers() error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	usersFile := filepath.Join(pm.dataDir, "users.json")

	// Check if users file exists
	if _, err := os.Stat(usersFile); os.IsNotExist(err) {
		return nil // Nothing to backup
	}

	// Create backup with timestamp
	backupFile := filepath.Join(pm.dataDir, fmt.Sprintf("users.backup.json"))

	// Read original file
	data, err := os.ReadFile(usersFile)
	if err != nil {
		return fmt.Errorf("failed to read users file for backup: %w", err)
	}

	// Write backup
	if err := os.WriteFile(backupFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}

	logrus.WithField("backup_file", backupFile).Debug("Created users backup")
	return nil
}
