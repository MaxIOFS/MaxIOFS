package storage

import (
	"hash/fnv"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var objectLocks [1024]sync.Mutex

// LockObject serializes data and index changes, including recovery, for one key.
func LockObject(root, bucket, key string) func() {
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	root = filepath.Clean(root)
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	h := fnv.New32a()
	for _, value := range []string{root, bucket, key} {
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	mu := &objectLocks[h.Sum32()%uint32(len(objectLocks))]
	mu.Lock()
	return mu.Unlock
}
