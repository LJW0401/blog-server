package admin_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/admin"
)

// --- WI-3.2 Smoke: portfolio in trash list + restore -----------------------

func TestTrash_Smoke_ListIncludesPortfolio(t *testing.T) {
	th, b := setupTrash(t)
	seedTrash(t, b, admin.TrashKindDoc, "20260101-120000-adoc.md", docMD("adoc"))
	seedTrash(t, b, admin.TrashKindPortfolio, "20260102-120000-apiece.md",
		"---\ntitle: apiece\nslug: apiece\nstatus: published\ncreated: 2026-04-19\nupdated: 2026-04-19\n---\nbody\n")
	w := b.authedGet(t, "/manage/trash", th.TrashList)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	out := w.Body.String()
	if !strings.Contains(out, "adoc") || !strings.Contains(out, "apiece") {
		t.Errorf("list missing one or both entries: %s", out)
	}
}

func TestTrash_Smoke_RestorePortfolio(t *testing.T) {
	th, b := setupTrash(t)
	src := seedTrash(t, b, admin.TrashKindPortfolio, "20260101-120000-apiece.md",
		"---\ntitle: apiece\nslug: apiece\nstatus: published\ncreated: 2026-04-19\nupdated: 2026-04-19\n---\nbody\n")
	w := b.authedPost(t, "/manage/trash/restore",
		url.Values{"csrf": {b.CSRF}, "filename": {"portfolio/20260101-120000-apiece.md"}},
		th.Restore)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("trash file should be gone: %v", err)
	}
	restored := filepath.Join(b.DataDir, "content", "portfolio", "apiece.md")
	if _, err := os.Stat(restored); err != nil {
		t.Errorf("restored portfolio missing: %v", err)
	}
}

// --- WI-3.3 Exception: migration --------------------------------------------

// discardLogger suppresses migration log noise in tests that don't assert on it.
func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// captureLogger returns a logger + a pointer to the buffer it writes to.
func captureLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

func TestMigrateFlatTrash_MovesLegacyFilesToKindSubdirs(t *testing.T) {
	_, b := setupTrash(t)
	// Seed legacy flat files: a doc (no proj-prefix) and a project (proj-).
	seedTrashLegacyFlat(t, b, "20260101-120000-adoc.md", "doc body")
	seedTrashLegacyFlat(t, b, "20260102-120000-proj-aproj.md", "proj body")
	// Also a non-matching file that should be left alone.
	seedTrashLegacyFlat(t, b, "readme.txt", "keep me")
	// And a file that matches the naming convention but has dots in slug.
	seedTrashLegacyFlat(t, b, "20260103-120000-has.dot.md", "dotted")

	if err := admin.MigrateFlatTrash(b.DataDir, discardLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Doc should be under trash/docs/
	mustExist(t, filepath.Join(b.DataDir, "trash", "docs", "20260101-120000-adoc.md"))
	// Project should be under trash/projects/ WITHOUT the proj- prefix
	mustExist(t, filepath.Join(b.DataDir, "trash", "projects", "20260102-120000-aproj.md"))
	// Non-matching file untouched at root
	mustExist(t, filepath.Join(b.DataDir, "trash", "readme.txt"))
	// Dotted-slug doc migrated to docs/
	mustExist(t, filepath.Join(b.DataDir, "trash", "docs", "20260103-120000-has.dot.md"))
	// The originals at root must be gone for the migrated ones.
	mustNotExist(t, filepath.Join(b.DataDir, "trash", "20260101-120000-adoc.md"))
	mustNotExist(t, filepath.Join(b.DataDir, "trash", "20260102-120000-proj-aproj.md"))
}

func TestMigrateFlatTrash_Idempotent(t *testing.T) {
	_, b := setupTrash(t)
	seedTrashLegacyFlat(t, b, "20260101-120000-adoc.md", "doc body")
	logger, buf1 := captureLogger()
	if err := admin.MigrateFlatTrash(b.DataDir, logger); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf1.String(), "admin.trash.migrate.moved") {
		t.Errorf("first run should log a move")
	}
	// Second run: nothing left to move.
	logger2, buf2 := captureLogger()
	if err := admin.MigrateFlatTrash(b.DataDir, logger2); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf2.String(), "admin.trash.migrate.moved") {
		t.Errorf("second run should be a no-op; got log:\n%s", buf2.String())
	}
	// File still in new location.
	mustExist(t, filepath.Join(b.DataDir, "trash", "docs", "20260101-120000-adoc.md"))
}

func TestMigrateFlatTrash_EmptyOrMissingTrashDir(t *testing.T) {
	_, b := setupTrash(t)
	// trash dir doesn't exist at all — must not error.
	if err := admin.MigrateFlatTrash(b.DataDir, discardLogger()); err != nil {
		t.Errorf("missing trash dir should be tolerated: %v", err)
	}
	// Create empty trash/
	_ = os.MkdirAll(filepath.Join(b.DataDir, "trash"), 0o700)
	if err := admin.MigrateFlatTrash(b.DataDir, discardLogger()); err != nil {
		t.Errorf("empty trash dir should be tolerated: %v", err)
	}
}

func TestMigrateFlatTrash_CollisionKeepsSource(t *testing.T) {
	_, b := setupTrash(t)
	// Pre-populate a colliding target in new location.
	_ = os.MkdirAll(filepath.Join(b.DataDir, "trash", "docs"), 0o700)
	target := filepath.Join(b.DataDir, "trash", "docs", "20260101-120000-dupe.md")
	_ = os.WriteFile(target, []byte("existing"), 0o600)
	// Legacy flat with same name.
	src := seedTrashLegacyFlat(t, b, "20260101-120000-dupe.md", "incoming")

	logger, buf := captureLogger()
	if err := admin.MigrateFlatTrash(b.DataDir, logger); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "admin.trash.migrate.dst_exists") {
		t.Errorf("expected dst_exists log; got:\n%s", buf.String())
	}
	// Source still present (skipped, not lost)
	mustExist(t, src)
	// Target kept its original content (not overwritten)
	data, _ := os.ReadFile(target)
	if string(data) != "existing" {
		t.Errorf("target overwritten: %q", data)
	}
}

func TestTrash_Exception_TokenPathTraversalRejected(t *testing.T) {
	th, b := setupTrash(t)
	// Build a real file in trash/docs/ so there's a plausible target for attacks.
	seedTrash(t, b, admin.TrashKindDoc, "20260101-120000-alpha.md", docMD("alpha"))
	// Target a file outside trash we don't want touched.
	outside := filepath.Join(b.DataDir, "content", "docs", "victim.md")
	_ = os.MkdirAll(filepath.Dir(outside), 0o700)
	_ = os.WriteFile(outside, []byte("secret"), 0o600)

	for _, bad := range []string{
		"../20260101-120000-alpha.md",
		"docs/../docs/20260101-120000-alpha.md",
		"docs/../../content/docs/victim.md",
		"/etc/passwd",
		"unknown/20260101-120000-alpha.md", // kind not in whitelist
		"docs/not-a-trash-name",
		"docs/",
		"",
	} {
		w := b.authedPost(t, "/manage/trash/purge",
			url.Values{"csrf": {b.CSRF}, "filename": {bad}}, th.Purge)
		if w.Code != http.StatusBadRequest {
			t.Errorf("token=%q should 400, got %d", bad, w.Code)
		}
	}
	// Victim untouched
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("victim gone after traversal attempts: %v", err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file %s to exist: %v", path, err)
	}
}
func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file %s to be gone, got err=%v", path, err)
	}
}
