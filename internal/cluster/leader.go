package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	LeaseDuration = 15 * time.Second

	// RenewInterval is how often the leader renews. Comfortably shorter than
	// the lease so a single failed round does not cost it the leadership.
	RenewInterval = 5 * time.Second

	leaderSafetyMargin = 3 * time.Second
)

// LeaseRequest is a candidate asking a node to grant it the lease.
type LeaseRequest struct {
	CandidateID string `json:"candidate_id"`
	Term        int64  `json:"term"`
	// Renewal marks a leader extending a lease it already holds, which a node
	// grants without needing the previous lease to have expired.
	Renewal bool `json:"renewal"`
	PreVote bool `json:"pre_vote,omitempty"`
}

// LeaseResponse is a node's answer.
type LeaseResponse struct {
	Granted bool   `json:"granted"`
	Term    int64  `json:"term"`
	Leader  string `json:"leader,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// LeaderManager runs the election and answers whether this node leads.
type LeaderManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	stopOnce       sync.Once
	log            *logrus.Entry

	mu sync.RWMutex
	// isLeader and leaderUntil are read on every configuration write, so they
	// are held in memory rather than queried.
	isLeader    bool
	leaderUntil time.Time
	currentTerm int64
	knownLeader string
	// campaignAt is when this node may next stand for election, set to a random
	// point after it first notices the lease is free.
	campaignAt time.Time
	peerSilent map[string]bool
	localNodeID string
}

// NewLeaderManager creates the leader manager.
func NewLeaderManager(db *sql.DB, clusterManager *Manager) *LeaderManager {
	return &LeaderManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewDynamicProxyClient(clusterManager.GetTLSConfig),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "leader"),
	}
}

// EnsureSchema creates the lease and vote tables.
func (m *LeaderManager) EnsureSchema() error {
	if _, err := m.db.Exec(`
		CREATE TABLE IF NOT EXISTS cluster_leader (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			leader_id TEXT NOT NULL,
			term INTEGER NOT NULL,
			granted_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}
	_, err := m.db.Exec(`
		CREATE TABLE IF NOT EXISTS cluster_vote (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			term INTEGER NOT NULL,
			candidate_id TEXT NOT NULL,
			voted_at INTEGER NOT NULL
		)
	`)
	return err
}

// readLease returns the recorded lease, and whether there is one at all.
func (m *LeaderManager) readLease(ctx context.Context) (leader string, term, expiresAt int64, ok bool) {
	err := m.db.QueryRowContext(ctx,
		`SELECT leader_id, term, expires_at FROM cluster_leader WHERE id = 1`).
		Scan(&leader, &term, &expiresAt)
	if err != nil {
		return "", 0, 0, false
	}
	return leader, term, expiresAt, true
}

// readVote returns the vote this node last cast: the term, and who for.
func (m *LeaderManager) readVote(ctx context.Context) (term int64, candidate string) {
	if err := m.db.QueryRowContext(ctx,
		`SELECT term, candidate_id FROM cluster_vote WHERE id = 1`).Scan(&term, &candidate); err != nil {
		return 0, ""
	}
	return term, candidate
}

// recordVote writes down this node's answer, so it cannot answer twice in one
// term even across a restart.
func (m *LeaderManager) recordVote(ctx context.Context, term int64, candidate string) error {
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO cluster_vote (id, term, candidate_id, voted_at)
		VALUES (1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			term = excluded.term,
			candidate_id = excluded.candidate_id,
			voted_at = excluded.voted_at
	`, term, candidate, time.Now().Unix())
	return err
}

// recordLease writes down that a node holds the lease until a given instant.
func (m *LeaderManager) recordLease(ctx context.Context, leader string, term int64) error {
	now := time.Now()
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO cluster_leader (id, leader_id, term, granted_at, expires_at)
		VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			leader_id = excluded.leader_id,
			term = excluded.term,
			granted_at = excluded.granted_at,
			expires_at = excluded.expires_at
	`, leader, term, now.Unix(), now.Add(LeaseDuration).Unix())
	return err
}

// IsLeader reports whether this node currently holds the lease.
func (m *LeaderManager) IsLeader() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.isLeader {
		return false
	}
	// The safety margin is already baked into leaderUntil.
	return time.Now().Before(m.leaderUntil)
}

// CurrentTerm returns the term this node believes is current. Writes carry it
// so a stale leader's work can be refused.
func (m *LeaderManager) CurrentTerm() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentTerm
}

// LeaderID returns the node believed to hold the lease, for error messages that
// tell a caller where to go.
func (m *LeaderManager) LeaderID(ctx context.Context) string {
	m.mu.RLock()
	known := m.knownLeader
	m.mu.RUnlock()
	if known != "" {
		return known
	}

	var leaderID string
	var expiresAt int64
	err := m.db.QueryRowContext(ctx,
		`SELECT leader_id, expires_at FROM cluster_leader WHERE id = 1`).Scan(&leaderID, &expiresAt)
	if err != nil || time.Now().Unix() >= expiresAt {
		return ""
	}
	return leaderID
}

// Start begins campaigning and renewing. A single-node deployment leads
// immediately: with one node, a majority is itself.
func (m *LeaderManager) Start(ctx context.Context) {
	m.rememberLocalID(ctx)
	go m.loop(ctx)
}

// Stop halts the loop and releases the lease, so a planned shutdown hands over
// in seconds instead of leaving the cluster leaderless for a full lease.
func (m *LeaderManager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopChan)
		m.release(context.Background())
	})
}

func (m *LeaderManager) loop(ctx context.Context) {
	ticker := time.NewTicker(RenewInterval)
	defer ticker.Stop()

	m.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

// tick renews the lease when leading, and campaigns when the lease is free.
func (m *LeaderManager) tick(ctx context.Context) {
	if !m.clusterManager.IsClusterEnabled() {
		// Standalone: this node is the whole cluster, so it leads.
		m.mu.Lock()
		m.isLeader = true
		m.leaderUntil = time.Now().Add(LeaseDuration)
		m.mu.Unlock()
		return
	}

	if m.IsLeader() {
		if err := m.campaign(ctx, true); err != nil {
			m.log.WithError(err).Warn("Could not renew the leader lease; standing down")
			m.standDown()
		}
		return
	}

	// Somebody else may hold a live lease; only campaign when it has lapsed.
	if leader := m.LiveLeaseHolder(ctx); leader != "" {
		m.mu.Lock()
		m.knownLeader = leader
		// A lease was seen, so the next time one lapses this node waits a fresh
		// random interval rather than a stale one.
		m.campaignAt = time.Time{}
		m.mu.Unlock()
		return
	}

	if m.hasPeers(ctx) {
		m.mu.Lock()
		if m.campaignAt.IsZero() {
			m.campaignAt = time.Now().Add(electionDelay())
			m.mu.Unlock()
			return
		}
		if time.Now().Before(m.campaignAt) {
			m.mu.Unlock()
			return
		}
		m.campaignAt = time.Time{}
		m.mu.Unlock()
	}

	if err := m.campaign(ctx, false); err != nil {
		m.log.WithError(err).Warn("No coordinator: this node could not win the election")
	}
}

// LiveLeaseHolder returns the node holding an unexpired lease, or "" when the
// lease is free.
func (m *LeaderManager) LiveLeaseHolder(ctx context.Context) string {
	var leaderID string
	var expiresAt int64
	err := m.db.QueryRowContext(ctx,
		`SELECT leader_id, expires_at FROM cluster_leader WHERE id = 1`).Scan(&leaderID, &expiresAt)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		return ""
	}
	if time.Now().Unix() >= expiresAt {
		return ""
	}
	return leaderID
}

// campaign asks every node for the lease and takes it on a majority.
func (m *LeaderManager) campaign(ctx context.Context, renewal bool) error {
	localNodeID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.localNodeID = localNodeID
	m.mu.Unlock()

	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return err
	}

	nodes, err := m.clusterManager.ListNodes(ctx)
	if err != nil {
		return err
	}

	term := m.nextTerm(ctx, renewal)

	// Ask before committing. A renewal skips this: the leader already holds the
	// lease and is not raising the term, so it has nothing to disturb.
	if !renewal {
		round := m.countVotes(ctx, nodes, localNodeID, nodeToken,
			&LeaseRequest{CandidateID: localNodeID, Term: term, PreVote: true})
		if round.votes < round.needed() {
			return fmt.Errorf("pre-vote got %d of %d needed; not raising the term",
				round.votes, round.needed())
		}
	}

	// A node always votes for itself, and records the vote locally first: if it
	// cannot persist its own vote it has no business claiming the lease.
	if err := m.grantLocally(ctx, localNodeID, term, renewal); err != nil {
		return fmt.Errorf("could not record own vote: %w", err)
	}

	round := m.countVotes(ctx, nodes, localNodeID, nodeToken,
		&LeaseRequest{CandidateID: localNodeID, Term: term, Renewal: renewal})
	votes, needed := round.votes, round.needed()
	if votes < needed {
		return fmt.Errorf("got %d of %d votes needed among %d responding nodes",
			votes, needed, round.participants)
	}

	// Won. Only now does the lease exist, and only now is it written down.
	if err := m.recordLease(ctx, localNodeID, term); err != nil {
		return fmt.Errorf("won the election but could not record the lease: %w", err)
	}

	m.mu.Lock()
	wasLeader := m.isLeader
	m.isLeader = true
	// The leader's own view of the lease ends earlier than the followers', so
	// it stops writing before anybody could grant the lease to someone else.
	m.leaderUntil = time.Now().Add(LeaseDuration - leaderSafetyMargin)
	m.currentTerm = term
	m.knownLeader = localNodeID
	m.mu.Unlock()

	if !wasLeader {
		m.log.WithFields(logrus.Fields{"term": term, "votes": votes, "needed": needed}).
			Info("This node is now the cluster coordinator")
	}
	return nil
}

// electionRound is the outcome of asking every node once: how many voted for
// this candidate, and how many took part at all.
type electionRound struct {
	votes        int
	participants int
}

// needed is the majority required to win, counted over the nodes that TOOK PART
func (r electionRound) needed() int {
	if r.participants < 1 {
		return 1
	}
	return r.participants/2 + 1
}

// countVotes collects this node's own answer and every other node's.
func (m *LeaderManager) countVotes(ctx context.Context, nodes []*Node, localNodeID, nodeToken string, req *LeaseRequest) electionRound {
	// This node always takes part in its own election.
	round := electionRound{participants: 1}

	if req.PreVote {
		// The local answer goes through exactly the same rules as everybody
		// else's, so a node that would refuse itself never campaigns at all.
		if m.GrantLease(ctx, req).Granted {
			round.votes++
		}
	} else {
		// The real round has already recorded this node's vote for itself.
		round.votes = 1
	}

	for _, node := range nodes {
		if node.ID == localNodeID {
			continue
		}
		granted, answered := m.requestVote(ctx, node, req, localNodeID, nodeToken)
		if answered {
			round.participants++
		}
		if granted {
			round.votes++
		}
	}
	return round
}

// nextTerm returns the term to campaign with: the same one when renewing, the
// next one when taking the lease from somebody.
func (m *LeaderManager) nextTerm(ctx context.Context, renewal bool) int64 {
	if renewal {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.currentTerm
	}

	_, leaseTerm, _, _ := m.readLease(ctx)
	votedTerm, _ := m.readVote(ctx)
	stored := leaseTerm
	if votedTerm > stored {
		stored = votedTerm
	}

	m.mu.RLock()
	known := m.currentTerm
	m.mu.RUnlock()

	if known > stored {
		stored = known
	}
	return stored + 1
}

// hasPeers reports whether the cluster holds any node other than this one.
func (m *LeaderManager) hasPeers(ctx context.Context) bool {
	nodes, err := m.clusterManager.ListNodes(ctx)
	if err != nil {
		// Unknown: assume peers exist, which is the cautious answer — waiting a
		// moment costs an election round, campaigning blindly costs safety.
		return true
	}
	return len(nodes) > 1
}

// electionDelay is how long a node waits before campaigning once it notices the
func electionDelay() time.Duration {
	return time.Duration(rand.Int63n(int64(RenewInterval * 2)))
}

// GrantLease is the answer this node gives a candidate.
func (m *LeaderManager) GrantLease(ctx context.Context, req *LeaseRequest) *LeaseResponse {
	m.mu.Lock()
	defer m.mu.Unlock()

	leaseLeader, leaseTerm, expiresAt, hasLease := m.readLease(ctx)
	votedTerm, votedFor := m.readVote(ctx)

	// The term this node has already seen is the newest of the two records.
	seenTerm := leaseTerm
	if votedTerm > seenTerm {
		seenTerm = votedTerm
	}

	refuse := func(reason string) *LeaseResponse {
		return &LeaseResponse{Granted: false, Term: seenTerm, Leader: leaseLeader, Reason: reason}
	}

	// A term already left behind belongs to a leader that has been superseded;
	// that is how one returning from a partition is fenced out.
	if req.Term < seenTerm {
		return refuse("stale term")
	}
	// One vote per term, or two candidates could each collect a majority.
	if req.Term == votedTerm && votedFor != "" && votedFor != req.CandidateID {
		return refuse("term already granted to another node")
	}
	if hasLease && time.Now().Unix() < expiresAt && leaseLeader != req.CandidateID {
		return refuse("lease still held")
	}

	if req.PreVote {
		return &LeaseResponse{Granted: true, Term: seenTerm, Leader: leaseLeader}
	}

	if err := m.recordVote(ctx, req.Term, req.CandidateID); err != nil {
		return refuse("could not record the vote")
	}

	if req.CandidateID != m.localNodeID {
		if err := m.recordLease(ctx, req.CandidateID, req.Term); err != nil {
			return refuse("could not record the grant")
		}
		// Granting the lease to somebody else means this node is not the
		// leader, whatever it believed a moment ago.
		m.isLeader = false
		m.knownLeader = req.CandidateID
	}

	if req.Term > m.currentTerm {
		m.currentTerm = req.Term
	}

	return &LeaseResponse{Granted: true, Term: req.Term, Leader: req.CandidateID}
}

// grantLocally records this node's vote for itself.
func (m *LeaderManager) grantLocally(ctx context.Context, candidateID string, term int64, renewal bool) error {
	response := m.GrantLease(ctx, &LeaseRequest{CandidateID: candidateID, Term: term, Renewal: renewal})
	if !response.Granted {
		return fmt.Errorf("own node refused the lease: %s", response.Reason)
	}
	return nil
}

// requestVote asks one node for its vote.
func (m *LeaderManager) requestVote(ctx context.Context, node *Node, req *LeaseRequest, sourceNodeID, nodeToken string) (granted, answered bool) {
	entryFor := func(reason string, err error) *logrus.Entry {
		entry := m.log.WithFields(logrus.Fields{
			"peer":          node.Name,
			"peer_endpoint": node.Endpoint,
			"reason":        reason,
		})
		if err != nil {
			entry = entry.WithError(err)
		}
		return entry
	}

	refuse := func(reason string, err error) (bool, bool) {
		if m.peerWasSilent(node.ID) {
			entryFor(reason, err).Debug("Peer is still not answering")
		} else {
			entryFor(reason, err).Warn("Peer is not answering; electing without it")
		}
		m.setPeerSilent(node.ID, true)
		return false, false
	}

	answeredNo := func(reason string) (bool, bool) {
		if req.PreVote {
			entryFor(reason, nil).Debug("Peer would not vote for this node")
		} else {
			entryFor(reason, nil).Warn("Peer did not grant the leader lease")
		}
		// It answered, so it is not silent, whatever it answered.
		m.notePeerAnswered(node)
		return false, true
	}

	body, err := json.Marshal(req)
	if err != nil {
		return refuse("could not encode the request", err)
	}

	// Bounded: an unreachable node must not hold up an election.
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/api/internal/cluster/leader-lease", node.Endpoint)
	httpReq, err := m.proxyClient.CreateAuthenticatedRequest(callCtx, "POST", url,
		bytes.NewReader(body), sourceNodeID, nodeToken)
	if err != nil {
		return refuse("could not build the request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := m.proxyClient.DoAuthenticatedRequest(httpReq)
	if err != nil {
		return refuse("unreachable", err)
	}
	defer resp.Body.Close()

	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return answeredNo(fmt.Sprintf("answered %d: %s", resp.StatusCode,
			strings.TrimSpace(string(payload))))
	}
	if readErr != nil {
		return refuse("could not read the answer", readErr)
	}

	var answer LeaseResponse
	if err := json.Unmarshal(payload, &answer); err != nil {
		return refuse("unreadable answer", err)
	}

	if answer.Term > m.CurrentTerm() {
		m.mu.Lock()
		m.currentTerm = answer.Term
		m.mu.Unlock()
	}

	if !answer.Granted {
		return answeredNo(fmt.Sprintf("voted no: %s (peer term %d, peer leader %q)",
			answer.Reason, answer.Term, answer.Leader))
	}
	m.notePeerAnswered(node)
	return true, true
}

// peerWasSilent reports whether this peer was already known not to be
// answering.
func (m *LeaderManager) peerWasSilent(nodeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.peerSilent[nodeID]
}

// setPeerSilent records whether a peer is answering.
func (m *LeaderManager) setPeerSilent(nodeID string, silent bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.peerSilent == nil {
		m.peerSilent = make(map[string]bool)
	}
	m.peerSilent[nodeID] = silent
}

// notePeerAnswered records that a peer is responding again, and says so once
// when it had been silent — the return of a node is worth a line, and worth
// exactly one.
func (m *LeaderManager) notePeerAnswered(node *Node) {
	if m.peerWasSilent(node.ID) {
		m.log.WithFields(logrus.Fields{
			"peer":          node.Name,
			"peer_endpoint": node.Endpoint,
		}).Info("Peer is answering again and is taking part in elections")
	}
	m.setPeerSilent(node.ID, false)
}

// standDown gives up leadership locally, without waiting for the lease to lapse.
func (m *LeaderManager) standDown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isLeader = false
	m.leaderUntil = time.Time{}
}

// release hands the lease back on a clean shutdown so the cluster elects a new
// coordinator in the next tick rather than after a full lease.
func (m *LeaderManager) release(ctx context.Context) {
	m.mu.Lock()
	wasLeader := m.isLeader
	m.isLeader = false
	m.mu.Unlock()

	if !wasLeader {
		return
	}
	if _, err := m.db.ExecContext(ctx,
		`UPDATE cluster_leader SET expires_at = 0 WHERE id = 1`); err != nil {
		m.log.WithError(err).Debug("Could not release the lease on shutdown")
	}
}

// rememberLocalID caches this node's own identifier, so answering a vote never
// depends on a database read.
func (m *LeaderManager) rememberLocalID(ctx context.Context) {
	id, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return
	}
	m.mu.Lock()
	m.localNodeID = id
	m.mu.Unlock()
}
