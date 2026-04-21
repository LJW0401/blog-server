package public_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/assets"
	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/public"
	"github.com/penguin/blog-server/internal/render"
	"github.com/penguin/blog-server/internal/settings"
	"github.com/penguin/blog-server/internal/storage"
)

// setupWithPortfolio builds a Handlers instance seeded with the provided
// portfolio files (keys are slugs, values are full md file bodies).
func setupWithPortfolio(t *testing.T, portfolios map[string]string) *public.Handlers {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"docs", "projects", "portfolio"} {
		if err := os.MkdirAll(filepath.Join(dir, "content", d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for slug, body := range portfolios {
		if err := os.WriteFile(filepath.Join(dir, "content", "portfolio", slug+".md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store := content.New(dir, logger)
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}
	tpl, err := render.NewTemplates(assets.Templates(), render.NewMarkdown())
	if err != nil {
		t.Fatal(err)
	}
	st, err := storage.Open(dir)
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := public.NewHandlers(store, tpl, logger)
	h.SettingsDB = settings.New(st.DB)
	return h
}

const fixturePublishedPortfolio = `---
title: 可视化 Demo
slug: viz-demo
description: 一段 80 字以内的介绍
cover: /images/viz-demo.png
category: 可视化
tags: [data, d3]
order: 2
demo_url: https://demo.example.com/viz
source_url: https://github.com/example/viz
created: 2026-03-01
updated: 2026-03-15
status: published
featured: true
---
<!-- portfolio:intro -->
这是主页卡片显示的长简介，可以跨多段描述项目。
<!-- /portfolio:intro -->

# 项目背景

这里是详情页的正文内容。
`

const fixtureDraftPortfolio = `---
title: Draft Work
slug: draft-work
created: 2026-03-10
updated: 2026-03-11
status: draft
---
draft body
`

const fixtureNoCoverPortfolio = `---
title: No Cover
slug: no-cover
created: 2026-02-01
updated: 2026-02-05
status: published
---
body
`

// --- WI-2.2 Smoke -----------------------------------------------------------

func TestPortfolioDetail_Smoke_PublishedRenders(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{"viz-demo": fixturePublishedPortfolio})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/portfolio/viz-demo", nil)
	h.PortfolioDetail(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "可视化 Demo") {
		t.Errorf("title missing")
	}
	if !strings.Contains(body, "/images/viz-demo.png") {
		t.Errorf("cover missing")
	}
	if !strings.Contains(body, "https://demo.example.com/viz") {
		t.Errorf("demo_url missing")
	}
	if !strings.Contains(body, "https://github.com/example/viz") {
		t.Errorf("source_url missing")
	}
	if !strings.Contains(body, "# 项目背景") == false {
		// markdown renders the h1 into <h1> tag
	}
	if !strings.Contains(body, "项目背景") {
		t.Errorf("body content missing")
	}
	// Intro comment block must be stripped from both Body source and rendered HTML
	if strings.Contains(body, "portfolio:intro") {
		t.Errorf("intro marker leaked into rendered body")
	}
	if !strings.Contains(body, `property="og:title"`) {
		t.Errorf("og:title meta missing")
	}
	if !strings.Contains(body, `property="og:image"`) {
		t.Errorf("og:image meta missing")
	}
}

func TestPortfolioDetail_Smoke_DefaultCoverWhenEmpty(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{"no-cover": fixtureNoCoverPortfolio})
	rr := httptest.NewRecorder()
	h.PortfolioDetail(rr, httptest.NewRequest("GET", "/portfolio/no-cover", nil))
	body := rr.Body.String()
	if !strings.Contains(body, public.PortfolioDefaultCover) {
		t.Errorf("default cover path %q not rendered", public.PortfolioDefaultCover)
	}
}

func TestPortfolioDetail_Smoke_SharedSlugKindIsolation(t *testing.T) {
	// Doc and portfolio with same slug — detail handler must hit portfolio.
	dir := t.TempDir()
	for _, d := range []string{"docs", "portfolio", "projects"} {
		if err := os.MkdirAll(filepath.Join(dir, "content", d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "content", "docs", "shared.md"),
		[]byte("---\ntitle: Doc Shared\nslug: shared\n---\nDOC BODY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "content", "portfolio", "shared.md"),
		[]byte("---\ntitle: Portfolio Shared\nslug: shared\nstatus: published\ncreated: 2026-01-01\nupdated: 2026-01-01\n---\nPTFOLIO_BODY_MARKER\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := content.New(dir, logger)
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}
	tpl, _ := render.NewTemplates(assets.Templates(), render.NewMarkdown())
	st, _ := storage.Open(dir)
	t.Cleanup(func() { _ = st.Close() })
	h := public.NewHandlers(store, tpl, logger)
	h.SettingsDB = settings.New(st.DB)

	rr := httptest.NewRecorder()
	h.PortfolioDetail(rr, httptest.NewRequest("GET", "/portfolio/shared", nil))
	body := rr.Body.String()
	if !strings.Contains(body, "Portfolio Shared") {
		t.Errorf("portfolio title missing")
	}
	if strings.Contains(body, "DOC BODY") {
		t.Errorf("doc content leaked into portfolio detail")
	}
}

// --- WI-2.3 Exception -------------------------------------------------------

func TestPortfolioDetail_Exception_NotFound(t *testing.T) {
	h := setupWithPortfolio(t, nil)
	rr := httptest.NewRecorder()
	h.PortfolioDetail(rr, httptest.NewRequest("GET", "/portfolio/does-not-exist", nil))
	if rr.Code != 404 {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestPortfolioDetail_Exception_DraftHidden(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{"draft-work": fixtureDraftPortfolio})
	rr := httptest.NewRecorder()
	h.PortfolioDetail(rr, httptest.NewRequest("GET", "/portfolio/draft-work", nil))
	if rr.Code != 404 {
		t.Errorf("anonymous draft should be 404, got %d", rr.Code)
	}

	// With preview header, draft becomes visible.
	rr2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/portfolio/draft-work", nil)
	req.Header.Set("X-Preview-Admin", "1")
	h.PortfolioDetail(rr2, req)
	if rr2.Code != 200 {
		t.Errorf("preview status=%d", rr2.Code)
	}
}

func TestPortfolioDetail_Exception_InvalidSlug(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{"viz-demo": fixturePublishedPortfolio})
	cases := []string{
		"/portfolio/",
		"/portfolio//double-slash",
		"/portfolio/" + strings.Repeat("a", 65),
		"/portfolio/..",
		"/portfolio/with%20spaces",      // URL-encoded space
		"/portfolio/%E4%B8%AD%E6%96%87", // URL-encoded 中文
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.PortfolioDetail(rr, httptest.NewRequest("GET", p, nil))
			if rr.Code != 404 {
				t.Errorf("%s status=%d want 404", p, rr.Code)
			}
		})
	}
}

func TestPortfolioDetail_Exception_JavascriptCoverNeutralized(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{"evil": `---
title: Evil
slug: evil
cover: "javascript:alert(1)"
created: 2026-01-01
updated: 2026-01-01
status: published
---
body
`})
	rr := httptest.NewRecorder()
	h.PortfolioDetail(rr, httptest.NewRequest("GET", "/portfolio/evil", nil))
	body := rr.Body.String()
	// html/template must not emit the raw javascript: URI as a src attribute
	if strings.Contains(body, `src="javascript:alert(1)"`) {
		t.Errorf("javascript: URL passed through to src")
	}
}

func TestPortfolioDetail_Exception_BoundarySlugOneAnd64Chars(t *testing.T) {
	short := `---
title: X
slug: a
created: 2026-01-01
updated: 2026-01-01
status: published
---
body
`
	long := `---
title: Y
slug: ` + strings.Repeat("a", 64) + `
created: 2026-01-01
updated: 2026-01-01
status: published
---
body
`
	h := setupWithPortfolio(t, map[string]string{"a": short, strings.Repeat("a", 64): long})
	for _, slug := range []string{"a", strings.Repeat("a", 64)} {
		rr := httptest.NewRecorder()
		h.PortfolioDetail(rr, httptest.NewRequest("GET", "/portfolio/"+slug, nil))
		if rr.Code != 200 {
			t.Errorf("slug=%q status=%d", slug, rr.Code)
		}
	}
}
