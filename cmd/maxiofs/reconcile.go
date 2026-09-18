package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/recovery"
	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newReconcileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Rebuild metadata entries for objects that are on disk but not in the index",
		Long: `First undoes any write that was interrupted between storing an object's bytes
and committing its index entry, using the copy kept next to the object tree.

Then walks the stored objects and restores the metadata entry of anything the
index has forgotten, reading the object's identity from its sidecar, and
corrects entries that no longer describe the bytes on disk.

The server does this by itself after an unclean shutdown. Run it by hand when an
object exists on disk but no longer lists — after a delete marker was removed
with no version behind it, for instance.

The pass only goes disk to index: it never removes an entry, never deletes a
file, and never overwrites an entry that is already there.

Run with the server STOPPED.`,
		Example: `  maxiofs reconcile --data-dir /var/lib/maxiofs`,
		RunE:    runReconcile,
	}
	cmd.Flags().String("storage-root", "", "Object storage directory (defaults to <data-dir>/objects)")
	return cmd
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

	objectsRoot, _ := cmd.Flags().GetString("storage-root")
	if objectsRoot == "" {
		objectsRoot = filepath.Join(dataDir, "objects")
	}
	backend, err := storage.NewBackend(storage.Config{Backend: "filesystem", Root: objectsRoot})
	if err != nil {
		return fmt.Errorf("could not open the object store: %w", err)
	}
	undone, err := rollback.Undo(context.Background(), objectsRoot, backend, store, logrus.StandardLogger())
	if undone != nil {
		fmt.Println()
		fmt.Println("=== Interrupted writes ===")
		fmt.Printf("Objects rolled back: %d\n", undone.ObjectsRestored)
		fmt.Printf("Parts rolled back:   %d\n", undone.PartsRestored)
		fmt.Printf("Already committed:   %d\n", undone.Committed)
		fmt.Printf("Copies discarded:    %d\n", undone.Discarded)
		fmt.Printf("Copies retained:     %d\n", undone.Retained)
		for _, f := range undone.Failures {
			fmt.Printf("  %s\n", f)
		}
	}
	if err != nil {
		return err
	}
	if undone != nil && len(undone.Failures) > 0 {
		return fmt.Errorf("interrupted-write rollback has %d failures", len(undone.Failures))
	}

	report, err := recovery.ReconcileRoot(context.Background(), objectsRoot, store, logrus.StandardLogger())
	if report != nil {
		fmt.Println()
		fmt.Println("=== Reconcile report ===")
		fmt.Printf("Buckets scanned:    %d\n", report.Buckets)
		fmt.Printf("Files scanned:      %d\n", report.FilesScanned)
		fmt.Printf("Entries restored:   %d\n", report.EntriesRestored)
		fmt.Printf("Versions restored:  %d\n", report.VersionsRestored)
		fmt.Printf("Entries repaired:   %d\n", report.EntriesRepaired)
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
