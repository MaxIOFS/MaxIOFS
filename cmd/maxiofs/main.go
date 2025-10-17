package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/server"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	version = "0.2.0-dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "maxiofs",
		Short: "MaxIOFS - High-Performance S3-Compatible Object Storage",
		Long: `MaxIOFS is a high-performance, S3-compatible object storage system
built in Go with an embedded Next.js web interface.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		RunE:    runServer,
	}

	// Add configuration flags
	rootCmd.PersistentFlags().StringP("config", "c", "", "Configuration file path")
	rootCmd.PersistentFlags().StringP("data-dir", "d", "", "Data directory path")
	rootCmd.PersistentFlags().StringP("listen", "l", ":8080", "API server listen address")
	rootCmd.PersistentFlags().StringP("console-listen", "", ":8081", "Web console listen address")
	rootCmd.PersistentFlags().StringP("log-level", "", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringP("tls-cert", "", "", "TLS certificate file (enables TLS if provided with --tls-key)")
	rootCmd.PersistentFlags().StringP("tls-key", "", "", "TLS private key file (enables TLS if provided with --tls-cert)")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runServer(cmd *cobra.Command, args []string) error {
	// Get TLS flags
	tlsCert, _ := cmd.Flags().GetString("tls-cert")
	tlsKey, _ := cmd.Flags().GetString("tls-key")

	// Validate TLS configuration
	if (tlsCert != "" && tlsKey == "") || (tlsCert == "" && tlsKey != "") {
		return fmt.Errorf("both --tls-cert and --tls-key must be provided together")
	}

	// Load configuration
	cfg, err := config.Load(cmd)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Setup logging
	setupLogging(cfg.LogLevel)

	logrus.WithFields(logrus.Fields{
		"version": version,
		"commit":  commit,
		"date":    date,
	}).Info("Starting MaxIOFS")

	// Configure TLS if certificates are provided
	if tlsCert != "" && tlsKey != "" {
		logrus.WithFields(logrus.Fields{
			"cert_file": tlsCert,
			"key_file":  tlsKey,
		}).Info("TLS enabled - servers will use HTTPS")
		cfg.EnableTLS = true
		cfg.CertFile = tlsCert
		cfg.KeyFile = tlsKey
	} else {
		logrus.Info("TLS disabled - servers will use HTTP")
		cfg.EnableTLS = false
	}

	// Create server
	srv, err := server.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Start server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		logrus.Info("Received shutdown signal")
		cancel()
	}()

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	logrus.Info("MaxIOFS stopped")
	return nil
}

func setupLogging(level string) {
	logrus.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339,
	})

	switch level {
	case "debug":
		logrus.SetLevel(logrus.DebugLevel)
	case "info":
		logrus.SetLevel(logrus.InfoLevel)
	case "warn":
		logrus.SetLevel(logrus.WarnLevel)
	case "error":
		logrus.SetLevel(logrus.ErrorLevel)
	default:
		logrus.SetLevel(logrus.InfoLevel)
	}
}
