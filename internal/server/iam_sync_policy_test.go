package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A policy a peer sends is stored with its versions or not at all: a copy
// without its default version, or whose versions cannot all be stored, leaves
// this node's as it was.
func TestASynchronizedPolicyIsStoredWhole(t *testing.T) {
	s := newClusterTestNode(t)
	const doc = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`
	const changed = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`
	_, err := s.db.Exec(`INSERT INTO iam_policies (name, arn, path, description, default_version_id, is_builtin, created_at, updated_at)
		VALUES ('synced', 'arn:aws:iam:::policy/synced', '/', 'kept', 'v1', 0, 1, 100)`)
	require.NoError(t, err)
	_, err = s.db.Exec(`INSERT INTO iam_policy_versions (policy_name, version_id, document, created_at) VALUES ('synced', 'v1', ?, 1)`, doc)
	require.NoError(t, err)

	stored := func() (description, document string) {
		t.Helper()
		require.NoError(t, s.db.QueryRow(`SELECT description FROM iam_policies WHERE name = 'synced'`).Scan(&description))
		require.NoError(t, s.db.QueryRow(`SELECT document FROM iam_policy_versions WHERE policy_name = 'synced' AND version_id = 'v1'`).Scan(&document))
		return description, document
	}
	send := func(description string, versions ...*cluster.IAMPolicyVersionRow) {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleReceiveIAMSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/iam-sync", cluster.IAMSyncPayload{
			Policies: []*cluster.IAMPolicyData{{Name: "synced", ARN: "arn:aws:iam:::policy/synced", Path: "/", Description: description,
				DefaultVersionID: "v1", CreatedAt: 1, UpdatedAt: 100, Versions: versions}},
		}))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	send("emptied")
	description, document := stored()
	assert.Equal(t, "kept", description, "a copy without its default version")
	assert.Equal(t, doc, document)

	send("cut short",
		&cluster.IAMPolicyVersionRow{VersionID: "v1", Document: changed, CreatedAt: 1},
		&cluster.IAMPolicyVersionRow{VersionID: "v1", Document: changed, CreatedAt: 2})
	description, document = stored()
	assert.Equal(t, "kept", description, "a copy whose versions could not all be stored")
	assert.Equal(t, doc, document)

	send("whole", &cluster.IAMPolicyVersionRow{VersionID: "v1", Document: changed, CreatedAt: 1})
	description, document = stored()
	assert.Equal(t, "whole", description)
	assert.Equal(t, changed, document)
}
