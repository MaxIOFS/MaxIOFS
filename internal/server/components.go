package server

import (
	"reflect"

	"github.com/sirupsen/logrus"
)

// component is anything that owns background goroutines. Its Stop must wait for
// them: once the registry has been drained, nothing may still be writing to the
// stores, because Pebble panics on use after close.
type component interface {
	Stop()
}

// registry holds the components in construction order.
type registry struct {
	entries []registryEntry
}

type registryEntry struct {
	name string
	c    component
}

// supervise records a component and returns it unchanged, so registering and
// constructing are one expression:
//
//	userSyncMgr := supervise(reg, "userSync", cluster.NewUserSyncManager(db, mgr))
//
// There is no separate list to keep in step with the struct, which is how
// thirteen managers ended up never being stopped.
func supervise[T component](r *registry, name string, c T) T {
	if v := reflect.ValueOf(c); v.Kind() == reflect.Ptr && v.IsNil() {
		return c
	}
	if r == nil {
		return c
	}
	r.entries = append(r.entries, registryEntry{name: name, c: c})
	return c
}

// stopAll stops every registered component in reverse construction order, so a
// component is stopped before whatever it was built on top of.
func (r *registry) stopAll() {
	for i := len(r.entries) - 1; i >= 0; i-- {
		e := r.entries[i]
		e.c.Stop()
		logrus.WithField("component", e.name).Debug("Component stopped")
	}
}
