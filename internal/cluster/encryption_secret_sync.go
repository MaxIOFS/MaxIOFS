package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/bgwork"
	"github.com/sirupsen/logrus"
)

// encryptionSecretPath is where a node answers with the fingerprint of its
// encryption secret (GET) and is given the coordinator's (POST).
const encryptionSecretPath = "/api/internal/cluster/encryption-secret"

// EncryptionSecretHolder is this node's encryption secret for stored
// credentials.
type EncryptionSecretHolder interface {
	Current() string
	Fingerprint() string
}

// EncryptionSecretSync makes every node hold the coordinator's encryption
// secret: the coordinator compares each healthy node's fingerprint with its
// own and sends its secret to a node that differs, which re-encrypts the
// credentials it stores and adopts it.
type EncryptionSecretSync struct {
	mgr      *Manager
	leader   interface{ IsLeader() bool }
	secret   EncryptionSecretHolder
	interval time.Duration

	bgwork.Worker
}

// NewEncryptionSecretSync wires the synchronization.
func NewEncryptionSecretSync(mgr *Manager, leader interface{ IsLeader() bool }, secret EncryptionSecretHolder) *EncryptionSecretSync {
	return &EncryptionSecretSync{mgr: mgr, leader: leader, secret: secret, interval: time.Minute}
}

// Start checks the nodes every minute until ctx ends.
func (s *EncryptionSecretSync) Start(ctx context.Context) {
	s.Spawn(func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.Stopped():
				return
			case <-ticker.C:
				s.SyncPeers(ctx)
			}
		}
	})
}

// SyncPeers sends the secret to every healthy node whose fingerprint differs,
// when this node is the coordinator.
func (s *EncryptionSecretSync) SyncPeers(ctx context.Context) {
	if !s.mgr.IsClusterEnabled() || !s.leader.IsLeader() {
		return
	}
	localID, err := s.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return
	}
	nodes, err := s.mgr.GetHealthyNodes(ctx)
	if err != nil {
		return
	}
	own := s.secret.Fingerprint()
	for _, n := range nodes {
		if n.ID == localID {
			continue
		}
		fingerprint, err := s.mgr.EncryptionSecretFingerprint(ctx, n)
		if err != nil {
			logrus.WithError(err).WithField("node_id", n.ID).Warn("Could not read a node's encryption secret fingerprint")
			continue
		}
		if fingerprint == own {
			continue
		}
		if err := s.send(ctx, n, localID); err != nil {
			logrus.WithError(err).WithField("node_id", n.ID).Error("A node holds another encryption secret and did not take the coordinator's")
			continue
		}
		logrus.WithField("node_id", n.ID).Info("A node held another encryption secret; it now holds the coordinator's")
	}
}

func (s *EncryptionSecretSync) send(ctx context.Context, node *Node, localID string) error {
	body, err := json.Marshal(map[string]string{"secret": s.secret.Current()})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := NewProxyClient(s.mgr.GetTLSConfig())
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodPost, node.Endpoint+encryptionSecretPath, bytes.NewReader(body), localID, node.NodeToken)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// EncryptionSecretFingerprint asks node for the fingerprint of its encryption
// secret.
func (m *Manager) EncryptionSecretFingerprint(ctx context.Context, node *Node) (string, error) {
	localID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := NewProxyClient(m.GetTLSConfig())
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodGet, node.Endpoint+encryptionSecretPath, nil, localID, node.NodeToken)
	if err != nil {
		return "", err
	}
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	var out struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Fingerprint, nil
}
