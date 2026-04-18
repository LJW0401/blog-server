package backup_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/backup"
	"github.com/penguin/blog-server/internal/storage"
)

// setup creates a data dir with sample content/images/data.sqlite and returns
// it plus an opened Store (for WAL checkpointing by RunNow).
func setup(t *testing.T) (*backup.Store, string) {
	t.Helper()
	dir := t.TempDir()
	// content/
	docs := filepath.Join(dir, "content", "docs")
	if err := os.MkdirAll(docs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "a.md"), []byte("---\ntitle: A\nslug: a\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// images/
	imgs := filepath.Join(dir, "images")
	if err := os.MkdirAll(imgs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgs, "pixel.bin"), []byte("pixel"), 0o644); err != nil {
		t.Fatal(err)
	}
	// storage/
	st, err := storage.Open(dir)
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return backup.New(dir, st.DB, logger), dir
}

// --- Smoke (WI-6.7) --------------------------------------------------------

func TestRunNow_Smoke_ProducesArchive(t *testing.T) {
	s, dir := setup(t)
	if err := s.RunNow(context.Background()); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "backups"))
	if len(entries) != 1 {
		t.Fatalf("want 1 backup, got %d", len(entries))
	}
	name := entries[0].Name()
	if !strings.HasSuffix(name, ".tar.gz") {
		t.Errorf("unexpected name: %s", name)
	}
	// Unpack + verify key members exist.
	members := unpackNames(t, filepath.Join(dir, "backups", name))
	need := []string{"content/docs/a.md", "images/pixel.bin"}
	for _, n := range need {
		found := false
		for _, m := range members {
			if m == n {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("archive missing %s; members=%v", n, members)
		}
	}
}

// --- Edge (WI-6.8) --------------------------------------------------------

func TestRunNow_Edge_ReadOnlyBackupDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores chmod")
	}
	s, dir := setup(t)
	// Create + chmod read-only.
	bd := filepath.Join(dir, "backups")
	if err := os.MkdirAll(bd, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bd, 0o700) })
	err := s.RunNow(context.Background())
	if err == nil {
		t.Fatal("expected error on read-only backup dir")
	}
}

func TestRunNow_Edge_RotateKeepsLatest7(t *testing.T) {
	s, dir := setup(t)
	bd := filepath.Join(dir, "backups")
	if err := os.MkdirAll(bd, 0o700); err != nil {
		t.Fatal(err)
	}
	// Fake 8 older backups by writing empty .tar.gz files.
	olds := []string{
		"20260410.tar.gz", "20260411.tar.gz", "20260412.tar.gz",
		"20260413.tar.gz", "20260414.tar.gz", "20260415.tar.gz",
		"20260416.tar.gz", "20260417.tar.gz",
	}
	for _, n := range olds {
		if err := os.WriteFile(filepath.Join(bd, n), []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Today's backup triggers rotation.
	if err := s.RunNow(context.Background()); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	entries, _ := os.ReadDir(bd)
	if len(entries) != 7 {
		t.Errorf("want 7 after rotation, got %d: %v", len(entries), names(entries))
	}
	// Oldest (20260410) should be gone.
	for _, e := range entries {
		if e.Name() == "20260410.tar.gz" {
			t.Error("oldest should be deleted")
		}
	}
}

func TestRunNow_Edge_IdempotentSameDay(t *testing.T) {
	s, dir := setup(t)
	if err := s.RunNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Mutate source then run again — same-day file overwrites, no duplication.
	if err := os.WriteFile(filepath.Join(dir, "content", "docs", "a.md"),
		[]byte("---\ntitle: A2\nslug: a\n---\nnewbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RunNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "backups"))
	if len(entries) != 1 {
		t.Errorf("same day should remain 1 archive, got %d", len(entries))
	}
	// Contents should reflect the newer source.
	members := unpackRead(t, filepath.Join(dir, "backups", entries[0].Name()))
	if body := members["content/docs/a.md"]; !strings.Contains(body, "newbody") {
		t.Errorf("archive not refreshed: %s", body)
	}
}

// --- Helpers ---------------------------------------------------------------

func unpackNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var out []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			out = append(out, hdr.Name)
		}
	}
	return out
}

func unpackRead(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			out[hdr.Name] = string(b)
		}
	}
	return out
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
