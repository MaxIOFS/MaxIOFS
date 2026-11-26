package cluster

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

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
