package cluster

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// candidateOf is a node that asks its peers for their votes over HTTP.
func candidateOf(t *testing.T) *LeaderManager {
	t.Helper()
	m, cleanup := leaseNode(t)
	t.Cleanup(cleanup)
	m.proxyClient = NewDynamicProxyClient(func() *tls.Config { return nil })
	return m
}

// peerAt is a node answering at an HTTP endpoint with answer.
func peerAt(t *testing.T, answer http.HandlerFunc) *Node {
	t.Helper()
	srv := httptest.NewServer(answer)
	t.Cleanup(srv.Close)
	return &Node{ID: "peer", Name: "peer", Endpoint: srv.URL}
}

// votingAs answers lease requests by voter's own rules, as a node's
// leader-lease endpoint does, and keeps what it was asked.
type votingAs struct {
	voter *LeaderManager
	mu    sync.Mutex
	asked []LeaseRequest
	// refusing makes it vote no to everything, from a later term.
	refusing atomic.Bool
}

func (v *votingAs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/api/internal/cluster/leader-lease" ||
		r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "not a lease request", http.StatusBadRequest)
		return
	}
	var req LeaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v.mu.Lock()
	v.asked = append(v.asked, req)
	v.mu.Unlock()
	if v.refusing.Load() {
		_ = json.NewEncoder(w).Encode(&LeaseResponse{Granted: false, Term: 99, Reason: "refused"})
		return
	}
	_ = json.NewEncoder(w).Encode(v.voter.GrantLease(r.Context(), &req))
}

// A peer is asked for its vote over HTTP and votes by its own rules: a free
// lease is granted and recorded, a pre-vote is answered and not recorded.
func TestAPeerVotesOverHTTP(t *testing.T) {
	ctx := context.Background()
	candidate := candidateOf(t)
	voter, cleanup := leaseNode(t)
	defer cleanup()
	peer := &votingAs{voter: voter}
	node := peerAt(t, peer.ServeHTTP)

	granted, answered := candidate.requestVote(ctx, node, &LeaseRequest{CandidateID: "cand", Term: 3, PreVote: true}, "cand", "token")
	assert.True(t, granted)
	assert.True(t, answered)
	term, votedFor := voter.readVote(ctx)
	assert.Zero(t, term, "a pre-vote records nothing")
	assert.Empty(t, votedFor)

	granted, answered = candidate.requestVote(ctx, node, &LeaseRequest{CandidateID: "cand", Term: 3}, "cand", "token")
	assert.True(t, granted)
	assert.True(t, answered)
	assert.Equal(t, "cand", voter.LiveLeaseHolder(ctx))
	require.Len(t, peer.asked, 2)
	assert.Equal(t, LeaseRequest{CandidateID: "cand", Term: 3}, peer.asked[1])
}

// A peer that has seen a later term votes no, and the candidate takes that
// term as the newest it knows.
func TestAPeerOfALaterTermVotesNo(t *testing.T) {
	ctx := context.Background()
	candidate := candidateOf(t)
	voter, cleanup := leaseNode(t)
	defer cleanup()
	require.True(t, voter.GrantLease(ctx, &LeaseRequest{CandidateID: "other", Term: 5}).Granted)
	node := peerAt(t, (&votingAs{voter: voter}).ServeHTTP)

	granted, answered := candidate.requestVote(ctx, node, &LeaseRequest{CandidateID: "cand", Term: 3}, "cand", "token")
	assert.False(t, granted)
	assert.True(t, answered, "a no is an answer")
	assert.Equal(t, int64(5), candidate.CurrentTerm())
	assert.Equal(t, "other", voter.LiveLeaseHolder(ctx))
}

// A peer that cannot be reached, or answers what is not a vote, takes no part
// in the election; one that answers an error has answered. A peer silent
// before is not silent once it answers.
func TestAPeerThatGivesNoVoteTakesNoPart(t *testing.T) {
	ctx := context.Background()
	candidate := candidateOf(t)
	req := &LeaseRequest{CandidateID: "cand", Term: 1}

	gone := httptest.NewServer(http.NotFoundHandler())
	unreachable := &Node{ID: "peer", Name: "peer", Endpoint: gone.URL}
	gone.Close()
	granted, answered := candidate.requestVote(ctx, unreachable, req, "cand", "token")
	assert.False(t, granted)
	assert.False(t, answered)
	assert.True(t, candidate.peerWasSilent("peer"))

	garbled := peerAt(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not a vote")) })
	granted, answered = candidate.requestVote(ctx, garbled, req, "cand", "token")
	assert.False(t, granted)
	assert.False(t, answered)

	failing := peerAt(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Leader election not available", http.StatusServiceUnavailable)
	})
	granted, answered = candidate.requestVote(ctx, failing, req, "cand", "token")
	assert.False(t, granted)
	assert.True(t, answered)
	assert.False(t, candidate.peerWasSilent("peer"), "it answered")
}

// electionOf is the node cand of a cluster with a voting peer of its own
// rules at each endpoint of peers.
func electionOf(t *testing.T, peers ...*votingAs) *LeaderManager {
	t.Helper()
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	_, err := db.Exec(`INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled) VALUES ('cand', 'cand', 'token', 1)`)
	require.NoError(t, err)
	addNode := func(id, endpoint string) {
		_, err := db.Exec(`INSERT INTO cluster_nodes (id, name, endpoint, node_token, health_status) VALUES (?, ?, ?, 'token', ?)`,
			id, id, endpoint, HealthStatusHealthy)
		require.NoError(t, err)
	}
	addNode("cand", "https://127.0.0.1:1")
	for i, peer := range peers {
		addNode(fmt.Sprintf("peer-%d", i), peerAt(t, peer.ServeHTTP).Endpoint)
	}
	m := NewLeaderManager(db, newTestManager(t, db))
	require.NoError(t, m.EnsureSchema())
	return m
}

func votingPeer(t *testing.T) *votingAs {
	t.Helper()
	voter, cleanup := leaseNode(t)
	t.Cleanup(cleanup)
	return &votingAs{voter: voter}
}

// A candidate the majority votes for leads: it asks a pre-vote, then the
// vote, and every node records its lease; renewing keeps the term.
func TestACandidateTheMajorityVotesForLeads(t *testing.T) {
	ctx := context.Background()
	a, b := votingPeer(t), votingPeer(t)
	m := electionOf(t, a, b)

	require.NoError(t, m.campaign(ctx, false))
	assert.True(t, m.IsLeader())
	assert.Equal(t, int64(1), m.CurrentTerm())
	assert.Equal(t, "cand", m.LeaderID(ctx))
	for _, peer := range []*votingAs{a, b} {
		require.Len(t, peer.asked, 2)
		assert.Equal(t, LeaseRequest{CandidateID: "cand", Term: 1, PreVote: true}, peer.asked[0])
		assert.Equal(t, LeaseRequest{CandidateID: "cand", Term: 1}, peer.asked[1])
		assert.Equal(t, "cand", peer.voter.LiveLeaseHolder(ctx))
		assert.Equal(t, "cand", peer.voter.LeaderID(ctx))
	}

	require.NoError(t, m.campaign(ctx, true))
	assert.Equal(t, LeaseRequest{CandidateID: "cand", Term: 1, Renewal: true}, a.asked[2], "a renewal keeps the term")
	assert.True(t, m.IsLeader())
}

// A candidate the majority refuses does not lead, nor raise the term it
// records; a leader the majority stops renewing stands down at its next
// tick.
func TestALeaderWithoutTheMajorityDoesNotLead(t *testing.T) {
	ctx := context.Background()
	a, b := votingPeer(t), votingPeer(t)
	m := electionOf(t, a, b)
	a.refusing.Store(true)
	b.refusing.Store(true)

	require.Error(t, m.campaign(ctx, false))
	assert.False(t, m.IsLeader())
	term, _ := m.readVote(ctx)
	assert.Zero(t, term, "a lost pre-vote records no vote")
	for _, peer := range []*votingAs{a, b} {
		require.Len(t, peer.asked, 1)
		assert.True(t, peer.asked[0].PreVote)
	}

	a.refusing.Store(false)
	b.refusing.Store(false)
	m.currentTerm = 0
	require.NoError(t, m.campaign(ctx, false))
	require.True(t, m.IsLeader())
	a.refusing.Store(true)
	b.refusing.Store(true)
	m.tick(ctx)
	assert.False(t, m.IsLeader(), "it stood down")
}

// A node that grants the lease to another, once its own has lapsed, no
// longer leads; a lease that lapsed names no leader.
func TestANodeThatGrantsAnotherNoLongerLeads(t *testing.T) {
	ctx := context.Background()
	m := electionOf(t, votingPeer(t), votingPeer(t))
	require.NoError(t, m.campaign(ctx, false))
	require.True(t, m.IsLeader())

	_, err := m.db.Exec(`UPDATE cluster_leader SET expires_at = ?`, time.Now().Unix()-1)
	require.NoError(t, err)
	m.mu.Lock()
	m.knownLeader = ""
	m.mu.Unlock()
	require.True(t, m.GrantLease(ctx, &LeaseRequest{CandidateID: "other", Term: 2}).Granted)
	assert.False(t, m.IsLeader())
	assert.Equal(t, "other", m.LeaderID(ctx))

	_, err = m.db.Exec(`UPDATE cluster_leader SET expires_at = ?`, time.Now().Unix()-1)
	require.NoError(t, err)
	m.mu.Lock()
	m.knownLeader = ""
	m.mu.Unlock()
	assert.Empty(t, m.LeaderID(ctx), "a lapsed lease")
}

// A candidate that a majority of the nodes answering votes for leads, though
// one votes no.
func TestAMajorityWithOneNoElects(t *testing.T) {
	ctx := context.Background()
	yes, no := votingPeer(t), votingPeer(t)
	no.refusing.Store(true)
	m := electionOf(t, yes, no)
	require.NoError(t, m.campaign(ctx, false))
	assert.True(t, m.IsLeader())
}

// A leader stops taking itself for one before any other node could grant the
// lease to someone else.
func TestALeaderStopsBeforeItsLeaseEnds(t *testing.T) {
	ctx := context.Background()
	peer := votingPeer(t)
	m := electionOf(t, peer, votingPeer(t))
	require.NoError(t, m.campaign(ctx, false))
	var expiresAt int64
	require.NoError(t, peer.voter.db.QueryRow(`SELECT expires_at FROM cluster_leader`).Scan(&expiresAt))
	m.mu.RLock()
	until := m.leaderUntil
	m.mu.RUnlock()
	assert.LessOrEqual(t, until.Unix(), expiresAt-int64(leaderSafetyMargin/time.Second)+1,
		"it leads until %s, the peer granted until %s", until, time.Unix(expiresAt, 0))
	assert.Greater(t, until.Unix(), time.Now().Add(LeaseDuration-leaderSafetyMargin-2*time.Second).Unix())
}

// A tick makes a node without peers the leader at once; respects a lease
// another holds; and, with peers and the lease free, campaigns only once its
// random delay has passed. A leader's tick renews its lease in its term.
func TestATickCampaignsOnlyWhenItShould(t *testing.T) {
	ctx := context.Background()
	t.Run("alone", func(t *testing.T) {
		m := electionOf(t)
		_, err := m.db.Exec(`UPDATE cluster_config SET is_cluster_enabled = 0`)
		require.NoError(t, err)
		m.tick(ctx)
		assert.True(t, m.IsLeader())
	})
	t.Run("a lease held by another", func(t *testing.T) {
		peer := votingPeer(t)
		m := electionOf(t, peer)
		now := time.Now().Unix()
		_, err := m.db.Exec(`INSERT INTO cluster_leader (id, leader_id, term, granted_at, expires_at) VALUES (1, 'other', 3, ?, ?)`, now, now+60)
		require.NoError(t, err)
		m.tick(ctx)
		assert.False(t, m.IsLeader())
		assert.Equal(t, "other", m.LeaderID(ctx))
		assert.Empty(t, peer.asked, "no campaign")
	})
	t.Run("a free lease", func(t *testing.T) {
		peer := votingPeer(t)
		m := electionOf(t, peer, votingPeer(t))
		m.tick(ctx)
		assert.False(t, m.IsLeader())
		assert.Empty(t, peer.asked, "it waits its delay first")
		m.mu.Lock()
		m.campaignAt = time.Now().Add(-time.Second)
		m.mu.Unlock()
		m.tick(ctx)
		require.True(t, m.IsLeader())
		m.tick(ctx)
		assert.True(t, m.IsLeader())
		assert.Equal(t, int64(1), m.CurrentTerm(), "a renewal keeps the term")
		assert.True(t, peer.asked[len(peer.asked)-1].Renewal)
	})
}
