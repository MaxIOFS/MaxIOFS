package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errStopped = errors.New("stopped")

// stopAfter stops a recovery at its n-th step.
func stopAfter(n int) func(string) error {
	seen := 0
	return func(string) error {
		if seen++; seen == n {
			return errStopped
		}
		return nil
	}
}

// mixedBucket writes a bucket with objects from before versioning was enabled,
// and keys whose version entries interleave in the store ("k" and "k:b").
func mixedBucket(t *testing.T) func(ctx context.Context, buckets bucket.Manager, objects object.Manager) {
	return func(ctx context.Context, buckets bucket.Manager, objects object.Manager) {
		require.NoError(t, buckets.CreateBucket(ctx, "", "mixed-bucket", "admin"))
		for _, key := range []string{"k", "plain"} {
			_, err := objects.PutObject(ctx, "mixed-bucket", key, strings.NewReader("before "+key), http.Header{})
			require.NoError(t, err)
		}
		require.NoError(t, buckets.SetVersioning(ctx, "", "mixed-bucket", &bucket.VersioningConfig{Status: "Enabled"}))
		for _, key := range []string{"k", "k:b", "k", "k:b"} {
			_, err := objects.PutObject(ctx, "mixed-bucket", key, strings.NewReader("after "+key), http.Header{})
			require.NoError(t, err)
		}
	}
}

type recoveredBucket struct {
	Count, Size int64
	Versioning  string
	Objects     []string
	Versions    []string
}

// recoveredView is what a recovery left: its report and, by bucket, the
// counts, the objects and the versions of the store it rebuilt.
func recoveredView(t *testing.T, report *Report) (Report, map[string]recoveredBucket) {
	t.Helper()
	ctx := context.Background()
	store := openRecoveredStore(t, report.OutDB)
	defer store.Close()

	view := map[string]recoveredBucket{}
	for _, tenant := range []string{"", "tenant-x"} {
		buckets, err := store.ListBuckets(ctx, tenant)
		require.NoError(t, err)
		for _, b := range buckets {
			path := b.Name
			if b.TenantID != "" {
				path = b.TenantID + "/" + b.Name
			}
			rb := recoveredBucket{Count: b.ObjectCount, Size: b.TotalSize}
			if b.Versioning != nil {
				rb.Versioning = b.Versioning.Status
			}
			objects, _, err := store.ListObjects(ctx, path, "", "", 1000)
			require.NoError(t, err)
			for _, o := range objects {
				rb.Objects = append(rb.Objects, fmt.Sprintf("%s@%s latest=%t %d %s %d", o.Key, o.VersionID, o.IsLatest, o.Size, o.ETag, o.LastModified.Unix()))
			}
			versions, err := store.ListAllObjectVersions(ctx, path, "", 0)
			require.NoError(t, err)
			for _, v := range versions {
				rb.Versions = append(rb.Versions, fmt.Sprintf("%s@%s latest=%t %d %s", v.Key, v.VersionID, v.IsLatest, v.Size, v.ETag))
			}
			sort.Strings(rb.Versions)
			view[path] = rb
		}
	}
	kept := *report
	kept.OutDB = ""
	sort.Strings(kept.Failures)
	return kept, view
}

// A recovery stopped at any step, and stopped again as it continues, ends as
// one that never stopped: the same buckets, objects, versions and report.
func TestARecoveryStoppedAnywhereEndsAsOneThatNeverStopped(t *testing.T) {
	dataDir, bundlePath, passphrase := buildTestDeploymentWith(t, mixedBucket(t))
	formerEvery := checkpointEvery
	checkpointEvery = 2
	defer func() { checkpointEvery, stopAt = formerEvery, nil }()
	options := func(outDB string) Options {
		return Options{DataDir: dataDir, BundlePath: bundlePath, Passphrase: passphrase, OutDB: filepath.Join(dataDir, outDB)}
	}

	reference, err := Run(options("reference"))
	require.NoError(t, err)
	require.Empty(t, reference.Failures)
	wantReport, wantStore := recoveredView(t, reference)
	require.Contains(t, wantStore, "mixed-bucket")
	require.Len(t, wantStore["mixed-bucket"].Versions, 6, "two versions of k, two of k:b, the null k and plain")

	steps := 0
	for n := 1; steps == 0; n++ {
		outDB := fmt.Sprintf("stopped-%d", n)
		stopAt = stopAfter(n)
		_, err := Run(options(outDB))
		if err == nil {
			steps = n - 1
			continue
		}
		require.ErrorIs(t, err, errStopped)

		stopAt = stopAfter(n)
		report, err := Run(options(outDB))
		if err != nil {
			require.ErrorIs(t, err, errStopped)
			stopAt = nil
			report, err = Run(options(outDB))
		}
		stopAt = nil
		require.NoError(t, err, "stopped at step %d", n)
		_, err = os.Stat(filepath.Join(dataDir, outDB+".progress"))
		assert.True(t, os.IsNotExist(err), "a finished recovery leaves no progress")
		gotReport, gotStore := recoveredView(t, report)
		assert.Equal(t, wantReport, gotReport, "stopped at step %d", n)
		assert.Equal(t, wantStore, gotStore, "stopped at step %d", n)
	}
	assert.Greater(t, steps, 30, "every file, bucket and stage of the run is a step")
}

// An unfinished recovery is continued only by the same recovery: with the same
// bundle, of the same data directory and over the same files.
func TestAnUnfinishedRecoveryIsContinuedOnlyByTheSameOne(t *testing.T) {
	dataDir, bundlePath, passphrase := buildTestDeployment(t)
	formerEvery := checkpointEvery
	checkpointEvery = 1
	defer func() { checkpointEvery, stopAt = formerEvery, nil }()
	outDB := filepath.Join(dataDir, "metadata-recovered")
	same := Options{DataDir: dataDir, BundlePath: bundlePath, Passphrase: passphrase, OutDB: outDB}

	stopAt = stopAfter(3)
	_, err := Run(same)
	require.ErrorIs(t, err, errStopped)
	stopAt = nil

	_, err = Run(Options{DataDir: dataDir, OutDB: outDB})
	require.Error(t, err, "without the bundle")
	assert.Contains(t, err.Error(), "unfinished recovery")

	other := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(other, "objects"), 0750))
	_, err = Run(Options{DataDir: other, BundlePath: bundlePath, Passphrase: passphrase, OutDB: outDB})
	require.Error(t, err, "of another data directory")
	assert.Contains(t, err.Error(), "unfinished recovery")

	data, err := os.ReadFile(outDB + ".progress")
	require.NoError(t, err)
	var p struct {
		Last string `json:"last"`
	}
	require.NoError(t, json.Unmarshal(data, &p))
	require.NotEmpty(t, p.Last)
	require.NoError(t, os.Remove(filepath.Join(dataDir, "objects", filepath.FromSlash(p.Last))))
	_, err = Run(same)
	require.Error(t, err, "over other files")
	assert.Contains(t, err.Error(), "changed since")
}
