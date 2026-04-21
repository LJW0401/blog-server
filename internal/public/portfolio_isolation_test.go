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

// WI-1.9 / WI-1.10: Portfolio isolation hard assertion.
//
// Insert several portfolio entries (published / draft / slug-clashing with a
// doc / sharing a tag with a doc) and assert that none of their slugs or
// bodies appear in any existing public-facing route (/docs, /projects,
// /rss.xml, /sitemap.xml, home, doc/project detail). This guards against
// the Kind-filter invariant being broken (architecture risk R4).
func TestPublic_HardAssertion_PortfolioDoesNotLeak(t *testing.T) {
	const (
		pubSlug       = "ptfolio-published-marker"
		draftSlug     = "ptfolio-draft-marker"
		clashSlug     = "shared-slug" // also exists as a doc below
		sharedTagSlug = "ptfolio-sharedtag-marker"
		bodyMarker    = "PORTFOLIO_BODY_MARKER_xyz_cafebabe"
	)

	dir := t.TempDir()
	for _, d := range []string{"docs", "projects", "portfolio"} {
		if err := os.MkdirAll(filepath.Join(dir, "content", d), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	// --- seed docs ---
	writeFile(t, filepath.Join(dir, "content", "docs", "shared-slug.md"), `---
title: Doc With Shared Slug
slug: shared-slug
tags: [alpha]
created: 2026-01-01
updated: 2026-01-10
status: published
---
DOC_BODY_FOR_SHARED_SLUG
`)
	writeFile(t, filepath.Join(dir, "content", "docs", "normal-doc.md"), `---
title: Normal Doc
slug: normal-doc
tags: [alpha]
created: 2026-01-02
updated: 2026-01-12
status: published
---
NORMAL_DOC_BODY
`)

	// --- seed portfolios (not expected to leak anywhere except portfolio routes, which don't exist yet in阶段 1) ---
	writeFile(t, filepath.Join(dir, "content", "portfolio", pubSlug+".md"), `---
title: Pub Portfolio
slug: `+pubSlug+`
tags: [alpha]
created: 2026-02-01
updated: 2026-02-10
status: published
featured: true
---
`+bodyMarker+`
`)
	writeFile(t, filepath.Join(dir, "content", "portfolio", draftSlug+".md"), `---
title: Draft Portfolio
slug: `+draftSlug+`
created: 2026-02-02
updated: 2026-02-11
status: draft
---
`+bodyMarker+`
`)
	// Same slug as doc; Kind isolation must make them coexist peacefully.
	writeFile(t, filepath.Join(dir, "content", "portfolio", clashSlug+".md"), `---
title: Portfolio With Shared Slug
slug: `+clashSlug+`
created: 2026-02-03
updated: 2026-02-12
status: published
---
PTFOLIO_BODY_FOR_CLASH_SLUG
`)
	writeFile(t, filepath.Join(dir, "content", "portfolio", sharedTagSlug+".md"), `---
title: Shared Tag Portfolio
slug: `+sharedTagSlug+`
tags: [alpha]
created: 2026-02-04
updated: 2026-02-13
status: published
---
PTFOLIO_BODY_FOR_SHARED_TAG
`)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cstore := content.New(dir, logger)
	if err := cstore.Reload(); err != nil {
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
	h := public.NewHandlers(cstore, tpl, logger)
	h.SettingsDB = settings.New(st.DB)

	// NOTE: the home route is intentionally excluded here — as of WI-2.8 the
	// homepage deliberately shows featured portfolios. The isolation
	// invariant is narrower than "portfolio never appears anywhere": it is
	// "portfolio never leaks into the *legacy* surfaces for docs / projects /
	// feeds / sitemap / docs tag cloud".
	cases := []struct {
		name string
		call func(rr *httptest.ResponseRecorder)
	}{
		{
			name: "docs_list",
			call: func(rr *httptest.ResponseRecorder) {
				h.DocsList(rr, httptest.NewRequest("GET", "/docs", nil))
			},
		},
		{
			name: "docs_list_filter_by_shared_tag",
			call: func(rr *httptest.ResponseRecorder) {
				h.DocsList(rr, httptest.NewRequest("GET", "/docs?view=tag&tag=alpha", nil))
			},
		},
		{
			name: "doc_detail_shared_slug_hits_doc",
			call: func(rr *httptest.ResponseRecorder) {
				h.DocDetail(rr, httptest.NewRequest("GET", "/docs/shared-slug", nil))
			},
		},
		{
			name: "projects_list",
			call: func(rr *httptest.ResponseRecorder) {
				h.ProjectsList(rr, httptest.NewRequest("GET", "/projects", nil))
			},
		},
		{
			name: "rss",
			call: func(rr *httptest.ResponseRecorder) {
				h.RSS(rr, httptest.NewRequest("GET", "/rss.xml", nil))
			},
		},
		{
			name: "sitemap",
			call: func(rr *httptest.ResponseRecorder) {
				h.Sitemap(rr, httptest.NewRequest("GET", "/sitemap.xml", nil))
			},
		},
	}

	forbidden := []string{
		pubSlug, draftSlug, sharedTagSlug,
		"Pub Portfolio", "Draft Portfolio", "Shared Tag Portfolio",
		"PTFOLIO_BODY_FOR_CLASH_SLUG", "PTFOLIO_BODY_FOR_SHARED_TAG",
		bodyMarker,
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			c.call(rr)
			body := rr.Body.String()
			for _, needle := range forbidden {
				if strings.Contains(body, needle) {
					t.Errorf("%s leaked portfolio marker %q; status=%d", c.name, needle, rr.Code)
				}
			}
		})
	}

	// Doc with shared slug must still work — and must return the DOC content,
	// not the portfolio's.
	rr := httptest.NewRecorder()
	h.DocDetail(rr, httptest.NewRequest("GET", "/docs/shared-slug", nil))
	if rr.Code != 200 {
		t.Errorf("doc /docs/shared-slug status=%d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "DOC_BODY_FOR_SHARED_SLUG") {
		t.Errorf("doc /docs/shared-slug missing expected doc body")
	}
}

// writeFile is a small helper local to this test file (avoid conflict with
// other test files' helpers).
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
