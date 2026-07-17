package main

import (
	"fmt"

	"github.com/maxiofs/maxiofs/internal/recovery"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// newRepairPointersCmd rebuilds "latest object" pointers (obj:bucket:key) that
// were wrongly deleted by the faulty reconcile, from the surviving per-version
// entries. Strictly additive: only writes missing pointers, never deletes,
// never touches object files. Run with the server STOPPED.
func newRepairPointersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair-pointers",
		Short: "Rebuild deleted latest-object pointers from surviving version entries",
		Long: `Rebuilds the obj:bucket:key "latest" pointers that a faulty metadata
reconcile deleted, using the per-version entries (version:bucket:key:versionID)
that were NOT deleted — a separate keyspace carrying the full object metadata
including Object Lock retention/legal-hold.

The operation is strictly ADDITIVE: it only writes pointers that are currently
MISSING, copying them from the latest surviving version. It never deletes any
key, never modifies a version entry, and never touches object files on disk.
Object Lock / immutability is preserved because it is copied from the version
entry.

Run with the server STOPPED. Use --dry-run first to see how many pointers would
be rebuilt without changing anything.`,
		Example: `  maxiofs repair-pointers --data-dir /var/lib/maxiofs --dry-run
  maxiofs repair-pointers --data-dir /var/lib/maxiofs`,
		RunE: runRepairPointers,
	}
	cmd.Flags().Bool("dry-run", false, "Scan and report without writing anything")
	return cmd
}

func runRepairPointers(cmd *cobra.Command, args []string) error {
	dataDir, _ := cmd.Flags().GetString("data-dir")
	if dataDir == "" {
		return fmt.Errorf("--data-dir is required")
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	report, err := recovery.RepairLatestPointers(dataDir, dryRun, logrus.StandardLogger())
	if err != nil {
		if report != nil {
			printRepairReport(report, dryRun)
		}
		return err
	}
	printRepairReport(report, dryRun)
	if len(report.Failures) > 0 {
		return fmt.Errorf("repair completed with %d failure(s) — review the report above", len(report.Failures))
	}
	return nil
}

func printRepairReport(report *recovery.RepairReport, dryRun bool) {
	fmt.Println()
	fmt.Println("=== Pointer repair report ===")
	fmt.Printf("Version entries scanned:   %d\n", report.VersionKeysScanned)
	fmt.Printf("Distinct objects:          %d\n", report.DistinctObjects)
	fmt.Printf("Pointers already present:  %d\n", report.PointersPresent)
	if dryRun {
		fmt.Printf("Pointers that WOULD be rebuilt: %d\n", report.PointersRebuilt)
	} else {
		fmt.Printf("Pointers rebuilt:          %d\n", report.PointersRebuilt)
	}
	fmt.Printf("Latest = delete marker (left absent): %d\n", report.DeleteMarkerLatest)
	if len(report.Failures) > 0 {
		fmt.Printf("FAILURES (%d):\n", len(report.Failures))
		max := len(report.Failures)
		if max > 20 {
			max = 20
		}
		for _, f := range report.Failures[:max] {
			fmt.Printf("  - %s\n", f)
		}
		if len(report.Failures) > 20 {
			fmt.Printf("  ... and %d more\n", len(report.Failures)-20)
		}
	}
	if dryRun {
		fmt.Println("\nDry run — nothing was written. Re-run without --dry-run to apply.")
	} else {
		fmt.Println("\nDone. Start the server; recovered objects list and download normally.")
	}
}
