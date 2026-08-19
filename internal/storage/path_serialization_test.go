package storage

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Every method on Backend that changes an object's path must serialize on the
// path lock. The data file, the sidecar and the staged sidecar are one unit,
// and a method that skips the lock can tear another method's commit in half.
//
// The list of read-only methods is the only thing this test takes on trust: a
// new mutating method that is not covered here fails the completeness check
// below rather than passing silently.
var readOnlyBackendMethods = map[string]bool{
	"Get":         true,
	"GetMetadata": true,
	"Exists":      true,
	"List":        true,
	"Close":       true,
	"GetRootPath": true,
}

func mutatingBackendCalls(fs *FilesystemBackend, path string) map[string]func() {
	ctx := context.Background()
	return map[string]func(){
		"Put": func() {
			_ = fs.Put(ctx, path, strings.NewReader("payload"), nil)
		},
		"Delete": func() {
			_ = fs.Delete(ctx, path)
		},
		"SetMetadata": func() {
			_ = fs.SetMetadata(ctx, path, map[string]string{"k": "v"})
		},
	}
}

func TestBackendMutatorsAreAllCovered(t *testing.T) {
	covered := mutatingBackendCalls(nil, "")
	iface := reflect.TypeOf((*Backend)(nil)).Elem()

	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if readOnlyBackendMethods[name] {
			continue
		}
		if _, ok := covered[name]; !ok {
			t.Fatalf("Backend.%s changes state but is not covered by the path-serialization test; "+
				"add it to mutatingBackendCalls, or to readOnlyBackendMethods if it reads only", name)
		}
	}
}

func TestBackendMutatorsSerializeOnThePathLock(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-path-lock-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	const path = "b1/serialized.txt"
	if err := fs.Put(context.Background(), path, strings.NewReader("initial"), nil); err != nil {
		t.Fatal(err)
	}

	for name, call := range mutatingBackendCalls(fs, path) {
		t.Run(name, func(t *testing.T) {
			unlock := fs.lockPath(path)

			done := make(chan struct{})
			go func() {
				call()
				close(done)
			}()

			select {
			case <-done:
				unlock()
				t.Fatalf("%s completed while the path lock was held — it does not serialize", name)
			case <-time.After(150 * time.Millisecond):
			}

			unlock()

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("%s did not complete after the path lock was released", name)
			}
		})
	}
}
