package public_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func publishedPortfolioFixture(title, slug string, order int, tags ...string) string {
	tagLine := ""
	if len(tags) > 0 {
		tagLine = "tags: [" + strings.Join(tags, ", ") + "]\n"
	}
	return fmt.Sprintf(`---
title: %s
slug: %s
description: desc
order: %d
%screated: 2026-03-01
updated: 2026-03-01
status: published
---
body
`, title, slug, order, tagLine)
}

// --- WI-2.5 Smoke -----------------------------------------------------------

func TestPortfolioList_Smoke_OnlyPublished(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"p1": publishedPortfolioFixture("P One", "p1", 1),
		"p2": publishedPortfolioFixture("P Two", "p2", 2),
		"p3": publishedPortfolioFixture("P Three", "p3", 3),
		"draft": `---
title: DRAFT HIDDEN
slug: draft
status: draft
created: 2026-01-01
updated: 2026-01-01
---
body
`,
	})
	rr := httptest.NewRecorder()
	h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio", nil))
	body := rr.Body.String()
	for _, want := range []string{"P One", "P Two", "P Three"} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
	if strings.Contains(body, "DRAFT HIDDEN") {
		t.Errorf("draft leaked into anonymous list")
	}
}

func TestPortfolioList_Smoke_TagFilter(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"a": publishedPortfolioFixture("Has Alpha", "a", 1, "alpha"),
		"b": publishedPortfolioFixture("Has Beta", "b", 2, "beta"),
		"c": publishedPortfolioFixture("Has Alpha Too", "c", 3, "alpha", "beta"),
	})
	rr := httptest.NewRecorder()
	h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio?tag=alpha", nil))
	body := rr.Body.String()
	if !strings.Contains(body, "Has Alpha") || !strings.Contains(body, "Has Alpha Too") {
		t.Errorf("alpha filter missing expected entries")
	}
	if strings.Contains(body, "Has Beta") {
		t.Errorf("beta entry leaked into alpha filter")
	}
}

func TestPortfolioList_Smoke_OrderAsc(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"c3": publishedPortfolioFixture("Third", "c3", 3),
		"c1": publishedPortfolioFixture("First", "c1", 1),
		"c2": publishedPortfolioFixture("Second", "c2", 2),
	})
	rr := httptest.NewRecorder()
	h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio", nil))
	body := rr.Body.String()
	i1 := strings.Index(body, "First")
	i2 := strings.Index(body, "Second")
	i3 := strings.Index(body, "Third")
	if !(i1 > 0 && i1 < i2 && i2 < i3) {
		t.Errorf("order wrong: First=%d Second=%d Third=%d", i1, i2, i3)
	}
}

func TestPortfolioList_Smoke_DefaultCover(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"nc": publishedPortfolioFixture("No Cover", "nc", 1),
	})
	rr := httptest.NewRecorder()
	h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio", nil))
	body := rr.Body.String()
	if !strings.Contains(body, "/static/images/portfolio-default.svg") {
		t.Errorf("default cover missing in card")
	}
}

// --- WI-2.6 Exception -------------------------------------------------------

func TestPortfolioList_Exception_InvalidPage(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"a": publishedPortfolioFixture("A", "a", 1),
	})
	for _, p := range []string{"0", "-1", "abc", "99999", ""} {
		rr := httptest.NewRecorder()
		h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio?page="+p, nil))
		if rr.Code != 200 {
			t.Errorf("page=%q status=%d want 200 (normalized)", p, rr.Code)
		}
	}
}

func TestPortfolioList_Exception_EmptyList(t *testing.T) {
	h := setupWithPortfolio(t, nil)
	rr := httptest.NewRecorder()
	h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio", nil))
	if rr.Code != 200 {
		t.Errorf("status=%d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "还没有作品") {
		t.Errorf("empty state message missing")
	}
}

func TestPortfolioList_Exception_TagUnknown(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"a": publishedPortfolioFixture("A", "a", 1, "one"),
	})
	rr := httptest.NewRecorder()
	h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio?tag=nonexistent", nil))
	if rr.Code != 200 {
		t.Errorf("status=%d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "没有") || !strings.Contains(body, "nonexistent") {
		t.Errorf("filtered-empty message missing")
	}
}

func TestPortfolioList_Exception_DraftPreviewHeader(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"draft": `---
title: DraftOne
slug: draft
status: draft
created: 2026-01-01
updated: 2026-01-01
---
body
`,
	})
	// Without preview header: no draft
	rr := httptest.NewRecorder()
	h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio", nil))
	if strings.Contains(rr.Body.String(), "DraftOne") {
		t.Errorf("draft leaked without preview header")
	}
	// With preview header: draft visible
	rr2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/portfolio", nil)
	req.Header.Set("X-Preview-Admin", "1")
	h.PortfolioList(rr2, req)
	if !strings.Contains(rr2.Body.String(), "DraftOne") {
		t.Errorf("draft invisible even with preview header")
	}
}

func TestPortfolioList_Exception_PaginationBoundary(t *testing.T) {
	// 21 items → exactly 2 pages
	fixtures := map[string]string{}
	for i := 1; i <= 21; i++ {
		slug := fmt.Sprintf("s%02d", i)
		fixtures[slug] = publishedPortfolioFixture(fmt.Sprintf("Item%02d", i), slug, i)
	}
	h := setupWithPortfolio(t, fixtures)
	// Page 2 should have exactly 1 item.
	rr := httptest.NewRecorder()
	h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio?page=2", nil))
	body := rr.Body.String()
	// Item01..Item20 are on page 1; Item21 on page 2.
	if !strings.Contains(body, "Item21") {
		t.Errorf("page 2 missing Item21")
	}
	if strings.Contains(body, "Item01") {
		t.Errorf("page 2 leaked Item01")
	}
	if !strings.Contains(body, "第 2 / 2 页") {
		t.Errorf("pager label wrong")
	}
}
