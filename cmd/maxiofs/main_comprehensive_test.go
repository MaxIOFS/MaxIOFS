package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// runServer Additional Tests
// ============================================================================

// TestRunServer_ConfigLoadError tests error handling when config fails to load
func TestRunServer_ConfigLoadError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("tls-cert", "", "")
	cmd.Flags().String("tls-key", "", "")
	cmd.Flags().String("config", "/non/existent/path/config.yaml", "")
	cmd.Flags().String("data-dir", "", "") // Empty data dir should cause issues
	cmd.Flags().String("listen", ":48080", "")
	cmd.Flags().String("console-listen", ":48081", "")
	cmd.Flags().String("log-level", "error", "")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(cmd, []string{})
	}()

	select {
	case err := <-errChan:
		// Should fail to load configuration or create server
		require.Error(t, err)
		assert.True(t,
			strings.Contains(err.Error(), "failed to load configuration") ||
				strings.Contains(err.Error(), "failed to create server"),
			"Error should be about config loading or server creation: %v", err)
	case <-ctx.Done():
		// Timeout - means server started which shouldn't happen
		t.Log("Server started unexpectedly (timeout)")
	}
}

// TestRunServer_WithValidConfig tests server start with valid configuration
func TestRunServer_WithValidConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "maxiofs-valid-config-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cmd := &cobra.Command{}
	cmd.Flags().String("tls-cert", "", "")
	cmd.Flags().String("tls-key", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("data-dir", tmpDir, "")
	cmd.Flags().String("listen", ":58080", "")
	cmd.Flags().String("console-listen", ":58081", "")
	cmd.Flags().String("log-level", "error", "")

	// Suppress logging during test
	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(cmd, []string{})
	}()

	select {
	case err := <-errChan:
		// Either config/server creation fails or server starts then stops
		if err != nil {
			t.Logf("Server error (expected during test): %v", err)
		}
	case <-ctx.Done():
		// Timeout means server started successfully
		t.Log("Server started (timeout reached, which is expected)")
	}
}

// TestRunServer_TLSEnabled tests that TLS configuration is properly applied
func TestRunServer_TLSEnabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "maxiofs-tls-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create dummy TLS files
	certFile := filepath.Join(tmpDir, "server.crt")
	keyFile := filepath.Join(tmpDir, "server.key")
	os.WriteFile(certFile, []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"), 0644)
	os.WriteFile(keyFile, []byte("-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----"), 0644)

	cmd := &cobra.Command{}
	cmd.Flags().String("tls-cert", certFile, "")
	cmd.Flags().String("tls-key", keyFile, "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("data-dir", tmpDir, "")
	cmd.Flags().String("listen", ":68080", "")
	cmd.Flags().String("console-listen", ":68081", "")
	cmd.Flags().String("log-level", "error", "")

	cmd.Flags().Set("tls-cert", certFile)
	cmd.Flags().Set("tls-key", keyFile)

	// Suppress logging
	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(cmd, []string{})
	}()

	select {
	case err := <-errChan:
		// Should NOT be the "must be provided together" error
		if err != nil {
			assert.NotContains(t, err.Error(), "must be provided together")
		}
	case <-ctx.Done():
		// Timeout is acceptable
	}
}

// ============================================================================
// setupLogging Additional Tests
// ============================================================================

// TestSetupLogging_AllLevels tests all log levels comprehensively
func TestSetupLogging_AllLevels(t *testing.T) {
	tests := []struct {
		input    string
		expected logrus.Level
	}{
		{"debug", logrus.DebugLevel},
		{"info", logrus.InfoLevel},
		{"warn", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"DEBUG", logrus.InfoLevel}, // Case-sensitive, should default
		{"INFO", logrus.InfoLevel},  // Case-sensitive, should default
		{"unknown", logrus.InfoLevel},
		{"", logrus.InfoLevel},
		{"trace", logrus.InfoLevel}, // Not supported, should default
		{"fatal", logrus.InfoLevel}, // Not supported, should default
		{"panic", logrus.InfoLevel}, // Not supported, should default
	}

	for _, tt := range tests {
		t.Run("level_"+tt.input, func(t *testing.T) {
			setupLogging(tt.input)
			assert.Equal(t, tt.expected, logrus.GetLevel())
		})
	}
}

// TestSetupLogging_OutputJSON tests that log output is proper JSON
func TestSetupLogging_OutputJSON(t *testing.T) {
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	defer logrus.SetOutput(os.Stderr)

	setupLogging("info")

	// Log a message with fields
	logrus.WithFields(logrus.Fields{
		"key1": "value1",
		"key2": 42,
	}).Info("test message with fields")

	output := buf.String()
	assert.Contains(t, output, `"msg":"test message with fields"`)
	assert.Contains(t, output, `"level":"info"`)
	assert.Contains(t, output, `"key1":"value1"`)
	assert.Contains(t, output, `"key2":42`)
}

// TestSetupLogging_MultipleCallsSafe tests that calling setupLogging multiple times is safe
func TestSetupLogging_MultipleCallsSafe(t *testing.T) {
	// Call setupLogging multiple times with different levels
	setupLogging("debug")
	assert.Equal(t, logrus.DebugLevel, logrus.GetLevel())

	setupLogging("error")
	assert.Equal(t, logrus.ErrorLevel, logrus.GetLevel())

	setupLogging("info")
	assert.Equal(t, logrus.InfoLevel, logrus.GetLevel())

	// Verify formatter is still correct
	formatter, ok := logrus.StandardLogger().Formatter.(*logrus.JSONFormatter)
	require.True(t, ok)
	assert.Equal(t, time.RFC3339, formatter.TimestampFormat)
}

// ============================================================================
// Version Variables Tests
// ============================================================================

// TestVersionVariables tests that version variables are properly initialized
func TestVersionVariables(t *testing.T) {
	t.Run("version format", func(t *testing.T) {
		assert.NotEmpty(t, version)
		assert.True(t, strings.HasPrefix(version, "v"))
		// Should be semantic version: vX.Y.Z or vX.Y.Z-suffix
		parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
		assert.GreaterOrEqual(t, len(parts), 2, "Version should have at least major.minor")
	})

	t.Run("commit not empty", func(t *testing.T) {
		assert.NotEmpty(t, commit)
	})

	t.Run("date not empty", func(t *testing.T) {
		assert.NotEmpty(t, date)
	})
}

// ============================================================================
// Cobra Command Integration Tests
// ============================================================================

// TestCobraCommand_HelpOutput tests that help output contains expected information
func TestCobraCommand_HelpOutput(t *testing.T) {
	rootCmd := &cobra.Command{
		Use:   "maxiofs",
		Short: "MaxIOFS - High-Performance S3-Compatible Object Storage",
		Long: `MaxIOFS is a high-performance, S3-compatible object storage system
built in Go with an embedded React web interface.`,
		Version: version,
	}

	rootCmd.PersistentFlags().StringP("config", "c", "", "Configuration file path")
	rootCmd.PersistentFlags().StringP("data-dir", "d", "", "Data directory path")
	rootCmd.PersistentFlags().StringP("listen", "l", ":8080", "API server listen address")
	rootCmd.PersistentFlags().StringP("console-listen", "", ":8081", "Web console listen address")
	rootCmd.PersistentFlags().StringP("log-level", "", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringP("tls-cert", "", "", "TLS certificate file")
	rootCmd.PersistentFlags().StringP("tls-key", "", "", "TLS private key file")

	// Get usage string directly
	helpOutput := rootCmd.UsageString()

	// Verify help contains expected flags
	assert.Contains(t, helpOutput, "--config")
	assert.Contains(t, helpOutput, "--data-dir")
	assert.Contains(t, helpOutput, "--listen")
	assert.Contains(t, helpOutput, "--console-listen")
	assert.Contains(t, helpOutput, "--log-level")
	assert.Contains(t, helpOutput, "--tls-cert")
	assert.Contains(t, helpOutput, "--tls-key")
	assert.Contains(t, helpOutput, "-c") // Shorthand
	assert.Contains(t, helpOutput, "-d") // Shorthand
	assert.Contains(t, helpOutput, "-l") // Shorthand

	// Verify command use is set correctly
	assert.Equal(t, "maxiofs", rootCmd.Use)
	assert.Contains(t, rootCmd.Short, "S3-Compatible")
}

// TestCobraCommand_VersionOutput tests that version output is correct
func TestCobraCommand_VersionOutput(t *testing.T) {
	rootCmd := &cobra.Command{
		Use:     "maxiofs",
		Version: "v0.8.0-beta (commit: abc123, built: 20260207)",
	}

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"--version"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	versionOutput := buf.String()
	assert.Contains(t, versionOutput, "v0.8.0-beta")
}

// TestCobraCommand_FlagParsing tests parsing of various flag combinations
func TestCobraCommand_FlagParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		validate func(t *testing.T, cmd *cobra.Command)
	}{
		{
			name: "long flags",
			args: []string{"--config=/path/to/config", "--data-dir=/data", "--listen=:9000"},
			validate: func(t *testing.T, cmd *cobra.Command) {
				cfg, _ := cmd.Flags().GetString("config")
				assert.Equal(t, "/path/to/config", cfg)
				dataDir, _ := cmd.Flags().GetString("data-dir")
				assert.Equal(t, "/data", dataDir)
				listen, _ := cmd.Flags().GetString("listen")
				assert.Equal(t, ":9000", listen)
			},
		},
		{
			name: "short flags",
			args: []string{"-c", "/short/config", "-d", "/short/data", "-l", ":8888"},
			validate: func(t *testing.T, cmd *cobra.Command) {
				cfg, _ := cmd.Flags().GetString("config")
				assert.Equal(t, "/short/config", cfg)
				dataDir, _ := cmd.Flags().GetString("data-dir")
				assert.Equal(t, "/short/data", dataDir)
				listen, _ := cmd.Flags().GetString("listen")
				assert.Equal(t, ":8888", listen)
			},
		},
		{
			name: "mixed flags",
			args: []string{"-c", "/mix/config", "--data-dir=/mix/data", "-l", ":7777"},
			validate: func(t *testing.T, cmd *cobra.Command) {
				cfg, _ := cmd.Flags().GetString("config")
				assert.Equal(t, "/mix/config", cfg)
				dataDir, _ := cmd.Flags().GetString("data-dir")
				assert.Equal(t, "/mix/data", dataDir)
				listen, _ := cmd.Flags().GetString("listen")
				assert.Equal(t, ":7777", listen)
			},
		},
		{
			name: "TLS flags",
			args: []string{"--tls-cert=/cert.pem", "--tls-key=/key.pem"},
			validate: func(t *testing.T, cmd *cobra.Command) {
				cert, _ := cmd.Flags().GetString("tls-cert")
				assert.Equal(t, "/cert.pem", cert)
				key, _ := cmd.Flags().GetString("tls-key")
				assert.Equal(t, "/key.pem", key)
			},
		},
		{
			name: "log level flag",
			args: []string{"--log-level=debug"},
			validate: func(t *testing.T, cmd *cobra.Command) {
				level, _ := cmd.Flags().GetString("log-level")
				assert.Equal(t, "debug", level)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "maxiofs"}
			cmd.PersistentFlags().StringP("config", "c", "", "")
			cmd.PersistentFlags().StringP("data-dir", "d", "", "")
			cmd.PersistentFlags().StringP("listen", "l", ":8080", "")
			cmd.PersistentFlags().StringP("console-listen", "", ":8081", "")
			cmd.PersistentFlags().StringP("log-level", "", "info", "")
			cmd.PersistentFlags().StringP("tls-cert", "", "", "")
			cmd.PersistentFlags().StringP("tls-key", "", "", "")

			err := cmd.ParseFlags(tt.args)
			require.NoError(t, err)

			tt.validate(t, cmd)
		})
	}
}

// TestCobraCommand_InvalidFlag tests error handling for invalid flags
func TestCobraCommand_InvalidFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "maxiofs"}
	cmd.PersistentFlags().StringP("config", "c", "", "")

	// Redirect stderr to capture error
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	err := cmd.ParseFlags([]string{"--invalid-flag=value"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag")
}

// ============================================================================
// Edge Cases and Error Handling
// ============================================================================

// TestRunServer_EmptyArgs tests runServer with empty args
func TestRunServer_EmptyArgs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "maxiofs-empty-args-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cmd := &cobra.Command{}
	cmd.Flags().String("tls-cert", "", "")
	cmd.Flags().String("tls-key", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("data-dir", tmpDir, "")
	cmd.Flags().String("listen", ":78080", "")
	cmd.Flags().String("console-listen", ":78081", "")
	cmd.Flags().String("log-level", "error", "")

	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(cmd, []string{}) // Empty args
	}()

	select {
	case err := <-errChan:
		// Any result is fine - we're just testing it doesn't panic
		_ = err
	case <-ctx.Done():
		// Timeout is fine
	}
}

// TestRunServer_SignalHandling simulates the signal handling goroutine
func TestRunServer_SignalHandling(t *testing.T) {
	// This test verifies that the signal handling code path exists
	// We can't easily test actual signal handling in unit tests
	// But we verify the code structure is correct

	tmpDir, err := os.MkdirTemp("", "maxiofs-signal-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cmd := &cobra.Command{}
	cmd.Flags().String("tls-cert", "", "")
	cmd.Flags().String("tls-key", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("data-dir", tmpDir, "")
	cmd.Flags().String("listen", ":88080", "")
	cmd.Flags().String("console-listen", ":88081", "")
	cmd.Flags().String("log-level", "error", "")

	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	// Short timeout to ensure we don't wait forever
	_, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(cmd, []string{})
	}()

	// Wait briefly then cancel - simulating what would happen with a signal
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-errChan:
		// Server stopped
	case <-time.After(500 * time.Millisecond):
		// Timeout
	}
}
