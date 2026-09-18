package storage

// SyncDirectory persists directory entries where the platform supports it.
func SyncDirectory(dir string) error { return syncDir(dir) }
