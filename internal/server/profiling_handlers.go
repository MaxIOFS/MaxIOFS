package server

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/sirupsen/logrus"
)

// SystemStats contains real-time system statistics for profiling
type SystemStats struct {
	Timestamp    time.Time         `json:"timestamp"`
	Goroutines   int               `json:"goroutines"`
	MemStats     MemoryStats       `json:"memory_stats"`
	GCStats      GarbageCollector  `json:"gc_stats"`
	CPUStats     CPUStats          `json:"cpu_stats"`
}

// MemoryStats contains memory statistics
type MemoryStats struct {
	Alloc        uint64  `json:"alloc_bytes"`         // Currently allocated bytes
	TotalAlloc   uint64  `json:"total_alloc_bytes"`   // Cumulative bytes allocated
	Sys          uint64  `json:"sys_bytes"`           // Total memory from OS
	HeapAlloc    uint64  `json:"heap_alloc_bytes"`    // Heap allocated bytes
	HeapSys      uint64  `json:"heap_sys_bytes"`      // Heap memory from OS
	HeapIdle     uint64  `json:"heap_idle_bytes"`     // Idle heap bytes
	HeapInuse    uint64  `json:"heap_inuse_bytes"`    // Heap bytes in use
	HeapReleased uint64  `json:"heap_released_bytes"` // Heap bytes released to OS
	HeapObjects  uint64  `json:"heap_objects"`        // Number of heap objects
	StackInuse   uint64  `json:"stack_inuse_bytes"`   // Stack bytes in use
	StackSys     uint64  `json:"stack_sys_bytes"`     // Stack memory from OS
	GCSys        uint64  `json:"gc_sys_bytes"`        // GC metadata memory
}

// GarbageCollector contains GC statistics
type GarbageCollector struct {
	NumGC          uint32  `json:"num_gc"`            // Number of GC runs
	PauseTotalMs   float64 `json:"pause_total_ms"`    // Total GC pause time
	PauseLastMs    float64 `json:"pause_last_ms"`     // Last GC pause time
	NextGC         uint64  `json:"next_gc_bytes"`     // Next GC target heap size
	LastGC         time.Time `json:"last_gc"`         // Last GC time
	EnabledPercent float64 `json:"enabled_percent"`   // GC CPU percentage target
}

// CPUStats contains CPU statistics
type CPUStats struct {
	NumCPU      int `json:"num_cpu"`       // Number of logical CPUs
	NumCgoCall  int64 `json:"num_cgo_call"` // Number of cgo calls
}

// LatenciesResponse contains latency statistics for all operations
type LatenciesResponse struct {
	Timestamp time.Time                               `json:"timestamp"`
	Latencies map[string]*metrics.LatencyStats        `json:"latencies"`
}

// ThroughputResponse contains current throughput statistics
type ThroughputResponse struct {
	Current metrics.ThroughputStats `json:"current"`
}

// PerformanceHistoryResponse contains historical performance data
type PerformanceHistoryResponse struct {
	Operation string                    `json:"operation"`
	History   []metrics.OperationLatency `json:"history"`
	Limit     int                        `json:"limit"`
}

// HandleGetProfilingStats returns real-time system statistics
// GET /api/console/profiling/stats
func (s *Server) HandleGetProfilingStats(w http.ResponseWriter, r *http.Request) {
	// Check if user is global admin
	user, ok := r.Context().Value("user").(*auth.User)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !isGlobalAdmin {
		http.Error(w, "Forbidden: Only global administrators can access profiling", http.StatusForbidden)
		return
	}

	// Collect runtime statistics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	stats := SystemStats{
		Timestamp:  time.Now(),
		Goroutines: runtime.NumGoroutine(),
		MemStats: MemoryStats{
			Alloc:        m.Alloc,
			TotalAlloc:   m.TotalAlloc,
			Sys:          m.Sys,
			HeapAlloc:    m.HeapAlloc,
			HeapSys:      m.HeapSys,
			HeapIdle:     m.HeapIdle,
			HeapInuse:    m.HeapInuse,
			HeapReleased: m.HeapReleased,
			HeapObjects:  m.HeapObjects,
			StackInuse:   m.StackInuse,
			StackSys:     m.StackSys,
			GCSys:        m.GCSys,
		},
		GCStats: GarbageCollector{
			NumGC:          m.NumGC,
			PauseTotalMs:   float64(m.PauseTotalNs) / 1e6,
			PauseLastMs:    float64(m.PauseNs[(m.NumGC+255)%256]) / 1e6,
			NextGC:         m.NextGC,
			LastGC:         time.Unix(0, int64(m.LastGC)),
			EnabledPercent: float64(m.GCCPUFraction) * 100,
		},
		CPUStats: CPUStats{
			NumCPU:     runtime.NumCPU(),
			NumCgoCall: runtime.NumCgoCall(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		logrus.WithError(err).Error("Failed to encode profiling stats")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// HandleGetPerformanceLatencies returns latency statistics for all operations
// GET /api/console/metrics/performance/latencies
func (s *Server) HandleGetPerformanceLatencies(w http.ResponseWriter, r *http.Request) {
	collector := metrics.GetGlobalPerformanceCollector()
	if collector == nil {
		http.Error(w, "Performance collector not initialized", http.StatusServiceUnavailable)
		return
	}

	// Get latency stats for all operations
	allStats := collector.GetAllLatencyStats()

	// Convert map keys from OperationType to string for JSON
	latencies := make(map[string]*metrics.LatencyStats)
	for op, stats := range allStats {
		latencies[string(op)] = stats
	}

	response := LatenciesResponse{
		Timestamp: time.Now(),
		Latencies: latencies,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Error("Failed to encode latencies response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// HandleGetPerformanceThroughput returns current throughput statistics
// GET /api/console/metrics/performance/throughput
func (s *Server) HandleGetPerformanceThroughput(w http.ResponseWriter, r *http.Request) {
	collector := metrics.GetGlobalPerformanceCollector()
	if collector == nil {
		http.Error(w, "Performance collector not initialized", http.StatusServiceUnavailable)
		return
	}

	// Get current throughput
	throughput := collector.GetCurrentThroughput()

	response := ThroughputResponse{
		Current: throughput,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Error("Failed to encode throughput response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// HandleGetPerformanceHistory returns historical latency data for a specific operation
// GET /api/console/metrics/performance/history?operation=PutObject&limit=100
func (s *Server) HandleGetPerformanceHistory(w http.ResponseWriter, r *http.Request) {
	collector := metrics.GetGlobalPerformanceCollector()
	if collector == nil {
		http.Error(w, "Performance collector not initialized", http.StatusServiceUnavailable)
		return
	}

	// Get query parameters
	operationStr := r.URL.Query().Get("operation")
	limitStr := r.URL.Query().Get("limit")

	if operationStr == "" {
		http.Error(w, "Missing 'operation' query parameter", http.StatusBadRequest)
		return
	}

	// Parse limit (default 100)
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	// Get history for operation
	operation := metrics.OperationType(operationStr)
	history := collector.GetLatencyHistory(operation, limit)

	response := PerformanceHistoryResponse{
		Operation: operationStr,
		History:   history,
		Limit:     limit,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Error("Failed to encode history response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// HandleResetPerformanceMetrics resets all performance metrics
// POST /api/console/metrics/performance/reset
func (s *Server) HandleResetPerformanceMetrics(w http.ResponseWriter, r *http.Request) {
	// Check if user is global admin
	user, ok := r.Context().Value("user").(*auth.User)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !isGlobalAdmin {
		http.Error(w, "Forbidden: Only global administrators can reset metrics", http.StatusForbidden)
		return
	}

	collector := metrics.GetGlobalPerformanceCollector()
	if collector == nil {
		http.Error(w, "Performance collector not initialized", http.StatusServiceUnavailable)
		return
	}

	// Reset all metrics
	collector.Reset()

	logrus.WithFields(logrus.Fields{
		"user_id": user.ID,
		"username": user.Username,
	}).Info("Performance metrics reset")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Performance metrics reset successfully",
	})
}
