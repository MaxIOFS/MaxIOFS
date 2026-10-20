package cluster

import (
	"context"
	"fmt"
	"slices"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// ChangeStorageClass moves a write to another storage class, as a lifecycle
// transition does. In a cluster the first of the write's living holders moves
// it: every node is told its class with a change of its placement, then its
// copies or shards are moved to the nodes of the class (see repairWrite),
// copied there before they are dropped elsewhere; a move that does not end
// now is ended by the next repair. The other nodes report false, and so does
// every node for a write that names no node yet.
func (h *HAObjectManager) ChangeStorageClass(ctx context.Context, bucket, key string, change object.ClassChange) (bool, error) {
	changer, ok := h.Manager.(object.StorageClassChanger)
	if !ok {
		return false, fmt.Errorf("this node keeps no storage classes")
	}
	if !h.mgr.IsClusterEnabled() {
		return changer.ChangeStorageClass(ctx, bucket, key, change)
	}
	r, err := h.placementRepair(ctx)
	if err != nil || r == nil {
		return false, err
	}
	writes, err := keyWrites(ctx, h, bucket, key)
	if err != nil {
		return false, err
	}
	i := slices.IndexFunc(writes, func(w object.ObjectVersion) bool {
		return !w.IsDeleteMarker && w.VersionID == change.VersionID && w.ETag == change.ETag &&
			w.LastModified.Unix() == change.LastModified && w.WrittenAt == change.WrittenAt
	})
	if i < 0 {
		return false, nil
	}
	v := writes[i]
	if v.StorageClass == change.StorageClass || len(v.Locations) == 0 {
		return false, nil
	}
	living := r.livingHolders(v)
	if len(living) == 0 || slices.Min(living) != r.localID {
		return false, nil
	}
	moved := locationsChange(v, v.Locations, v.LocationsGen+1)
	moved.Layout, _ = v.Object.Erasure()
	moved.Class = change.StorageClass
	if err := h.SetLocations(ctx, bucket, key, moved); err != nil {
		return false, err
	}
	v.StorageClass, v.LocationsGen = change.StorageClass, moved.Gen
	if _, err := r.repairWrite(ctx, bucket, key, v); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": bucket, "key": key, "storage_class": change.StorageClass}).
			Warn("HA: the data of a write moved to another storage class is moved at the next repair")
	}
	return true, nil
}
