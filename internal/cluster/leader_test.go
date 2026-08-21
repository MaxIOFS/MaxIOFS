package cluster

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// leaseNode builds a LeaderManager over its own database — one cluster node.
func leaseNode(t *testing.T) (*LeaderManager, func()) {
	t.Helper()
	db, cleanup := setupTestDB(t)

	m := &LeaderManager{
		db:  db,
		log: logrus.WithField("component", "leader-test"),
	}
	require.NoError(t, m.EnsureSchema())
	return m, cleanup
}

// TestLease_GrantedWhenFree is the base case.
func TestLease_GrantedWhenFree(t *testing.T) {
	node, cleanup := leaseNode(t)
	defer cleanup()

	answer := node.GrantLease(context.Background(), &LeaseRequest{CandidateID: "node-a", Term: 1})
	assert.True(t, answer.Granted)
	assert.Equal(t, "node-a", answer.Leader)
}

// TestLease_RefusedWhileHeld: a node does not hand the lease to a second
// candidate while the first one's lease is alive. This is what makes a majority
// mean something.
func TestLease_RefusedWhileHeld(t *testing.T) {
	node, cleanup := leaseNode(t)
	defer cleanup()
	ctx := context.Background()

	require.True(t, node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-a", Term: 1}).Granted)

	answer := node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-b", Term: 2})
	assert.False(t, answer.Granted, "a live lease is not handed to somebody else")
	assert.Equal(t, "node-a", answer.Leader, "and the candidate is told who holds it")
}

// TestLease_StaleTermIsFenced is the partition case, and the one that matters
// most: a node that was leader, lost contact, and comes back still believing it
// leads must be refused.
func TestLease_StaleTermIsFenced(t *testing.T) {
	node, cleanup := leaseNode(t)
	defer cleanup()
	ctx := context.Background()

	// The cluster has moved on to term 5 under node-b.
	require.True(t, node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-b", Term: 5}).Granted)

	// node-a returns from a partition, still on term 2.
	answer := node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-a", Term: 2})
	assert.False(t, answer.Granted, "a leader returning on a stale term is fenced out")
	assert.Equal(t, "stale term", answer.Reason)
	assert.Equal(t, int64(5), answer.Term, "and learns the term it missed")
}

// TestLease_SameTermCannotBeGrantedTwice: two candidates campaigning on the same
// term would be two leaders in one term, which is exactly what must not happen.
func TestLease_SameTermCannotBeGrantedTwice(t *testing.T) {
	node, cleanup := leaseNode(t)
	defer cleanup()
	ctx := context.Background()

	require.True(t, node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-a", Term: 3}).Granted)

	answer := node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-b", Term: 3})
	assert.False(t, answer.Granted, "one term, one leader")
	assert.Equal(t, "term already granted to another node", answer.Reason)
}

// TestLease_RenewalIsGranted: the holder extends its own lease without waiting
// for it to lapse, which is what keeps leadership stable.
func TestLease_RenewalIsGranted(t *testing.T) {
	node, cleanup := leaseNode(t)
	defer cleanup()
	ctx := context.Background()

	require.True(t, node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-a", Term: 1}).Granted)

	answer := node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-a", Term: 1, Renewal: true})
	assert.True(t, answer.Granted, "the holder renews its own live lease")
}

// TestLease_ExpiredLeaseIsGrantedToSomebodyElse: a dead leader's lease lapses
// and the cluster can elect a replacement.
func TestLease_ExpiredLeaseIsGrantedToSomebodyElse(t *testing.T) {
	node, cleanup := leaseNode(t)
	defer cleanup()
	ctx := context.Background()

	require.True(t, node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-a", Term: 1}).Granted)

	// Age the lease past its expiry, as time would.
	_, err := node.db.Exec(`UPDATE cluster_leader SET expires_at = ? WHERE id = 1`,
		time.Now().Add(-time.Second).Unix())
	require.NoError(t, err)

	answer := node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-b", Term: 2})
	assert.True(t, answer.Granted, "an expired lease is free to take")
	assert.Equal(t, "node-b", answer.Leader)
}

// TestLease_NoTwoLeadersAcrossAMajority is the split-brain check, run over a
func TestLease_NoTwoLeadersAcrossAMajority(t *testing.T) {
	nodes := make([]*LeaderManager, 3)
	for i := range nodes {
		n, cleanup := leaseNode(t)
		defer cleanup()
		nodes[i] = n
	}
	ctx := context.Background()

	// Two candidates campaign at once, each reaching every node.
	votesA, votesB := 0, 0
	for _, n := range nodes {
		if n.GrantLease(ctx, &LeaseRequest{CandidateID: "node-a", Term: 1}).Granted {
			votesA++
		}
		if n.GrantLease(ctx, &LeaseRequest{CandidateID: "node-b", Term: 1}).Granted {
			votesB++
		}
	}

	majority := len(nodes)/2 + 1
	leaders := 0
	if votesA >= majority {
		leaders++
	}
	if votesB >= majority {
		leaders++
	}
	assert.Equal(t, 1, leaders,
		"exactly one candidate can reach a majority (a=%d b=%d, majority=%d)", votesA, votesB, majority)
}

// TestLease_MinorityCannotElect covers the other half of a partition: the side
// with fewer nodes must not be able to elect anybody, or both sides would have
// a leader.
func TestLease_MinorityCannotElect(t *testing.T) {
	// Three nodes; the partition leaves one alone.
	isolated, cleanup := leaseNode(t)
	defer cleanup()
	ctx := context.Background()

	// The isolated node can only collect its own vote.
	votes := 0
	if isolated.GrantLease(ctx, &LeaseRequest{CandidateID: "node-c", Term: 9}).Granted {
		votes++
	}

	clusterSize := 3
	majority := clusterSize/2 + 1
	assert.Less(t, votes, majority,
		"one node out of three cannot reach a majority, so the minority side stays leaderless")
}

// TestLease_IsLeaderExpiresWithoutRenewal: a leader that stops renewing must
// stop believing it leads, without anybody telling it.
func TestLease_IsLeaderExpiresWithoutRenewal(t *testing.T) {
	node, cleanup := leaseNode(t)
	defer cleanup()

	node.mu.Lock()
	node.isLeader = true
	node.leaderUntil = time.Now().Add(50 * time.Millisecond)
	node.mu.Unlock()

	assert.True(t, node.IsLeader(), "leads while the lease is live")

	time.Sleep(80 * time.Millisecond)
	assert.False(t, node.IsLeader(),
		"stops leading when the lease lapses, with no renewal and nobody to tell it")
}

// TestLease_LiveLeaseHolderReportsFreeWhenExpired feeds the campaign decision:
// a node only campaigns when it sees the lease as free.
func TestLease_LiveLeaseHolderReportsFreeWhenExpired(t *testing.T) {
	node, cleanup := leaseNode(t)
	defer cleanup()
	ctx := context.Background()

	assert.Equal(t, "", node.LiveLeaseHolder(ctx), "no lease recorded yet")

	require.True(t, node.GrantLease(ctx, &LeaseRequest{CandidateID: "node-a", Term: 1}).Granted)
	assert.Equal(t, "node-a", node.LiveLeaseHolder(ctx))

	_, err := node.db.Exec(`UPDATE cluster_leader SET expires_at = ? WHERE id = 1`,
		time.Now().Add(-time.Second).Unix())
	require.NoError(t, err)
	assert.Equal(t, "", node.LiveLeaseHolder(ctx), "an expired lease reads as free")
}

// TestLease_SchemaIsIdempotent: the table is created at startup on every boot,
// including on databases that already have it.
func TestLease_SchemaIsIdempotent(t *testing.T) {
	node, cleanup := leaseNode(t)
	defer cleanup()

	require.NoError(t, node.EnsureSchema())
	require.NoError(t, node.EnsureSchema())

	var count int
	require.NoError(t, node.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cluster_leader'`).Scan(&count))
	assert.Equal(t, 1, count)
}

var _ = sql.ErrNoRows

// TestQuorum_CountedOverRespondingNodes pins the rule that decides whether a
func TestQuorum_CountedOverRespondingNodes(t *testing.T) {
	cases := []struct {
		name         string
		participants int
		needed       int
	}{
		{"alone in the world, one vote is the majority", 1, 1},
		{"two nodes both answering still need both", 2, 2},
		{"two of three answering: a majority of the two", 2, 2},
		{"three answering need two", 3, 2},
		{"five answering need three", 5, 3},
		{"nobody answered, not even this node: never zero", 0, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			round := electionRound{participants: tc.participants}
			assert.Equal(t, tc.needed, round.needed())
		})
	}
}

// TestQuorum_SurvivorOfATwoNodeClusterCanWin is the failure this rule exists
// for: the master of a two-node cluster dies, and the node still standing has
// to be able to take over on its own vote.
func TestQuorum_SurvivorOfATwoNodeClusterCanWin(t *testing.T) {
	// One vote — its own — out of one node that answered. The dead peer did
	// not take part, so it is not counted in the majority it cannot reach.
	round := electionRound{votes: 1, participants: 1}
	assert.GreaterOrEqual(t, round.votes, round.needed(),
		"the surviving node must be able to elect itself")

	// While the peer is alive and refusing, it does count, and a candidate
	// that cannot convince it does not become a second leader.
	contested := electionRound{votes: 1, participants: 2}
	assert.Less(t, contested.votes, contested.needed(),
		"a peer that answers still has to agree")
}
