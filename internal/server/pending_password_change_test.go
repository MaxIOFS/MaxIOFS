package server

import (
	"net/http"
	"testing"
)

func TestPendingPasswordChange_LeavesOnlyTheWayOut(t *testing.T) {
	const me = "user-1"

	allowed := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/auth/logout"},
		{http.MethodPost, "/api/v1/auth/refresh"},
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodGet, "/api/v1/settings"},
		{http.MethodPut, "/api/v1/users/" + me + "/password"},
	}
	for _, c := range allowed {
		req, err := http.NewRequest(c.method, c.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !passwordChangeAllowsPath(req, me) {
			t.Fatalf("%s %s should stay reachable while a password change is pending", c.method, c.path)
		}
	}

	refused := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/buckets"},
		{http.MethodPost, "/api/v1/buckets"},
		{http.MethodDelete, "/api/v1/buckets/photos"},
		{http.MethodGet, "/api/v1/users"},
		{http.MethodPut, "/api/v1/settings"},
		// Someone else's password is not the way out of your own obligation.
		{http.MethodPut, "/api/v1/users/user-2/password"},
	}
	for _, c := range refused {
		req, err := http.NewRequest(c.method, c.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if passwordChangeAllowsPath(req, me) {
			t.Fatalf("%s %s should be refused while a password change is pending", c.method, c.path)
		}
	}
}
