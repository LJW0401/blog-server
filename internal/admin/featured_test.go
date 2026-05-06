// 文档与项目"主页展示"开关 ToggleFeatured 端点的回归测试。
// 覆盖正常切换、未登录跳转、CSRF 校验、未知 slug 返回 404 等场景。
package admin_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/content"
)

const docFeaturedFalseMD = `---
title: Doc A
slug: doca
tags: []
category: misc
excerpt:
created: 2026-04-19
updated: 2026-04-19
status: published
featured: false
---

正文。
`

const projFeaturedFalseMD = `---
slug: proja
repo: penguin/proja
display_name: Proj A
display_desc: desc
category: 后端
stack: []
status: active
featured: false
created: 2026-04-19
updated: 2026-04-19
---

body
`

func seedDocFile(t *testing.T, dir, slug, body string) {
	t.Helper()
	p := filepath.Join(dir, "content", "docs", slug+".md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func seedProjectFile(t *testing.T, dir, slug, body string) {
	t.Helper()
	p := filepath.Join(dir, "content", "projects", slug+".md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Smoke: 文档 featured 开关可以打开再关闭，文件落盘后 content 仓更新。
func TestDocsToggleFeatured_Smoke_RoundTrip(t *testing.T) {
	b := crudSetup(t)
	seedDocFile(t, b.DataDir, "doca", docFeaturedFalseMD)
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}

	w := b.authedPost(t, "/manage/docs/doca/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
		b.Docs.ToggleFeatured)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("toggle on status=%d body=%s", w.Code, w.Body.String())
	}
	e, _ := b.Content.Docs().Get(content.KindDoc, "doca")
	if e == nil || !e.Featured {
		t.Fatalf("doc not featured after toggle on: %+v", e)
	}

	w2 := b.authedPost(t, "/manage/docs/doca/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"false"}},
		b.Docs.ToggleFeatured)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("toggle off status=%d", w2.Code)
	}
	e2, _ := b.Content.Docs().Get(content.KindDoc, "doca")
	if e2.Featured {
		t.Error("doc still featured after toggle off")
	}
	// 文件 frontmatter 真实改写到 false（不仅仅是内存索引）。
	body, _ := os.ReadFile(filepath.Join(b.DataDir, "content", "docs", "doca.md"))
	if !strings.Contains(string(body), "featured: false") {
		t.Errorf("frontmatter not rewritten to false: %s", string(body))
	}
}

// Exception 类别：权限/认证 — 未携带 cookie 时跳转登录而不是写盘。
func TestDocsToggleFeatured_Exception_UnauthRedirectsToLogin(t *testing.T) {
	b := crudSetup(t)
	seedDocFile(t, b.DataDir, "doca", docFeaturedFalseMD)
	_ = b.Content.Reload()

	form := url.Values{"csrf": {b.CSRF}, "featured": {"true"}}
	req := httptest.NewRequest("POST", "/manage/docs/doca/featured", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// 不带 cookie。
	w := httptest.NewRecorder()
	b.Docs.ToggleFeatured(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasSuffix(loc, "/manage/login") {
		t.Errorf("expected redirect to login, got %q", loc)
	}
	e, _ := b.Content.Docs().Get(content.KindDoc, "doca")
	if e == nil || e.Featured {
		t.Errorf("featured must not change on unauth: %+v", e)
	}
}

// Exception 类别：非法输入 — CSRF 缺失时拒绝，且不改文件。
func TestDocsToggleFeatured_Exception_BadCSRF(t *testing.T) {
	b := crudSetup(t)
	seedDocFile(t, b.DataDir, "doca", docFeaturedFalseMD)
	_ = b.Content.Reload()
	w := b.authedPost(t, "/manage/docs/doca/featured",
		url.Values{"csrf": {"wrong"}, "featured": {"true"}},
		b.Docs.ToggleFeatured)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	e, _ := b.Content.Docs().Get(content.KindDoc, "doca")
	if e.Featured {
		t.Error("featured changed despite bad CSRF")
	}
}

// Exception 类别：边界 — slug 不存在时返回 404，没有副作用。
func TestDocsToggleFeatured_Exception_UnknownSlug(t *testing.T) {
	b := crudSetup(t)
	w := b.authedPost(t, "/manage/docs/ghost/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
		b.Docs.ToggleFeatured)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// 回归：旧手写的 md 可能根本没写 featured: 字段，第一次点开关不能直接
// 500。toggleFeaturedFile 走 setOrAddFrontmatterField 时应当在 frontmatter
// 末尾补一行 featured: <bool> 而不是把缺字段当硬错。
func TestDocsToggleFeatured_Regression_LegacyFrontmatterWithoutField(t *testing.T) {
	b := crudSetup(t)
	legacy := `---
title: Legacy
slug: legacy
status: published
created: 2026-01-01
updated: 2026-01-01
---

正文。
`
	seedDocFile(t, b.DataDir, "legacy", legacy)
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedPost(t, "/manage/docs/legacy/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
		b.Docs.ToggleFeatured)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", w.Code, w.Body.String())
	}
	body, _ := os.ReadFile(filepath.Join(b.DataDir, "content", "docs", "legacy.md"))
	if !strings.Contains(string(body), "featured: true") {
		t.Errorf("featured field not backfilled: %s", string(body))
	}
	e, _ := b.Content.Docs().Get(content.KindDoc, "legacy")
	if e == nil || !e.Featured {
		t.Errorf("doc not featured after backfill toggle: %+v", e)
	}
}

// Smoke: 项目 featured 开关同样可以双向切换。
func TestProjectsToggleFeatured_Smoke_RoundTrip(t *testing.T) {
	b := crudSetup(t)
	seedProjectFile(t, b.DataDir, "proja", projFeaturedFalseMD)
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedPost(t, "/manage/projects/proja/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
		b.Projects.ToggleFeatured)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	e, _ := b.Content.Projects().Get(content.KindProject, "proja")
	if e == nil || !e.Featured {
		t.Errorf("project not featured: %+v", e)
	}
	// 切回 false。
	w2 := b.authedPost(t, "/manage/projects/proja/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"false"}},
		b.Projects.ToggleFeatured)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("status=%d", w2.Code)
	}
	e2, _ := b.Content.Projects().Get(content.KindProject, "proja")
	if e2.Featured {
		t.Error("project still featured after toggle off")
	}
}

// 回归：toggle 端点的 Location 不应携带 `#row-<slug>` fragment。anchor 会让
// 浏览器在原生表单回退路径下把目标行拖到视口顶端，列表末尾的行因此把页面
// 滚到文档最大值（"跳到底部"，issue 复现）。fix 是不再带 anchor，浏览器
// 退化路径最差也只跳到 /manage/docs 顶端。JS fetch 路径不受影响。
func TestDocsToggleFeatured_Regression_RedirectHasNoFragment(t *testing.T) {
	b := crudSetup(t)
	seedDocFile(t, b.DataDir, "doca", docFeaturedFalseMD)
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedPost(t, "/manage/docs/doca/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
		b.Docs.ToggleFeatured)
	loc := w.Header().Get("Location")
	if strings.Contains(loc, "#") {
		t.Errorf("docs toggle redirect must not carry a fragment, got %q", loc)
	}
}

func TestProjectsToggleFeatured_Regression_RedirectHasNoFragment(t *testing.T) {
	b := crudSetup(t)
	seedProjectFile(t, b.DataDir, "proja", projFeaturedFalseMD)
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedPost(t, "/manage/projects/proja/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
		b.Projects.ToggleFeatured)
	loc := w.Header().Get("Location")
	if strings.Contains(loc, "#") {
		t.Errorf("projects toggle redirect must not carry a fragment, got %q", loc)
	}
}

// Exception 类别：边界 — 项目 slug 未知时 404。
func TestProjectsToggleFeatured_Exception_UnknownSlug(t *testing.T) {
	b := crudSetup(t)
	w := b.authedPost(t, "/manage/projects/ghost/featured",
		url.Values{"csrf": {b.CSRF}, "featured": {"true"}},
		b.Projects.ToggleFeatured)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
