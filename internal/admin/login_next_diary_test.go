package admin_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Regression：/manage/login?next=/diary 登陆后应该回 /diary，而不是被
// 当时只放行 `/manage` 前缀的老白名单拍回 /manage 根页。
//
// 三条用例一起：
//  1. POST /manage/login + next=/diary → 303 到 /diary
//  2. 已登录 GET /manage/login?next=/diary → 303 到 /diary（LoginPage 分支）
//  3. 安全基线：next=//evil.com、http://evil.com、裸 evil.com 不能逃逸为外链
func TestLogin_Regression_NextDiaryRespected(t *testing.T) {
	h, _, _ := setupHandlers(t)

	// 1. POST 登录成功分支
	w := postForm(t, h.LoginSubmit, url.Values{
		"username": {"admin"},
		"password": {"supersecret"},
		"next":     {"/diary"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/diary" {
		t.Errorf("LoginSubmit with next=/diary → %q, want /diary", loc)
	}

	// 2. 已登录访问 login 页（LoginPage 分支）
	cookie, _ := authenticated(t, h)
	req := httptest.NewRequest("GET", "/manage/login?next=/diary", nil)
	req.AddCookie(cookie)
	req.Header.Set("User-Agent", "test/ua")
	rr := httptest.NewRecorder()
	h.LoginPage(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("LoginPage status %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/diary" {
		t.Errorf("LoginPage with next=/diary → %q, want /diary", loc)
	}

	// 3. 外链必须被挡回默认页，不能保留跨站跳转
	for _, bad := range []string{"//evil.com", "http://evil.com", "https://evil.com/x", "evil.com", ""} {
		w := postForm(t, h.LoginSubmit, url.Values{
			"username": {"admin"},
			"password": {"supersecret"},
			"next":     {bad},
		})
		loc := w.Header().Get("Location")
		if strings.Contains(loc, "evil.com") {
			t.Errorf("next=%q leaked external redirect: %q", bad, loc)
		}
		if loc != "/manage" && loc != "/diary" {
			// 必须落回白名单内部路径之一
			t.Errorf("next=%q → %q, should fall back to internal default", bad, loc)
		}
	}
}
