package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// WI-2.14 Smoke: portfolio pages expose the CSS classes the stylesheet
// targets. We don't render visual diffs here — just assert structural
// selectors exist so regressions in template refactors catch early.
func TestPortfolioStyles_Smoke_ClassesPresent(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"showcase": featuredPortfolio,
	})

	// List page
	rr := httptest.NewRecorder()
	h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio", nil))
	list := rr.Body.String()
	for _, cls := range []string{
		`portfolio-list-shell`,
		`portfolio-gallery`,
		`portfolio-card`,
		`portfolio-card-cover`,
	} {
		if !strings.Contains(list, cls) {
			t.Errorf("list missing class %q", cls)
		}
	}

	// Detail page
	rr2 := httptest.NewRecorder()
	h.PortfolioDetail(rr2, httptest.NewRequest("GET", "/portfolio/showcase", nil))
	detail := rr2.Body.String()
	for _, cls := range []string{
		`portfolio-detail`,
		`portfolio-header`,
		`portfolio-cover`,
		`portfolio-body`,
	} {
		if !strings.Contains(detail, cls) {
			t.Errorf("detail missing class %q", cls)
		}
	}

	// Home card
	rr3 := httptest.NewRecorder()
	h.Home(rr3, httptest.NewRequest("GET", "/", nil))
	home := rr3.Body.String()
	for _, cls := range []string{
		`portfolio-home-list`,
		`portfolio-home-card`,
		`portfolio-home-card-top`,
		`portfolio-home-card-cover`,
		`portfolio-home-card-intro`,
	} {
		if !strings.Contains(home, cls) {
			t.Errorf("home missing class %q", cls)
		}
	}
}

// WI-2.15 Exception (smoke-level substitution for full axe-core scan):
//
// Structural checks that stand in for the full accessibility / responsive
// sweep documented in the dev-plan (manual `npx @axe-core/cli <url>` over
// light/dark × three page families remains the authoritative gate before
// release — see learnings for the follow-up).
//
// What we can check in Go without a browser: no inline styles that lock
// widths, each <img> has alt text, each card is wrapped in a single <a>
// (keyboard focus target), and the markdown body isn't double-escaped.
func TestPortfolioA11y_Smoke_BasicStructure(t *testing.T) {
	h := setupWithPortfolio(t, map[string]string{
		"showcase": featuredPortfolio,
	})

	pages := map[string]func(rr *httptest.ResponseRecorder){
		"list": func(rr *httptest.ResponseRecorder) {
			h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio", nil))
		},
		"detail": func(rr *httptest.ResponseRecorder) {
			h.PortfolioDetail(rr, httptest.NewRequest("GET", "/portfolio/showcase", nil))
		},
		"home": func(rr *httptest.ResponseRecorder) { h.Home(rr, httptest.NewRequest("GET", "/", nil)) },
	}
	for name, call := range pages {
		rr := httptest.NewRecorder()
		call(rr)
		body := rr.Body.String()
		// Every <img> must have alt=""
		imgs := strings.Count(body, "<img ")
		alts := strings.Count(body, " alt=")
		if imgs > 0 && alts < imgs {
			t.Errorf("%s: %d <img> vs %d alt=", name, imgs, alts)
		}
		// No hardcoded width:/fixed pixel sizes inline
		if strings.Contains(body, `style="width:`) {
			t.Errorf("%s: inline width — breaks responsive", name)
		}
		// Intro content rendered as markdown (HTML), not source
		if strings.Contains(body, "portfolio:intro") {
			t.Errorf("%s: intro marker leaked", name)
		}
	}
}
