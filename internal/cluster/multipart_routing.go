package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// UploadNode returns the node a multipart upload was started on, where its
// parts, completion and abort are made, when every node holds every bucket (a
// replication factor above 1); with a factor of 1 a request goes to the
// bucket's node, which holds its uploads. local is true when that is this
// node, or when the upload ID names no node. A node that is no longer in the
// cluster is ErrNodeNotFound: its uploads are gone.
func (m *Manager) UploadNode(ctx context.Context, uploadID string) (node *Node, local bool, err error) {
	owner := object.UploadOwner(uploadID)
	if owner == "" || !m.IsClusterEnabled() {
		return nil, true, nil
	}
	if factor, err := m.GetReplicationFactor(ctx); err != nil {
		return nil, false, err
	} else if factor <= 1 {
		return nil, true, nil
	}
	localID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return nil, false, err
	}
	if owner == localID {
		return nil, true, nil
	}
	node, err = m.GetNode(ctx, owner)
	if err != nil {
		return nil, false, err
	}
	return node, false, nil
}

// PeerMultipartUploads lists the multipart uploads of a bucket started on the
// other healthy nodes, when every node holds every bucket (a replication factor
// above 1). A node that does not answer is left out.
func (m *Manager) PeerMultipartUploads(ctx context.Context, bucket string) ([]object.MultipartUpload, error) {
	if !m.IsClusterEnabled() {
		return nil, nil
	}
	factor, err := m.GetReplicationFactor(ctx)
	if err != nil || factor <= 1 {
		return nil, err
	}
	localID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := m.GetHealthyNodes(ctx)
	if err != nil {
		return nil, err
	}
	client := NewProxyClient(m.GetTLSConfig())
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		uploads []object.MultipartUpload
	)
	for _, n := range nodes {
		if n.ID == localID {
			continue
		}
		wg.Add(1)
		go func(n *Node) {
			defer wg.Done()
			got, err := nodeMultipartUploads(ctx, client, n, localID, bucket)
			if err != nil {
				logrus.WithError(err).WithField("node_id", n.ID).
					Warn("Multipart uploads of a node not listed")
				return
			}
			mu.Lock()
			uploads = append(uploads, got...)
			mu.Unlock()
		}(n)
	}
	wg.Wait()
	return uploads, nil
}

func nodeMultipartUploads(ctx context.Context, client *ProxyClient, n *Node, localID, bucket string) ([]object.MultipartUpload, error) {
	target := fmt.Sprintf("%s/api/internal/cluster/ha/multipart-uploads?bucket=%s", n.Endpoint, url.QueryEscape(bucket))
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodGet, target, nil, localID, n.NodeToken)
	if err != nil {
		return nil, err
	}
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Uploads []object.MultipartUpload `json:"uploads"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode multipart uploads: %w", err)
	}
	return out.Uploads, nil
}
