package cluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// errMetadataOpRefused is a replica's 4xx answer to a metadata change: the
// change no longer applies there, for instance to an object since deleted.
// Retrying cannot change that.
var errMetadataOpRefused = errors.New("replica refused the metadata change")

// metadataOpTimeout bounds one delivery of a metadata change.
const metadataOpTimeout = 5 * time.Second

// sendMetadataOp delivers one metadata change to node.
func sendMetadataOp(ctx context.Context, client *ProxyClient, node *Node, localID, bucket string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), metadataOpTimeout)
	defer cancel()
	url := fmt.Sprintf("%s/api/internal/cluster/ha/metadata-op", node.Endpoint)
	req, err := client.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(body), localID, node.NodeToken)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HABucketHeader, bucket)
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode < 300:
		return nil
	case resp.StatusCode < 500:
		return fmt.Errorf("%w: status %d", errMetadataOpRefused, resp.StatusCode)
	default:
		return fmt.Errorf("status %d", resp.StatusCode)
	}
}

// queueMetadataOp keeps a metadata change for node until it can be delivered,
// and records the node as having missed a write so that it is caught up when
// it is healthy.
func (m *Manager) queueMetadataOp(ctx context.Context, nodeID, bucket string, body []byte) {
	ctx = context.WithoutCancel(ctx)
	now := time.Now()
	if _, err := m.db.ExecContext(ctx,
		`INSERT INTO ha_pending_metadata_ops (node_id, bucket, op, created_at) VALUES (?, ?, ?, ?)`,
		nodeID, bucket, string(body), now.Unix()); err != nil {
		m.log.WithError(err).WithField("node_id", nodeID).
			Error("HA: failed to queue a metadata change; the replica may diverge")
		return
	}
	m.noteMissedWrites(ctx, "", now, nodeID)
}

// hasQueuedMetadataOps reports whether node still has changes waiting. A new
// change then waits behind them, so the node applies them in order.
func (m *Manager) hasQueuedMetadataOps(ctx context.Context, nodeID string) bool {
	var one int
	err := m.db.QueryRowContext(ctx,
		`SELECT 1 FROM ha_pending_metadata_ops WHERE node_id = ? LIMIT 1`, nodeID).Scan(&one)
	return err == nil
}

// replayMetadataOps delivers node's queued metadata changes in the order they
// were made. A change the node refuses is dropped; any other failure stops the
// replay, keeping the rest in order for the next attempt.
func (m *Manager) replayMetadataOps(ctx context.Context, client *ProxyClient, node *Node, localID string) error {
	for {
		type queued struct {
			id     int64
			bucket string
			body   string
		}
		rows, err := m.db.QueryContext(ctx,
			`SELECT id, bucket, op FROM ha_pending_metadata_ops WHERE node_id = ? ORDER BY id LIMIT 100`, node.ID)
		if err != nil {
			return fmt.Errorf("read queued metadata changes: %w", err)
		}
		var batch []queued
		for rows.Next() {
			var q queued
			if err := rows.Scan(&q.id, &q.bucket, &q.body); err != nil {
				rows.Close()
				return fmt.Errorf("read queued metadata changes: %w", err)
			}
			batch = append(batch, q)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read queued metadata changes: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		for _, q := range batch {
			err := sendMetadataOp(ctx, client, node, localID, q.bucket, []byte(q.body))
			if errors.Is(err, errMetadataOpRefused) {
				logrus.WithError(err).WithFields(logrus.Fields{"node_id": node.ID, "bucket": q.bucket}).
					Error("HA: a replica refused a queued metadata change; dropping it")
			} else if err != nil {
				return err
			}
			if _, err := m.db.ExecContext(ctx, `DELETE FROM ha_pending_metadata_ops WHERE id = ?`, q.id); err != nil {
				return fmt.Errorf("remove delivered metadata change: %w", err)
			}
		}
	}
}

// dropQueuedMetadataOps forgets the changes queued for a node that left the
// cluster.
func (m *Manager) dropQueuedMetadataOps(ctx context.Context, nodeID string) {
	if _, err := m.db.ExecContext(ctx, `DELETE FROM ha_pending_metadata_ops WHERE node_id = ?`, nodeID); err != nil {
		m.log.WithError(err).WithField("node_id", nodeID).Warn("HA: failed to drop queued metadata changes")
	}
}
