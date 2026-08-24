package server

import (
	"reflect"
	"testing"
)

// Where each Server field is released at shutdown. A stoppable field missing
// here fails the test below.
var lifecycleRelease = map[string]string{
	"httpServer":     "shutdown(): httpServer.Shutdown",
	"consoleServer":  "shutdown(): consoleServer.Shutdown",
	"clusterServer":  "shutdown(): shutdownClusterServer under clusterServerMu",
	"storageBackend": "shutdown(): storageBackend.Close",
	"metadataStore":  "shutdown(): metadataStore.Close, last",
	"db":             "owned by authManager, closed by its Close",
	"auditManager":   "shutdown(): auditManager.Close",
	"authManager":    "shutdown(): authManager.Close",
	"clusterManager": "shutdown(): clusterManager.Close",
	"loggingManager": "shutdown(): loggingManager.Close, after the stores",

	"bucketManager":       "registry: bucketManager",
	"metricsManager":      "registry: metrics",
	"replicationManager":  "registry: replication",
	"clusterRouter":       "registry: clusterRouter",
	"apiRateLimiter":      "registry: apiRateLimiter",
	"lifecycleWorker":     "registry: lifecycle",
	"inventoryWorker":     "registry: inventory",
	"accessLogger":        "registry: accessLogger",
	"haSyncWorker":        "registry: haSync",
	"antiEntropyScrubber": "registry: antiEntropy",
	"deadNodeReconciler":  "registry: deadNodeReconciler",
	"leaderMgr":           "registry: leader",
	"tenantSyncMgr":       "registry: tenantSync",
	"userSyncMgr":         "registry: userSync",
	"accessKeySyncMgr":    "registry: accessKeySync",
	"stsSessionSyncMgr":   "registry: stsSessionSync",
	"iamSyncMgr":          "registry: iamSync",

	"bucketPermissionSyncMgr": "registry: bucketPermissionSync",
	"idpProviderSyncMgr":      "registry: idpProviderSync",
	"groupMappingSyncMgr":     "registry: groupMappingSync",
	"groupSyncMgr":            "registry: groupSync",
	"deletionLogSyncMgr":      "registry: deletionLogSync",
	"globalConfigSyncMgr":     "registry: globalConfigSync",

	"objectManager":       "no lifecycle of its own; wraps the stores",
	"bucketAggregator":    "no background work; per-request fanout only",
	"quotaAggregator":     "no background work; per-request fanout only",
	"staleReconciler":     "runs under goWorker, covered by workers.Wait",
	"notificationManager": "no background work",
	"notificationHub":     "per-client goroutines, ended when the request ends",
	"shareManager":        "no background work",
	"settingsManager":     "no background work",
	"idpManager":          "no background work",
	"inventoryManager":    "no background work; the worker owns the loop",
	"kekStore":            "no background work",
	"systemMetrics":       "sampled on demand, no loop",
	"consoleRouter":       "routing table, no lifecycle",
	"reg":                 "the registry itself",
	"serverCtx":           "cancelled by Start's caller; the waits below outlive it",
}

func stoppable(t reflect.Type) bool {
	if t.Kind() == reflect.Interface {
		return true
	}
	probe := t
	if probe.Kind() != reflect.Ptr {
		probe = reflect.PointerTo(probe)
	}
	for _, name := range []string{"Stop", "Close", "Shutdown"} {
		if _, ok := probe.MethodByName(name); ok {
			return true
		}
	}
	return false
}

func TestShutdown_ReleasesEveryLifecycleField(t *testing.T) {
	st := reflect.TypeOf(Server{})

	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if !stoppable(f.Type) {
			continue
		}
		if _, ok := lifecycleRelease[f.Name]; !ok {
			t.Errorf("Server.%s (%s) can be stopped but nothing records where it is released.\n"+
				"Stop it in shutdown() or register it with supervise(), then add it to lifecycleRelease.",
				f.Name, f.Type)
		}
	}
}

func TestLifecycleRelease_HasNoStaleEntries(t *testing.T) {
	st := reflect.TypeOf(Server{})

	for name := range lifecycleRelease {
		if _, ok := st.FieldByName(name); !ok {
			t.Errorf("lifecycleRelease names %q, which is no longer a Server field", name)
		}
	}
}
