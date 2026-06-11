// transfer_test.go verifies the export→import round-trip preserves the data
// subtrees, that the importer rejects foreign archives and path-traversal
// members, and that it refuses a populated target without force.
package transfer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// seedDataDir builds a minimal data_dir with content/, images/ and a fake
// data.sqlite, returning its path.
func seedDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"content/docs/hello.md": "# hello",
		"content/about.md":      "about me",
		"images/avatar/pic.png": "\x89PNG-fake",
		"data.sqlite":           "SQLitefake-db-bytes",
	}
	for rel, body := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRoundTrip(t *testing.T) {
	src := seedDataDir(t)
	var buf bytes.Buffer
	if err := WriteBundle(context.Background(), &buf, src, "2026-06-11T00:00:00Z", "test", nil); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	dst := t.TempDir()
	m, err := ImportBundle(&buf, dst, false)
	if err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if m.Format != FormatMagic || m.Version != FormatVersion {
		t.Fatalf("manifest mismatch: %+v", m)
	}

	// Every seeded file must reappear with identical bytes.
	for rel, want := range map[string]string{
		"content/docs/hello.md": "# hello",
		"content/about.md":      "about me",
		"images/avatar/pic.png": "\x89PNG-fake",
		"data.sqlite":           "SQLitefake-db-bytes",
	} {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
}

func TestImportRefusesPopulatedTarget(t *testing.T) {
	src := seedDataDir(t)
	var buf bytes.Buffer
	if err := WriteBundle(context.Background(), &buf, src, "t", "v", nil); err != nil {
		t.Fatal(err)
	}
	// Target already has content/ → refuse without force.
	dst := seedDataDir(t)
	if _, err := ImportBundle(bytes.NewReader(buf.Bytes()), dst, false); !errors.Is(err, ErrTargetNotEmpty) {
		t.Fatalf("want ErrTargetNotEmpty, got %v", err)
	}
	// With force it proceeds.
	if _, err := ImportBundle(bytes.NewReader(buf.Bytes()), dst, true); err != nil {
		t.Fatalf("force import: %v", err)
	}
}

func TestImportRejectsForeignArchive(t *testing.T) {
	// A gzip-tar with a non-manifest first member.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("nope")
	_ = tw.WriteHeader(&tar.Header{Name: "random.txt", Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()

	if _, err := ImportBundle(&buf, t.TempDir(), false); !errors.Is(err, ErrBadFormat) {
		t.Fatalf("want ErrBadFormat, got %v", err)
	}
}

func TestImportRejectsTraversal(t *testing.T) {
	// Hand-craft a bundle whose manifest is valid but a member escapes.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := writeManifest(tw, Manifest{Format: FormatMagic, Version: FormatVersion, Entries: DefaultEntries}); err != nil {
		t.Fatal(err)
	}
	evil := []byte("pwned")
	_ = tw.WriteHeader(&tar.Header{Name: "content/../../etc/evil", Mode: 0o600, Size: int64(len(evil)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(evil)
	_ = tw.Close()
	_ = gz.Close()

	if _, err := ImportBundle(&buf, t.TempDir(), false); !errors.Is(err, ErrBadFormat) {
		t.Fatalf("want ErrBadFormat for traversal, got %v", err)
	}
}
