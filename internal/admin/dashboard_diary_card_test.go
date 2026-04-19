package admin_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Smoke (WI-3.12): 仪表盘 /manage 下含日记入口链接。
// 纯展示 + 静态链接，无输入无副作用，按开发方案豁免异常测试。
func TestDashboard_Smoke_IncludesDiaryLink(t *testing.T) {
	h, _, _ := setupHandlers(t)
	// 构造已登录的仪表盘请求
	req := httptest.NewRequest("GET", "/manage", nil)
	req.Header.Set("User-Agent", "test/ua")
	// IssueSession 并附 cookie
	sess, cookie, err := h.Auth.IssueSession("admin", "test/ua")
	if err != nil {
		t.Fatal(err)
	}
	_ = sess
	req.AddCookie(cookie)

	rr := httptest.NewRecorder()
	h.Dashboard(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `href="/diary"`) {
		t.Errorf("dashboard missing href=/diary link; body:\n%s", body)
	}
	if !strings.Contains(body, "日记") {
		t.Errorf("dashboard missing '日记' label")
	}
}
