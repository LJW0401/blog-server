package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// fileLock serialises writes to the same destination path within a process.
// OS-level flock is not necessary here because there is a single blog-server
// process; goroutine-level locking is sufficient and portable.
var (
	fileLocksMu sync.Mutex
	fileLocks   = map[string]*sync.Mutex{}
)

func lockFor(path string) *sync.Mutex {
	fileLocksMu.Lock()
	defer fileLocksMu.Unlock()
	if m, ok := fileLocks[path]; ok {
		return m
	}
	m := &sync.Mutex{}
	fileLocks[path] = m
	return m
}

// AtomicWrite writes data to path using the classic temp-file + rename pattern.
// Guarantees: readers either see the old complete file or the new complete
// file. A crash between Write and Rename leaves the old file intact. Parent
// directories are created with 0o700.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("atomic write: mkdir %s: %w", dir, err)
	}
	m := lockFor(path)
	m.Lock()
	defer m.Unlock()

	tmp, err := os.CreateTemp(dir, ".atomic-*")
	if err != nil {
		return fmt.Errorf("atomic write: create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("atomic write: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("atomic write: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("atomic write: close: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		cleanup()
		return fmt.Errorf("atomic write: chmod: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("atomic write: rename: %w", err)
	}
	return nil
}
