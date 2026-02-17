package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// Test validate() function
func TestValidate_MissingDataDir(t *testing.T) {
	cfg := &Config{}
	err := validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "data_dir is required")
}

func TestValidate_ValidConfig(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &Config{
		DataDir: tempDir,
		Storage: StorageConfig{
			Root: "",
		},
		Auth: AuthConfig{
			EnableAuth: true,
			JWTSecret:  "",
		},
		Audit: AuditConfig{
			Enable: true,
			DBPath: "",
		},
	}

	err := validate(cfg)
	require.NoError(t, err)

	// Check that storage root was set based on data_dir
	expectedStorageRoot := filepath.Join(tempDir, "objects")
	assert.Equal(t, expectedStorageRoot, cfg.Storage.Root)

	// Check that JWT secret was generated and flagged as auto-generated
	assert.NotEmpty(t, cfg.Auth.JWTSecret)
	assert.Len(t, cfg.Auth.JWTSecret, 32)
	assert.True(t, cfg.Auth.JWTSecretAutoGenerated, "JWTSecretAutoGenerated should be true when secret is auto-generated")

	// Check that audit DB path was set
	expectedAuditPath := filepath.Join(tempDir, "audit.db")
	assert.Equal(t, expectedAuditPath, cfg.Audit.DBPath)
}

func TestValidate_StorageRootCreation(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &Config{
		DataDir: tempDir,
		Storage: StorageConfig{
			Root: filepath.Join(tempDir, "custom", "storage"),
		},
	}

	err := validate(cfg)
	require.NoError(t, err)

	// Check that storage root directory was created
	_, err = os.Stat(cfg.Storage.Root)
	assert.NoError(t, err, "Storage root should be created")
}

func TestValidate_TLSEnabledWithoutCerts(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &Config{
		DataDir:   tempDir,
		EnableTLS: true,
		CertFile:  "",
		KeyFile:   "",
	}

	err := validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS enabled but cert-file or key-file not specified")
}

func TestValidate_TLSEnabledWithCerts(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &Config{
		DataDir:   tempDir,
		EnableTLS: true,
		CertFile:  "/path/to/cert.pem",
		KeyFile:   "/path/to/key.pem",
		Auth: AuthConfig{
			EnableAuth: true,
		},
		Audit: AuditConfig{
			Enable: true,
		},
	}

	err := validate(cfg)
	require.NoError(t, err)
}

func TestValidate_RelativeStorageRootBecomesAbsolute(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &Config{
		DataDir: tempDir,
		Storage: StorageConfig{
			Root: "relative/path",
		},
		Auth: AuthConfig{
			EnableAuth: true,
		},
		Audit: AuditConfig{
			Enable: true,
		},
	}

	err := validate(cfg)
	require.NoError(t, err)

	// Check that storage root is now absolute
	assert.True(t, filepath.IsAbs(cfg.Storage.Root))
}

func TestValidate_OldDefaultStorageRoot(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &Config{
		DataDir: tempDir,
		Storage: StorageConfig{
			Root: "./data/objects", // Old default
		},
		Auth: AuthConfig{
			EnableAuth: true,
		},
		Audit: AuditConfig{
			Enable: true,
		},
	}

	err := validate(cfg)
	require.NoError(t, err)

	// Check that old default was replaced with new data_dir based path
	expectedStorageRoot := filepath.Join(tempDir, "objects")
	assert.Equal(t, expectedStorageRoot, cfg.Storage.Root)
}

func TestValidate_JWTSecretNotGeneratedWhenAuthDisabled(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &Config{
		DataDir: tempDir,
		Auth: AuthConfig{
			EnableAuth: false,
			JWTSecret:  "",
		},
		Audit: AuditConfig{
			Enable: true,
		},
	}

	err := validate(cfg)
	require.NoError(t, err)

	// JWT secret should remain empty when auth is disabled
	assert.Empty(t, cfg.Auth.JWTSecret)
}

func TestValidate_JWTSecretPreservedWhenProvided(t *testing.T) {
	tempDir := t.TempDir()
	customSecret := "my-custom-jwt-secret"

	cfg := &Config{
		DataDir: tempDir,
		Auth: AuthConfig{
			EnableAuth: true,
			JWTSecret:  customSecret,
		},
		Audit: AuditConfig{
			Enable: true,
		},
	}

	err := validate(cfg)
	require.NoError(t, err)

	// Custom JWT secret should be preserved and NOT flagged as auto-generated
	assert.Equal(t, customSecret, cfg.Auth.JWTSecret)
	assert.False(t, cfg.Auth.JWTSecretAutoGenerated, "JWTSecretAutoGenerated should be false when secret is explicitly provided")
}

func TestValidate_AuditDBPathNotSetWhenDisabled(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &Config{
		DataDir: tempDir,
		Auth: AuthConfig{
			EnableAuth: true,
		},
		Audit: AuditConfig{
			Enable: false,
			DBPath: "",
		},
	}

	err := validate(cfg)
	require.NoError(t, err)

	// Audit DB path should remain empty when audit is disabled
	assert.Empty(t, cfg.Audit.DBPath)
}

func TestValidate_AuditDBPathPreservedWhenProvided(t *testing.T) {
	tempDir := t.TempDir()
	customPath := filepath.Join(tempDir, "custom-audit.db")

	cfg := &Config{
		DataDir: tempDir,
		Auth: AuthConfig{
			EnableAuth: true,
		},
		Audit: AuditConfig{
			Enable: true,
			DBPath: customPath,
		},
	}

	err := validate(cfg)
	require.NoError(t, err)

	// Custom audit DB path should be preserved
	assert.Equal(t, customPath, cfg.Audit.DBPath)
}

// Test bindFlags() function
func TestBindFlags_Success(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("listen", ":8080", "listen address")
	cmd.Flags().String("console-listen", ":8081", "console listen address")
	cmd.Flags().String("data-dir", "", "data directory")
	cmd.Flags().String("log-level", "info", "log level")
	cmd.Flags().String("tls-cert", "", "TLS certificate file")
	cmd.Flags().String("tls-key", "", "TLS key file")

	v := viper.New()
	err := bindFlags(cmd, v)
	require.NoError(t, err)
}

func TestBindFlags_MissingFlag(t *testing.T) {
	// Create command without any flags
	cmd := &cobra.Command{}

	v := viper.New()
	err := bindFlags(cmd, v)
	// Should not error even if flags don't exist, viper just won't bind them
	require.Error(t, err)
}

// Test Load() function
func TestLoad_WithDefaults(t *testing.T) {
	tempDir := t.TempDir()

	cmd := &cobra.Command{}
	cmd.Flags().String("listen", ":8080", "listen address")
	cmd.Flags().String("console-listen", ":8081", "console listen address")
	cmd.Flags().String("data-dir", tempDir, "data directory")
	cmd.Flags().String("log-level", "info", "log level")
	cmd.Flags().String("tls-cert", "", "TLS certificate file")
	cmd.Flags().String("tls-key", "", "TLS key file")
	cmd.Flags().String("config", "", "config file")

	// Set the data-dir flag
	err := cmd.Flags().Set("data-dir", tempDir)
	require.NoError(t, err)

	cfg, err := Load(cmd)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Check defaults were applied
	assert.Equal(t, ":8080", cfg.Listen)
	assert.Equal(t, ":8081", cfg.ConsoleListen)
	assert.Equal(t, tempDir, cfg.DataDir)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "filesystem", cfg.Storage.Backend)
	assert.True(t, cfg.Auth.EnableAuth)
	assert.True(t, cfg.Metrics.Enable)
	assert.True(t, cfg.Audit.Enable)
}

func TestLoad_FromConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.yaml")

	// Create a config file - use fmt.Sprintf to properly format the path
	configContent := "listen: \":9090\"\n" +
		"console_listen: \":9091\"\n" +
		"data_dir: \"" + filepath.ToSlash(tempDir) + "\"\n" +
		"log_level: \"debug\"\n" +
		"storage:\n" +
		"  backend: \"filesystem\"\n" +
		"  enable_compression: true\n" +
		"  compression_level: 9\n" +
		"auth:\n" +
		"  enable_auth: true\n" +
		"metrics:\n" +
		"  enable: true\n" +
		"  interval: 5\n"

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	cmd.Flags().String("listen", ":8080", "listen address")
	cmd.Flags().String("console-listen", ":8081", "console listen address")
	cmd.Flags().String("data-dir", "", "data directory")
	cmd.Flags().String("log-level", "info", "log level")
	cmd.Flags().String("tls-cert", "", "TLS certificate file")
	cmd.Flags().String("tls-key", "", "TLS key file")
	cmd.Flags().String("config", configFile, "config file")

	// Set the config flag
	err = cmd.Flags().Set("config", configFile)
	require.NoError(t, err)

	cfg, err := Load(cmd)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Check values from config file
	assert.Equal(t, ":9090", cfg.Listen)
	assert.Equal(t, ":9091", cfg.ConsoleListen)
	// Normalize paths for comparison (Windows uses \ but YAML may use /)
	assert.Equal(t, filepath.Clean(tempDir), filepath.Clean(cfg.DataDir))
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.True(t, cfg.Storage.EnableCompression)
	assert.Equal(t, 9, cfg.Storage.CompressionLevel)
	assert.Equal(t, 5, cfg.Metrics.Interval)
}

func TestLoad_InvalidConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "invalid-config.yaml")

	// Create an invalid config file
	configContent := `
listen: ":8080"
invalid yaml content [[[
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	cmd.Flags().String("listen", ":8080", "listen address")
	cmd.Flags().String("console-listen", ":8081", "console listen address")
	cmd.Flags().String("data-dir", "", "data directory")
	cmd.Flags().String("log-level", "info", "log level")
	cmd.Flags().String("tls-cert", "", "TLS certificate file")
	cmd.Flags().String("tls-key", "", "TLS key file")
	cmd.Flags().String("config", configFile, "config file")

	err = cmd.Flags().Set("config", configFile)
	require.NoError(t, err)

	cfg, err := Load(cmd)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoad_NonExistentConfigFile(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("listen", ":8080", "listen address")
	cmd.Flags().String("console-listen", ":8081", "console listen address")
	cmd.Flags().String("data-dir", "", "data directory")
	cmd.Flags().String("log-level", "info", "log level")
	cmd.Flags().String("tls-cert", "", "TLS certificate file")
	cmd.Flags().String("tls-key", "", "TLS key file")
	cmd.Flags().String("config", "/nonexistent/config.yaml", "config file")

	err := cmd.Flags().Set("config", "/nonexistent/config.yaml")
	require.NoError(t, err)

	cfg, err := Load(cmd)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoad_MissingDataDir(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("listen", ":8080", "listen address")
	cmd.Flags().String("console-listen", ":8081", "console listen address")
	cmd.Flags().String("data-dir", "", "data directory")
	cmd.Flags().String("log-level", "info", "log level")
	cmd.Flags().String("tls-cert", "", "TLS certificate file")
	cmd.Flags().String("tls-key", "", "TLS key file")
	cmd.Flags().String("config", "", "config file")

	// Don't set data-dir flag - should fail validation
	cfg, err := Load(cmd)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "data_dir is required")
}

func TestLoad_WithEnvironmentVariables(t *testing.T) {
	tempDir := t.TempDir()

	// Set environment variables
	os.Setenv("MAXIOFS_DATA_DIR", tempDir)
	os.Setenv("MAXIOFS_LISTEN", ":9999")
	os.Setenv("MAXIOFS_LOG_LEVEL", "debug")

	defer func() {
		os.Unsetenv("MAXIOFS_DATA_DIR")
		os.Unsetenv("MAXIOFS_LISTEN")
		os.Unsetenv("MAXIOFS_LOG_LEVEL")
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("listen", ":8080", "listen address")
	cmd.Flags().String("console-listen", ":8081", "console listen address")
	cmd.Flags().String("data-dir", "", "data directory")
	cmd.Flags().String("log-level", "info", "log level")
	cmd.Flags().String("tls-cert", "", "TLS certificate file")
	cmd.Flags().String("tls-key", "", "TLS key file")
	cmd.Flags().String("config", "", "config file")

	cfg, err := Load(cmd)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Check that environment variables were loaded
	assert.Equal(t, tempDir, cfg.DataDir)
	assert.Equal(t, ":9999", cfg.Listen)
	assert.Equal(t, "debug", cfg.LogLevel)
}

func TestLoad_FlagOverridesEnvironment(t *testing.T) {
	tempDir := t.TempDir()

	// Set environment variable
	os.Setenv("MAXIOFS_LISTEN", ":9999")
	defer os.Unsetenv("MAXIOFS_LISTEN")

	cmd := &cobra.Command{}
	cmd.Flags().String("listen", ":8080", "listen address")
	cmd.Flags().String("console-listen", ":8081", "console listen address")
	cmd.Flags().String("data-dir", tempDir, "data directory")
	cmd.Flags().String("log-level", "info", "log level")
	cmd.Flags().String("tls-cert", "", "TLS certificate file")
	cmd.Flags().String("tls-key", "", "TLS key file")
	cmd.Flags().String("config", "", "config file")

	// Set flag explicitly
	err := cmd.Flags().Set("listen", ":7777")
	require.NoError(t, err)
	err = cmd.Flags().Set("data-dir", tempDir)
	require.NoError(t, err)

	cfg, err := Load(cmd)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Flag should override environment variable
	assert.Equal(t, ":7777", cfg.Listen)
}

func TestValidate_JWTSecretAutoGeneratedFlag(t *testing.T) {
	t.Run("auto-generated sets flag", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &Config{
			DataDir: tempDir,
			Auth: AuthConfig{
				EnableAuth: true,
				JWTSecret:  "", // Will be auto-generated
			},
		}

		err := validate(cfg)
		require.NoError(t, err)
		assert.NotEmpty(t, cfg.Auth.JWTSecret)
		assert.True(t, cfg.Auth.JWTSecretAutoGenerated)
	})

	t.Run("explicit secret does not set flag", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &Config{
			DataDir: tempDir,
			Auth: AuthConfig{
				EnableAuth: true,
				JWTSecret:  "my-explicit-secret",
			},
		}

		err := validate(cfg)
		require.NoError(t, err)
		assert.Equal(t, "my-explicit-secret", cfg.Auth.JWTSecret)
		assert.False(t, cfg.Auth.JWTSecretAutoGenerated)
	})

	t.Run("auth disabled does not set flag", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &Config{
			DataDir: tempDir,
			Auth: AuthConfig{
				EnableAuth: false,
				JWTSecret:  "",
			},
		}

		err := validate(cfg)
		require.NoError(t, err)
		assert.Empty(t, cfg.Auth.JWTSecret)
		assert.False(t, cfg.Auth.JWTSecretAutoGenerated)
	})
}
