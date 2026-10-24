package cluster

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storageclass"
	"github.com/sirupsen/logrus"
)

// HALocationsHeader carries, comma-separated, the nodes that hold a copy's
// data on a legacy transfer; a raw transfer carries them in its entry.
const HALocationsHeader = "X-HA-Locations"

// HALocationsGenHeader carries the number of the change that set the
// locations of a copy's write, when above 0.
const HALocationsGenHeader = "X-HA-Locations-Gen"

// SetHALocations attaches the locations of obj's write and the number of the
// change that set them.
func SetHALocations(h http.Header, obj *object.Object) {
	if len(obj.Locations) > 0 {
		h.Set(HALocationsHeader, strings.Join(obj.Locations, ","))
	}
	if obj.LocationsGen > 0 {
		h.Set(HALocationsGenHeader, strconv.FormatInt(obj.LocationsGen, 10))
	}
}

// HAETagHeader carries the ETag of the object a node serves another: the
// reader checks it is the data of the entry it holds.
const HAETagHeader = "X-HA-ETag"

// placement is where a write made here goes: the nodes that hold its data,
// this node first, and the other healthy nodes, which hold its entry, those of
// the write's class first, then least full first.
type placement struct {
	localID string
	factor  int
	holders []string
	remote  []*Node
	entries []*Node
	// class is the class whose nodes keep the write (see classView.of).
	class string
	view  *classView
	// passOn is set when this node keeps no new data of the write: under
	// storage pressure, drained, or in another class.
	passOn bool
	// inClassOnly is set when this node passes its copy on for being in
	// another class alone: only a node of the write's class takes it.
	inClassOnly bool
	// parity: the write is held here alone, then spread over takers, the
	// nodes that take data, those of the write's class first and this node
	// first among those it is in (see spread).
	parity bool
	takers []*Node
}

// place chooses the nodes of a write of storage class sc made here in a
// cluster: this node and the least full healthy nodes of the write's class
// hold its data, up to the factor, and nodes of other classes when the class
// has too few; the other healthy nodes hold its entry. A node
// neither healthy nor dead misses the write and is caught up when it is
// back. This node, under storage pressure, drained or in another class, then
// passes its copy on (see relieve).
func (h *HAObjectManager) place(ctx context.Context, sc string) (*placement, bool) {
	view, err := h.mgr.classView(ctx)
	if err != nil {
		return nil, false
	}
	class := view.of(sc)
	protection := view.protection[class]
	factor := protection.Factor
	localID, err := h.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return nil, false
	}
	healthy, err := h.mgr.GetHealthyNodes(ctx)
	if err != nil {
		return nil, false
	}
	h.mgr.noteMissedWrites(ctx, localID, time.Now())

	var others, entriesOnly []*Node
	for _, n := range healthy {
		switch {
		case n.ID == localID:
		case n.TakesData():
			others = append(others, n)
		default:
			entriesOnly = append(entriesOnly, n)
		}
	}
	slices.SortStableFunc(others, h.mgr.leastFull)
	view.sortForClass(others, class)
	p := &placement{localID: localID, factor: factor, holders: []string{localID}, parity: protection.Parity, class: class, view: view}
	local, err := h.mgr.GetNode(ctx, localID)
	if err == nil {
		elsewhere := view.node(local) != class
		p.passOn = local.HealthStatus == HealthStatusStoragePressure || local.Drained || elsewhere
		p.inClassOnly = elsewhere && local.TakesData()
	}
	if p.parity {
		if local != nil && local.TakesData() {
			p.takers = append(p.takers, local)
		}
		p.takers = append(p.takers, others...)
		view.sortForClass(p.takers, class)
		p.entries = append(slices.Clone(others), entriesOnly...)
		return p, true
	}
	for _, n := range others {
		if len(p.holders) < factor {
			p.holders = append(p.holders, n.ID)
			p.remote = append(p.remote, n)
		} else {
			p.entries = append(p.entries, n)
		}
	}
	p.entries = append(p.entries, entriesOnly...)
	return p, true
}

// Space is the data STANDARD has room for and the room left, as each node's
// last health check found its disk: each byte is held on factor different
// nodes, or with parity its shards each on its own node. Dead nodes do not
// count.
func (m *Manager) Space(ctx context.Context) (capacity, available int64, ok bool) {
	if !m.IsClusterEnabled() {
		return 0, 0, false
	}
	view, err := m.classView(ctx)
	if err != nil {
		return 0, 0, false
	}
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		return 0, 0, false
	}
	target := view.target(nodes, storageclass.Standard, 1)
	var total, free []int64
	for _, n := range nodes {
		if n.HealthStatus == HealthStatusDead || view.node(n) != storageclass.Standard {
			continue
		}
		total = append(total, n.CapacityTotal)
		free = append(free, freeSpace(n))
	}
	return target.space(total), target.space(free), true
}

// space is the most data the given room of each node holds kept as t keeps
// it: each shard of a byte is 1/Data of it, on its own node.
func (t dataTarget) space(room []int64) int64 {
	if t.sharded() {
		return int64(t.layout.Data) * spaceForCopies(room, t.layout.Width())
	}
	return spaceForCopies(room, t.copies)
}

// spaceForCopies is the most data the given room of each node holds when each
// byte is kept on factor different nodes. A node holds one copy of a byte at
// most, so the k largest hold k copies at most and the others the rest.
func spaceForCopies(room []int64, factor int) int64 {
	sorted := make([]int64, len(room))
	var rest int64
	for i, r := range room {
		sorted[i] = max(r, 0)
		rest += sorted[i]
	}
	slices.SortFunc(sorted, func(a, b int64) int { return cmp.Compare(b, a) })
	space := rest / int64(factor)
	for k := 1; k < factor; k++ {
		if k <= len(sorted) {
			rest -= sorted[k-1]
		}
		space = min(space, rest/int64(factor-k))
	}
	return space
}

// freeSpace is a node's free space as its last health check found it, or -1
// when it reported none.
func freeSpace(n *Node) int64 {
	if n.CapacityTotal <= 0 {
		return -1
	}
	return n.CapacityTotal - n.CapacityUsed
}

func (m *Manager) noteRooms(rooms map[string]int64) {
	for id, room := range rooms {
		m.rooms.Store(id, room)
	}
}

// fill is how full node n is: of its room as last counted, or of its disk
// before a count. A node of unknown free space is the fullest.
func (m *Manager) fill(n *Node) float64 {
	free := freeSpace(n)
	if free < 0 {
		return math.Inf(1)
	}
	room := n.CapacityTotal
	if r, ok := m.rooms.Load(n.ID); ok {
		room = r.(int64)
	}
	if room <= 0 {
		return math.Inf(1)
	}
	return 1 - float64(free)/float64(room)
}

// leastFull orders nodes least full first, then by ID.
func (m *Manager) leastFull(a, b *Node) int {
	if c := cmp.Compare(m.fill(a), m.fill(b)); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

// replicate sends a write made here to the nodes of p: its data to the nodes
// that hold it, then, once enough copies hold it, its entry to the others. A
// node that fails is unavailable from then on and is caught up when it is
// back. Too few copies is ErrClusterDegraded.
func (h *HAObjectManager) replicate(ctx context.Context, p *placement, bucket, key, versionID string, modified time.Time) error {
	client := NewProxyClient(h.mgr.GetTLSConfig())
	ch := make(chan fanoutResult, len(p.remote))
	for _, n := range p.remote {
		go func(n *Node) {
			ch <- fanoutResult{n.ID, sendObjectVersion(ctx, client, h.Manager, n, p.localID, bucket, key, versionID)}
		}(n)
	}
	if err := h.collectAndCheckQuorum(ctx, ch, len(p.remote), RequiredReplicaAcks(p.factor), modified, "PUT", bucket, key); err != nil {
		h.noteEntriesMissed(ctx, p, modified)
		return err
	}
	h.sendEntry(ctx, client, p, bucket, key, versionID, modified)
	return nil
}

// relieve passes this node's copy of a write, when this node is under storage
// pressure, drained or in another class, to the first node that takes data
// among those that hold its entry only: those of the write's class first,
// least full first; of the write's class only when this node is just in
// another. The copy stays here when none takes it. Each copy names its node
// with a number of its own, and every node is then told the holders with a
// higher one.
func (h *HAObjectManager) relieve(ctx context.Context, p *placement, bucket, key string, obj *object.Object) {
	if !p.passOn || len(p.entries) == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	client := NewProxyClient(h.mgr.GetTLSConfig())
	others := slices.DeleteFunc(slices.Clone(p.holders), func(id string) bool { return id == p.localID })
	gen, locations := obj.LocationsGen, p.holders
	for _, n := range p.entries {
		if !n.TakesData() || (p.inClassOnly && p.view.node(n) != p.class) {
			continue
		}
		gen++
		named := append(slices.Clone(others), n.ID)
		if err := sendNamingCopy(ctx, client, h.Manager, n, p.localID, bucket, key, obj.VersionID, gen, named); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"node_id": n.ID, "bucket": bucket, "key": key}).
				Warn("HA: a node did not take the copy of a node under storage pressure")
			continue
		}
		locations = named
		break
	}
	err := h.SetLocations(ctx, bucket, key, object.LocationsChange{
		VersionID:    obj.VersionID,
		ETag:         obj.ETag,
		LastModified: obj.LastModified.Unix(),
		WrittenAt:    obj.WrittenAt,
		Locations:    locations,
		Gen:          gen + 1,
	})
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key}).
			Warn("HA: the holders of a write a node under storage pressure took were not all told")
	}
}

// noteEntriesMissed records the nodes that hold a write's entry only as having
// missed it.
func (h *HAObjectManager) noteEntriesMissed(ctx context.Context, p *placement, modified time.Time) {
	if len(p.entries) == 0 {
		return
	}
	ids := make([]string, len(p.entries))
	for i, n := range p.entries {
		ids[i] = n.ID
	}
	h.mgr.noteMissedWrites(ctx, "", modified, ids...)
}

// sendEntry sends a write's entry to the nodes that do not hold its data.
func (h *HAObjectManager) sendEntry(ctx context.Context, client *ProxyClient, p *placement, bucket, key, versionID string, modified time.Time) {
	if len(p.entries) == 0 {
		return
	}
	writer, ok := ReplicaWriter(h.Manager)
	if !ok {
		h.noteEntriesMissed(ctx, p, modified)
		return
	}
	entry, err := writer.ObjectEntry(ctx, bucket, key, versionID)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key}).
			Warn("HA: the entry of a write could not be read to send it")
		h.noteEntriesMissed(ctx, p, modified)
		return
	}
	body, err := json.Marshal(entry)
	if err != nil {
		h.noteEntriesMissed(ctx, p, modified)
		return
	}
	var wg sync.WaitGroup
	for _, n := range p.entries {
		wg.Add(1)
		go func(n *Node) {
			defer wg.Done()
			if err := sendObjectEntry(ctx, client, n, p.localID, bucket, body); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{"node_id": n.ID, "bucket": bucket, "key": key}).
					Warn("HA: a node did not take the entry of a write")
				h.mgr.markNodeUnavailable(ctx, n.ID, "HA entry")
				h.mgr.noteMissedWrites(ctx, "", modified, n.ID)
			}
		}(n)
	}
	wg.Wait()
}

// entryOn is n's entry of a write of key, the version versionID or, without
// one, the current object; nil when n has none.
func entryOn(ctx context.Context, client *ProxyClient, n *Node, localID, bucket, key, versionID string) (*metadata.ObjectMetadata, error) {
	target := fmt.Sprintf("%s/api/internal/cluster/ha/object-entry?bucket=%s&key=%s", n.Endpoint, url.QueryEscape(bucket), url.QueryEscape(key))
	if versionID != "" {
		target += "&versionId=" + url.QueryEscape(versionID)
	}
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodGet, target, nil, localID, n.NodeToken)
	if err != nil {
		return nil, err
	}
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GET entry returned %d: %s", resp.StatusCode, b)
	}
	var entry metadata.ObjectMetadata
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("decode entry: %w", err)
	}
	return &entry, nil
}

// sendObjectEntry sends n the entry of an object whose data it does not hold.
func sendObjectEntry(ctx context.Context, client *ProxyClient, n *Node, localID, bucket string, body []byte) error {
	target := fmt.Sprintf("%s/api/internal/cluster/ha/object-entry", n.Endpoint)
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodPut, target, bytes.NewReader(body), localID, n.NodeToken)
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// GetObject reads an object. One whose data other nodes hold is read from
// them, from the part asked for; one cut in shards from its shards. A node
// that missed a change of the write's placement takes it from the others and
// reads by it: the nodes of the new placement dropped the data its entry
// names.
func (h *HAObjectManager) GetObject(ctx context.Context, bucket, key string, versionID ...string) (*object.Object, io.ReadCloser, error) {
	return h.getObject(ctx, bucket, key, true, versionID...)
}

func (h *HAObjectManager) getObject(ctx context.Context, bucket, key string, takeLater bool, versionID ...string) (*object.Object, io.ReadCloser, error) {
	obj, reader, err := h.Manager.GetObject(ctx, bucket, key, versionID...)
	var elsewhere *object.DataElsewhereError
	if !errors.As(err, &elsewhere) || isHAReplica(ctx) {
		return obj, reader, err
	}
	if !h.mgr.IsClusterEnabled() {
		return nil, nil, object.ErrObjectNotFound
	}
	var data io.ReadCloser
	if _, sharded := elsewhere.Object.Erasure(); sharded {
		data, err = h.openShards(ctx, elsewhere.Object)
	} else {
		data, err = h.openRemote(ctx, bucket, key, elsewhere.Object, elsewhere.Locations)
	}
	if errors.Is(err, object.ErrDataUnavailable) && takeLater && h.takeLaterPlacement(ctx, bucket, key, elsewhere.Object) {
		return h.getObject(ctx, bucket, key, false, versionID...)
	}
	if err != nil {
		return nil, nil, err
	}
	return elsewhere.Object, data, nil
}

// placementAskTimeout bounds the asks of a read for a later placement.
const placementAskTimeout = 5 * time.Second

// takeLaterPlacement asks the other nodes in service for their entry of the
// write obj is, and takes the latest placement of it they hold, as a change of
// its locations sent to this node. It reports whether it took one.
func (h *HAObjectManager) takeLaterPlacement(ctx context.Context, bucket, key string, obj *object.Object) bool {
	writer, ok := ReplicaWriter(h.Manager)
	if !ok {
		return false
	}
	localID, err := h.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return false
	}
	nodes, err := h.mgr.ListNodes(ctx)
	if err != nil {
		return false
	}
	askCtx, cancel := context.WithTimeout(ctx, placementAskTimeout)
	defer cancel()
	client := NewProxyClient(h.mgr.GetTLSConfig())
	var mu sync.Mutex
	var latest *metadata.ObjectMetadata
	var wg sync.WaitGroup
	for _, n := range nodes {
		if n.ID == localID || !n.InService() {
			continue
		}
		wg.Go(func() {
			entry, err := entryOn(askCtx, client, n, localID, bucket, key, obj.VersionID)
			if err != nil || entry == nil || entry.VersionID != obj.VersionID || entry.ETag != obj.ETag ||
				entry.LastModified.Unix() != obj.LastModified.Unix() || entry.WrittenAt != obj.WrittenAt {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if latest == nil || entry.LocationsGen > latest.LocationsGen {
				latest = entry
			}
		})
	}
	wg.Wait()
	if latest == nil || latest.LocationsGen <= obj.LocationsGen {
		return false
	}
	layout, _ := object.ErasureLayout(latest)
	if err := writer.SetLocations(ctx, bucket, key, object.LocationsChange{
		VersionID: latest.VersionID, ETag: latest.ETag, LastModified: latest.LastModified.Unix(), WrittenAt: latest.WrittenAt,
		Locations: latest.Locations, Gen: latest.LocationsGen, Layout: layout, Class: latest.StorageClass,
	}); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key}).
			Warn("HA: a later placement of a write was not taken")
		return false
	}
	logrus.WithFields(logrus.Fields{"bucket": bucket, "key": key, "gen": latest.LocationsGen}).
		Info("HA: took a later placement of a write this node missed")
	return true
}

// openRemote opens the data of obj on the first of the nodes holding it that
// serves it, healthy nodes first. None is object.ErrDataUnavailable.
func (h *HAObjectManager) openRemote(ctx context.Context, bucket, key string, obj *object.Object, locations []string) (io.ReadCloser, error) {
	localID, err := h.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return nil, err
	}
	var holders []*Node
	for _, id := range locations {
		if id == localID {
			continue
		}
		if n, err := h.mgr.GetNode(ctx, id); err == nil && n.HealthStatus != HealthStatusDead {
			holders = append(holders, n)
		}
	}
	slices.SortStableFunc(holders, func(a, b *Node) int {
		return cmp.Compare(boolRank(a.HealthStatus != HealthStatusHealthy), boolRank(b.HealthStatus != HealthStatusHealthy))
	})
	client := NewProxyClient(h.mgr.GetTLSConfig())
	r := &remoteReader{size: obj.Size, open: func(offset int64) (io.ReadCloser, error) {
		for _, n := range holders {
			body, err := readObjectData(ctx, client, n, localID, bucket, key, obj, offset)
			if err == nil {
				return body, nil
			}
			logrus.WithError(err).WithFields(logrus.Fields{"node_id": n.ID, "bucket": bucket, "key": key}).
				Debug("HA: a node holding the data did not serve it")
		}
		return nil, object.ErrDataUnavailable
	}}
	if r.body, err = r.open(0); err != nil {
		return nil, err
	}
	return r, nil
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// readObjectData opens the data of obj on n from offset. The data must be that
// of obj: a node that holds another write of the key does not serve it.
func readObjectData(ctx context.Context, client *ProxyClient, n *Node, localID, bucket, key string, obj *object.Object, offset int64) (io.ReadCloser, error) {
	target := fmt.Sprintf("%s/api/internal/cluster/ha/objects/%s?bucket=%s", n.Endpoint, escapeHAObjectKey(key), url.QueryEscape(bucket))
	if obj.VersionID != "" {
		target += "&versionId=" + url.QueryEscape(obj.VersionID)
	}
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodGet, target, nil, localID, n.NodeToken)
	if err != nil {
		return nil, err
	}
	want := http.StatusOK
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		want = http.StatusPartialContent
	}
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != want || resp.Header.Get(HAETagHeader) != obj.ETag {
		resp.Body.Close()
		return nil, fmt.Errorf("status %d, ETag %q", resp.StatusCode, resp.Header.Get(HAETagHeader))
	}
	return resp.Body, nil
}

// remoteReader reads an object's data from the nodes that hold it. A Seek
// before reading opens the data again from there, so a range is read alone.
type remoteReader struct {
	open   func(offset int64) (io.ReadCloser, error)
	body   io.ReadCloser
	offset int64
	size   int64
}

func (r *remoteReader) Read(p []byte) (int, error) {
	if r.body == nil {
		body, err := r.open(r.offset)
		if err != nil {
			return 0, err
		}
		r.body = body
	}
	n, err := r.body.Read(p)
	r.offset += int64(n)
	return n, err
}

func (r *remoteReader) Seek(offset int64, whence int) (int64, error) {
	var to int64
	switch whence {
	case io.SeekStart:
		to = offset
	case io.SeekCurrent:
		to = r.offset + offset
	case io.SeekEnd:
		to = r.size + offset
	default:
		return r.offset, fmt.Errorf("invalid whence %d", whence)
	}
	if to < 0 {
		return r.offset, fmt.Errorf("negative position %d", to)
	}
	if to != r.offset && r.body != nil {
		r.body.Close()
		r.body = nil
	}
	r.offset = to
	return to, nil
}

func (r *remoteReader) Close() error {
	if r.body == nil {
		return nil
	}
	return r.body.Close()
}
