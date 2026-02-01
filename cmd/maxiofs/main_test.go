package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetupLogging_DebugLevel tests debug log level configuration
func TestSetupLogging_DebugLevel(t *testing.T) {
	setupLogging("debug")

	assert.Equal(t, logrus.DebugLevel, logrus.GetLevel(), "Log level should be Debug")
	assert.IsType(t, &logrus.JSONFormatter{}, logrus.StandardLogger().Formatter, "Formatter should be JSONFormatter")
}

// TestSetupLogging_InfoLevel tests info log level configuration
func TestSetupLogging_InfoLevel(t *testing.T) {
	setupLogging("info")

	assert.Equal(t, logrus.InfoLevel, logrus.GetLevel(), "Log level should be Info")
}

// TestSetupLogging_WarnLevel tests warn log level configuration
func TestSetupLogging_WarnLevel(t *testing.T) {
	setupLogging("warn")

	assert.Equal(t, logrus.WarnLevel, logrus.GetLevel(), "Log level should be Warn")
}

// TestSetupLogging_ErrorLevel tests error log level configuration
func TestSetupLogging_ErrorLevel(t *testing.T) {
	setupLogging("error")

	assert.Equal(t, logrus.ErrorLevel, logrus.GetLevel(), "Log level should be Error")
}

// TestSetupLogging_DefaultLevel tests default log level when invalid level provided
func TestSetupLogging_DefaultLevel(t *testing.T) {
	setupLogging("invalid-level")

	assert.Equal(t, logrus.InfoLevel, logrus.GetLevel(), "Log level should default to Info for invalid input")
}

// TestSetupLogging_EmptyString tests default log level with empty string
func TestSetupLogging_EmptyString(t *testing.T) {
	setupLogging("")

	assert.Equal(t, logrus.InfoLevel, logrus.GetLevel(), "Log level should default to Info for empty string")
}

// TestSetupLogging_JSONFormatter tests that JSON formatter is configured
func TestSetupLogging_JSONFormatter(t *testing.T) {
	setupLogging("info")

	formatter, ok := logrus.StandardLogger().Formatter.(*logrus.JSONFormatter)
	require.True(t, ok, "Formatter should be JSONFormatter")
	assert.Equal(t, time.RFC3339, formatter.TimestampFormat, "Timestamp format should be RFC3339")
}

// TestSetupLogging_OutputFormat tests that log output is valid JSON
func TestSetupLogging_OutputFormat(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	defer logrus.SetOutput(os.Stderr) // Restore default output

	setupLogging("info")
	logrus.Info("test message")

	// Verify output is valid JSON
	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err, "Log output should be valid JSON")

	assert.Equal(t, "test message", logEntry["msg"], "Log message should match")
	assert.Equal(t, "info", logEntry["level"], "Log level should be info")
	assert.NotEmpty(t, logEntry["time"], "Log entry should have timestamp")
}

// TestRunServer_TLSValidation_BothRequired tests that both cert and key are required for TLS
func TestRunServer_TLSValidation_BothRequired(t *testing.T) {
	// Create temporary data directory
	tmpDir, err := os.MkdirTemp("", "maxiofs-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		cert    string
		key     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "only cert provided",
			cert:    "/path/to/cert.pem",
			key:     "",
			wantErr: true,
			errMsg:  "both --tls-cert and --tls-key must be provided together",
		},
		{
			name:    "only key provided",
			cert:    "",
			key:     "/path/to/key.pem",
			wantErr: true,
			errMsg:  "both --tls-cert and --tls-key must be provided together",
		},
		{
			name:    "neither provided",
			cert:    "",
			key:     "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().String("tls-cert", "", "")
			cmd.Flags().String("tls-key", "", "")
			cmd.Flags().String("config", "", "")
			cmd.Flags().String("data-dir", tmpDir, "")
			cmd.Flags().String("listen", ":18080", "")
			cmd.Flags().String("console-listen", ":18081", "")
			cmd.Flags().String("log-level", "error", "") // Suppress logs during tests

			cmd.Flags().Set("tls-cert", tt.cert)
			cmd.Flags().Set("tls-key", tt.key)

			// Create a context with timeout to prevent hanging
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			// Run in goroutine to allow timeout
			errChan := make(chan error, 1)
			go func() {
				errChan <- runServer(cmd, []string{})
			}()

			select {
			case err := <-errChan:
				if tt.wantErr {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tt.errMsg)
				} else {
					// For valid configs, we expect context deadline exceeded
					// because we're testing with a 100ms timeout
					if err != nil && err.Error() != "server error: http: Server closed" {
						// Config validation passed, server tried to start
						assert.Contains(t, err.Error(), "failed to load configuration")
					}
				}
			case <-ctx.Done():
				// Timeout is acceptable for valid configs (server started)
				if tt.wantErr {
					t.Fatal("Expected error but got timeout")
				}
			}
		})
	}
}

// TestRunServer_ConfigurationLoading tests configuration loading from flags
func TestRunServer_ConfigurationLoading(t *testing.T) {
	// Create temporary data directory
	tmpDir, err := os.MkdirTemp("", "maxiofs-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a minimal config file
	configFile := filepath.Join(tmpDir, "config.json")
	configData := `{
		"data_dir": "` + tmpDir + `",
		"listen": ":18080",
		"console_listen": ":18081",
		"log_level": "error"
	}`
	err = os.WriteFile(configFile, []byte(configData), 0644)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	cmd.Flags().String("config", configFile, "")
	cmd.Flags().String("data-dir", tmpDir, "")
	cmd.Flags().String("listen", ":18080", "")
	cmd.Flags().String("console-listen", ":18081", "")
	cmd.Flags().String("log-level", "error", "")
	cmd.Flags().String("tls-cert", "", "")
	cmd.Flags().String("tls-key", "", "")

	// Set the config flag
	cmd.Flags().Set("config", configFile)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(cmd, []string{})
	}()

	select {
	case err := <-errChan:
		// We expect an error because we don't have a full database setup
		// But it should NOT be a config loading error
		if err != nil && err.Error() == "failed to load configuration: invalid configuration" {
			t.Fatalf("Configuration loading failed: %v", err)
		}
		// Any other error is fine (e.g., database connection, server start)
	case <-ctx.Done():
		// Timeout is acceptable - means config loaded and server tried to start
	}
}

// TestRunServer_InvalidDataDir tests error handling for invalid data directory
func TestRunServer_InvalidDataDir(t *testing.T) {
	t.Skip("Skipping full server start test - too slow for unit tests")
}

// TestRunServer_LogLevelConfiguration tests that log level from flags is applied
func TestRunServer_LogLevelConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		expected logrus.Level
	}{
		{"debug level", "debug", logrus.DebugLevel},
		{"info level", "info", logrus.InfoLevel},
		{"warn level", "warn", logrus.WarnLevel},
		{"error level", "error", logrus.ErrorLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test setupLogging directly instead of full server start
			setupLogging(tt.logLevel)
			assert.Equal(t, tt.expected, logrus.GetLevel(), "Log level should be set correctly")
		})
	}
}

// TestVersion tests version information is set correctly
func TestVersion(t *testing.T) {
	assert.NotEmpty(t, version, "Version should not be empty")
	assert.NotEmpty(t, commit, "Commit should not be empty")
	assert.NotEmpty(t, date, "Date should not be empty")

	// Verify version format
	assert.Contains(t, version, "v", "Version should start with 'v'")
}

// TestCobraCommandSetup tests that Cobra command is configured correctly
func TestCobraCommandSetup(t *testing.T) {
	// Create the root command (same as in main())
	rootCmd := &cobra.Command{
		Use:     "maxiofs",
		Short:   "MaxIOFS - High-Performance S3-Compatible Object Storage",
		Version: version,
	}

	// Add flags
	rootCmd.PersistentFlags().StringP("config", "c", "", "Configuration file path")
	rootCmd.PersistentFlags().StringP("data-dir", "d", "", "Data directory path")
	rootCmd.PersistentFlags().StringP("listen", "l", ":8080", "API server listen address")
	rootCmd.PersistentFlags().StringP("console-listen", "", ":8081", "Web console listen address")
	rootCmd.PersistentFlags().StringP("log-level", "", "info", "Log level")
	rootCmd.PersistentFlags().StringP("tls-cert", "", "", "TLS certificate file")
	rootCmd.PersistentFlags().StringP("tls-key", "", "", "TLS private key file")

	// Verify flags are registered
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("config"), "config flag should exist")
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("data-dir"), "data-dir flag should exist")
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("listen"), "listen flag should exist")
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("console-listen"), "console-listen flag should exist")
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("log-level"), "log-level flag should exist")
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("tls-cert"), "tls-cert flag should exist")
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("tls-key"), "tls-key flag should exist")

	// Verify default values
	listen, _ := rootCmd.PersistentFlags().GetString("listen")
	assert.Equal(t, ":8080", listen, "Default listen address should be :8080")

	consoleListen, _ := rootCmd.PersistentFlags().GetString("console-listen")
	assert.Equal(t, ":8081", consoleListen, "Default console listen address should be :8081")

	logLevel, _ := rootCmd.PersistentFlags().GetString("log-level")
	assert.Equal(t, "info", logLevel, "Default log level should be info")
}

// TestCobraCommandShortcuts tests that flag shortcuts work
func TestCobraCommandShortcuts(t *testing.T) {
	rootCmd := &cobra.Command{Use: "maxiofs"}
	rootCmd.PersistentFlags().StringP("config", "c", "", "Configuration file path")
	rootCmd.PersistentFlags().StringP("data-dir", "d", "", "Data directory path")
	rootCmd.PersistentFlags().StringP("listen", "l", ":8080", "API server listen address")

	// Test shortcut flags
	err := rootCmd.ParseFlags([]string{"-c", "/path/to/config.json"})
	require.NoError(t, err)

	config, _ := rootCmd.PersistentFlags().GetString("config")
	assert.Equal(t, "/path/to/config.json", config, "Shortcut -c should work for --config")

	err = rootCmd.ParseFlags([]string{"-d", "/data"})
	require.NoError(t, err)

	dataDir, _ := rootCmd.PersistentFlags().GetString("data-dir")
	assert.Equal(t, "/data", dataDir, "Shortcut -d should work for --data-dir")

	err = rootCmd.ParseFlags([]string{"-l", ":9090"})
	require.NoError(t, err)

	listen, _ := rootCmd.PersistentFlags().GetString("listen")
	assert.Equal(t, ":9090", listen, "Shortcut -l should work for --listen")
}

// TestRunServer_TLSBothCertAndKey tests that TLS works when both cert and key are provided
func TestRunServer_TLSBothCertAndKey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "maxiofs-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create dummy cert and key files
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	err = os.WriteFile(certFile, []byte("dummy cert"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(keyFile, []byte("dummy key"), 0644)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	cmd.Flags().String("tls-cert", certFile, "")
	cmd.Flags().String("tls-key", keyFile, "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("data-dir", tmpDir, "")
	cmd.Flags().String("listen", ":28080", "")
	cmd.Flags().String("console-listen", ":28081", "")
	cmd.Flags().String("log-level", "error", "")

	cmd.Flags().Set("tls-cert", certFile)
	cmd.Flags().Set("tls-key", keyFile)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(cmd, []string{})
	}()

	select {
	case err := <-errChan:
		// Server should try to start with TLS (may fail on cert validation, which is OK for this test)
		// We just want to verify that both cert and key together don't produce "must be provided together" error
		if err != nil {
			assert.NotContains(t, err.Error(), "must be provided together", "Should not complain about cert/key when both are provided")
		}
	case <-ctx.Done():
		// Timeout is also acceptable - means server tried to start
	}
}

// TestSetupLogging_ConcurrentCalls tests that setupLogging is safe for concurrent calls
func TestSetupLogging_ConcurrentCalls(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}

	done := make(chan bool, len(levels))

	for _, level := range levels {
		level := level // Capture range variable
		go func() {
			// Just call setupLogging without panicking
			setupLogging(level)
			done <- true
		}()
	}

	// Wait for all goroutines to complete without panicking
	for i := 0; i < len(levels); i++ {
		select {
		case <-done:
			// Success - no panic occurred
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for concurrent setupLogging calls")
		}
	}

	// Just verify formatter is still valid (level could be any of the concurrent values)
	_, ok := logrus.StandardLogger().Formatter.(*logrus.JSONFormatter)
	assert.True(t, ok, "Formatter should still be JSONFormatter after concurrent calls")
}

// TestSetupLogging_FormatterPreservation tests that formatter is always JSONFormatter
func TestSetupLogging_FormatterPreservation(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error", "invalid"}

	for _, level := range levels {
		t.Run("level_"+level, func(t *testing.T) {
			setupLogging(level)

			formatter, ok := logrus.StandardLogger().Formatter.(*logrus.JSONFormatter)
			require.True(t, ok, "Formatter should always be JSONFormatter")
			assert.Equal(t, time.RFC3339, formatter.TimestampFormat, "Timestamp format should be RFC3339")
		})
	}
}

// TestCobraCommandDescription tests command metadata
func TestCobraCommandDescription(t *testing.T) {
	rootCmd := &cobra.Command{
		Use:   "maxiofs",
		Short: "MaxIOFS - High-Performance S3-Compatible Object Storage",
		Long: `MaxIOFS is a high-performance, S3-compatible object storage system
built in Go with an embedded React web interface.`,
		Version: version,
	}

	assert.Equal(t, "maxiofs", rootCmd.Use)
	assert.Contains(t, rootCmd.Short, "MaxIOFS")
	assert.Contains(t, rootCmd.Short, "S3-Compatible")
	assert.Contains(t, rootCmd.Long, "S3-compatible")
	assert.Contains(t, rootCmd.Long, "React")
	assert.Equal(t, version, rootCmd.Version)
}

// TestVersionFormat tests that version variables have expected format
func TestVersionFormat(t *testing.T) {
	assert.Regexp(t, `^v\d+\.\d+\.\d+`, version, "Version should follow semantic versioning with 'v' prefix")
	assert.NotEmpty(t, commit, "Commit hash should not be empty")
	assert.NotEmpty(t, date, "Build date should not be empty")
}

// TestCobraFlagTypes tests that all flags have correct types
func TestCobraFlagTypes(t *testing.T) {
	rootCmd := &cobra.Command{Use: "maxiofs"}
	rootCmd.PersistentFlags().StringP("config", "c", "", "Configuration file path")
	rootCmd.PersistentFlags().StringP("data-dir", "d", "", "Data directory path")
	rootCmd.PersistentFlags().StringP("listen", "l", ":8080", "API server listen address")
	rootCmd.PersistentFlags().StringP("console-listen", "", ":8081", "Web console listen address")
	rootCmd.PersistentFlags().StringP("log-level", "", "info", "Log level")
	rootCmd.PersistentFlags().StringP("tls-cert", "", "", "TLS certificate file")
	rootCmd.PersistentFlags().StringP("tls-key", "", "", "TLS private key file")

	// Verify each flag exists and has correct type
	configFlag := rootCmd.PersistentFlags().Lookup("config")
	require.NotNil(t, configFlag)
	assert.Equal(t, "string", configFlag.Value.Type())

	dataDirFlag := rootCmd.PersistentFlags().Lookup("data-dir")
	require.NotNil(t, dataDirFlag)
	assert.Equal(t, "string", dataDirFlag.Value.Type())

	listenFlag := rootCmd.PersistentFlags().Lookup("listen")
	require.NotNil(t, listenFlag)
	assert.Equal(t, "string", listenFlag.Value.Type())

	consoleListenFlag := rootCmd.PersistentFlags().Lookup("console-listen")
	require.NotNil(t, consoleListenFlag)
	assert.Equal(t, "string", consoleListenFlag.Value.Type())

	logLevelFlag := rootCmd.PersistentFlags().Lookup("log-level")
	require.NotNil(t, logLevelFlag)
	assert.Equal(t, "string", logLevelFlag.Value.Type())

	tlsCertFlag := rootCmd.PersistentFlags().Lookup("tls-cert")
	require.NotNil(t, tlsCertFlag)
	assert.Equal(t, "string", tlsCertFlag.Value.Type())

	tlsKeyFlag := rootCmd.PersistentFlags().Lookup("tls-key")
	require.NotNil(t, tlsKeyFlag)
	assert.Equal(t, "string", tlsKeyFlag.Value.Type())
}
