package admin_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// WI: 编辑文档 加 编辑/预览 切换。/manage/docs/preview 接受 MD 正文，
// 返回 goldmark 渲染后的 HTML 片段，供前端 tab 切换时展示。

func postPreview(t *testing.T, b *crudBundle, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/manage/docs/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.RemoteAddr = "203.0.113.5:12345"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	b.Docs.Preview(w, req)
	return w
}

// Smoke: frontmatter + 正文，返回渲染出的 HTML 片段（<h1>、<p>、<code>、链接等）
func TestPreview_Smoke_RendersBody(t *testing.T) {
	b := crudSetup(t)
	body := "---\ntitle: Test\nslug: test\n---\n\n# Hello **world**\n\n有个 [链接](https://example.com) 和 `code`\n"
	w := postPreview(t, b, url.Values{"csrf": {b.CSRF}, "body": {body}}, b.Cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type=%q, want text/html", ct)
	}
	got := w.Body.String()
	for _, want := range []string{"<h1", "Hello", "<strong>world</strong>", `href="https://example.com"`, "<code>code</code>"} {
		if !strings.Contains(got, want) {
			t.Errorf("preview missing %q\nbody:\n%s", want, got)
		}
	}
	// frontmatter 不能出现在渲染结果里
	if strings.Contains(got, "slug: test") || strings.Contains(got, "title: Test") {
		t.Errorf("frontmatter leaked into preview: %s", got)
	}
}

// 异常：CSRF 缺失 → 403（权限/认证类）
func TestPreview_Edge_CSRFMissing(t *testing.T) {
	b := crudSetup(t)
	w := postPreview(t, b, url.Values{"csrf": {""}, "body": {"# x"}}, b.Cookie)
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", w.Code)
	}
}

// 异常：未登录 → 401（权限/认证类；XHR 场景给 401 而不是 302，便于前端感知）
func TestPreview_Edge_Unauthenticated(t *testing.T) {
	b := crudSetup(t)
	w := postPreview(t, b, url.Values{"csrf": {b.CSRF}, "body": {"# x"}}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", w.Code)
	}
}

// 异常：空 body（边界值）→ 200，返回空片段
func TestPreview_Edge_EmptyBody(t *testing.T) {
	b := crudSetup(t)
	w := postPreview(t, b, url.Values{"csrf": {b.CSRF}, "body": {""}}, b.Cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != "" {
		t.Errorf("expected empty preview, got %q", w.Body.String())
	}
}

// 异常：只有 frontmatter 没正文（边界值）→ 200，片段为空
func TestPreview_Edge_FrontmatterOnly(t *testing.T) {
	b := crudSetup(t)
	body := "---\ntitle: t\nslug: s\n---\n"
	w := postPreview(t, b, url.Values{"csrf": {b.CSRF}, "body": {body}}, b.Cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "slug") {
		t.Errorf("frontmatter leaked: %s", w.Body.String())
	}
}

// 异常：未闭合 frontmatter（非法输入）→ 不崩，按整体正文渲染
func TestPreview_Edge_UnterminatedFrontmatter(t *testing.T) {
	b := crudSetup(t)
	body := "---\ntitle: t\n# 正文但没闭合 frontmatter\n"
	w := postPreview(t, b, url.Values{"csrf": {b.CSRF}, "body": {body}}, b.Cookie)
	if w.Code != http.StatusOK {
		t.Errorf("status %d, should gracefully render", w.Code)
	}
}

// 异常：HTML 注入（非法输入）→ goldmark safe 模式下 <script> 被转义
func TestPreview_Edge_ScriptEscaped(t *testing.T) {
	b := crudSetup(t)
	body := "# title\n\n<script>alert('xss')</script>\n"
	w := postPreview(t, b, url.Values{"csrf": {b.CSRF}, "body": {body}}, b.Cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	got := w.Body.String()
	if strings.Contains(got, "<script>alert") {
		t.Errorf("script tag NOT escaped by preview renderer: %s", got)
	}
}

// Regression：编辑器页面必须 embed KaTeX（CSS/JS）+ math-init + doc_edit.js，
// 否则公式预览不出来、Tab 切换按钮不工作。
func TestEditor_Regression_MathAssetsEmbedded(t *testing.T) {
	b := crudSetup(t)
	req := httptest.NewRequest("GET", "/manage/docs/new", nil)
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(b.Cookie)
	rr := httptest.NewRecorder()
	b.Docs.NewDoc(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	out := rr.Body.String()
	for _, want := range []string{
		`href="/static/math/katex.min.css"`,
		`src="/static/math/katex.min.js"`,
		`src="/static/math/auto-render.min.js"`,
		`src="/static/math/math-init.js"`,
		`src="/static/js/doc_edit.js"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("admin editor missing asset %q", want)
		}
	}
}

// Regression：前端必须用 urlencoded 发 preview，而不是 multipart/FormData；
// r.ParseForm() 不解析 multipart body，会读不到 csrf 直接 403。这条用例钉死
// content-type 契约，前端一旦改回 FormData 立刻挂。
func TestPreview_Regression_MultipartRejected(t *testing.T) {
	b := crudSetup(t)
	// 手工构造 multipart 请求（模拟前端用 new FormData() 的场景）
	body := "--X\r\nContent-Disposition: form-data; name=\"csrf\"\r\n\r\n" + b.CSRF +
		"\r\n--X\r\nContent-Disposition: form-data; name=\"body\"\r\n\r\n# hi\r\n--X--\r\n"
	req := httptest.NewRequest("POST", "/manage/docs/preview", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=X")
	req.AddCookie(b.Cookie)
	req.Header.Set("User-Agent", "test/ua")
	w := httptest.NewRecorder()
	b.Docs.Preview(w, req)
	// 当前实现：拿不到 csrf → 403。若未来改成 FormValue 或显式 ParseMultipartForm
	// 也支持 multipart，这条用例就应该同步更新
	if w.Code != http.StatusForbidden {
		t.Errorf("multipart preview status %d, want 403 (csrf unreadable from multipart body under ParseForm)", w.Code)
	}
}

// 异常：GET 方法（非法调用）→ 405
func TestPreview_Edge_GetMethodNotAllowed(t *testing.T) {
	b := crudSetup(t)
	req := httptest.NewRequest("GET", "/manage/docs/preview", nil)
	req.AddCookie(b.Cookie)
	req.Header.Set("User-Agent", "test/ua")
	w := httptest.NewRecorder()
	b.Docs.Preview(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", w.Code)
	}
}
