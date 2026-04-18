package content_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/content"
)

// --- Helpers ----------------------------------------------------------------

func newStore(t *testing.T) (*content.Store, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "content", "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "content", "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return content.New(dir, logger), dir
}

func writeDoc(t *testing.T, dir, slug, body string) string {
	t.Helper()
	p := filepath.Join(dir, "content", "docs", slug+".md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const sampleDoc = `---
title: Hello World
slug: hello-world
tags: [a, b]
category: general
created: 2026-04-01
updated: 2026-04-10
status: published
featured: true
---

# Hello

Body of the doc.
`

// --- Smoke: scan & index (WI-2.3) -------------------------------------------

func TestStore_Smoke_ReloadBuildsIndex(t *testing.T) {
	store, dir := newStore(t)
	writeDoc(t, dir, "a", sampleDoc)
	writeDoc(t, dir, "b", `---
title: B
slug: b
updated: 2026-04-02
---
body b
`)
	writeDoc(t, dir, "c", `---
title: C
slug: c
status: draft
updated: 2026-04-05
---
c
`)
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := store.Docs().Len(); got != 3 {
		t.Errorf("len=%d want 3", got)
	}
	a, ok := store.Docs().Get(content.KindDoc, "hello-world")
	if !ok {
		t.Fatal("missing slug hello-world")
	}
	if a.Title != "Hello World" || !a.Featured {
		t.Errorf("got %+v", a)
	}
	// Sort by updated desc: hello-world (04-10), c (04-05), b (04-02).
	list := store.Docs().List(content.KindDoc)
	if len(list) != 3 || list[0].Slug != "hello-world" || list[2].Slug != "b" {
		t.Errorf("sort order: %q %q %q", list[0].Slug, list[1].Slug, list[2].Slug)
	}
}

// Smoke: project kind requires `repo`.
func TestStore_Smoke_ProjectsIndex(t *testing.T) {
	store, dir := newStore(t)
	p := filepath.Join(dir, "content", "projects", "x.md")
	if err := os.WriteFile(p, []byte(`---
slug: x
repo: penguin/x
display_name: X
---
body
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if store.Projects().Len() != 1 {
		t.Errorf("projects len=%d", store.Projects().Len())
	}
	pe, _ := store.Projects().Get(content.KindProject, "x")
	if pe.Repo != "penguin/x" {
		t.Errorf("repo=%q", pe.Repo)
	}
}

// --- Edge: content (WI-2.4) -------------------------------------------------

func TestStore_Edge_InvalidYAMLSkipped(t *testing.T) {
	store, dir := newStore(t)
	writeDoc(t, dir, "ok", sampleDoc)
	writeDoc(t, dir, "broken", "---\ntitle: [broken: yaml]\nslug: broken\n---\n")
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if store.Docs().Len() != 1 {
		t.Errorf("len=%d want 1 (broken should be skipped)", store.Docs().Len())
	}
}

func TestStore_Edge_MissingRequiredFieldsSkipped(t *testing.T) {
	store, dir := newStore(t)
	writeDoc(t, dir, "no-title", "---\nslug: no-title\n---\nbody\n")
	writeDoc(t, dir, "no-slug", "---\ntitle: X\n---\nbody\n")
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if store.Docs().Len() != 0 {
		t.Errorf("len=%d want 0", store.Docs().Len())
	}
}

func TestStore_Edge_InvalidSlug(t *testing.T) {
	store, dir := newStore(t)
	writeDoc(t, dir, "bad-slug", "---\ntitle: X\nslug: Bad Slug!\n---\nbody\n")
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if store.Docs().Len() != 0 {
		t.Error("invalid slug should be skipped")
	}
}

func TestStore_Edge_DuplicateSlugFatal(t *testing.T) {
	store, dir := newStore(t)
	writeDoc(t, dir, "a", sampleDoc)
	// Different file, same slug inside frontmatter.
	dup := filepath.Join(dir, "content", "docs", "a-copy.md")
	if err := os.WriteFile(dup, []byte(`---
title: Hello
slug: hello-world
updated: 2026-04-10
---
body
`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := store.Reload()
	if err == nil {
		t.Fatal("expected duplicate slug error")
	}
	if !errors.Is(err, content.ErrDuplicateSlug) {
		t.Errorf("want ErrDuplicateSlug, got %v", err)
	}
}

func TestStore_Edge_EmptyDirOK(t *testing.T) {
	store, _ := newStore(t)
	if err := store.Reload(); err != nil {
		t.Fatalf("reload empty: %v", err)
	}
	if store.Docs().Len() != 0 {
		t.Error("empty dir should yield zero entries")
	}
}

func TestStore_Edge_NoFrontmatterSkipped(t *testing.T) {
	store, dir := newStore(t)
	writeDoc(t, dir, "raw", "just body no fence\n")
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if store.Docs().Len() != 0 {
		t.Error("frontmatter-less file should be skipped")
	}
}

func TestStore_Edge_UnknownFrontmatterField(t *testing.T) {
	store, dir := newStore(t)
	writeDoc(t, dir, "mystery",
		"---\ntitle: X\nslug: x\nupdated: 2026-04-10\nmystery_field: true\n---\nbody\n")
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if store.Docs().Len() != 0 {
		t.Error("unknown field should fail parse and file should be skipped")
	}
}

// --- Smoke: fsnotify (WI-2.2.5) ---------------------------------------------

func TestStore_Smoke_FsnotifyHotReload(t *testing.T) {
	store, dir := newStore(t)
	writeDoc(t, dir, "a", sampleDoc)
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stop, err := store.Watch(ctx, 60*time.Millisecond)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	t.Cleanup(stop)

	// Add a new file.
	writeDoc(t, dir, "b", "---\ntitle: B\nslug: b\nupdated: 2026-04-09\n---\nb\n")
	if !waitFor(t, 2*time.Second, func() bool { return store.Docs().Len() == 2 }) {
		t.Fatalf("expected len=2 within 2s, got %d", store.Docs().Len())
	}

	// Remove a file.
	if err := os.Remove(filepath.Join(dir, "content", "docs", "a.md")); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return store.Docs().Len() == 1 }) {
		t.Fatalf("expected len=1 after remove, got %d", store.Docs().Len())
	}
}

// Edge: burst of 5 writes is debounced to a single reload (approximately).
// We verify the final state only; debounce-fires count is tested at the loop
// level via a small sleep tolerance.
func TestStore_Edge_FsnotifyBurstDebounced(t *testing.T) {
	store, dir := newStore(t)
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stop, err := store.Watch(ctx, 80*time.Millisecond)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	t.Cleanup(stop)

	path := filepath.Join(dir, "content", "docs", "a.md")
	// Five rapid writes well within the debounce window.
	for i := 0; i < 5; i++ {
		body := sampleDoc + "\niter=" + string(rune('0'+i))
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !waitFor(t, 2*time.Second, func() bool { return store.Docs().Len() == 1 }) {
		t.Fatalf("expected len=1 after burst, got %d", store.Docs().Len())
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
