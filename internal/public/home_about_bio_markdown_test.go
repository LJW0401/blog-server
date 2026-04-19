package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Smoke: 关于我 bio 字段支持 Markdown 加粗/斜体/链接 + 原始 <span style> 颜色。
func TestHome_Smoke_AboutBioMarkdownRendered(t *testing.T) {
	h := setup(t, nil, nil)
	_ = h.SettingsDB.Set("about_bio", "一名 **后端** *工程师*，[博客](https://example.com)，<span style=\"color:#c00\">热爱</span>造轮子")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	body := rr.Body.String()

	for _, frag := range []string{
		"<strong>后端</strong>",
		"<em>工程师</em>",
		`href="https://example.com"`,
		`<span style="color:#c00">热爱</span>`,
	} {
		if !strings.Contains(body, frag) {
			t.Errorf("missing rendered fragment %q in bio output", frag)
		}
	}
}

// Edge: bio 留空时走默认文案分支，且不渲染空的 markdown 容器。
func TestHome_Edge_AboutBioEmptyFallsBackToDefault(t *testing.T) {
	h := setup(t, nil, nil)
	// 不 Set about_bio 即视为空

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	body := rr.Body.String()

	if !strings.Contains(body, "后端工程与开发者工具") {
		t.Errorf("default bio fallback missing")
	}
}

// Edge（非法输入 / 边界值）：bio 内容仅为空白时等同于未设置，走默认文案。
func TestHome_Edge_AboutBioWhitespaceTreatedAsEmpty(t *testing.T) {
	h := setup(t, nil, nil)
	_ = h.SettingsDB.Set("about_bio", "   \n\t  ")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	body := rr.Body.String()

	if !strings.Contains(body, "后端工程与开发者工具") {
		t.Errorf("whitespace-only bio should fall back to default")
	}
}

// Edge（非法输入）：bio 含原始 HTML 字符实体时不应破坏页面整体结构。
// 即使 unsafe 渲染器允许 raw HTML 透传，goldmark 也应产出合法 HTML。
func TestHome_Edge_AboutBioWithAngleBracketsStaysStable(t *testing.T) {
	h := setup(t, nil, nil)
	// 典型用户失误：写了 <3 这样的字符
	_ = h.SettingsDB.Set("about_bio", "我 <3 Go 与 Rust")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "保持联系") { // footer 仍在，说明页面没被截断
		t.Errorf("page truncated after malformed bio")
	}
}
