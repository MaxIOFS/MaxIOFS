package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestSetDefaults(t *testing.T) {
	v := viper.New()
	setDefaults(v)

	assert.Equal(t, ":8080", v.GetString("listen"))
	assert.Equal(t, ":8081", v.GetString("console_listen"))
	assert.Equal(t, "info", v.GetString("log_level"))
	assert.Equal(t, "http://localhost:8080", v.GetString("public_api_url"))
	assert.Equal(t, "http://localhost:8081", v.GetString("public_console_url"))
	assert.False(t, v.GetBool("enable_tls"))
}

func TestSetDefaults_Storage(t *testing.T) {
	v := viper.New()
	setDefaults(v)

	assert.Equal(t, "filesystem", v.GetString("storage.backend"))
	assert.False(t, v.GetBool("storage.enable_compression"))
	assert.Equal(t, 6, v.GetInt("storage.compression_level"))
	assert.Equal(t, "gzip", v.GetString("storage.compression_type"))
}

func TestSetDefaults_Auth(t *testing.T) {
	v := viper.New()
	setDefaults(v)

	// Auth should be enabled by default
	assert.True(t, v.GetBool("auth.enable_auth"))
}

func TestSetDefaults_Metrics(t *testing.T) {
	v := viper.New()
	setDefaults(v)

	assert.True(t, v.GetBool("metrics.enable"))
	assert.Equal(t, "/metrics", v.GetString("metrics.path"))
	assert.Equal(t, 10, v.GetInt("metrics.interval"))
}

func TestSetDefaults_Audit(t *testing.T) {
	v := viper.New()
	setDefaults(v)

	assert.True(t, v.GetBool("audit.enable"))
	assert.Equal(t, 90, v.GetInt("audit.retention_days"))
}

func TestConfig_Struct(t *testing.T) {
	cfg := Config{
		Listen:        ":8080",
		ConsoleListen: ":8081",
		DataDir:       "/tmp/data",
		LogLevel:      "info",
	}

	assert.Equal(t, ":8080", cfg.Listen)
	assert.Equal(t, ":8081", cfg.ConsoleListen)
	assert.Equal(t, "/tmp/data", cfg.DataDir)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestStorageConfig_Struct(t *testing.T) {
	cfg := StorageConfig{
		Backend:           "filesystem",
		Root:              "/data/storage",
		EnableCompression: true,
		CompressionLevel:  9,
		CompressionType:   "gzip",
		EnableEncryption:  true,
		EncryptionKey:     "test-key",
		EnableObjectLock:  true,
	}

	assert.Equal(t, "filesystem", cfg.Backend)
	assert.Equal(t, "/data/storage", cfg.Root)
	assert.True(t, cfg.EnableCompression)
	assert.Equal(t, 9, cfg.CompressionLevel)
	assert.Equal(t, "gzip", cfg.CompressionType)
	assert.True(t, cfg.EnableEncryption)
	assert.Equal(t, "test-key", cfg.EncryptionKey)
	assert.True(t, cfg.EnableObjectLock)
}

func TestAuthConfig_Struct(t *testing.T) {
	cfg := AuthConfig{
		EnableAuth: true,
		JWTSecret:  "secret",
		AccessKey:  "AKIAIOSFODNN7EXAMPLE",
		SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		UsersFile:  "/etc/users.json",
	}

	assert.True(t, cfg.EnableAuth)
	assert.Equal(t, "secret", cfg.JWTSecret)
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", cfg.AccessKey)
	assert.Equal(t, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", cfg.SecretKey)
	assert.Equal(t, "/etc/users.json", cfg.UsersFile)
}

func TestMetricsConfig_Struct(t *testing.T) {
	cfg := MetricsConfig{
		Enable:   true,
		Path:     "/metrics",
		Interval: 10,
	}

	assert.True(t, cfg.Enable)
	assert.Equal(t, "/metrics", cfg.Path)
	assert.Equal(t, 10, cfg.Interval)
}

func TestAuditConfig_Struct(t *testing.T) {
	cfg := AuditConfig{
		Enable:        true,
		RetentionDays: 90,
		DBPath:        "/data/audit.db",
	}

	assert.True(t, cfg.Enable)
	assert.Equal(t, 90, cfg.RetentionDays)
	assert.Equal(t, "/data/audit.db", cfg.DBPath)
}

func TestConfig_TLSSettings(t *testing.T) {
	cfg := Config{
		EnableTLS: true,
		CertFile:  "/path/to/cert.pem",
		KeyFile:   "/path/to/key.pem",
	}

	assert.True(t, cfg.EnableTLS)
	assert.Equal(t, "/path/to/cert.pem", cfg.CertFile)
	assert.Equal(t, "/path/to/key.pem", cfg.KeyFile)
}

func TestConfig_PublicURLs(t *testing.T) {
	cfg := Config{
		PublicAPIURL:     "https://api.example.com",
		PublicConsoleURL: "https://console.example.com",
	}

	assert.Equal(t, "https://api.example.com", cfg.PublicAPIURL)
	assert.Equal(t, "https://console.example.com", cfg.PublicConsoleURL)
}

func TestStorageConfig_CompressionTypes(t *testing.T) {
	types := []string{"gzip", "lz4", "zstd"}

	for _, compressionType := range types {
		t.Run(compressionType, func(t *testing.T) {
			cfg := StorageConfig{
				EnableCompression: true,
				CompressionType:   compressionType,
			}

			assert.True(t, cfg.EnableCompression)
			assert.Equal(t, compressionType, cfg.CompressionType)
		})
	}
}

func TestGenerateRandomString(t *testing.T) {
	// Test that it generates a string of the correct length
	result := generateRandomString(32)
	assert.NotEmpty(t, result)
	assert.Len(t, result, 32)

	// Test another length
	result2 := generateRandomString(64)
	assert.NotEmpty(t, result2)
	assert.Len(t, result2, 64)
}
