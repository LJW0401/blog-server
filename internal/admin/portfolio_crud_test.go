package admin_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/penguin/blog-server/internal/admin"
	"github.com/penguin/blog-server/internal/content"
)

func portfolioMD(slug, title string, featured bool, order int) string {
	f := "false"
	if featured {
		f = "true"
	}
	return fmt.Sprintf(`---
title: %s
slug: %s
description: one-liner
category: 设计
tags: [sample]
cover: /images/c.png
order: %d
demo_url:
source_url:
created: 2026-04-19
updated: 2026-04-19
status: published
featured: %s
---
<!-- portfolio:intro -->
intro
<!-- /portfolio:intro -->

正文
`, title, slug, order, f)
}

// --- WI-3.5 Smoke ------------------------------------------------------------

func TestPortfolioCRUD_Smoke_CreateAndList(t *testing.T) {
	b := crudSetup(t)
	// Create via POST /manage/portfolio/new.
	md := portfolioMD("brand-new", "Brand New", false, 7)
	w := b.authedPost(t, "/manage/portfolio/new",
		url.Values{"csrf": {b.CSRF}, "body": {md}}, b.Portfolio.Save)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("save status=%d body=%s", w.Code, w.Body.String())
	}
	// File exists.
	path := filepath.Join(b.DataDir, "content", "portfolio", "brand-new.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing: %v", err)
	}
	// Index contains it.
	if _, ok := b.Content.Portfolios().Get(content.KindPortfolio, "brand-new"); !ok {
		t.Fatal("index missing new entry")
	}
	// List page renders it.
	w2 := b.authedGet(t, "/manage/portfolio", b.Portfolio.List)
	if w2.Code != 200 {
		t.Fatalf("list status %d", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "Brand New") {
		t.Error("list missing new title")
	}
}

func TestPortfolioCRUD_Smoke_Edit(t *testing.T) {
	b := crudSetup(t)
	// Seed via filesystem + reload (faster than round-tripping the new endpoint).
	seedPortfolioFile(t, b, "edit-me", portfolioMD("edit-me", "Original", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	// POST to edit endpoint with updated title.
	updated := portfolioMD("edit-me", "Updated Title", false, 0)
	w := b.authedPost(t, "/manage/portfolio/edit-me/edit",
		url.Values{"csrf": {b.CSRF}, "body": {updated}}, b.Portfolio.Save)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	// File content reflects update.
	body, _ := os.ReadFile(filepath.Join(b.DataDir, "content", "portfolio", "edit-me.md"))
	if !strings.Contains(string(body), "Updated Title") {
		t.Errorf("file not updated: %s", string(body))
	}
}

func TestPortfolioCRUD_Smoke_Delete(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "delme", portfolioMD("delme", "Del Me", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedPost(t, "/manage/portfolio/delme/delete",
		url.Values{"csrf": {b.CSRF}}, b.Portfolio.Delete)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status=%d", w.Code)
	}
	if _, ok := b.Content.Portfolios().Get(content.KindPortfolio, "delme"); ok {
		t.Error("entry still indexed after delete")
	}
	// Should now live under trash/portfolio/
	entries, _ := os.ReadDir(filepath.Join(b.DataDir, "trash", admin.TrashKindPortfolio))
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "delme") {
			found = true
		}
	}
	if !found {
		t.Error("trash file not under trash/portfolio/")
	}
}

func TestPortfolioCRUD_Smoke_ToggleFeatured(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "feat", portfolioMD("feat", "Feat", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedPost(t, "/manage/portfolio/feat/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
		b.Portfolio.ToggleFeatured)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	e, _ := b.Content.Portfolios().Get(content.KindPortfolio, "feat")
	if e == nil || !e.Featured {
		t.Errorf("entry not featured after toggle: %+v", e)
	}
	// Toggle back off.
	w2 := b.authedPost(t, "/manage/portfolio/feat/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"false"}},
		b.Portfolio.ToggleFeatured)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("status=%d", w2.Code)
	}
	e2, _ := b.Content.Portfolios().Get(content.KindPortfolio, "feat")
	if e2.Featured {
		t.Error("entry still featured after untoggle")
	}
}

// Smoke: 非置顶切到置顶时，order 自动变为"现有置顶 max + 10"，
// 让新加入主页的作品自然落在末尾。
func TestPortfolioCRUD_Smoke_FeaturedAutoOrderAppendsToEnd(t *testing.T) {
	b := crudSetup(t)
	// 三个已置顶：order 10 / 20 / 30
	seedPortfolioFile(t, b, "f10", portfolioMD("f10", "F10", true, 10))
	seedPortfolioFile(t, b, "f20", portfolioMD("f20", "F20", true, 20))
	seedPortfolioFile(t, b, "f30", portfolioMD("f30", "F30", true, 30))
	// 一个待上榜（默认 order=0）
	seedPortfolioFile(t, b, "joiner", portfolioMD("joiner", "Joiner", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedPost(t, "/manage/portfolio/joiner/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
		b.Portfolio.ToggleFeatured)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	e, _ := b.Content.Portfolios().Get(content.KindPortfolio, "joiner")
	if e == nil || !e.Featured {
		t.Fatalf("joiner not featured: %+v", e)
	}
	if e.Order != 40 {
		t.Errorf("joiner order=%d, want 40", e.Order)
	}
}

// Exception: 场景覆盖
// - 边界值：无其他置顶时，新上榜 order=10
// - 幂等：已置顶重放 featured=true 不变 order
// - 非法状态：切回非置顶不修改 order
func TestPortfolioCRUD_Exception_FeaturedAutoOrderEdgeCases(t *testing.T) {
	t.Run("边界值：首个置顶 order=10", func(t *testing.T) {
		b := crudSetup(t)
		seedPortfolioFile(t, b, "first", portfolioMD("first", "First", false, 0))
		if err := b.Content.Reload(); err != nil {
			t.Fatal(err)
		}
		w := b.authedPost(t, "/manage/portfolio/first/featured",
			url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
			b.Portfolio.ToggleFeatured)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("status=%d", w.Code)
		}
		e, _ := b.Content.Portfolios().Get(content.KindPortfolio, "first")
		if e.Order != 10 {
			t.Errorf("first order=%d, want 10", e.Order)
		}
	})
	t.Run("幂等：已置顶重放不动 order", func(t *testing.T) {
		b := crudSetup(t)
		seedPortfolioFile(t, b, "keep", portfolioMD("keep", "Keep", true, 77))
		seedPortfolioFile(t, b, "peer", portfolioMD("peer", "Peer", true, 100))
		if err := b.Content.Reload(); err != nil {
			t.Fatal(err)
		}
		w := b.authedPost(t, "/manage/portfolio/keep/featured",
			url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
			b.Portfolio.ToggleFeatured)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("status=%d", w.Code)
		}
		e, _ := b.Content.Portfolios().Get(content.KindPortfolio, "keep")
		if e.Order != 77 {
			t.Errorf("keep order mutated to %d, want 77", e.Order)
		}
	})
	t.Run("切回非置顶：order 保持", func(t *testing.T) {
		b := crudSetup(t)
		seedPortfolioFile(t, b, "demote", portfolioMD("demote", "Demote", true, 55))
		if err := b.Content.Reload(); err != nil {
			t.Fatal(err)
		}
		w := b.authedPost(t, "/manage/portfolio/demote/featured",
			url.Values{"csrf": {b.CSRF}, "featured": {"false"}},
			b.Portfolio.ToggleFeatured)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("status=%d", w.Code)
		}
		e, _ := b.Content.Portfolios().Get(content.KindPortfolio, "demote")
		if e.Featured {
			t.Error("not demoted")
		}
		if e.Order != 55 {
			t.Errorf("demote order mutated to %d, want 55", e.Order)
		}
	})
}

// Smoke: list page shows the new "显示到主页 / 从主页移除" wording on the
// featured toggle buttons (replaces the legacy 置顶/取消置顶 labels).
// 豁免异常测试：纯模板文案改动，端点和数据流未动。
func TestPortfolioCRUD_Smoke_ListShowsHomepageToggleWording(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "on-home", portfolioMD("on-home", "On Home", true, 10))
	seedPortfolioFile(t, b, "off-home", portfolioMD("off-home", "Off Home", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedGet(t, "/manage/portfolio", b.Portfolio.List)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"显示到主页", "从主页移除"} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing wording %q", want)
		}
	}
	for _, gone := range []string{"☆ 置顶", "★ 取消置顶"} {
		if strings.Contains(body, gone) {
			t.Errorf("legacy wording %q still present", gone)
		}
	}
	// 新布局：编辑/删除与"主页管理"分两行；主页开关采用 btn-pill 胶囊按钮
	for _, want := range []string{
		`class="admin-row-actions"`,
		`class="admin-row-homepage"`,
		`class="btn-pill btn-pill-on"`,  // 置顶行
		`class="btn-pill btn-pill-off"`, // 非置顶行
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing markup %q", want)
		}
	}
}

// --- WI-3.6 Exception --------------------------------------------------------

func TestPortfolioCRUD_Exception_SlugConflict(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "dup", portfolioMD("dup", "A", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedPost(t, "/manage/portfolio/new",
		url.Values{"csrf": {b.CSRF}, "body": {portfolioMD("dup", "B", false, 0)}},
		b.Portfolio.Save)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "已存在") {
		t.Errorf("error message missing: %s", w.Body.String())
	}
}

func TestPortfolioCRUD_Exception_InvalidSlug(t *testing.T) {
	b := crudSetup(t)
	bad := strings.Replace(portfolioMD("Valid", "X", false, 0), "slug: Valid", "slug: Bad/Slug", 1)
	w := b.authedPost(t, "/manage/portfolio/new",
		url.Values{"csrf": {b.CSRF}, "body": {bad}}, b.Portfolio.Save)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d (want 400)", w.Code)
	}
}

func TestPortfolioCRUD_Exception_EmptyBody(t *testing.T) {
	b := crudSetup(t)
	w := b.authedPost(t, "/manage/portfolio/new",
		url.Values{"csrf": {b.CSRF}, "body": {""}}, b.Portfolio.Save)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d (want 400)", w.Code)
	}
}

func TestPortfolioCRUD_Exception_MissingFrontmatter(t *testing.T) {
	b := crudSetup(t)
	w := b.authedPost(t, "/manage/portfolio/new",
		url.Values{"csrf": {b.CSRF}, "body": {"just body no frontmatter"}}, b.Portfolio.Save)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d", w.Code)
	}
}

func TestPortfolioCRUD_Exception_UnauthenticatedSaveRedirects(t *testing.T) {
	b := crudSetup(t)
	// POST without cookie
	form := url.Values{"csrf": {"x"}, "body": {portfolioMD("x", "X", false, 0)}}
	req := httptest.NewRequest("POST", "/manage/portfolio/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	b.Portfolio.Save(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("unauthed should redirect, got %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Location"), "/manage/login") {
		t.Errorf("redirect target wrong: %s", rr.Header().Get("Location"))
	}
}

func TestPortfolioCRUD_Exception_CSRFMissing(t *testing.T) {
	b := crudSetup(t)
	w := b.authedPost(t, "/manage/portfolio/new",
		url.Values{"csrf": {""}, "body": {portfolioMD("nocsrf", "NC", false, 0)}},
		b.Portfolio.Save)
	if w.Code != http.StatusForbidden {
		t.Errorf("CSRF-missing should 403, got %d", w.Code)
	}
}

func TestPortfolioCRUD_Exception_DeleteUnknownSlug(t *testing.T) {
	b := crudSetup(t)
	w := b.authedPost(t, "/manage/portfolio/ghost/delete",
		url.Values{"csrf": {b.CSRF}}, b.Portfolio.Delete)
	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", w.Code)
	}
}

func TestPortfolioCRUD_Exception_ConcurrentToggleFeatured(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "race", portfolioMD("race", "Race", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	// Fire 5 concurrent toggles; all should complete and leave the file in
	// one of the two states with no half-written content.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(want string) {
			defer wg.Done()
			b.authedPost(t, "/manage/portfolio/race/featured",
				url.Values{"csrf": {b.CSRF}, "featured": {want}},
				b.Portfolio.ToggleFeatured)
		}([]string{"true", "false"}[i%2])
	}
	wg.Wait()
	raw, err := os.ReadFile(filepath.Join(b.DataDir, "content", "portfolio", "race.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "---") {
		t.Error("file frontmatter corrupted by concurrent writes")
	}
}

// Helpers

func seedPortfolioFile(t *testing.T, b *crudBundle, slug, body string) {
	t.Helper()
	dir := filepath.Join(b.DataDir, "content", "portfolio")
	_ = os.MkdirAll(dir, 0o700)
	if err := os.WriteFile(filepath.Join(dir, slug+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
