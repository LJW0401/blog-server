package admin_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Smoke：GET /manage/sessions 列出自己的会话，含"当前会话"标注。
func TestSessionsPage_Smoke_ListsOwnSession(t *testing.T) {
	b := crudSetup(t)
	w := b.authedGet(t, "/manage/sessions", b.Admin.SessionsPage)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "登陆设备") {
		t.Error("标题缺失")
	}
	if !strings.Contains(body, "当前会话") {
		t.Error("当前会话标注缺失 —— 用户无法区分自己在哪一行")
	}
	if !strings.Contains(body, "203.0.113.5") {
		t.Error("登陆 IP 未展示")
	}
}

// Smoke：撤销后，该 cookie 再次请求 SessionsPage 会被踢回登录页。
// 这是整个功能的核心验收点 —— 对应需求"删除登陆过的设备，然后该设备再次
// 访问私密 URL 需要重新登陆"。
func TestSessionsRevoke_Smoke_RevokedCookieRedirectsToLogin(t *testing.T) {
	b := crudSetup(t)
	// 先跑一次 list 拿 sid
	w := b.authedGet(t, "/manage/sessions", b.Admin.SessionsPage)
	body := w.Body.String()
	// 从 body 里提取 hidden sid 值 —— 用 CSRF 旁边那个 input
	sid := extractHiddenValue(t, body, "sid")

	// 再开一个"另一台设备"的登录，拿到自己的 cookie，再用 cookieA 撤销 cookieB
	// 简化：直接用 RevokeSession 下层 API 是 auth test 已经覆盖；handler 层这里
	// 只验 form 路径，所以我们撤销"自己当前"的 sid
	form := url.Values{"csrf": {b.CSRF}, "sid": {sid}}
	wr := b.authedPost(t, "/manage/sessions/revoke", form, b.Admin.SessionsRevoke)
	if wr.Code != 303 {
		t.Fatalf("revoke status %d", wr.Code)
	}
	// 撤销当前会话后，handler 会清 cookie 并跳 login
	loc := wr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/manage/login") {
		t.Errorf("revoke current session should redirect to login, got %q", loc)
	}

	// 关键断言：同一个 cookie 再次访问受保护页，ParseSession 应该失败
	req := httptest.NewRequest("GET", "/manage/sessions", nil)
	req.AddCookie(b.Cookie)
	req.Header.Set("User-Agent", "test/ua")
	if _, ok := b.Admin.SessionFromRequest(req); ok {
		t.Error("已撤销的 cookie 必须被 ParseSession 拒绝 —— 否则需求里的'再次访问需要重新登录'不成立")
	}
}

// Edge（权限/认证）：未携带有效 session 访问 SessionsPage 必须跳 login。
func TestSessionsPage_Edge_NoSessionRedirectsToLogin(t *testing.T) {
	b := crudSetup(t)
	req := httptest.NewRequest("GET", "/manage/sessions", nil)
	req.Header.Set("User-Agent", "test/ua")
	w := httptest.NewRecorder()
	b.Admin.SessionsPage(w, req)
	if w.Code != 303 {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Location"), "/manage/login") {
		t.Errorf("location = %q", w.Header().Get("Location"))
	}
}

// Edge（非法输入 + CSRF）：缺 csrf 的 revoke 请求返回 403，不执行任何撤销。
func TestSessionsRevoke_Edge_MissingCSRF403(t *testing.T) {
	b := crudSetup(t)
	// 拿 sid
	body := b.authedGet(t, "/manage/sessions", b.Admin.SessionsPage).Body.String()
	sid := extractHiddenValue(t, body, "sid")

	form := url.Values{"sid": {sid}} // 故意不带 csrf
	w := b.authedPost(t, "/manage/sessions/revoke", form, b.Admin.SessionsRevoke)
	if w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	// 验证会话仍活跃
	req := httptest.NewRequest("GET", "/manage", nil)
	req.AddCookie(b.Cookie)
	req.Header.Set("User-Agent", "test/ua")
	if _, ok := b.Admin.SessionFromRequest(req); !ok {
		t.Error("CSRF 拦截失败后不应影响原会话")
	}
}

// Regression：模板里 hidden csrf 的值必须是真正的 session CSRF，而不是空串。
// 原 bug：模板用 `{{ $.CSRF }}` 想从根作用域取，但 `{{ with .Data }}` 不改变 `$`，
// 根是外层 wrapper —— `$.CSRF` 实际解析为空，浏览器提交后 handler 校验 403。
func TestSessionsPage_Regression_FormHasRealCSRF(t *testing.T) {
	b := crudSetup(t)
	body := b.authedGet(t, "/manage/sessions", b.Admin.SessionsPage).Body.String()
	csrf := extractHiddenValue(t, body, "csrf")
	if csrf == "" {
		t.Fatal("模板渲染的 csrf hidden 为空 —— 浏览器点撤销会直接 403")
	}
	if csrf != b.CSRF {
		t.Errorf("模板 csrf %q 与 session CSRF %q 不一致", csrf, b.CSRF)
	}
}

// Edge（边界值）：空 sid 的 revoke 请求重定向回 sessions 页并带错误信息，不撤销任何记录。
func TestSessionsRevoke_Edge_EmptySIDShowsError(t *testing.T) {
	b := crudSetup(t)
	form := url.Values{"csrf": {b.CSRF}, "sid": {""}}
	w := b.authedPost(t, "/manage/sessions/revoke", form, b.Admin.SessionsRevoke)
	if w.Code != 303 {
		t.Fatalf("status %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/manage/sessions?e=") {
		t.Errorf("empty sid should redirect with ?e=, got %q", loc)
	}
}

// extractHiddenValue 从 HTML 片段里抠出 <input type="hidden" name="xxx" value="..."> 的 value。
// 简易实现，够跑测试：找 `name="xxx" value="` 后面到 `"` 的内容。
func extractHiddenValue(t *testing.T, body, name string) string {
	t.Helper()
	marker := `name="` + name + `" value="`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("未找到 name=%q 的 hidden input", name)
	}
	rest := body[i+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("hidden input 未闭合")
	}
	return rest[:end]
}
