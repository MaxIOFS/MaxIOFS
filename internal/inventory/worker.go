package inventory

import (
	"context"
	"fmt"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// Worker handles periodic inventory report generation
type Worker struct {
	manager       *Manager
	generator     *ReportGenerator
	bucketManager bucket.Manager
	ticker        *time.Ticker
	stopChan      chan struct{}
	log           *logrus.Entry
}

// NewWorker creates a new inventory worker
func NewWorker(
	manager *Manager,
	bucketManager bucket.Manager,
	metadataStore metadata.Store,
	storageBackend storage.Backend,
) *Worker {
	return &Worker{
		manager:       manager,
		generator:     NewReportGenerator(bucketManager, metadataStore, storageBackend),
		bucketManager: bucketManager,
		stopChan:      make(chan struct{}),
		log:           logrus.WithField("component", "inventory_worker"),
	}
}

// Start begins the inventory worker
func (w *Worker) Start(ctx context.Context, interval time.Duration) {
	w.ticker = time.NewTicker(interval)

	w.log.WithField("interval", interval).Info("Inventory worker started")

	// Run immediately on start
	go w.processInventories(ctx)

	go func() {
		for {
			select {
			case <-w.ticker.C:
				w.processInventories(ctx)
			case <-w.stopChan:
				w.ticker.Stop()
				w.log.Info("Inventory worker stopped")
				return
			case <-ctx.Done():
				w.ticker.Stop()
				w.log.Info("Inventory worker stopped due to context cancellation")
				return
			}
		}
	}()
}

// Stop stops the inventory worker
func (w *Worker) Stop() {
	close(w.stopChan)
}

// processInventories processes all ready inventory configurations
func (w *Worker) processInventories(ctx context.Context) {
	w.log.Debug("Processing inventory configurations...")

	// Get all configurations that are ready to run
	configs, err := w.manager.ListReadyConfigs(ctx)
	if err != nil {
		w.log.WithError(err).Error("Failed to list ready inventory configurations")
		return
	}

	if len(configs) == 0 {
		w.log.Debug("No inventory configurations ready to run")
		return
	}

	w.log.WithField("count", len(configs)).Info("Found inventory configurations ready to run")

	// Process each configuration
	for _, config := range configs {
		if err := w.processInventoryConfig(ctx, config); err != nil {
			w.log.WithError(err).WithFields(logrus.Fields{
				"bucket":    config.BucketName,
				"config_id": config.ID,
			}).Error("Failed to process inventory configuration")
			continue
		}
	}
}

// processInventoryConfig processes a single inventory configuration
func (w *Worker) processInventoryConfig(ctx context.Context, config *InventoryConfig) error {
	w.log.WithFields(logrus.Fields{
		"bucket":    config.BucketName,
		"config_id": config.ID,
		"frequency": config.Frequency,
	}).Info("Processing inventory configuration")

	// Validate that source bucket exists
	_, err := w.bucketManager.GetBucketInfo(ctx, config.TenantID, config.BucketName)
	if err != nil {
		return w.recordFailure(ctx, config, fmt.Sprintf("Source bucket not found: %v", err))
	}

	// Validate that destination bucket exists
	_, err = w.bucketManager.GetBucketInfo(ctx, config.TenantID, config.DestinationBucket)
	if err != nil {
		return w.recordFailure(ctx, config, fmt.Sprintf("Destination bucket not found: %v", err))
	}

	// Check for circular reference
	if config.BucketName == config.DestinationBucket {
		return w.recordFailure(ctx, config, "Circular reference: source and destination buckets are the same")
	}

	// Create initial report record
	report := &InventoryReport{
		ConfigID:   config.ID,
		BucketName: config.BucketName,
		Status:     "pending",
	}

	if err := w.manager.CreateReport(ctx, report); err != nil {
		return fmt.Errorf("failed to create report record: %w", err)
	}

	// Generate the inventory report
	startTime := time.Now().Unix()
	report.StartedAt = &startTime
	report.Status = "pending"

	if err := w.manager.UpdateReport(ctx, report); err != nil {
		w.log.WithError(err).Warn("Failed to update report start time")
	}

	// Generate report
	generatedReport, err := w.generator.GenerateReport(ctx, config)
	if err != nil {
		errorMsg := fmt.Sprintf("Report generation failed: %v", err)
		report.Status = "failed"
		report.ErrorMessage = &errorMsg
		completedTime := time.Now().Unix()
		report.CompletedAt = &completedTime

		if updateErr := w.manager.UpdateReport(ctx, report); updateErr != nil {
			w.log.WithError(updateErr).Warn("Failed to update report with error")
		}

		return fmt.Errorf("failed to generate report: %w", err)
	}

	// Update report with generated data
	report.ReportPath = generatedReport.ReportPath
	report.ObjectCount = generatedReport.ObjectCount
	report.TotalSize = generatedReport.TotalSize
	report.Status = "completed"
	report.CompletedAt = generatedReport.CompletedAt

	if err := w.manager.UpdateReport(ctx, report); err != nil {
		w.log.WithError(err).Warn("Failed to update report with completion data")
	}

	// Update configuration with last run time and calculate next run
	lastRunTime := time.Now().Unix()
	config.LastRunAt = &lastRunTime

	nextRunTime, err := CalculateNextRunTime(config.Frequency, config.ScheduleTime, &lastRunTime)
	if err != nil {
		w.log.WithError(err).Warn("Failed to calculate next run time")
	} else {
		config.NextRunAt = &nextRunTime
	}

	if err := w.manager.UpdateConfig(ctx, config); err != nil {
		w.log.WithError(err).Warn("Failed to update config with run times")
	}

	w.log.WithFields(logrus.Fields{
		"bucket":       config.BucketName,
		"config_id":    config.ID,
		"report_path":  report.ReportPath,
		"object_count": report.ObjectCount,
		"total_size":   report.TotalSize,
	}).Info("Inventory report generated successfully")

	return nil
}

// recordFailure records a failure for a configuration
func (w *Worker) recordFailure(ctx context.Context, config *InventoryConfig, errorMessage string) error {
	w.log.WithFields(logrus.Fields{
		"bucket":    config.BucketName,
		"config_id": config.ID,
		"error":     errorMessage,
	}).Warn("Inventory configuration validation failed")

	// Create failed report
	report := &InventoryReport{
		ConfigID:     config.ID,
		BucketName:   config.BucketName,
		Status:       "failed",
		ErrorMessage: &errorMessage,
	}

	now := time.Now().Unix()
	report.StartedAt = &now
	report.CompletedAt = &now

	if err := w.manager.CreateReport(ctx, report); err != nil {
		w.log.WithError(err).Warn("Failed to create failure report")
	}

	// Still update next run time so it will retry later
	nextRunTime, err := CalculateNextRunTime(config.Frequency, config.ScheduleTime, config.LastRunAt)
	if err == nil {
		config.NextRunAt = &nextRunTime
		if updateErr := w.manager.UpdateConfig(ctx, config); updateErr != nil {
			w.log.WithError(updateErr).Warn("Failed to update config next run time")
		}
	}

	return fmt.Errorf("inventory validation failed: %s", errorMessage)
}
