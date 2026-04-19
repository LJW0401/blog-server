package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Smoke: 直接调用 NotFound → 返回 404 + 渲染品牌化 404 页面 + 含返回主页按钮。
func TestNotFound_Smoke_RendersBrandedPageWithHomeButton(t *testing.T) {
	h := setup(t, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatever", nil)
	h.NotFound(rr, req)

	if rr.Code != 404 {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type not HTML: %q", ct)
	}
	body := rr.Body.String()
	for _, frag := range []string{
		`class="notfound"`,
		`class="notfound-code"`,
		">404<",
		`href="/"`,
		"返回主页",
	} {
		if !strings.Contains(body, frag) {
			t.Errorf("404 page missing fragment %q", frag)
		}
	}
}

// Edge（非法输入）：未知 doc slug → 走品牌化 404 页面，而不是 net/http 的纯文本 404。
func TestDocDetail_Edge_UnknownSlugRendersBrandedNotFound(t *testing.T) {
	h := setup(t, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/docs/nope", nil)
	h.DocDetail(rr, req)

	if rr.Code != 404 {
		t.Fatalf("status: %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `class="notfound"`) {
		t.Errorf("branded 404 not rendered for unknown doc; body: %s", rr.Body.String())
	}
}

// Edge（非法输入）：未知 project slug → 同样走品牌化 404。
func TestProjectDetail_Edge_UnknownSlugRendersBrandedNotFound(t *testing.T) {
	h := setup(t, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/projects/nope", nil)
	h.ProjectDetail(rr, req)

	if rr.Code != 404 {
		t.Fatalf("status: %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `class="notfound"`) {
		t.Errorf("branded 404 not rendered for unknown project")
	}
}

// Edge（边界值）：路径遍历尝试（../etc/passwd）→ 品牌化 404，不暴露任何系统信息。
func TestDocDetail_Edge_TraversalSlugRendersBrandedNotFound(t *testing.T) {
	h := setup(t, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/docs/..%2Fetc%2Fpasswd", nil)
	h.DocDetail(rr, req)

	if rr.Code != 404 {
		t.Fatalf("status: %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `class="notfound"`) {
		t.Errorf("branded 404 not rendered for traversal attempt")
	}
	// 确认不泄露系统信息
	if strings.Contains(body, "passwd") || strings.Contains(body, "/etc/") {
		t.Errorf("traversal input leaked into 404 page body")
	}
}

// Edge（权限/认证）：草稿文档匿名访问 → 品牌化 404（不透露草稿存在）。
func TestDocDetail_Edge_DraftAnonymousRendersBrandedNotFound(t *testing.T) {
	h := setup(t, map[string]string{
		"secret": doc("secret", "2026-04-10", "draft", false, ""),
	}, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/docs/secret", nil)
	h.DocDetail(rr, req)

	if rr.Code != 404 {
		t.Fatalf("status: %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `class="notfound"`) {
		t.Errorf("branded 404 not rendered for draft anon access")
	}
}
