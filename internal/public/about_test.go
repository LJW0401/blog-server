package public_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aboutFile writes content/about.md under the test bundle's data dir and
// returns the absolute path, which is what public.Handlers.AboutPath expects.
func aboutFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "content", "about.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// Smoke：about.md 存在且非空 → /about 返回 200，渲染 markdown，不带 doc chrome。
func TestAbout_Smoke_FileRendersCleanPage(t *testing.T) {
	h := setup(t, map[string]string{
		// 放一篇普通 doc，确保 about 不跟它串台
		"a": doc("a", "2026-04-18", "published", false, ""),
	}, nil)
	h.AboutPath = aboutFile(t, "# 我是谁\n\n这是**详细**描述。\n")

	req := httptest.NewRequest("GET", "/about", nil)
	w := httptest.NewRecorder()
	h.About(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<h1>关于我</h1>") {
		t.Errorf("固定页面标题 <h1>关于我</h1> 缺失")
	}
	if !strings.Contains(body, "<strong>详细</strong>") {
		t.Errorf("markdown 没渲染")
	}
	if strings.Contains(body, `class="doc-nav"`) {
		t.Errorf("/about 不该有 prev/next 导航")
	}
	if strings.Contains(body, `href="/docs/a"`) {
		t.Errorf("/about 不该混入普通 doc 的链接")
	}
}

// Smoke：文件不存在时回退到内置默认文案，/about 仍 200 而不是 404，避免
// 新部署的站点一点内容都没有。默认文案里的关键词"默认文案"必须出现。
func TestAbout_Smoke_MissingFileFallsBackToDefault(t *testing.T) {
	h := setup(t, nil, nil)
	h.AboutPath = filepath.Join(t.TempDir(), "does-not-exist.md")

	req := httptest.NewRequest("GET", "/about", nil)
	w := httptest.NewRecorder()
	h.About(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "默认文案") {
		t.Errorf("should fall back to assets.DefaultAbout()")
	}
}

// Smoke：AboutPath 为空字符串（未配置）也要走默认，不能 404。
func TestAbout_Smoke_EmptyAboutPathFallsBackToDefault(t *testing.T) {
	h := setup(t, nil, nil)
	h.AboutPath = ""

	req := httptest.NewRequest("GET", "/about", nil)
	w := httptest.NewRecorder()
	h.About(w, req)

	if w.Code != 200 {
		t.Errorf("empty path should still render default, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "默认文案") {
		t.Errorf("default fallback missing")
	}
}

// Edge（边界值）：文件存在但仅空白——视作空，同样回退到默认而不是 404。
func TestAbout_Edge_WhitespaceOnlyFileFallsBackToDefault(t *testing.T) {
	h := setup(t, nil, nil)
	h.AboutPath = aboutFile(t, "   \n\n\t  \n")

	req := httptest.NewRequest("GET", "/about", nil)
	w := httptest.NewRecorder()
	h.About(w, req)

	if w.Code != 200 {
		t.Errorf("whitespace file should fall back to default, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "默认文案") {
		t.Errorf("default fallback missing")
	}
}

// Smoke：文件非空则覆盖默认——管理员保存后，/about 显示管理员内容而非默认。
func TestAbout_Smoke_FileOverridesDefault(t *testing.T) {
	h := setup(t, nil, nil)
	h.AboutPath = aboutFile(t, "# 我是自定义版本")

	req := httptest.NewRequest("GET", "/about", nil)
	w := httptest.NewRecorder()
	h.About(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "我是自定义版本") {
		t.Errorf("admin-authored content missing")
	}
	if strings.Contains(body, "默认文案") {
		t.Errorf("默认文案 shouldn't appear once admin file exists")
	}
}

// Edge（非法输入）：正文含原始 HTML（markdownUnsafe 允许），页面不崩。
func TestAbout_Edge_RawHTMLDoesNotBreakPage(t *testing.T) {
	h := setup(t, nil, nil)
	h.AboutPath = aboutFile(t, `我 <span style="color:#c00">喜欢</span> Go`)

	req := httptest.NewRequest("GET", "/about", nil)
	w := httptest.NewRecorder()
	h.About(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `<span style="color:#c00">喜欢</span>`) {
		t.Errorf("raw HTML passthrough failed")
	}
	if !strings.Contains(body, "保持联系") {
		t.Errorf("footer 缺失 — 页面被截断")
	}
}

// Smoke：导航栏"关于"入口仍在。
func TestAbout_Smoke_NavLinkPresent(t *testing.T) {
	h := setup(t, nil, nil)
	h.AboutPath = aboutFile(t, "hi")

	req := httptest.NewRequest("GET", "/about", nil)
	w := httptest.NewRecorder()
	h.About(w, req)
	if !strings.Contains(w.Body.String(), `href="/about"`) {
		t.Errorf("nav 缺少 /about 链接")
	}
	if !strings.Contains(w.Body.String(), "关于</a>") {
		t.Errorf("nav 缺少 关于 文案")
	}
}

// Smoke：content/docs/about.md 这样的 slug 不影响 /about —— 本 handler 读的是
// AboutPath（不在 docs 子目录），证"单独管理、不与文档混"。无 AboutPath 文件时
// 走默认文案，而不是从 content/docs 捡。
func TestAbout_Smoke_ContentDocNotUsedAsSource(t *testing.T) {
	h := setup(t, map[string]string{
		"about": "---\ntitle: 过期 doc\nslug: about\nupdated: 2026-04-21\nstatus: published\n---\n这段不该出现。\n",
	}, nil)
	h.AboutPath = filepath.Join(t.TempDir(), "about.md") // 不存在 → 走默认

	req := httptest.NewRequest("GET", "/about", nil)
	w := httptest.NewRecorder()
	h.About(w, req)

	if strings.Contains(w.Body.String(), "这段不该出现") {
		t.Errorf("/about 意外读了 content/docs/about.md")
	}
}
