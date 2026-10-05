package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fromPeer is a request that arrives from another node of the cluster.
func fromPeer(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	return req.WithContext(context.WithValue(req.Context(), "cluster_node_id", "peer"))
}

func deletedAt(t *testing.T, s *Server, entityType, entityID string) int64 {
	t.Helper()
	var at int64
	require.NoError(t, s.db.QueryRow(`SELECT deleted_at FROM cluster_deletion_log WHERE entity_type = ? AND entity_id = ?`,
		entityType, entityID).Scan(&at))
	return at
}

// A deletion another node reports keeps the time it was made.
func TestAReceivedDeletionKeepsItsTime(t *testing.T) {
	s := newClusterTestNode(t)
	const madeAt = 1_700_000_000
	w := httptest.NewRecorder()
	s.handleReceiveDeletionLogSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/deletion-log-sync", []*cluster.DeletionEntry{
		{ID: "x", EntityType: cluster.EntityTypeAccessKey, EntityID: "AKGONE", DeletedByNodeID: "peer", DeletedAt: madeAt},
		{ID: "y", EntityType: cluster.EntityTypeAccessKey, EntityID: "AKUNDATED", DeletedByNodeID: "peer"},
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.EqualValues(t, madeAt, deletedAt(t, s, cluster.EntityTypeAccessKey, "AKGONE"))
	assert.False(t, hasRow(t, s, `SELECT COUNT(*) FROM cluster_deletion_log WHERE entity_id = 'AKUNDATED'`), "a deletion without a time is not recorded")

	// A deletion older than the local copy is not kept.
	_, err := s.db.Exec(`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('kept-group', 'kept-group', 1, ?)`, madeAt+10)
	require.NoError(t, err)
	w = httptest.NewRecorder()
	s.handleReceiveDeletionLogSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/deletion-log-sync", []*cluster.DeletionEntry{
		{ID: "z", EntityType: cluster.EntityTypeGroup, EntityID: "kept-group", DeletedByNodeID: "peer", DeletedAt: madeAt},
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, hasRow(t, s, `SELECT COUNT(*) FROM cluster_deletion_log WHERE entity_id = 'kept-group'`))

	// So does a deletion each entity's own sync sends.
	for _, c := range []struct {
		entityType, id string
		send           func(w http.ResponseWriter, r *http.Request)
	}{
		{cluster.EntityTypeAccessKey, "AK-DEL", s.handleReceiveAccessKeyDeleteSync},
		{cluster.EntityTypeBucketPermission, "perm-del", s.handleReceiveBucketPermissionDeleteSync},
		{cluster.EntityTypeIDPProvider, "idp-del", s.handleReceiveIDPProviderDeleteSync},
		{cluster.EntityTypeGroupMapping, "map-del", s.handleReceiveGroupMappingDeleteSync},
		{cluster.EntityTypeTenant, "tenant-del", s.handleReceiveTenantDeleteSync},
		{cluster.EntityTypeUser, "user-del", s.handleReceiveUserDeleteSync},
		{cluster.EntityTypeGroup, "group-del", s.handleReceiveGroupDeleteSync},
	} {
		w := httptest.NewRecorder()
		c.send(w, fromPeer(t, http.MethodPost, "/delete-sync", map[string]any{"id": c.id, "deleted_at": madeAt}))
		require.Less(t, w.Code, 300, "%s: %s", c.entityType, w.Body.String())
		assert.EqualValues(t, madeAt, deletedAt(t, s, c.entityType, c.id), c.entityType)
	}

	// And a revoked temporary credential.
	w = httptest.NewRecorder()
	s.handleReceiveSTSSessionSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/sts-session-sync", cluster.STSSessionSyncPayload{
		Deletions: []string{"ASIAREVOKED"}, DeletedAt: map[string]int64{"ASIAREVOKED": madeAt},
	}))
	require.Less(t, w.Code, 300, w.Body.String())
	assert.EqualValues(t, madeAt, deletedAt(t, s, cluster.EntityTypeSTSSession, "ASIAREVOKED"))
}

// A deletion that arrives by the deletion log removes this node's copy, as
// one sent by the entity's own synchronization does, with what goes with it.
func TestADeletionFromTheLogRemovesTheCopy(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	now := time.Now().Unix()
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := s.db.Exec(query, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO tenants (id, name, created_at, updated_at) VALUES ('gone-tenant', 'gone-tenant', 1, 1)`)
	for _, u := range []struct{ id, tenant string }{{"gone-user", ""}, {"tenant-member", "gone-tenant"}, {"key-holder", ""}} {
		require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: u.id, Username: u.id, TenantID: u.tenant,
			Status: auth.UserStatusActive, Roles: []string{auth.RoleUser}, CreatedAt: now - 60, UpdatedAt: now - 60}))
	}
	userKey, err := s.authManager.GenerateAccessKey(ctx, "gone-user")
	require.NoError(t, err)
	memberKey, err := s.authManager.GenerateAccessKey(ctx, "tenant-member")
	require.NoError(t, err)
	goneKey, err := s.authManager.GenerateAccessKey(ctx, "key-holder")
	require.NoError(t, err)
	exec(`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('gone-group', 'gone-group', 1, 1)`)
	exec(`INSERT INTO group_members (group_id, user_id, added_at) VALUES ('gone-group', 'key-holder', 1)`)
	exec(`INSERT INTO bucket_permissions (id, bucket_name, bucket_tenant_id, group_id, permission_level, granted_by, granted_at)
		VALUES ('group-perm', 'b', '', 'gone-group', 'read', 'admin', 1)`)
	exec(`INSERT INTO bucket_permissions (id, bucket_name, bucket_tenant_id, user_id, permission_level, granted_by, granted_at)
		VALUES ('gone-perm', 'b', '', NULL, 'read', 'admin', 1)`)
	exec(`INSERT INTO identity_providers (id, name, type, status, config, created_by, created_at, updated_at)
		VALUES ('gone-idp', 'gone-idp', 'ldap', 'active', '{}', 'admin', 1, 1)`)
	exec(`INSERT INTO idp_group_mappings (id, provider_id, external_group, role, created_at, updated_at)
		VALUES ('idp-mapping', 'gone-idp', 'cn=a', 'user', 1, 1)`)
	exec(`INSERT INTO identity_providers (id, name, type, status, config, created_by, created_at, updated_at)
		VALUES ('kept-idp', 'kept-idp', 'ldap', 'active', '{}', 'admin', 1, 1)`)
	exec(`INSERT INTO idp_group_mappings (id, provider_id, external_group, role, created_at, updated_at)
		VALUES ('gone-mapping', 'kept-idp', 'cn=b', 'user', 1, 1)`)
	exec(`INSERT INTO sts_sessions (temp_access_key_id, secret_access_key, session_token, user_id, created_at, expires_at)
		VALUES ('ASIAGONE', 's', 't', 'key-holder', 1, ?)`, now+3600)
	_, err = s.authManager.(auth.IAMManager).CreateIAMPolicy(ctx, "gone-policy", "/", "", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`, "")
	require.NoError(t, err)
	exec(`UPDATE iam_policies SET updated_at = 1 WHERE name = 'gone-policy'`)
	require.NoError(t, s.clusterManager.AddNode(ctx, &cluster.Node{ID: "gone-node", Name: "gone-node", Endpoint: "https://127.0.0.1:9", NodeToken: "t", Priority: 100, Metadata: "{}", ChangedAt: 1}))

	var entries []*cluster.DeletionEntry
	for _, d := range []struct{ entityType, id string }{
		{cluster.EntityTypeUser, "gone-user"},
		{cluster.EntityTypeTenant, "gone-tenant"},
		{cluster.EntityTypeAccessKey, goneKey.AccessKeyID},
		{cluster.EntityTypeGroup, "gone-group"},
		{cluster.EntityTypeBucketPermission, "gone-perm"},
		{cluster.EntityTypeIDPProvider, "gone-idp"},
		{cluster.EntityTypeGroupMapping, "gone-mapping"},
		{cluster.EntityTypeSTSSession, "ASIAGONE"},
		{cluster.EntityTypeIAMPolicy, "gone-policy"},
		{cluster.EntityTypeClusterNode, "gone-node"},
	} {
		entries = append(entries, &cluster.DeletionEntry{ID: d.id, EntityType: d.entityType, EntityID: d.id, DeletedByNodeID: "peer", DeletedAt: now + 5})
	}
	w := httptest.NewRecorder()
	s.handleReceiveDeletionLogSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/deletion-log-sync", entries))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	for what, query := range map[string]string{
		"user":                      `SELECT COUNT(*) FROM users WHERE id = 'gone-user'`,
		"the user's key":            `SELECT COUNT(*) FROM access_keys WHERE access_key_id = '` + userKey.AccessKeyID + `'`,
		"tenant":                    `SELECT COUNT(*) FROM tenants WHERE id = 'gone-tenant'`,
		"the tenant's user":         `SELECT COUNT(*) FROM users WHERE id = 'tenant-member'`,
		"the tenant's user's key":   `SELECT COUNT(*) FROM access_keys WHERE access_key_id = '` + memberKey.AccessKeyID + `'`,
		"access key":                `SELECT COUNT(*) FROM access_keys WHERE access_key_id = '` + goneKey.AccessKeyID + `'`,
		"group":                     `SELECT COUNT(*) FROM groups WHERE id = 'gone-group'`,
		"the group's members":       `SELECT COUNT(*) FROM group_members WHERE group_id = 'gone-group'`,
		"the group's permission":    `SELECT COUNT(*) FROM bucket_permissions WHERE id = 'group-perm'`,
		"bucket permission":         `SELECT COUNT(*) FROM bucket_permissions WHERE id = 'gone-perm'`,
		"identity provider":         `SELECT COUNT(*) FROM identity_providers WHERE id = 'gone-idp'`,
		"the provider's mapping":    `SELECT COUNT(*) FROM idp_group_mappings WHERE id = 'idp-mapping'`,
		"group mapping":             `SELECT COUNT(*) FROM idp_group_mappings WHERE id = 'gone-mapping'`,
		"temporary credential":      `SELECT COUNT(*) FROM sts_sessions WHERE temp_access_key_id = 'ASIAGONE'`,
		"IAM policy":                `SELECT COUNT(*) FROM iam_policies WHERE name = 'gone-policy'`,
		"node removed from cluster": `SELECT COUNT(*) FROM cluster_nodes WHERE id = 'gone-node'`,
	} {
		assert.False(t, hasRow(t, s, query), what)
	}
	assert.True(t, hasRow(t, s, `SELECT COUNT(*) FROM users WHERE id = 'key-holder'`), "what was not deleted stays")
	assert.True(t, hasRow(t, s, `SELECT COUNT(*) FROM identity_providers WHERE id = 'kept-idp'`))
}

// A revoked temporary credential reaches the other node with the time it was
// revoked.
func TestARevokedSessionReachesTheOtherNodeWithItsTime(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	revokedAt := time.Now().Add(-time.Hour).Unix()
	require.NoError(t, cluster.RecordDeletion(ctx, p.a.db, cluster.EntityTypeSTSSession, "ASIAREVOKED", p.aID, revokedAt))
	p.a.stsSessionSyncMgr.TriggerSync(ctx)
	assert.Eventually(t, func() bool {
		return cluster.DeletionTime(ctx, p.b.db, cluster.EntityTypeSTSSession, "ASIAREVOKED") == revokedAt
	},
		10*time.Second, 20*time.Millisecond)
}

// A bucket deleted and created again keeps its owner's policy on a node that
// holds the old deletion, and the new policy reaches a node that holds it.
func TestARecreatedBucketKeepsItsOwnerPolicy(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	const owner = "owner-user"
	policyID := cluster.IAMInlinePolicyID("user", owner, "owner-reborn")
	ownerPolicy := func(on *Server) bool {
		t.Helper()
		var n int
		require.NoError(t, on.db.QueryRow(`SELECT COUNT(*) FROM iam_inline_policies WHERE target_type = 'user' AND target_id = ? AND name = 'owner-reborn'`, owner).Scan(&n))
		return n == 1
	}
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "reborn", owner))
	require.True(t, ownerPolicy(s))
	require.NoError(t, s.bucketManager.DeleteBucket(ctx, "", "reborn"))
	require.False(t, ownerPolicy(s))
	time.Sleep(1100 * time.Millisecond) // the new bucket is created after the second of the deletion
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "reborn", owner))
	require.True(t, ownerPolicy(s))

	// Every other node still holds the deletion and sends it.
	w := httptest.NewRecorder()
	s.handleReceiveIAMSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/iam-sync", cluster.IAMSyncPayload{
		Deletions: map[string][]string{cluster.EntityTypeIAMInlinePolicy: {policyID}},
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, ownerPolicy(s), "the deletion is older than the policy")

	// A node that holds the deletion takes the newer policy.
	peer := newClusterTestNode(t)
	var created, updated int64
	require.NoError(t, s.db.QueryRow(`SELECT created_at, updated_at FROM iam_inline_policies WHERE target_type = 'user' AND target_id = ? AND name = 'owner-reborn'`, owner).Scan(&created, &updated))
	var document string
	require.NoError(t, s.db.QueryRow(`SELECT document FROM iam_inline_policies WHERE target_type = 'user' AND target_id = ? AND name = 'owner-reborn'`, owner).Scan(&document))
	w = httptest.NewRecorder()
	peer.handleReceiveDeletionLogSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/deletion-log-sync", []*cluster.DeletionEntry{
		{ID: "d", EntityType: cluster.EntityTypeIAMInlinePolicy, EntityID: policyID, DeletedByNodeID: "peer", DeletedAt: updated - 1},
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = httptest.NewRecorder()
	peer.handleReceiveIAMSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/iam-sync", cluster.IAMSyncPayload{
		Inline: []*cluster.IAMInlinePolicyData{{TargetType: "user", TargetID: owner, Name: "owner-reborn", Document: document, CreatedAt: created, UpdatedAt: updated}},
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, ownerPolicy(peer), "the policy is newer than the deletion")
}

// A key deleted once and written again, on a node whose peer missed the new
// write, is sent to the peer when it is caught up; the deletion does not
// remove it.
func TestARewrittenKeyReachesTheNodeThatMissedIt(t *testing.T) {
	p := newHAPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "cycle", "admin"))
	require.True(t, hasBucket(p.b, "", "cycle"))

	// The key was deleted a minute ago; both nodes recorded it.
	longAgo := time.Now().Add(-time.Minute).Unix()
	tombstone := cluster.ObjectTombstoneID("cycle", "k")
	for _, s := range []*Server{p.a, p.b} {
		_, err := s.db.ExecContext(ctx, `INSERT INTO cluster_deletion_log (id, entity_type, entity_id, deleted_by_node_id, deleted_at)
			VALUES (?, ?, ?, 'x', ?)`, "del-"+s.config.DataDir, cluster.EntityTypeObject, tombstone, longAgo)
		require.NoError(t, err)
	}
	// It is written again on A while B is down.
	p.bDown.Store(true)
	_, err := p.a.objectManager.PutObject(ctx, "cycle", "k", strings.NewReader("second life"), http.Header{})
	require.NoError(t, err)
	p.bDown.Store(false)

	// The nodes exchange their deletion logs, as they do every 30 seconds.
	var entries []*cluster.DeletionEntry
	rows, err := p.b.db.QueryContext(ctx, `SELECT id, entity_type, entity_id, deleted_by_node_id, deleted_at FROM cluster_deletion_log`)
	require.NoError(t, err)
	for rows.Next() {
		e := &cluster.DeletionEntry{}
		require.NoError(t, rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.DeletedByNodeID, &e.DeletedAt))
		entries = append(entries, e)
	}
	require.NoError(t, rows.Close())
	w := httptest.NewRecorder()
	p.a.handleReceiveDeletionLogSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/deletion-log-sync", entries))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	p.a.antiEntropyScrubber.Start(ctx)
	_, err = p.a.clusterManager.CheckNodeHealth(ctx, p.bID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := p.b.metadataStore.GetObject(ctx, "cycle", "k")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "the new write reaches the node that missed it")
	assert.Equal(t, "second life", readBody(t, p.a, "cycle", "k"))
}

func hasRow(t *testing.T, s *Server, query string, args ...any) bool {
	t.Helper()
	var n int
	require.NoError(t, s.db.QueryRow(query, args...).Scan(&n))
	return n > 0
}

// A copy another node sends is taken when it changed after the deletion this
// node recorded, and refused when it is older.
func TestSyncedCopiesAreOrderedAgainstDeletions(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	const deleted = 1_000_000
	for _, c := range []struct {
		name, entityType, id, table string
		send                        func(w http.ResponseWriter, r *http.Request)
		body                        func(updated int64) any
	}{
		{"tenant", cluster.EntityTypeTenant, "t-sync", "tenants", s.handleReceiveTenantSync, func(u int64) any {
			return map[string]any{"id": "t-sync", "name": "t-sync", "status": "active", "created_at": 1, "updated_at": u}
		}},
		{"user", cluster.EntityTypeUser, "u-sync", "users", s.handleReceiveUserSync, func(u int64) any {
			return map[string]any{"id": "u-sync", "username": "u-sync", "password_hash": "x", "status": "active", "roles": "[]", "policies": "[]", "metadata": "{}", "created_at": 1, "updated_at": u}
		}},
		{"group", cluster.EntityTypeGroup, "g-sync", "groups", s.handleReceiveGroupSync, func(u int64) any {
			return map[string]any{"id": "g-sync", "name": "g-sync", "created_at": 1, "updated_at": u}
		}},
		{"identity provider", cluster.EntityTypeIDPProvider, "idp-sync", "identity_providers", s.handleReceiveIDPProviderSync, func(u int64) any {
			return map[string]any{"id": "idp-sync", "name": "idp-sync", "type": "ldap", "status": "active", "config": "{}", "created_by": "admin", "created_at": 1, "updated_at": u}
		}},
		{"group mapping", cluster.EntityTypeGroupMapping, "map-sync", "idp_group_mappings", s.handleReceiveGroupMappingSync, func(u int64) any {
			return map[string]any{"id": "map-sync", "provider_id": "idp-sync", "external_group": "cn=x", "role": "user", "created_at": 1, "updated_at": u}
		}},
	} {
		require.NoError(t, cluster.RecordDeletion(ctx, s.db, c.entityType, c.id, "peer", deleted))
		c.send(httptest.NewRecorder(), fromPeer(t, http.MethodPost, "/sync", c.body(deleted-1)))
		assert.False(t, hasRow(t, s, `SELECT COUNT(*) FROM `+c.table+` WHERE id = ?`, c.id), "%s older than its deletion", c.name)
		w := httptest.NewRecorder()
		c.send(w, fromPeer(t, http.MethodPost, "/sync", c.body(deleted)))
		require.Less(t, w.Code, 300, "%s: %s", c.name, w.Body.String())
		assert.True(t, hasRow(t, s, `SELECT COUNT(*) FROM `+c.table+` WHERE id = ?`, c.id), "%s changed in the second of its deletion", c.name)
	}
}

// A deletion another node sends removes an IAM entity that did not change after
// it, and keeps one that did. An earlier release sends no time: the time the
// deletion log recorded is used. A copy older than a recorded deletion is
// refused; a newer one is taken.
func TestIAMDeletionsAreOrderedAgainstEntities(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	im := s.authManager.(auth.IAMManager)
	const doc = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`
	require.NoError(t, im.PutIAMInlinePolicy(ctx, auth.IAMTargetUser, "someone", "p", doc))
	id := cluster.IAMInlinePolicyID(auth.IAMTargetUser, "someone", "p")
	changed, ok := cluster.EntityUpdatedAt(ctx, s.db, cluster.EntityTypeIAMInlinePolicy, id)
	require.True(t, ok)
	inline := func(name string) bool {
		return hasRow(t, s, `SELECT COUNT(*) FROM iam_inline_policies WHERE target_id = 'someone' AND name = ?`, name)
	}
	send := func(payload cluster.IAMSyncPayload) {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleReceiveIAMSync(w, fromPeer(t, http.MethodPost, "/api/internal/cluster/iam-sync", payload))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	dated := func(entityType, entityID string, at int64) cluster.IAMSyncPayload {
		return cluster.IAMSyncPayload{
			Deletions: map[string][]string{entityType: {entityID}},
			DeletedAt: map[string]map[string]int64{entityType: {entityID: at}},
		}
	}

	send(dated(cluster.EntityTypeIAMInlinePolicy, id, changed))
	assert.True(t, inline("p"), "deleted in the second of its last change")
	send(cluster.IAMSyncPayload{Deletions: map[string][]string{cluster.EntityTypeIAMInlinePolicy: {id}}})
	assert.True(t, inline("p"), "no time and none recorded")
	require.NoError(t, cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeIAMInlinePolicy, id, "peer", changed+5))
	send(cluster.IAMSyncPayload{Deletions: map[string][]string{cluster.EntityTypeIAMInlinePolicy: {id}}})
	assert.False(t, inline("p"), "an earlier release's deletion takes the recorded time")

	require.NoError(t, im.PutIAMInlinePolicy(ctx, auth.IAMTargetUser, "someone", "q", doc))
	qid := cluster.IAMInlinePolicyID(auth.IAMTargetUser, "someone", "q")
	qChanged, _ := cluster.EntityUpdatedAt(ctx, s.db, cluster.EntityTypeIAMInlinePolicy, qid)
	send(dated(cluster.EntityTypeIAMInlinePolicy, qid, qChanged+1))
	assert.False(t, inline("q"))
	assert.EqualValues(t, qChanged+1, deletedAt(t, s, cluster.EntityTypeIAMInlinePolicy, qid))
	send(cluster.IAMSyncPayload{Inline: []*cluster.IAMInlinePolicyData{{TargetType: auth.IAMTargetUser, TargetID: "someone", Name: "q", Document: doc, CreatedAt: 1, UpdatedAt: qChanged}}})
	assert.False(t, inline("q"), "a copy older than its deletion")
	send(cluster.IAMSyncPayload{Inline: []*cluster.IAMInlinePolicyData{{TargetType: auth.IAMTargetUser, TargetID: "someone", Name: "q", Document: doc, CreatedAt: 1, UpdatedAt: qChanged + 1}}})
	assert.True(t, inline("q"), "a copy changed in the second of its deletion")

	policy := func(name string, updated int64) cluster.IAMSyncPayload {
		return cluster.IAMSyncPayload{Policies: []*cluster.IAMPolicyData{{Name: name, ARN: "arn:aws:iam:::policy/" + name, Path: "/", DefaultVersionID: "v1", CreatedAt: 1, UpdatedAt: updated}}}
	}
	require.NoError(t, cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeIAMPolicy, "gone-policy", "peer", 100))
	send(policy("gone-policy", 99))
	assert.False(t, hasRow(t, s, `SELECT COUNT(*) FROM iam_policies WHERE name = 'gone-policy'`))
	send(policy("gone-policy", 100))
	assert.True(t, hasRow(t, s, `SELECT COUNT(*) FROM iam_policies WHERE name = 'gone-policy'`))

	role := func(updated int64) cluster.IAMSyncPayload {
		return cluster.IAMSyncPayload{Roles: []*cluster.IAMRoleData{{Name: "gone-role", ARN: "arn:aws:iam:::role/gone-role", Path: "/", CreatedAt: 1, UpdatedAt: updated}}}
	}
	require.NoError(t, cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeIAMRole, "gone-role", "peer", 100))
	send(role(99))
	assert.False(t, hasRow(t, s, `SELECT COUNT(*) FROM iam_roles WHERE name = 'gone-role'`))
	send(role(100))
	assert.True(t, hasRow(t, s, `SELECT COUNT(*) FROM iam_roles WHERE name = 'gone-role'`))

	attachment := func(at int64) cluster.IAMSyncPayload {
		return cluster.IAMSyncPayload{Attachments: []*cluster.IAMAttachmentData{{PolicyName: "gone-policy", TargetType: "user", TargetID: "someone", AttachedAt: at}}}
	}
	require.NoError(t, cluster.RecordDeletion(ctx, s.db, cluster.EntityTypeIAMAttachment, cluster.IAMAttachmentID("gone-policy", "user", "someone"), "peer", 100))
	send(attachment(99))
	assert.False(t, hasRow(t, s, `SELECT COUNT(*) FROM iam_policy_attachments WHERE policy_name = 'gone-policy'`))
	send(attachment(100))
	assert.True(t, hasRow(t, s, `SELECT COUNT(*) FROM iam_policy_attachments WHERE policy_name = 'gone-policy'`))
}

// A deletion made here is dated after the last change of what it removes, even
// in the same second, so no node keeps a copy changed just before it; deleting
// a user, a tenant or a group records the deletion of the IAM policies they
// held.
func TestLocalDeletionsAreDatedAfterTheLastChange(t *testing.T) {
	p := newHAPair(t)
	s := p.a
	ctx := context.Background()
	im := s.authManager.(auth.IAMManager)
	const doc = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`
	admin := func(method, path string, vars map[string]string) *http.Request {
		return mux.SetURLVars(createAuthenticatedRequest(method, path, nil, "", "global-admin", true), vars)
	}
	after := func(entityType, id string, changed int64) {
		t.Helper()
		assert.Greater(t, deletedAt(t, s, entityType, id), changed, "%s %s", entityType, id)
	}

	pol, err := im.CreateIAMPolicy(ctx, "short-lived", "/", "", doc, "")
	require.NoError(t, err)
	w := httptest.NewRecorder()
	s.handleDeleteIAMPolicy(w, admin(http.MethodDelete, "/api/v1/iam/policies/short-lived", map[string]string{"name": "short-lived"}))
	require.Less(t, w.Code, 300, w.Body.String())
	after(cluster.EntityTypeIAMPolicy, "short-lived", pol.UpdatedAt)

	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "short-user", Username: "short-user", Password: "Secret123!", Status: "active", Roles: []string{"user"}}))
	require.NoError(t, im.PutIAMInlinePolicy(ctx, auth.IAMTargetUser, "short-user", "own", doc))
	userChanged, _ := cluster.EntityUpdatedAt(ctx, s.db, cluster.EntityTypeUser, "short-user")
	w = httptest.NewRecorder()
	s.handleDeleteUser(w, admin(http.MethodDelete, "/api/v1/users/short-user", map[string]string{"user": "short-user"}))
	require.Less(t, w.Code, 300, w.Body.String())
	after(cluster.EntityTypeUser, "short-user", userChanged)
	assert.Positive(t, deletedAt(t, s, cluster.EntityTypeIAMInlinePolicy, cluster.IAMInlinePolicyID(auth.IAMTargetUser, "short-user", "own")))

	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "short-tenant", Name: "short-tenant", Status: "active"}))
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "tenant-user", Username: "tenant-user", Password: "Secret123!", TenantID: "short-tenant", Status: "active", Roles: []string{"user"}}))
	require.NoError(t, im.PutIAMInlinePolicy(ctx, auth.IAMTargetUser, "tenant-user", "theirs", doc))
	tenantChanged, _ := cluster.EntityUpdatedAt(ctx, s.db, cluster.EntityTypeTenant, "short-tenant")
	w = httptest.NewRecorder()
	s.handleDeleteTenant(w, admin(http.MethodDelete, "/api/v1/tenants/short-tenant", map[string]string{"tenant": "short-tenant"}))
	require.Less(t, w.Code, 300, w.Body.String())
	after(cluster.EntityTypeTenant, "short-tenant", tenantChanged)
	assert.Positive(t, deletedAt(t, s, cluster.EntityTypeIAMInlinePolicy, cluster.IAMInlinePolicyID(auth.IAMTargetUser, "tenant-user", "theirs")))

	_, err = s.db.Exec(`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('short-group', 'short-group', 1, ?)`, time.Now().Unix())
	require.NoError(t, err)
	require.NoError(t, im.PutIAMInlinePolicy(ctx, auth.IAMTargetGroup, "short-group", "team", doc))
	groupChanged, _ := cluster.EntityUpdatedAt(ctx, s.db, cluster.EntityTypeGroup, "short-group")
	_, err = s.deleteGroupAndRecordTombstone(ctx, "short-group", p.aID)
	require.NoError(t, err)
	after(cluster.EntityTypeGroup, "short-group", groupChanged)
	assert.Positive(t, deletedAt(t, s, cluster.EntityTypeIAMInlinePolicy, cluster.IAMInlinePolicyID(auth.IAMTargetGroup, "short-group", "team")))

	_, err = s.db.Exec(`INSERT INTO identity_providers (id, name, type, status, config, created_by, created_at, updated_at)
		VALUES ('short-idp', 'short-idp', 'ldap', 'active', '{}', 'admin', 1, ?)`, time.Now().Unix())
	require.NoError(t, err)
	_, err = s.db.Exec(`INSERT INTO idp_group_mappings (id, provider_id, external_group, role, created_at, updated_at)
		VALUES ('short-map', 'short-idp', 'cn=g', 'user', 1, ?)`, time.Now().Unix())
	require.NoError(t, err)
	mappingChanged, _ := cluster.EntityUpdatedAt(ctx, s.db, cluster.EntityTypeGroupMapping, "short-map")
	w = httptest.NewRecorder()
	s.handleDeleteGroupMapping(w, admin(http.MethodDelete, "/api/v1/identity-providers/short-idp/group-mappings/short-map", map[string]string{"id": "short-idp", "mapId": "short-map"}))
	require.Less(t, w.Code, 300, w.Body.String())
	after(cluster.EntityTypeGroupMapping, "short-map", mappingChanged)
	providerChanged, _ := cluster.EntityUpdatedAt(ctx, s.db, cluster.EntityTypeIDPProvider, "short-idp")
	w = httptest.NewRecorder()
	s.handleDeleteIDP(w, admin(http.MethodDelete, "/api/v1/identity-providers/short-idp", map[string]string{"id": "short-idp"}))
	require.Less(t, w.Code, 300, w.Body.String())
	after(cluster.EntityTypeIDPProvider, "short-idp", providerChanged)

	// An object, and a version, deleted in the second they were written.
	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "quick", "admin"))
	objects := cluster.NewHAObjectManager(s.objectManager, s.clusterManager)
	obj, err := objects.PutObject(ctx, "quick", "k", strings.NewReader("x"), http.Header{})
	require.NoError(t, err)
	_, err = objects.DeleteObject(ctx, "quick", "k", false)
	require.NoError(t, err)
	after(cluster.EntityTypeObject, cluster.ObjectTombstoneID("quick", "k"), obj.LastModified.Unix())

	require.NoError(t, s.bucketManager.SetVersioning(ctx, "", "quick", &bucket.VersioningConfig{Status: "Enabled"}))
	version, err := objects.PutObject(ctx, "quick", "v", strings.NewReader("x"), http.Header{})
	require.NoError(t, err)
	require.NotEmpty(t, version.VersionID)
	_, err = objects.DeleteObject(ctx, "quick", "v", false, version.VersionID)
	require.NoError(t, err)
	after(cluster.EntityTypeObjectVersion, cluster.ObjectVersionTombstoneID("quick", "v", version.VersionID), version.LastModified.Unix())
}
