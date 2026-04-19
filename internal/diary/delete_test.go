package diary_test

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Smoke (WI-3.2) --------------------------------------------------------

func TestDelete_Smoke_RemovesExistingEntry(t *testing.T) {
	h, dir, cookie, csrf := setupHandlersWithCSRF(t)
	_ = h.Store.Put("2026-04-19", "scratch")

	form := url.Values{"date": {"2026-04-19"}, "csrf": {csrf}}
	req := httptest.NewRequest("POST", "/diary/api/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APIDelete(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d, body: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "content", "diary", "2026-04-19.md")); !os.IsNotExist(err) {
		t.Errorf("file should be gone; stat err=%v", err)
	}
}

// --- 异常 / 边界 (WI-3.3) --------------------------------------------------

// 异常恢复：删除不存在的日期也应 200 ok（幂等）。
func TestDelete_Edge_IdempotentForMissing(t *testing.T) {
	h, _, cookie, csrf := setupHandlersWithCSRF(t)

	form := url.Values{"date": {"2026-04-19"}, "csrf": {csrf}}
	req := httptest.NewRequest("POST", "/diary/api/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APIDelete(rr, req)

	if rr.Code != 200 {
		t.Errorf("delete-missing status = %d, want 200 (idempotent)", rr.Code)
	}
}

// 权限：无 session → 401；POST 无 csrf → 403；方法不对 → 405。
func TestDelete_Edge_AuthAndCSRF(t *testing.T) {
	h, _, cookie, _ := setupHandlersWithCSRF(t)

	// 无 cookie
	form := url.Values{"date": {"2026-04-19"}, "csrf": {"any"}}
	req := httptest.NewRequest("POST", "/diary/api/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.APIDelete(rr, req)
	if rr.Code != 401 {
		t.Errorf("no-session status = %d, want 401", rr.Code)
	}

	// 有 cookie 但无 csrf
	form = url.Values{"date": {"2026-04-19"}}
	req = httptest.NewRequest("POST", "/diary/api/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr = httptest.NewRecorder()
	h.APIDelete(rr, req)
	if rr.Code != 403 {
		t.Errorf("no-csrf status = %d, want 403", rr.Code)
	}

	// GET 而非 POST
	req = httptest.NewRequest("GET", "/diary/api/delete", nil)
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr = httptest.NewRecorder()
	h.APIDelete(rr, req)
	if rr.Code != 405 {
		t.Errorf("GET /api/delete status = %d, want 405", rr.Code)
	}
}

// 非法输入：空 / 路径穿越 / 非法日历日 → 400，不触碰任何文件。
func TestDelete_Edge_InvalidDateReturns400AndLeavesOthersIntact(t *testing.T) {
	h, _, cookie, csrf := setupHandlersWithCSRF(t)
	// 先写一条合法日记；恶意 delete 不应误伤
	_ = h.Store.Put("2026-04-19", "keep me")

	for _, d := range []string{"", "..", "../foo", "2026-13-01", "abc", "2025-02-29"} {
		form := url.Values{"date": {d}, "csrf": {csrf}}
		req := httptest.NewRequest("POST", "/diary/api/delete", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "test/ua")
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		h.APIDelete(rr, req)
		if rr.Code != 400 {
			t.Errorf("delete date=%q → %d, want 400", d, rr.Code)
		}
	}
	// 合法日记仍在
	body, exists, _ := h.Store.Get("2026-04-19")
	if !exists || body != "keep me" {
		t.Errorf("malicious delete inputs corrupted valid entry; body=%q exists=%v", body, exists)
	}
}
