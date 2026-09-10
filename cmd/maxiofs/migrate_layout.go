package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/maxiofs/maxiofs/internal/layout"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newMigrateLayoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate-layout",
		Short: "Move stored objects to the current on-disk layout",
		Long: `Moves every object from the layout that used the object key as its path to
the current one, where the file name is a digest of the key.

The server runs this on its own at startup. Run it by hand to see the outcome
first, or to migrate a large deployment outside a maintenance window.

Nothing is removed before its replacement is in place and verified, and a run
that is interrupted picks up where it stopped. It refuses to start if two
buckets share a name across tenants, or if a bucket is named after a tenant,
because the new layout cannot tell those apart.

Run with the server STOPPED. Use --dry-run first.`,
		Example: `  maxiofs migrate-layout --data-dir /var/lib/maxiofs --dry-run
  maxiofs migrate-layout --data-dir /var/lib/maxiofs`,
		RunE: runMigrateLayout,
	}
	cmd.Flags().Bool("dry-run", false, "Report what would move without touching anything")
	cmd.Flags().String("root", "", "Storage root (default: <data-dir>/objects)")
	return cmd
}

func runMigrateLayout(cmd *cobra.Command, args []string) error {
	dataDir, _ := cmd.Flags().GetString("data-dir")
	if dataDir == "" {
		return fmt.Errorf("--data-dir is required")
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	root, _ := cmd.Flags().GetString("root")
	if root == "" {
		root = filepath.Join(dataDir, "objects")
	}

	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: dataDir,
		Logger:  logrus.StandardLogger(),
	})
	if err != nil {
		return fmt.Errorf("could not open the metadata store: %w", err)
	}
	defer store.Close() //nolint:errcheck

	report, err := layout.Migrate(context.Background(), layout.Options{
		Root:   root,
		Store:  store,
		Logger: logrus.StandardLogger(),
		DryRun: dryRun,
	})
	if err != nil {
		return err
	}

	printLayoutReport(report)
	if len(report.Failures) > 0 {
		return fmt.Errorf("migration completed with %d failure(s) — review the report above", len(report.Failures))
	}
	return nil
}

func printLayoutReport(report *layout.Report) {
	fmt.Println()
	fmt.Println("=== Storage layout migration ===")
	if report.AlreadyCurrent {
		fmt.Println("Already on the current layout — nothing to move.")
		printLayoutList("Bucket directories the index does not know (left untouched)", report.Stranded)
		return
	}
	if report.DryRun {
		fmt.Println("DRY RUN — nothing was written.")
	}
	fmt.Printf("Buckets migrated:   %d\n", report.Buckets)
	fmt.Printf("Buckets skipped:    %d (already done in an earlier run)\n", report.BucketsSkipped)
	fmt.Printf("Objects moved:      %d\n", report.ObjectsMoved)
	fmt.Printf("Versions moved:     %d\n", report.VersionsMoved)
	fmt.Printf("Folder markers:     %d\n", report.MarkersCreated)
	fmt.Printf("Bytes moved:        %d\n", report.BytesMoved)

	printLayoutList("Bucket directories the index does not know (left untouched)", report.Stranded)
	printLayoutList("Indexed objects with no data on disk", report.MissingData)
	printLayoutList("Objects that could not be migrated", report.Damaged)
	printLayoutList("Failures", report.Failures)
}

func printLayoutList(title string, entries []string) {
	if len(entries) == 0 {
		return
	}
	fmt.Printf("\n%s (%d):\n", title, len(entries))
	for _, entry := range entries {
		fmt.Printf("  %s\n", entry)
	}
}
