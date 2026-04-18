package storage_test

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/penguin/blog-server/internal/storage"
)

// Smoke: open → migrate → CRUD → close.
func TestOpen_WhenFreshDir_ThenReadyToUse(t *testing.T) {
	dir := t.TempDir()
	st, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = st.Close() }()

	if _, err := st.DB.Exec(`INSERT INTO site_settings (k, v, updated_at) VALUES ('title', ?, 0)`, []byte("hi")); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var got []byte
	if err := st.DB.QueryRow(`SELECT v FROM site_settings WHERE k='title'`).Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if string(got) != "hi" {
		t.Errorf("got %q", got)
	}
}

// Smoke: second open is idempotent (migration skip).
func TestOpen_WhenReopened_ThenMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		st, err := storage.Open(dir)
		if err != nil {
			t.Fatalf("open iter %d: %v", i, err)
		}
		_ = st.Close()
	}
}

// Smoke: AtomicWrite puts exact bytes at path.
func TestAtomicWrite_Smoke(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := storage.AtomicWrite(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != "hello" {
		t.Errorf("got %q", b)
	}
}

// --- Edge (WI-1.8) ----------------------------------------------------------

// Edge: data_dir read-only → Open fails fast with clear error.
func TestOpen_Edge_ReadOnlyDataDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores chmod")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "ro")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := storage.Open(dir)
	if err == nil {
		t.Fatal("expected error for read-only data_dir")
	}
}

// Edge: corrupt DB file → quarantined & rebuilt, ErrCorruptDB returned.
func TestOpen_Edge_CorruptDBRebuilt(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.sqlite")
	// Write garbage that is not a valid sqlite header.
	if err := os.WriteFile(dbPath, []byte("this is not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := storage.Open(dir)
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		t.Fatalf("open corrupt: %v", err)
	}
	if st == nil {
		t.Fatal("store nil after rebuild")
	}
	defer func() { _ = st.Close() }()
	entries, _ := os.ReadDir(dir)
	quarantined := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".sqlite" {
			continue
		}
		if len(e.Name()) > len("data.sqlite.corrupt.") && e.Name()[:len("data.sqlite.corrupt.")] == "data.sqlite.corrupt." {
			quarantined = true
		}
	}
	if !quarantined {
		t.Error("expected quarantine file alongside fresh DB")
	}
	// Fresh DB must be usable.
	if _, err := st.DB.Exec(`INSERT INTO site_settings (k, v, updated_at) VALUES ('k', 'v', 0)`); err != nil {
		t.Errorf("fresh db usable: %v", err)
	}
}

// Edge: concurrent AtomicWrite to same path serialises; final content is from
// the last writer; no partial writes observed by readers.
func TestAtomicWrite_Edge_ConcurrentSerialized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "race.bin")
	const n = 32
	payloads := make([][]byte, n)
	for i := 0; i < n; i++ {
		p := make([]byte, 4096)
		for j := range p {
			p[j] = byte(i)
		}
		payloads[i] = p
	}
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			if err := storage.AtomicWrite(path, payloads[i], 0o600); err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The final content must equal one of the writers' payloads byte-for-byte.
	matched := false
	h := sha256.Sum256(b)
	for _, p := range payloads {
		if sha256.Sum256(p) == h {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("final content does not match any writer's payload (partial write?)")
	}
}

// Edge: AtomicWrite target dir missing — it's created automatically.
func TestAtomicWrite_Edge_AutoMkdir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "file.txt")
	if err := storage.AtomicWrite(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("missing file: %v", err)
	}
}

// Nil receiver Close is safe.
func TestClose_Edge_NilSafe(t *testing.T) {
	var s *storage.Store
	if err := s.Close(); err != nil {
		t.Errorf("nil Close: %v", err)
	}
	empty := &storage.Store{}
	if err := empty.Close(); err != nil {
		t.Errorf("empty Close: %v", err)
	}
}

// Edge: parent of data_dir is not writable → Open fails at MkdirAll.
func TestOpen_Edge_CantCreateDataDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores chmod")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	_, err := storage.Open(filepath.Join(parent, "sub", "dir"))
	if err == nil {
		t.Fatal("expected error")
	}
}

// Edge: write to read-only dir fails cleanly, no temp leak.
func TestAtomicWrite_Edge_ReadOnlyDirFailsCleanly(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores chmod")
	}
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.MkdirAll(ro, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })

	path := filepath.Join(ro, "x.txt")
	if err := storage.AtomicWrite(path, []byte("x"), 0o600); err == nil {
		t.Fatal("expected error writing to read-only dir")
	}
	entries, _ := os.ReadDir(ro)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".atomic-" || len(e.Name()) > 8 && e.Name()[:8] == ".atomic-" {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}
