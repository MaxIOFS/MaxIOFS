package main

import (
	"context"
	"fmt"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/recovery"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newReconcileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Rebuild metadata entries for objects that are on disk but not in the index",
		Long: `Walks the stored objects and restores the metadata entry of anything the index
has forgotten, reading the object's identity from its sidecar.

The server does this by itself after an unclean shutdown. Run it by hand when an
object exists on disk but no longer lists — after a delete marker was removed
with no version behind it, for instance.

The pass only goes disk to index: it never removes an entry, never deletes a
file, and never overwrites an entry that is already there.

Run with the server STOPPED.`,
		Example: `  maxiofs reconcile --data-dir /var/lib/maxiofs`,
		RunE:    runReconcile,
	}
}

func runReconcile(cmd *cobra.Command, args []string) error {
	dataDir, _ := cmd.Flags().GetString("data-dir")
	if dataDir == "" {
		return fmt.Errorf("--data-dir is required")
	}

	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: dataDir,
		Logger:  logrus.StandardLogger(),
	})
	if err != nil {
		return fmt.Errorf("could not open the metadata store: %w", err)
	}
	defer store.Close() //nolint:errcheck

	report, err := recovery.Reconcile(context.Background(), dataDir, store, logrus.StandardLogger())
	if report != nil {
		fmt.Println()
		fmt.Println("=== Reconcile report ===")
		fmt.Printf("Buckets scanned:    %d\n", report.Buckets)
		fmt.Printf("Files scanned:      %d\n", report.FilesScanned)
		fmt.Printf("Entries restored:   %d\n", report.EntriesRestored)
		fmt.Printf("Versions restored:  %d\n", report.VersionsRestored)
		if len(report.Failures) > 0 {
			fmt.Printf("\nFailures (%d):\n", len(report.Failures))
			for _, f := range report.Failures {
				fmt.Printf("  %s\n", f)
			}
		}
	}
	if err != nil {
		return err
	}
	if report != nil && len(report.Failures) > 0 {
		return fmt.Errorf("reconcile finished with %d failure(s) — review the report above", len(report.Failures))
	}
	return nil
}
