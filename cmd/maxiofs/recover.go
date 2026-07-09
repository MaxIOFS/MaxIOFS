package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/maxiofs/maxiofs/internal/recovery"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newRecoverCmd builds the offline disaster-recovery subcommand: it rebuilds
// the Pebble metadata store from the filesystem object tree (data files +
// .metadata sidecars) and restores the encryption keys from a recovery
// bundle. Run it with the server STOPPED.
func newRecoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Rebuild the metadata store from the filesystem (disaster recovery)",
		Long: `Rebuilds the Pebble metadata store from the on-disk object tree — the
recovery path for "metadata database lost or corrupt, object files intact".

Buckets are recreated from their .maxiofs-bucket markers, objects and
versions from each file's .metadata sidecar. With --recovery-bundle the
encryption keys are restored into the auth database and every encrypted
object's wrapped DEK is verified against them.

The rebuild always writes into a FRESH directory (default:
<data-dir>/metadata-recovered) and never touches an existing store. After a
successful run, stop everything, move the corrupt store aside, rename the
recovered directory to <data-dir>/metadata and start the server.

Run with the server STOPPED.`,
		Example: `  maxiofs recover --data-dir /var/lib/maxiofs --recovery-bundle /safe/maxiofs-recovery-bundle.json
  maxiofs recover --data-dir /var/lib/maxiofs --recovery-bundle bundle.json --passphrase-file /safe/pass.txt --dry-run`,
		RunE: runRecover,
	}

	cmd.Flags().String("recovery-bundle", "", "Path to the KEK recovery bundle (required to verify/serve encrypted objects)")
	cmd.Flags().String("passphrase-file", "", "File containing the bundle passphrase (otherwise prompted)")
	cmd.Flags().String("out-db", "", "Directory for the rebuilt metadata store (default: <data-dir>/metadata-recovered)")
	cmd.Flags().Bool("dry-run", false, "Walk and verify without writing anything")
	cmd.Flags().Bool("verbose", false, "Log every recovered object")

	return cmd
}

func runRecover(cmd *cobra.Command, args []string) error {
	dataDir, _ := cmd.Flags().GetString("data-dir")
	if dataDir == "" {
		return fmt.Errorf("--data-dir is required")
	}

	bundlePath, _ := cmd.Flags().GetString("recovery-bundle")
	passphraseFile, _ := cmd.Flags().GetString("passphrase-file")
	outDB, _ := cmd.Flags().GetString("out-db")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	verbose, _ := cmd.Flags().GetBool("verbose")

	var passphrase string
	if bundlePath != "" {
		switch {
		case passphraseFile != "":
			data, err := os.ReadFile(passphraseFile)
			if err != nil {
				return fmt.Errorf("failed to read passphrase file: %w", err)
			}
			passphrase = strings.TrimSpace(string(data))
		default:
			fmt.Fprint(os.Stderr, "Recovery bundle passphrase: ")
			raw, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return fmt.Errorf("failed to read passphrase: %w", err)
			}
			passphrase = strings.TrimSpace(string(raw))
		}
	} else {
		fmt.Fprintln(os.Stderr, "WARNING: no --recovery-bundle given — encrypted objects will be indexed but cannot be verified or served until the keys are restored.")
	}

	report, err := recovery.Run(recovery.Options{
		DataDir:    dataDir,
		BundlePath: bundlePath,
		Passphrase: passphrase,
		OutDB:      outDB,
		DryRun:     dryRun,
		Verbose:    verbose,
	})
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("=== Recovery report ===")
	fmt.Printf("Buckets:               %d\n", report.Buckets)
	fmt.Printf("Objects:               %d (+ %d versions)\n", report.Objects, report.Versions)
	fmt.Printf("Encrypted (verified):  %d\n", report.EncryptedVerified)
	fmt.Printf("Encrypted (unverified):%d\n", report.EncryptedUnverified)
	fmt.Printf("Legacy encrypted:      %d\n", report.LegacyEncrypted)
	fmt.Printf("Plaintext:             %d\n", report.Plaintext)
	fmt.Printf("Skipped files:         %d\n", report.Skipped)
	fmt.Printf("Encryption keys restored: %d\n", report.KEKsRestored)
	if len(report.Failures) > 0 {
		fmt.Printf("FAILURES (%d):\n", len(report.Failures))
		for _, f := range report.Failures {
			fmt.Printf("  - %s\n", f)
		}
	}
	if dryRun {
		fmt.Println("\nDry run — nothing was written.")
		return nil
	}
	fmt.Printf("\nRebuilt metadata store: %s\n", report.OutDB)
	fmt.Println("Next steps:")
	fmt.Println("  1. Move the corrupt store aside:  mv <data-dir>/metadata <data-dir>/metadata.corrupt")
	fmt.Printf("  2. Activate the recovered store:  mv %s <data-dir>/metadata\n", report.OutDB)
	fmt.Println("  3. Start the server. Buckets/objects are back; users, permissions and bucket")
	fmt.Println("     configuration (policies, lifecycle, quotas) must be re-applied by the admin.")

	if len(report.Failures) > 0 {
		return fmt.Errorf("recovery completed with %d failure(s) — review the report above", len(report.Failures))
	}
	return nil
}
