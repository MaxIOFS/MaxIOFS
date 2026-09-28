package server

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"slices"
	"strings"

	"github.com/gorilla/mux"
)

// bucketWriteGate holds the writes to a bucket being migrated off this node:
// they are refused with 503 until the migration ends. A write passes the gate
// for as long as it runs, so a migration starts copying only once the writes
// under way have ended. reads tells the requests of the router that change
// nothing although their method could.
func (s *Server) bucketWriteGate(reads func(*http.Request) bool, refuse func(http.ResponseWriter, string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bucket := mux.Vars(r)["bucket"]
			if s.bucketGate == nil || bucket == "" || readMethod(r.Method) || reads(r) {
				next.ServeHTTP(w, r)
				return
			}
			leave, ok := s.bucketGate.Enter(bucket)
			if !ok {
				w.Header().Set("Retry-After", "60")
				refuse(w, bucket)
				return
			}
			defer leave()
			next.ServeHTTP(w, r)
		})
	}
}

func readMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func bucketMigratingMessage(bucket string) string {
	return fmt.Sprintf("Bucket %s is being migrated to another node and takes no writes until the migration ends", bucket)
}

// s3GateReads: S3 Select reads with a POST. Told by the route the request
// matched, never by the key, which the client chooses.
func s3GateReads(r *http.Request) bool {
	route := mux.CurrentRoute(r)
	if route == nil || r.Method != http.MethodPost {
		return false
	}
	queries, err := route.GetQueriesTemplates()
	return err == nil && slices.Contains(queries, "select=")
}

func refuseS3BucketWrite(w http.ResponseWriter, bucket string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<Error><Code>ServiceUnavailable</Code><Message>`+html.EscapeString(bucketMigratingMessage(bucket))+
		`</Message><BucketName>`+html.EscapeString(bucket)+`</BucketName></Error>`)
}

// consoleReadRoutes are console POST routes that hand out a way to read and
// change nothing.
var consoleReadRoutes = []string{"/download-zip-token", "/download-token", "/presigned-url"}

func consoleGateReads(r *http.Request) bool {
	route := mux.CurrentRoute(r)
	if route == nil {
		return false
	}
	template, err := route.GetPathTemplate()
	if err != nil {
		return false
	}
	for _, suffix := range consoleReadRoutes {
		if strings.HasSuffix(template, suffix) {
			return true
		}
	}
	return false
}

func refuseConsoleBucketWrite(w http.ResponseWriter, bucket string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"success": false,
		"error":   bucketMigratingMessage(bucket),
		"code":    "BUCKET_MIGRATING",
	})
}
