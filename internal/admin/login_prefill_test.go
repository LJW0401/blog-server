package admin_test

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Smoke: 登录页预填 admin_username（因为只有一个管理员，省得每次手敲）。
func TestLoginPage_Smoke_PrefillsUsernameFromConfig(t *testing.T) {
	h, _, _ := setupHandlers(t) // admin_username: "admin"
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/manage/login", nil)
	h.LoginPage(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	// 捕获 username input 的 value 属性
	re := regexp.MustCompile(`<input[^>]*name="username"[^>]*value="([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("username input with value=... not found; body:\n%s", body)
	}
	if m[1] != "admin" {
		t.Errorf("username input value = %q, want %q", m[1], "admin")
	}
	// 用户名预填后焦点应该自动到密码字段，避免用户看到 autofocus 在已填值的输入
	if !strings.Contains(body, `type="password"`) {
		t.Fatal("password input missing")
	}
	// password 标签范围内应有 autofocus
	pwBlock := body[strings.Index(body, `type="password"`):]
	pwBlock = pwBlock[:strings.Index(pwBlock, "/>")]
	if !strings.Contains(pwBlock, "autofocus") {
		t.Errorf("password input should carry autofocus when username is prefilled; got: %s", pwBlock)
	}
}

// Edge（非法输入 / XSS）：若 AdminUsername 含 HTML 元字符，渲染必须转义，
// 防止通过 config.yaml 注入脚本。Go html/template 默认开转义，这里回归测一下。
func TestLoginPage_Edge_UsernameHTMLIsEscaped(t *testing.T) {
	h, cfg, _ := setupHandlers(t)
	cfg.AdminUsername = `"><script>alert(1)</script>`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/manage/login", nil)
	h.LoginPage(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, "<script>alert(1)") {
		t.Errorf("unescaped script tag leaked from AdminUsername into output")
	}
	// 合法转义后的字符串应仍能作为属性值出现
	if !strings.Contains(body, "&lt;script&gt;") && !strings.Contains(body, "&#34;") {
		t.Errorf("expected html-escaped form of the username; body: %s", body)
	}
}

// Edge（边界值）：AdminUsername 为空时不应破坏 input 渲染，也不应加 autofocus 到 password。
func TestLoginPage_Edge_EmptyUsernameKeepsFieldEditable(t *testing.T) {
	h, cfg, _ := setupHandlers(t)
	cfg.AdminUsername = ""
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/manage/login", nil)
	h.LoginPage(rr, req)

	body := rr.Body.String()
	re := regexp.MustCompile(`<input[^>]*name="username"[^>]*value="([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("username input missing")
	}
	if m[1] != "" {
		t.Errorf("expected empty value when config has no AdminUsername; got %q", m[1])
	}
	// 空用户名时不应该把 autofocus 放在 password
	pwStart := strings.Index(body, `type="password"`)
	if pwStart < 0 {
		t.Fatal("password input missing")
	}
	pwBlock := body[pwStart:]
	pwBlock = pwBlock[:strings.Index(pwBlock, "/>")]
	if strings.Contains(pwBlock, "autofocus") {
		t.Errorf("password should not autofocus when username is empty; got: %s", pwBlock)
	}
}
