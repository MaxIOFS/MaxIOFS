package server

import (
	"bytes"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

// signInReasonPattern bounds the reasons a redirect may carry into a label.
var signInReasonPattern = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)

// countSignIns counts the sign-ins a console route answers, read from its
// response: the status of a JSON answer, or the error a redirect carries to
// the login page.
func (s *Server) countSignIns(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || s.metricsManager == nil {
			next(w, r)
			return
		}
		rec := &signInRecorder{ResponseWriter: w, status: http.StatusOK}
		next(rec, r)
		reason := rec.reason()
		s.metricsManager.RecordAuthAttempt(method, reason == "", reason)
	}
}

// signInRecorder keeps a sign-in response's status and the start of its body.
type signInRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        []byte
}

func (w *signInRecorder) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *signInRecorder) Write(b []byte) (int, error) {
	w.wroteHeader = true
	if keep := 512 - len(w.body); keep > 0 {
		if keep > len(b) {
			keep = len(b)
		}
		w.body = append(w.body, b[:keep]...)
	}
	return w.ResponseWriter.Write(b)
}

// reason is why the sign-in failed, or "" when it succeeded.
func (w *signInRecorder) reason() string {
	switch {
	case w.status >= 300 && w.status < 400:
		location, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			return "redirect"
		}
		reason := location.Query().Get("error")
		if reason == "" {
			return ""
		}
		if !signInReasonPattern.MatchString(reason) {
			return "oauth_error"
		}
		return reason
	case w.status < 300:
		return ""
	case w.status == http.StatusBadRequest && bytes.Contains(w.body, []byte(`"sso_hint"`)):
		return "sso_required"
	case w.status == http.StatusBadRequest:
		return "bad_request"
	case w.status == http.StatusUnauthorized:
		return "invalid_credentials"
	case w.status == http.StatusForbidden && bytes.Contains(w.body, []byte(`"locked":true`)):
		return "account_locked"
	case w.status == http.StatusForbidden:
		return "forbidden"
	case w.status == http.StatusTooManyRequests:
		return "rate_limited"
	case w.status >= 500:
		return "server_error"
	}
	return strconv.Itoa(w.status)
}
