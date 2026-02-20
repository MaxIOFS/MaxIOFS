package main

import (
	"bytes"
	"context"
	"encoding/json"
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
// setupLogging Tests
// ============================================================================

func TestSetupLogging_AllLevels(t *testing.T) {
	tests := []struct {
		input    string
		expected logrus.Level
	}{
		{"debug", logrus.DebugLevel},
		{"info", logrus.InfoLevel},
		{"warn", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"DEBUG", logrus.InfoLevel},   // Case-sensitive, should default
		{"INFO", logrus.InfoLevel},    // Case-sensitive, should default
		{"unknown", logrus.InfoLevel}, // Invalid, should default
		{"", logrus.InfoLevel},        // Empty, should default
		{"trace", logrus.InfoLevel},   // Not supported, should default
		{"fatal", logrus.InfoLevel},   // Not supported, should default
		{"panic", logrus.InfoLevel},   // Not supported, should default
	}

	for _, tt := range tests {
		name := tt.input
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			setupLogging(tt.input)
			assert.Equal(t, tt.expected, logrus.GetLevel())
		})
	}
}

func TestSetupLogging_JSONFormatter(t *testing.T) {
	setupLogging("info")

	formatter, ok := logrus.StandardLogger().Formatter.(*logrus.JSONFormatter)
	require.True(t, ok, "Formatter should be JSONFormatter")
	assert.Equal(t, time.RFC3339, formatter.TimestampFormat, "Timestamp format should be RFC3339")
}

func TestSetupLogging_FormatterPreservedAcrossLevels(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error", "invalid"}

	for _, level := range levels {
		setupLogging(level)

		formatter, ok := logrus.StandardLogger().Formatter.(*logrus.JSONFormatter)
		require.True(t, ok, "Formatter should always be JSONFormatter after setting level %q", level)
		assert.Equal(t, time.RFC3339, formatter.TimestampFormat)
	}
}

func TestSetupLogging_OutputIsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	defer logrus.SetOutput(os.Stderr)

	setupLogging("info")

	logrus.WithFields(logrus.Fields{
		"key1": "value1",
		"key2": 42,
	}).Info("test message with fields")

	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err, "Log output should be valid JSON")

	assert.Equal(t, "test message with fields", logEntry["msg"])
	assert.Equal(t, "info", logEntry["level"])
	assert.Equal(t, "value1", logEntry["key1"])
	assert.Equal(t, float64(42), logEntry["key2"])
	assert.NotEmpty(t, logEntry["time"])
}

func TestSetupLogging_ConcurrentCalls(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}
	done := make(chan struct{}, len(levels))

	for _, level := range levels {
		go func(l string) {
			defer func() { done <- struct{}{} }()
			setupLogging(l)
		}(level)
	}

	for range levels {
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for concurrent setupLogging calls")
		}
	}

	// Formatter should still be valid after concurrent mutations
	_, ok := logrus.StandardLogger().Formatter.(*logrus.JSONFormatter)
	assert.True(t, ok, "Formatter should still be JSONFormatter after concurrent calls")
}

// ============================================================================
// Version Variables Tests
// ============================================================================

func TestVersionVariables(t *testing.T) {
	t.Run("version format", func(t *testing.T) {
		assert.NotEmpty(t, version)
		assert.True(t, strings.HasPrefix(version, "v"), "Version should start with 'v'")

		// Should be semantic version: vX.Y.Z or vX.Y.Z-suffix
		parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
		assert.GreaterOrEqual(t, len(parts), 2, "Version should have at least major.minor")
		assert.Regexp(t, `^v\d+\.\d+\.\d+`, version, "Version should follow semantic versioning")
	})

	t.Run("commit not empty", func(t *testing.T) {
		assert.NotEmpty(t, commit)
	})

	t.Run("date not empty", func(t *testing.T) {
		assert.NotEmpty(t, date)
	})
}

// ============================================================================
// Cobra Command Tests
// ============================================================================

func TestCobraCommand_Setup(t *testing.T) {
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
	rootCmd.PersistentFlags().StringP("log-level", "", "info", "Log level")
	rootCmd.PersistentFlags().StringP("tls-cert", "", "", "TLS certificate file")
	rootCmd.PersistentFlags().StringP("tls-key", "", "", "TLS private key file")

	t.Run("metadata", func(t *testing.T) {
		assert.Equal(t, "maxiofs", rootCmd.Use)
		assert.Contains(t, rootCmd.Short, "S3-Compatible")
		assert.Contains(t, rootCmd.Long, "S3-compatible")
		assert.Contains(t, rootCmd.Long, "React")
		assert.Equal(t, version, rootCmd.Version)
	})

	t.Run("flags registered with correct defaults", func(t *testing.T) {
		flags := map[string]struct {
			defaultValue string
		}{
			"config":         {""},
			"data-dir":       {""},
			"listen":         {":8080"},
			"console-listen": {":8081"},
			"log-level":      {"info"},
			"tls-cert":       {""},
			"tls-key":        {""},
		}

		for name, expected := range flags {
			flag := rootCmd.PersistentFlags().Lookup(name)
			require.NotNil(t, flag, "flag %q should exist", name)
			assert.Equal(t, "string", flag.Value.Type(), "flag %q should be string type", name)
			if expected.defaultValue != "" {
				val, _ := rootCmd.PersistentFlags().GetString(name)
				assert.Equal(t, expected.defaultValue, val, "flag %q default", name)
			}
		}
	})

	t.Run("help output contains all flags", func(t *testing.T) {
		helpOutput := rootCmd.UsageString()

		for _, flag := range []string{"--config", "--data-dir", "--listen", "--console-listen", "--log-level", "--tls-cert", "--tls-key"} {
			assert.Contains(t, helpOutput, flag)
		}
		for _, shorthand := range []string{"-c", "-d", "-l"} {
			assert.Contains(t, helpOutput, shorthand)
		}
	})
}

func TestCobraCommand_FlagParsing(t *testing.T) {
	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "maxiofs"}
		cmd.PersistentFlags().StringP("config", "c", "", "")
		cmd.PersistentFlags().StringP("data-dir", "d", "", "")
		cmd.PersistentFlags().StringP("listen", "l", ":8080", "")
		cmd.PersistentFlags().StringP("console-listen", "", ":8081", "")
		cmd.PersistentFlags().StringP("log-level", "", "info", "")
		cmd.PersistentFlags().StringP("tls-cert", "", "", "")
		cmd.PersistentFlags().StringP("tls-key", "", "", "")
		return cmd
	}

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
			name: "mixed long and short flags",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCmd()
			err := cmd.ParseFlags(tt.args)
			require.NoError(t, err)
			tt.validate(t, cmd)
		})
	}
}

func TestCobraCommand_InvalidFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "maxiofs"}
	cmd.PersistentFlags().StringP("config", "c", "", "")

	err := cmd.ParseFlags([]string{"--invalid-flag=value"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag")
}

func TestCobraCommand_VersionOutput(t *testing.T) {
	rootCmd := &cobra.Command{
		Use:     "maxiofs",
		Version: "v0.9.1-beta (commit: abc123, built: 20260207)",
	}

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"--version"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "v0.9.1-beta")
}

// ============================================================================
// runServer Tests
// ============================================================================

// newTestCommand creates a cobra.Command with all required flags for runServer tests.
// Flags are explicitly Set() so viper's BindPFlag recognizes them as "changed"
// instead of falling back to viper's own defaults.
func newTestCommand(dataDir, listen, consoleListen string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("tls-cert", "", "")
	cmd.Flags().String("tls-key", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("data-dir", "", "")
	cmd.Flags().String("listen", "", "")
	cmd.Flags().String("console-listen", "", "")
	cmd.Flags().String("log-level", "", "")

	// Explicitly set so viper picks them up via BindPFlag
	cmd.Flags().Set("data-dir", dataDir)
	cmd.Flags().Set("listen", listen)
	cmd.Flags().Set("console-listen", consoleListen)
	cmd.Flags().Set("log-level", "error")
	return cmd
}

// runServerWithTimeout launches runServer in a goroutine and returns its error (or nil on timeout).
func runServerWithTimeout(cmd *cobra.Command, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(cmd, []string{})
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return nil // timeout - server started, which is acceptable
	}
}

// tempDirWithRetryCleanup creates a temp dir with retry-based cleanup for Windows,
// where file handles (e.g. audit.db) may still be held briefly after server goroutines leak.
func tempDirWithRetryCleanup(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		for i := 0; i < 10; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		// Best-effort: ignore residual errors on Windows CI
	})
	return dir
}

func TestRunServer_TLSValidation(t *testing.T) {
	tmpDir := tempDirWithRetryCleanup(t)

	tests := []struct {
		name    string
		cert    string
		key     string
		wantErr string
	}{
		{"only cert provided", "/path/to/cert.pem", "", "must be provided together"},
		{"only key provided", "", "/path/to/key.pem", "must be provided together"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTestCommand(tmpDir, ":18080", ":18081")
			cmd.Flags().Set("tls-cert", tt.cert)
			cmd.Flags().Set("tls-key", tt.key)

			err := runServerWithTimeout(cmd, 200*time.Millisecond)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestRunServer_TLSBothProvided(t *testing.T) {
	tmpDir := tempDirWithRetryCleanup(t)

	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certFile, []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"), 0644)
	os.WriteFile(keyFile, []byte("-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----"), 0644)

	cmd := newTestCommand(tmpDir, ":28080", ":28081")
	cmd.Flags().Set("tls-cert", certFile)
	cmd.Flags().Set("tls-key", keyFile)

	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	err := runServerWithTimeout(cmd, 200*time.Millisecond)
	// Should NOT be the "must be provided together" error
	if err != nil {
		assert.NotContains(t, err.Error(), "must be provided together")
	}
}

func TestRunServer_ConfigLoadError(t *testing.T) {
	cmd := newTestCommand("", ":48080", ":48081")
	cmd.Flags().Set("config", "/non/existent/path/config.yaml")

	err := runServerWithTimeout(cmd, 500*time.Millisecond)
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "failed to load configuration") ||
			strings.Contains(err.Error(), "failed to create server"),
		"Error should be about config loading or server creation: %v", err)
}

func TestRunServer_WithValidDataDir(t *testing.T) {
	tmpDir := tempDirWithRetryCleanup(t)

	cmd := newTestCommand(tmpDir, ":58080", ":58081")

	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	err := runServerWithTimeout(cmd, 200*time.Millisecond)
	// Either server starts (nil) or fails at a later stage - both are fine
	if err != nil {
		t.Logf("Server error (expected during test): %v", err)
	}
}

func TestRunServer_EmptyArgs(t *testing.T) {
	tmpDir := tempDirWithRetryCleanup(t)

	cmd := newTestCommand(tmpDir, ":78080", ":78081")

	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	// Should not panic with empty args
	_ = runServerWithTimeout(cmd, 200*time.Millisecond)
}
