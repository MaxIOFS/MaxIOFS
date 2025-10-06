package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Config holds all configuration for MaxIOFS
type Config struct {
	// Server configuration
	Listen        string `mapstructure:"listen"`
	ConsoleListen string `mapstructure:"console_listen"`
	DataDir       string `mapstructure:"data_dir"`
	LogLevel      string `mapstructure:"log_level"`

	// Public URLs (for redirects, presigned URLs, etc.)
	PublicAPIURL     string `mapstructure:"public_api_url"`     // e.g., https://s3.example.com or http://localhost:8080
	PublicConsoleURL string `mapstructure:"public_console_url"` // e.g., https://console.example.com or http://localhost:8081

	// TLS configuration
	EnableTLS bool   `mapstructure:"enable_tls"`
	CertFile  string `mapstructure:"cert_file"`
	KeyFile   string `mapstructure:"key_file"`

	// Storage configuration
	Storage StorageConfig `mapstructure:"storage"`

	// Auth configuration
	Auth AuthConfig `mapstructure:"auth"`

	// Metrics configuration
	Metrics MetricsConfig `mapstructure:"metrics"`
}

// StorageConfig defines storage backend configuration
type StorageConfig struct {
	Backend string `mapstructure:"backend"` // filesystem, s3, gcs, azure

	// Filesystem backend
	Root string `mapstructure:"root"`

	// Compression
	EnableCompression bool   `mapstructure:"enable_compression"`
	CompressionLevel  int    `mapstructure:"compression_level"`
	CompressionType   string `mapstructure:"compression_type"` // gzip, lz4, zstd

	// Encryption
	EnableEncryption bool   `mapstructure:"enable_encryption"`
	EncryptionKey    string `mapstructure:"encryption_key"`

	// Object locking
	EnableObjectLock bool `mapstructure:"enable_object_lock"`
}

// AuthConfig defines authentication configuration
type AuthConfig struct {
	EnableAuth bool   `mapstructure:"enable_auth"`
	JWTSecret  string `mapstructure:"jwt_secret"`

	// Default credentials
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`

	// Users configuration file
	UsersFile string `mapstructure:"users_file"`
}

// MetricsConfig defines metrics configuration
type MetricsConfig struct {
	Enable   bool   `mapstructure:"enable"`
	Path     string `mapstructure:"path"`
	Interval int    `mapstructure:"interval"`
}

// Load loads configuration from various sources
func Load(cmd *cobra.Command) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Bind command line flags
	if err := bindFlags(cmd, v); err != nil {
		return nil, fmt.Errorf("failed to bind flags: %w", err)
	}

	// Read from config file if specified
	if configFile, _ := cmd.Flags().GetString("config"); configFile != "" {
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Read from environment variables
	v.SetEnvPrefix("MAXIOFS")
	v.AutomaticEnv()

	// Unmarshal configuration
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate and setup defaults
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// Server defaults - usando puertos diferentes a MinIO para evitar conflictos
	v.SetDefault("listen", ":8080")        // API server
	v.SetDefault("console_listen", ":8081") // Web console
	v.SetDefault("data_dir", "./data")
	v.SetDefault("log_level", "info")

	// Public URL defaults (will be auto-detected from request if not set)
	v.SetDefault("public_api_url", "http://localhost:8080")
	v.SetDefault("public_console_url", "http://localhost:8081")

	// TLS defaults
	v.SetDefault("enable_tls", false)

	// Storage defaults
	v.SetDefault("storage.backend", "filesystem")
	v.SetDefault("storage.root", "./data/objects")
	v.SetDefault("storage.enable_compression", false)
	v.SetDefault("storage.compression_level", 6)
	v.SetDefault("storage.compression_type", "gzip")
	v.SetDefault("storage.enable_encryption", false)
	v.SetDefault("storage.enable_object_lock", true)

	// Auth defaults - credenciales diferentes para diferenciación
	v.SetDefault("auth.enable_auth", true)
	v.SetDefault("auth.access_key", "maxioadmin")
	v.SetDefault("auth.secret_key", "maxioadmin")

	// Metrics defaults
	v.SetDefault("metrics.enable", true)
	v.SetDefault("metrics.path", "/metrics")
	v.SetDefault("metrics.interval", 60)
}

func bindFlags(cmd *cobra.Command, v *viper.Viper) error {
	flags := map[string]string{
		"listen":         "listen",
		"console-listen": "console_listen",
		"data-dir":       "data_dir",
		"log-level":      "log_level",
		"enable-tls":     "enable_tls",
		"cert-file":      "cert_file",
		"key-file":       "key_file",
	}

	for flag, key := range flags {
		if err := v.BindPFlag(key, cmd.Flags().Lookup(flag)); err != nil {
			return err
		}
	}

	return nil
}

func validate(cfg *Config) error {
	// Ensure data directory exists
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Setup storage root
	if cfg.Storage.Root == "" {
		cfg.Storage.Root = filepath.Join(cfg.DataDir, "objects")
	}

	if err := os.MkdirAll(cfg.Storage.Root, 0755); err != nil {
		return fmt.Errorf("failed to create storage root: %w", err)
	}

	// Validate TLS configuration
	if cfg.EnableTLS {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return fmt.Errorf("TLS enabled but cert-file or key-file not specified")
		}
	}

	// Generate JWT secret if not provided
	if cfg.Auth.EnableAuth && cfg.Auth.JWTSecret == "" {
		cfg.Auth.JWTSecret = generateRandomString(32)
	}

	return nil
}

func generateRandomString(length int) string {
	// Simple random string generation for JWT secret
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[i%len(charset)]
	}
	return string(b)
}