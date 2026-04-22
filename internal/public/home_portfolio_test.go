package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

const featuredPortfolio = `---
title: Showcase Piece
slug: showcase
description: 一句话展示
cover: /images/showcase.png
order: 1
created: 2026-03-01
updated: 2026-03-15
status: published
featured: true
---
<!-- portfolio:intro -->
这是主页卡片下半的长简介段落。支持 **Markdown**。
<!-- /portfolio:intro -->

# 详情标题
`

const featuredNoCover = `---
title: No Cover Piece
slug: no-cover
order: 2
created: 2026-03-02
updated: 2026-03-16
status: published
featured: true
---
body no intro
`

const nonFeaturedPortfolio = `---
title: Hidden From Home
slug: hidden
created: 2026-03-03
updated: 2026-03-17
status: published
featured: false
---
body
`

// --- WI-2.9 Smoke -----------------------------------------------------------

func TestHome_Smoke_PortfolioBlockRenders(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"showcase": featuredPortfolio,
		"no-cover": featuredNoCover,
	})
	rr := httptest.NewRecorder()
	h.Home(rr, httptest.NewRequest("GET", "/", nil))
	body := rr.Body.String()
	if !strings.Contains(body, "作品集") {
		t.Errorf("portfolio section heading missing")
	}
	if !strings.Contains(body, "Showcase Piece") {
		t.Errorf("featured card missing")
	}
	if !strings.Contains(body, `href="/portfolio"`) {
		t.Errorf("'view all' link missing")
	}
	// Intro rendered as markdown → <p>…<strong>Markdown</strong></p>
	if !strings.Contains(body, "<strong>Markdown</strong>") {
		t.Errorf("intro markdown not rendered")
	}
	// Intro marker must NOT leak as source text
	if strings.Contains(body, "portfolio:intro") {
		t.Errorf("intro source marker leaked to home")
	}
	// Default cover applied for no-cover piece
	if !strings.Contains(body, "/static/images/portfolio-default.svg") {
		t.Errorf("default cover missing for no-cover card")
	}
}

func TestHome_Smoke_PortfolioOrderAsc(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"c-third":  strings.Replace(featuredPortfolio, "slug: showcase", "slug: c-third", 1),
		"a-first":  strings.Replace(strings.Replace(featuredPortfolio, "slug: showcase", "slug: a-first", 1), "order: 1", "order: 1", 1),
		"b-second": strings.Replace(strings.Replace(featuredPortfolio, "slug: showcase", "slug: b-second", 1), "order: 1", "order: 5", 1),
	})
	// Override titles to something identifiable
	// (simpler: overwrite files fresh)
	h2 := setupWithPortfolio(t, map[string]string{
		"a": `---
title: Alpha
slug: a
order: 1
status: published
featured: true
created: 2026-01-01
updated: 2026-01-10
---
body
`,
		"b": `---
title: Beta
slug: b
order: 5
status: published
featured: true
created: 2026-01-01
updated: 2026-01-20
---
body
`,
		"c": `---
title: Gamma
slug: c
order: 10
status: published
featured: true
created: 2026-01-01
updated: 2026-01-30
---
body
`,
	})
	rr := httptest.NewRecorder()
	h2.Home(rr, httptest.NewRequest("GET", "/", nil))
	body := rr.Body.String()
	i1 := strings.Index(body, "Alpha")
	i2 := strings.Index(body, "Beta")
	i3 := strings.Index(body, "Gamma")
	if !(i1 > 0 && i1 < i2 && i2 < i3) {
		t.Errorf("order wrong: Alpha=%d Beta=%d Gamma=%d", i1, i2, i3)
	}
	_ = h
}

// --- WI-2.10 Exception ------------------------------------------------------

func TestHome_Exception_NoFeaturedHidesBlock(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"hidden": nonFeaturedPortfolio,
	})
	rr := httptest.NewRecorder()
	h.Home(rr, httptest.NewRequest("GET", "/", nil))
	body := rr.Body.String()
	// Section wrapper id="portfolio" must not render when empty
	if strings.Contains(body, `id="portfolio"`) {
		t.Errorf("empty portfolio section should not render")
	}
	// Also the heading text must not appear
	if strings.Contains(body, "作品集</h2>") {
		t.Errorf("portfolio heading should not render")
	}
	// "Hidden From Home" must not leak (not featured)
	if strings.Contains(body, "Hidden From Home") {
		t.Errorf("non-featured entry appeared on home")
	}
}

func TestHome_Exception_EmptyIntroHidesBottom(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"nc": featuredNoCover, // no <!-- portfolio:intro --> block
	})
	rr := httptest.NewRecorder()
	h.Home(rr, httptest.NewRequest("GET", "/", nil))
	body := rr.Body.String()
	if !strings.Contains(body, "No Cover Piece") {
		t.Errorf("card should still render (only intro is missing)")
	}
	// portfolio-home-card-intro container must be absent for entries with no intro
	if strings.Contains(body, "portfolio-home-card-intro") {
		t.Errorf("empty intro should not produce the bottom container")
	}
}

func TestHome_Exception_DraftNeverOnHome(t *testing.T) {
	// Even with preview header, draft entries are not "featured published"
	// and should not appear on home.
	h := setupWithPortfolio(t, map[string]string{
		"drafty": `---
title: Drafty
slug: drafty
status: draft
featured: true
created: 2026-01-01
updated: 2026-01-01
---
body
`,
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Preview-Admin", "1")
	h.Home(rr, req)
	if strings.Contains(rr.Body.String(), "Drafty") {
		t.Errorf("draft entry appeared on home")
	}
}
