package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/penguin/blog-server/internal/content"
)

// --- WI-3.9 Smoke ---------------------------------------------------------

func TestPortfolioOrder_Smoke_UpdateReflectsInIndexAndFile(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "order-one", portfolioMD("order-one", "Order", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedPost(t, "/manage/portfolio/order-one/order",
		url.Values{"csrf": {b.CSRF}, "order": {"42"}}, b.Portfolio.UpdateOrder)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Errorf("ok=false error=%q", resp.Error)
	}
	e, _ := b.Content.Portfolios().Get(content.KindPortfolio, "order-one")
	if e == nil || e.Order != 42 {
		t.Errorf("order not updated: %+v", e)
	}
	// File frontmatter contains order: 42
	raw, _ := os.ReadFile(filepath.Join(b.DataDir, "content", "portfolio", "order-one.md"))
	if !strings.Contains(string(raw), "order: 42") {
		t.Errorf("frontmatter not updated: %s", string(raw))
	}
}

// --- WI-3.10 Exception -----------------------------------------------------

func TestPortfolioOrder_Exception_InvalidValues(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "bad-order", portfolioMD("bad-order", "Bad", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		value  string
		status int
	}{
		{"alpha", "abc", http.StatusBadRequest},
		{"negative", "-1", http.StatusBadRequest},
		{"overLimit", "10000", http.StatusBadRequest},
		{"empty", "", http.StatusBadRequest},
		{"scientific", "1e3", http.StatusBadRequest},
		{"floatVal", "3.14", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := b.authedPost(t, "/manage/portfolio/bad-order/order",
				url.Values{"csrf": {b.CSRF}, "order": {c.value}}, b.Portfolio.UpdateOrder)
			if w.Code != c.status {
				t.Errorf("value=%q status=%d want %d body=%s", c.value, w.Code, c.status, w.Body.String())
			}
			// Response is JSON
			if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
				t.Errorf("response not JSON: %s", ct)
			}
		})
	}
	// Value should still be 0 (no changes applied)
	e, _ := b.Content.Portfolios().Get(content.KindPortfolio, "bad-order")
	if e.Order != 0 {
		t.Errorf("order mutated by invalid input: %d", e.Order)
	}
}

func TestPortfolioOrder_Exception_BoundaryAccepted(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "bounds", portfolioMD("bounds", "B", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"0", "9999"} {
		w := b.authedPost(t, "/manage/portfolio/bounds/order",
			url.Values{"csrf": {b.CSRF}, "order": {v}}, b.Portfolio.UpdateOrder)
		if w.Code != 200 {
			t.Errorf("value=%q status=%d body=%s", v, w.Code, w.Body.String())
		}
	}
}

func TestPortfolioOrder_Exception_UnauthenticatedReturnsUnauthorized(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "auth", portfolioMD("auth", "A", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf": {"x"}, "order": {"3"}}
	req := httptest.NewRequest("POST", "/manage/portfolio/auth/order", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	b.Portfolio.UpdateOrder(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("unauthed should 401, got %d", rr.Code)
	}
}

func TestPortfolioOrder_Exception_CSRFMissing(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "csrf", portfolioMD("csrf", "C", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedPost(t, "/manage/portfolio/csrf/order",
		url.Values{"csrf": {""}, "order": {"5"}}, b.Portfolio.UpdateOrder)
	if w.Code != http.StatusForbidden {
		t.Errorf("CSRF-missing should 403, got %d", w.Code)
	}
}

func TestPortfolioOrder_Exception_UnknownSlug(t *testing.T) {
	b := crudSetup(t)
	w := b.authedPost(t, "/manage/portfolio/ghost/order",
		url.Values{"csrf": {b.CSRF}, "order": {"5"}}, b.Portfolio.UpdateOrder)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown slug should 404, got %d", w.Code)
	}
}

func TestPortfolioOrder_Exception_ConcurrentWritesNoHalfFile(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "conc", portfolioMD("conc", "C", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			b.authedPost(t, "/manage/portfolio/conc/order",
				url.Values{"csrf": {b.CSRF}, "order": {v}}, b.Portfolio.UpdateOrder)
		}([]string{"1", "2", "3", "4", "5", "6", "7", "8"}[i])
	}
	wg.Wait()
	raw, err := os.ReadFile(filepath.Join(b.DataDir, "content", "portfolio", "conc.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Must still begin with frontmatter fence, not be half-written.
	if !strings.HasPrefix(string(raw), "---\n") {
		t.Errorf("file corrupted: %s", string(raw[:min(80, len(raw))]))
	}
	if !strings.Contains(string(raw), "order:") {
		t.Errorf("order field lost: %s", string(raw))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
