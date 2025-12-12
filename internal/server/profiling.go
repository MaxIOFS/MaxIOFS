package server

import (
	"net/http"
	"net/http/pprof"
	"runtime"
	rtpprof "runtime/pprof"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/sirupsen/logrus"
)

// RegisterProfilingRoutes registers pprof profiling endpoints
// These endpoints are protected and only accessible to global administrators
func (s *Server) RegisterProfilingRoutes(router *mux.Router) {
	// Create subrouter for profiling endpoints
	pprofRouter := router.PathPrefix("/debug/pprof").Subrouter()

	// Apply authentication middleware (only global admins)
	pprofRouter.Use(s.requireGlobalAdminMiddleware)

	// Index page
	pprofRouter.HandleFunc("/", s.handlePprofIndex)

	// Standard pprof endpoints
	pprofRouter.HandleFunc("/cmdline", pprof.Cmdline)
	pprofRouter.HandleFunc("/profile", s.handleProfile)
	pprofRouter.HandleFunc("/symbol", pprof.Symbol)
	pprofRouter.HandleFunc("/trace", s.handleTrace)

	// Profiles accessible via /debug/pprof/{profile}
	pprofRouter.HandleFunc("/heap", s.handleHeap)
	pprofRouter.HandleFunc("/goroutine", s.handleGoroutine)
	pprofRouter.HandleFunc("/threadcreate", s.handleThreadCreate)
	pprofRouter.HandleFunc("/block", s.handleBlock)
	pprofRouter.HandleFunc("/mutex", s.handleMutex)
	pprofRouter.HandleFunc("/allocs", s.handleAllocs)

	logrus.Info("Profiling endpoints registered at /debug/pprof/* (global admin only)")
}

// requireGlobalAdminMiddleware is middleware that requires the user to be a global admin
func (s *Server) requireGlobalAdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get user from context (should be set by auth middleware)
		user, ok := r.Context().Value("user").(*auth.User)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Check if global admin (admin role WITHOUT tenant)
		isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
		if !isGlobalAdmin {
			http.Error(w, "Forbidden: Global administrator access required", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handlePprofIndex serves the pprof index page
func (s *Server) handlePprofIndex(w http.ResponseWriter, r *http.Request) {
	pprof.Index(w, r)
}

// handleProfile handles CPU profiling requests with custom duration
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	// Allow custom duration via query parameter
	durationStr := r.URL.Query().Get("seconds")
	duration := 30 * time.Second // Default 30 seconds

	if durationStr != "" {
		if d, err := strconv.Atoi(durationStr); err == nil && d > 0 && d <= 300 {
			duration = time.Duration(d) * time.Second
		}
	}

	// Log the profile capture
	logrus.WithFields(logrus.Fields{
		"type":     "cpu",
		"duration": duration.Seconds(),
		"user":     r.Context().Value("user_id"),
	}).Info("CPU profile captured")

	// Set custom duration
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=profile.prof")

	if err := rtpprof.StartCPUProfile(w); err != nil {
		logrus.WithError(err).Error("Failed to start CPU profile")
		http.Error(w, "Failed to start CPU profile", http.StatusInternalServerError)
		return
	}

	time.Sleep(duration)
	rtpprof.StopCPUProfile()
}

// handleTrace handles execution trace requests
func (s *Server) handleTrace(w http.ResponseWriter, r *http.Request) {
	// Allow custom duration via query parameter
	durationStr := r.URL.Query().Get("seconds")
	duration := 5 * time.Second // Default 5 seconds

	if durationStr != "" {
		if d, err := strconv.Atoi(durationStr); err == nil && d > 0 && d <= 60 {
			duration = time.Duration(d) * time.Second
		}
	}

	// Log the trace capture
	logrus.WithFields(logrus.Fields{
		"type":     "trace",
		"duration": duration.Seconds(),
		"user":     r.Context().Value("user_id"),
	}).Info("Execution trace captured")

	// Create a custom handler that limits trace duration
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=trace.out")

	// Use a timeout context to limit trace duration
	done := make(chan bool)
	go func() {
		pprof.Trace(w, r)
		close(done)
	}()

	select {
	case <-done:
		// Trace completed
	case <-time.After(duration):
		// Trace timeout - this is handled by pprof internally
	}
}

// handleHeap handles heap profile requests
func (s *Server) handleHeap(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"type": "heap",
		"user": r.Context().Value("user_id"),
	}).Info("Heap profile captured")

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=heap.prof")

	// Force GC before heap snapshot for more accurate results
	gc := r.URL.Query().Get("gc")
	if gc == "1" || gc == "true" {
		runtime.GC()
	}

	pprof.Handler("heap").ServeHTTP(w, r)
}

// handleGoroutine handles goroutine profile requests
func (s *Server) handleGoroutine(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"type": "goroutine",
		"user": r.Context().Value("user_id"),
	}).Info("Goroutine profile captured")

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=goroutine.prof")

	pprof.Handler("goroutine").ServeHTTP(w, r)
}

// handleThreadCreate handles thread creation profile requests
func (s *Server) handleThreadCreate(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"type": "threadcreate",
		"user": r.Context().Value("user_id"),
	}).Info("Thread creation profile captured")

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=threadcreate.prof")

	pprof.Handler("threadcreate").ServeHTTP(w, r)
}

// handleBlock handles block profile requests
func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"type": "block",
		"user": r.Context().Value("user_id"),
	}).Info("Block profile captured")

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=block.prof")

	// Enable block profiling if not already enabled
	runtime.SetBlockProfileRate(1)

	pprof.Handler("block").ServeHTTP(w, r)
}

// handleMutex handles mutex profile requests
func (s *Server) handleMutex(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"type": "mutex",
		"user": r.Context().Value("user_id"),
	}).Info("Mutex profile captured")

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=mutex.prof")

	// Enable mutex profiling if not already enabled
	runtime.SetMutexProfileFraction(1)

	pprof.Handler("mutex").ServeHTTP(w, r)
}

// handleAllocs handles memory allocation profile requests
func (s *Server) handleAllocs(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"type": "allocs",
		"user": r.Context().Value("user_id"),
	}).Info("Memory allocation profile captured")

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=allocs.prof")

	pprof.Handler("allocs").ServeHTTP(w, r)
}
