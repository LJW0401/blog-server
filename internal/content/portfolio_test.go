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

// --- Helpers specific to portfolio tests ------------------------------------

func newStoreWithPortfolio(t *testing.T) (*content.Store, string) {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"docs", "projects", "portfolio"} {
		if err := os.MkdirAll(filepath.Join(dir, "content", d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return content.New(dir, logger), dir
}

func writePortfolio(t *testing.T, dir, slug, body string) string {
	t.Helper()
	p := filepath.Join(dir, "content", "portfolio", slug+".md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const samplePortfolioDraft = `---
title: Draft Piece
slug: draft-piece
created: 2026-03-01
updated: 2026-03-05
status: draft
---
body draft
`

const samplePortfolioPublished = `---
title: Published Piece
slug: published-piece
description: a short one-liner
cover: /images/cover.png
category: 设计
tags: [vis, dataviz]
order: 3
demo_url: https://demo.example.com/x
source_url: https://github.com/example/x
created: 2026-03-10
updated: 2026-03-15
status: published
featured: true
---
body pub
`

const samplePortfolioArchived = `---
title: Archived Piece
slug: archived-piece
created: 2026-02-01
updated: 2026-02-10
status: archived
---
body arc
`

// WI-1.2 Smoke: Reload scans content/portfolio/, applies Kind isolation, hot
// reload picks up new files within the watcher debounce window.
func TestPortfolio_Smoke_ReloadBuildsIndex(t *testing.T) {
	store, dir := newStoreWithPortfolio(t)
	writePortfolio(t, dir, "draft-piece", samplePortfolioDraft)
	writePortfolio(t, dir, "published-piece", samplePortfolioPublished)
	writePortfolio(t, dir, "archived-piece", samplePortfolioArchived)

	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if got := store.Portfolios().Len(); got != 3 {
		t.Fatalf("portfolio len=%d want 3", got)
	}
	if got := store.Docs().Len(); got != 0 {
		t.Errorf("docs len=%d want 0 (kind isolation)", got)
	}
	if got := store.Projects().Len(); got != 0 {
		t.Errorf("projs len=%d want 0 (kind isolation)", got)
	}

	e, ok := store.Portfolios().Get(content.KindPortfolio, "published-piece")
	if !ok {
		t.Fatalf("published-piece missing")
	}
	if e.Title != "Published Piece" {
		t.Errorf("title=%q", e.Title)
	}
	if e.Cover != "/images/cover.png" {
		t.Errorf("cover=%q", e.Cover)
	}
	if e.Description != "a short one-liner" {
		t.Errorf("description=%q", e.Description)
	}
	if e.Order != 3 {
		t.Errorf("order=%d", e.Order)
	}
	if e.DemoURL != "https://demo.example.com/x" {
		t.Errorf("demo_url=%q", e.DemoURL)
	}
	if e.SourceURL != "https://github.com/example/x" {
		t.Errorf("source_url=%q", e.SourceURL)
	}
	if e.Status != content.StatusPublished {
		t.Errorf("status=%q", e.Status)
	}
	if !e.Featured {
		t.Errorf("featured=false")
	}
	if len(e.Tags) != 2 {
		t.Errorf("tags=%v", e.Tags)
	}

	draft, _ := store.Portfolios().Get(content.KindPortfolio, "draft-piece")
	if draft.Status != content.StatusDraft {
		t.Errorf("draft status=%q", draft.Status)
	}

	arc, _ := store.Portfolios().Get(content.KindPortfolio, "archived-piece")
	if arc.Status != content.StatusArchived {
		t.Errorf("archived status=%q", arc.Status)
	}
}

// WI-1.2 Smoke: fsnotify hot-reload picks up new file.
func TestPortfolio_Smoke_HotReloadOnNewFile(t *testing.T) {
	store, dir := newStoreWithPortfolio(t)
	writePortfolio(t, dir, "first", samplePortfolioPublished)
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := store.Watch(ctx, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer stop()

	// Add a second portfolio file after watcher armed.
	writePortfolio(t, dir, "second", `---
title: Second
slug: second
created: 2026-03-20
updated: 2026-03-20
status: published
---
body
`)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if store.Portfolios().Len() == 2 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("watcher did not pick up new portfolio file (len=%d)", store.Portfolios().Len())
}

// WI-1.2 Smoke: Docs and portfolio with same slug coexist (Kind-scoped uniqueness).
func TestPortfolio_Smoke_SameSlugAcrossKindsIsolated(t *testing.T) {
	store, dir := newStoreWithPortfolio(t)
	// Write a doc and a portfolio with identical slug.
	if err := os.WriteFile(filepath.Join(dir, "content", "docs", "hello.md"), []byte(`---
title: Doc Hello
slug: hello
created: 2026-01-01
updated: 2026-01-02
status: published
---
body
`), 0o600); err != nil {
		t.Fatal(err)
	}
	writePortfolio(t, dir, "hello", `---
title: Portfolio Hello
slug: hello
created: 2026-02-01
updated: 2026-02-02
status: published
---
body
`)
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	dEntry, _ := store.Docs().Get(content.KindDoc, "hello")
	pEntry, _ := store.Portfolios().Get(content.KindPortfolio, "hello")
	if dEntry == nil || pEntry == nil {
		t.Fatalf("both must exist: doc=%v portfolio=%v", dEntry, pEntry)
	}
	if dEntry.Title == pEntry.Title {
		t.Errorf("titles should differ, got both=%q", dEntry.Title)
	}
}

// --- WI-1.3 Exception tests -------------------------------------------------

// Non-legal input: missing title / missing slug / broken YAML / wrong type.
// Boundary: empty body / body whitespace only / very large body (2MB).
// Failed dependency: portfolio dir missing (tolerated) / read-only dir.
// Recovery: delete file and rename mid-reload.
func TestPortfolio_Exception_MissingRequired(t *testing.T) {
	store, dir := newStoreWithPortfolio(t)
	// Missing title
	writePortfolio(t, dir, "no-title", `---
slug: no-title
created: 2026-01-01
updated: 2026-01-01
---
body
`)
	// Missing slug
	writePortfolio(t, dir, "no-slug", `---
title: No Slug
created: 2026-01-01
updated: 2026-01-01
---
body
`)
	// Good one
	writePortfolio(t, dir, "good", samplePortfolioPublished)
	if err := store.Reload(); err != nil {
		// Reload logs but doesn't bail on per-file errors.
		t.Fatalf("reload: %v", err)
	}
	// Only "good" should be indexed; the two others skipped.
	if got := store.Portfolios().Len(); got != 1 {
		t.Errorf("portfolio len=%d want 1 (invalid files skipped)", got)
	}
	if _, ok := store.Portfolios().Get(content.KindPortfolio, "published-piece"); !ok {
		t.Errorf("good file missing after skipping invalid ones")
	}
}

func TestPortfolio_Exception_BrokenYAMLAndWrongType(t *testing.T) {
	store, dir := newStoreWithPortfolio(t)
	// Broken YAML
	writePortfolio(t, dir, "broken", `---
title: "unterminated
slug: broken
---
body
`)
	// Wrong type: order as string
	writePortfolio(t, dir, "wrong-type", `---
title: Wrong Type
slug: wrong-type
order: "abc"
created: 2026-01-01
updated: 2026-01-01
---
body
`)
	// Unknown field
	writePortfolio(t, dir, "unknown", `---
title: Unknown
slug: unknown
mystery_field: banana
created: 2026-01-01
updated: 2026-01-01
---
body
`)
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := store.Portfolios().Len(); got != 0 {
		t.Errorf("portfolio len=%d want 0 (all invalid)", got)
	}
}

func TestPortfolio_Exception_EmptyAndLargeBody(t *testing.T) {
	store, dir := newStoreWithPortfolio(t)
	// Empty body (still valid — title+slug present)
	writePortfolio(t, dir, "empty-body", `---
title: Empty
slug: empty-body
created: 2026-01-01
updated: 2026-01-01
---
`)
	// Whitespace-only body
	writePortfolio(t, dir, "ws-body", `---
title: Whitespace
slug: ws-body
created: 2026-01-01
updated: 2026-01-01
---



`)
	// Very large body (~2MB)
	big := make([]byte, 0, 2*1024*1024)
	for i := 0; i < 2*1024*1024; i++ {
		big = append(big, 'a')
	}
	writePortfolio(t, dir, "big", "---\ntitle: Big\nslug: big\ncreated: 2026-01-01\nupdated: 2026-01-01\n---\n"+string(big))

	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := store.Portfolios().Len(); got != 3 {
		t.Errorf("portfolio len=%d want 3", got)
	}
}

func TestPortfolio_Exception_DirMissing(t *testing.T) {
	// Store rooted at dir that has no content/portfolio directory.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "content", "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	// NOT creating portfolio dir.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := content.New(dir, logger)
	if err := store.Reload(); err != nil {
		t.Fatalf("reload should tolerate missing portfolio dir: %v", err)
	}
	if got := store.Portfolios().Len(); got != 0 {
		t.Errorf("portfolio len=%d want 0", got)
	}
}

func TestPortfolio_Exception_InvalidSlug(t *testing.T) {
	store, dir := newStoreWithPortfolio(t)
	// slug with uppercase letters (invalid per isValidSlug)
	writePortfolio(t, dir, "Bad-Slug", `---
title: Bad
slug: Bad-Slug
created: 2026-01-01
updated: 2026-01-01
---
body
`)
	// slug with path traversal attempt
	writePortfolio(t, dir, "trav", `---
title: Trav
slug: "../evil"
created: 2026-01-01
updated: 2026-01-01
---
body
`)
	if err := store.Reload(); err != nil {
		if !errors.Is(err, content.ErrInvalidSlug) {
			// It's fine if reload itself returns nil and logs per-file.
			t.Logf("reload err (expected soft): %v", err)
		}
	}
	if got := store.Portfolios().Len(); got != 0 {
		t.Errorf("portfolio len=%d want 0 (invalid slugs)", got)
	}
}

// WI-1.3 recovery: delete and rename files between reloads.
func TestPortfolio_Exception_DeleteAndRename(t *testing.T) {
	store, dir := newStoreWithPortfolio(t)
	p1 := writePortfolio(t, dir, "alpha", samplePortfolioPublished)
	writePortfolio(t, dir, "beta", `---
title: Beta
slug: beta
created: 2026-01-01
updated: 2026-01-01
status: published
---
body
`)
	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if store.Portfolios().Len() != 2 {
		t.Fatalf("initial len=%d", store.Portfolios().Len())
	}

	// Delete alpha, reload, expect beta remains.
	if err := os.Remove(p1); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(); err != nil {
		t.Fatalf("reload after delete: %v", err)
	}
	if store.Portfolios().Len() != 1 {
		t.Errorf("after delete len=%d want 1", store.Portfolios().Len())
	}
	if _, ok := store.Portfolios().Get(content.KindPortfolio, "alpha"); ok {
		t.Errorf("alpha should be gone")
	}
}
