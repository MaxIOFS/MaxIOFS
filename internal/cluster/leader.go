package cluster

// Leader election for the control plane.
//
// Object traffic needs no leader: buckets are owned by a node and reads fall
// back to replicas. Configuration is different — users, policies, permissions —
// because two nodes writing the same entity converge on last-write-wins, and
// timestamps have one-second resolution, so two writes in the same second can
// leave the cluster permanently disagreeing. One writer removes the problem
// rather than detecting it afterwards.
//
// The election is a lease held by majority consent:
//
//   - A candidate asks every node to grant it the lease for a term. It becomes
//     leader only with a majority. Two majorities always overlap, so two
//     leaders cannot exist at once — that is what makes split-brain impossible
//     rather than unlikely.
//   - The term is monotonic. A node that was leader, got cut off by a network
//     partition and comes back finds its term is stale and its writes refused,
//     even if it still believes it leads.
//   - The leader renews before the lease expires. A renewal that cannot reach a
//     majority makes it stand down immediately, so it stops writing before the
//     others could elect somebody else.
//   - The leader treats its own lease as expiring EARLIER than the followers do.
//     That guard is what keeps an old leader from writing during the instant a
//     new one is taking over.
//
// Nothing here depends on clocks being in sync between nodes. A node measures
// its own lease with its own clock, and the guard above covers the skew.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// LeaseDuration is how long a leader holds the lease without renewing.
	// Long enough to survive a slow round trip, short enough that a dead
	// leader is replaced quickly.
	LeaseDuration = 15 * time.Second

	// RenewInterval is how often the leader renews. Comfortably shorter than
	// the lease so a single failed round does not cost it the leadership.
	RenewInterval = 5 * time.Second

	// leaderSafetyMargin is how much earlier the leader considers its own lease
	// gone. A leader stops writing this long before any follower would consider
	// the lease free, which is the window that keeps two leaders from
	// overlapping when clocks differ.
	leaderSafetyMargin = 3 * time.Second
)

// LeaseRequest is a candidate asking a node to grant it the lease.
type LeaseRequest struct {
	CandidateID string `json:"candidate_id"`
	Term        int64  `json:"term"`
	// Renewal marks a leader extending a lease it already holds, which a node
	// grants without needing the previous lease to have expired.
	Renewal bool `json:"renewal"`
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
	// localNodeID is cached because GrantLease answers other nodes on the hot
	// path of an election, and reading it from the database there would make
	// the vote depend on a query that can be slow exactly when the cluster is
	// unhealthy.
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

// EnsureSchema creates the lease table. It lives here rather than in a
// migration so an installation that already applied the current schema still
// gets it: an amended migration never runs again on a database that applied it.
func (m *LeaderManager) EnsureSchema() error {
	_, err := m.db.Exec(`
		CREATE TABLE IF NOT EXISTS cluster_leader (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			leader_id TEXT NOT NULL,
			term INTEGER NOT NULL,
			granted_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		)
	`)
	return err
}

// IsLeader reports whether this node currently holds the lease.
//
// It re-checks expiry on every call rather than trusting a flag: a leader whose
// renewals are failing must stop considering itself leader the moment its lease
// lapses, not when the next renewal tick notices.
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
	// The proxy client is installed by the constructor with the same dynamic
	// getter, so re-creating it here bought nothing — and it was a plain
	// pointer write performed from the initialize/join HTTP handlers while
	// both servers were already serving and other goroutines were reading it.
	m.rememberLocalID(ctx)
	go m.loop(ctx)
}

// Stop halts the loop and releases the lease, so a planned shutdown hands over
// in seconds instead of leaving the cluster leaderless for a full lease.
func (m *LeaderManager) Stop() {
	// Idempotent: a bare close panics on the second call, and shutdown paths
	// that can both fire — a signal handler and an explicit stop — would take
	// the process down while it was already on its way out.
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
		m.mu.Unlock()
		return
	}

	if err := m.campaign(ctx, false); err != nil {
		m.log.WithError(err).Debug("Did not win the election this round")
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

	// A majority is counted over every node the cluster knows, not over the
	// ones currently reachable. Counting only reachable nodes would let each
	// side of a network partition elect its own leader.
	total := len(nodes)
	if total == 0 {
		total = 1
	}
	needed := total/2 + 1

	term := m.nextTerm(ctx, renewal)

	// A node always votes for itself, and records the vote locally first: if it
	// cannot persist its own grant it has no business claiming the lease.
	if err := m.grantLocally(ctx, localNodeID, term, renewal); err != nil {
		return fmt.Errorf("could not record own vote: %w", err)
	}
	votes := 1

	request := &LeaseRequest{CandidateID: localNodeID, Term: term, Renewal: renewal}
	for _, node := range nodes {
		if node.ID == localNodeID {
			continue
		}
		if m.requestVote(ctx, node, request, localNodeID, nodeToken) {
			votes++
		}
	}

	if votes < needed {
		return fmt.Errorf("got %d of %d votes needed", votes, needed)
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

// nextTerm returns the term to campaign with: the same one when renewing, the
// next one when taking the lease from somebody.
func (m *LeaderManager) nextTerm(ctx context.Context, renewal bool) int64 {
	if renewal {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.currentTerm
	}

	var stored int64
	_ = m.db.QueryRowContext(ctx, `SELECT term FROM cluster_leader WHERE id = 1`).Scan(&stored)

	m.mu.RLock()
	known := m.currentTerm
	m.mu.RUnlock()

	if known > stored {
		stored = known
	}
	return stored + 1
}

// GrantLease is the answer this node gives a candidate. It is the whole safety
// property, so the rules are explicit:
//
//   - A term at or below the one already seen is refused; that is how a leader
//     returning from a partition is fenced out.
//   - A live lease held by somebody else is refused unless the holder is the
//     candidate renewing its own.
func (m *LeaderManager) GrantLease(ctx context.Context, req *LeaseRequest) *LeaseResponse {
	m.mu.Lock()
	defer m.mu.Unlock()

	var currentLeader string
	var currentTerm, expiresAt int64
	err := m.db.QueryRowContext(ctx,
		`SELECT leader_id, term, expires_at FROM cluster_leader WHERE id = 1`).
		Scan(&currentLeader, &currentTerm, &expiresAt)
	hasLease := err == nil

	if hasLease {
		if req.Term < currentTerm {
			return &LeaseResponse{Granted: false, Term: currentTerm, Leader: currentLeader,
				Reason: "stale term"}
		}
		// The same term is only acceptable from the node that already holds it,
		// renewing. Anyone else would be a second leader in one term.
		if req.Term == currentTerm && currentLeader != req.CandidateID {
			return &LeaseResponse{Granted: false, Term: currentTerm, Leader: currentLeader,
				Reason: "term already granted to another node"}
		}
		if time.Now().Unix() < expiresAt && currentLeader != req.CandidateID {
			return &LeaseResponse{Granted: false, Term: currentTerm, Leader: currentLeader,
				Reason: "lease still held"}
		}
	}

	now := time.Now()
	expires := now.Add(LeaseDuration)
	if _, err := m.db.ExecContext(ctx, `
		INSERT INTO cluster_leader (id, leader_id, term, granted_at, expires_at)
		VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			leader_id = excluded.leader_id,
			term = excluded.term,
			granted_at = excluded.granted_at,
			expires_at = excluded.expires_at
	`, req.CandidateID, req.Term, now.Unix(), expires.Unix()); err != nil {
		return &LeaseResponse{Granted: false, Reason: "could not record the grant"}
	}

	if req.Term > m.currentTerm {
		m.currentTerm = req.Term
	}
	m.knownLeader = req.CandidateID

	// Granting the lease to somebody else means this node is not the leader,
	// whatever it believed a moment ago.
	if req.CandidateID != m.localNodeID {
		m.isLeader = false
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
func (m *LeaderManager) requestVote(ctx context.Context, node *Node, req *LeaseRequest, sourceNodeID, nodeToken string) bool {
	body, err := json.Marshal(req)
	if err != nil {
		return false
	}

	// Bounded: an unreachable node must not hold up an election.
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/api/internal/cluster/leader-lease", node.Endpoint)
	httpReq, err := m.proxyClient.CreateAuthenticatedRequest(callCtx, "POST", url,
		bytes.NewReader(body), sourceNodeID, nodeToken)
	if err != nil {
		return false
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := m.proxyClient.DoAuthenticatedRequest(httpReq)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return false
	}

	var answer LeaseResponse
	if err := json.Unmarshal(payload, &answer); err != nil {
		return false
	}

	// A node answering with a higher term knows about an election this
	// candidate has not seen; adopting it prevents campaigning on a stale term
	// for ever.
	if answer.Term > m.CurrentTerm() {
		m.mu.Lock()
		m.currentTerm = answer.Term
		m.mu.Unlock()
	}
	return answer.Granted
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
