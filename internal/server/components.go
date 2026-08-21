package server

import (
	"reflect"

	"github.com/sirupsen/logrus"
)

// Stop must wait for the component's goroutines, not just signal them.
type component interface {
	Stop()
}

type registry struct {
	entries []registryEntry
}

type registryEntry struct {
	name string
	c    component
}

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

// Reverse order: a component is stopped before whatever it was built on.
func (r *registry) stopAll() {
	for i := len(r.entries) - 1; i >= 0; i-- {
		e := r.entries[i]
		e.c.Stop()
		logrus.WithField("component", e.name).Debug("Component stopped")
	}
}
