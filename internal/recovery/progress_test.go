package recovery

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The total a recovery's progress is measured against is every object and
// version it walks, and none of the files kept beside them.
func TestARecoveryCountsTheObjectsItHasToWalk(t *testing.T) {
	dataDir, bundlePath, passphrase := buildTestDeploymentWith(t, mixedBucket(t))
	report, err := Run(Options{DataDir: dataDir, BundlePath: bundlePath, Passphrase: passphrase, DryRun: true})
	require.NoError(t, err)
	require.Empty(t, report.Failures)

	buckets, err := discoverBuckets(filepath.Join(dataDir, "objects"))
	require.NoError(t, err)
	var total atomic.Int64
	total.Store(-1)
	countObjects(context.Background(), buckets, &total)
	assert.Equal(t, int64(report.Objects+report.Versions), total.Load())
}

// The work of a recovery is reading each object and version, then making the
// newest version of each key current: the keys are taken to be as many as
// the versions, in the share of them read so far.
func TestTheWorkOfARecoveryCountsTheKeysItMakesCurrent(t *testing.T) {
	r := &rebuild{p: &progress{Done: 300, Marked: 100}, marking: 50}
	r.total.Store(-1)
	done, total := r.work()
	assert.Equal(t, int64(450), done)
	assert.Equal(t, int64(-1), total, "not counted yet")

	r.total.Store(1000)
	_, total = r.work()
	assert.Equal(t, int64(2000), total, "nothing read yet: every object a version")

	r.p.Report.Objects, r.p.Report.Versions = 150, 150
	_, total = r.work()
	assert.Equal(t, int64(1500), total, "half of what was read are versions")

	r.dryRun = true
	_, total = r.work()
	assert.Equal(t, int64(1000), total, "a dry run makes no key current")
}

// The progress says how much is done, at what rate, and when the recovery
// should end; a continued recovery's rate is that of the run going on.
func TestTheProgressSaysWhenTheRecoveryEnds(t *testing.T) {
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.Local)

	assert.Equal(t, logrus.Fields{"rate": "25/s"}, progressFields(250, -1, 250, 10*time.Second, now),
		"no percentage nor end before the total is known")

	assert.Equal(t, logrus.Fields{"percent": "14.3%", "rate": "10/s", "left": "10m0s", "eta": "2026-11-08 10:10"},
		progressFields(1000, 7000, 1000, 100*time.Second, now))

	continued := progressFields(6000, 7000, 500, 100*time.Second, now)
	assert.Equal(t, "85.7%", continued["percent"])
	assert.Equal(t, "5/s", continued["rate"])
	assert.Equal(t, "3m20s", continued["left"])

	over := progressFields(7200, 7000, 7200, 100*time.Second, now)
	assert.Equal(t, "100.0%", over["percent"], "an estimate passed is the end")
	assert.Equal(t, "0s", over["left"])

	assert.Equal(t, "12 of (counting)", ofTotal(12, -1))
	assert.Equal(t, "12 of 40", ofTotal(12, 40))
}
