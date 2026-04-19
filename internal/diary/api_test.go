package diary_test

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// --- Smoke (WI-2.2) --------------------------------------------------------

func TestAPI_Smoke_SaveThenGetRoundtrip(t *testing.T) {
	h, _, cookie, csrf := setupHandlersWithCSRF(t)

	form := url.Values{
		"date":    {"2026-04-19"},
		"content": {"first draft"},
		"csrf":    {csrf},
	}
	req := httptest.NewRequest("POST", "/diary/api/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APISave(rr, req)

	if rr.Code != 200 {
		t.Fatalf("save status %d, body: %s", rr.Code, rr.Body.String())
	}
	var sr map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &sr)
	if sr["ok"] != true {
		t.Errorf("ok != true: %v", sr)
	}
	if _, has := sr["savedAt"]; !has {
		t.Errorf("savedAt missing")
	}

	req = httptest.NewRequest("GET", "/diary/api/day?date=2026-04-19", nil)
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr = httptest.NewRecorder()
	h.APIDay(rr, req)

	if rr.Code != 200 {
		t.Fatalf("day status %d, body: %s", rr.Code, rr.Body.String())
	}
	var dr map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &dr)
	if dr["body"] != "first draft" {
		t.Errorf("body = %v, want 'first draft'", dr["body"])
	}
}

func TestAPI_Smoke_EmptyContentDeletesFile(t *testing.T) {
	h, dir, cookie, csrf := setupHandlersWithCSRF(t)
	_ = h.Store.Put("2026-04-19", "scratch")

	form := url.Values{"date": {"2026-04-19"}, "content": {""}, "csrf": {csrf}}
	req := httptest.NewRequest("POST", "/diary/api/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APISave(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "content", "diary", "2026-04-19.md")); !os.IsNotExist(err) {
		t.Errorf("empty content should delete file; stat err=%v", err)
	}
}

// --- 异常 / 边界 (WI-2.3) --------------------------------------------------

// 权限/认证：无 session cookie → 401 JSON（不是 302）。
func TestAPI_Edge_UnauthenticatedReturns401JSON(t *testing.T) {
	h, _, _, _ := setupHandlersWithCSRF(t)

	req := httptest.NewRequest("GET", "/diary/api/day?date=2026-04-19", nil)
	rr := httptest.NewRecorder()
	h.APIDay(rr, req)
	if rr.Code != 401 {
		t.Errorf("anon GET api/day status = %d, want 401", rr.Code)
	}
	if !strings.HasPrefix(rr.Header().Get("Content-Type"), "application/json") {
		t.Errorf("anon GET not JSON: %q", rr.Header().Get("Content-Type"))
	}

	form := url.Values{"date": {"2026-04-19"}, "content": {"x"}, "csrf": {"anything"}}
	req = httptest.NewRequest("POST", "/diary/api/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr = httptest.NewRecorder()
	h.APISave(rr, req)
	if rr.Code != 401 {
		t.Errorf("anon POST save status = %d, want 401", rr.Code)
	}
}

// 权限/认证：有 session 但 POST 无 CSRF token → 403。
func TestAPI_Edge_PostWithoutCSRFReturns403(t *testing.T) {
	h, _, cookie, _ := setupHandlersWithCSRF(t)
	form := url.Values{"date": {"2026-04-19"}, "content": {"x"}}
	req := httptest.NewRequest("POST", "/diary/api/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APISave(rr, req)
	if rr.Code != 403 {
		t.Errorf("no-csrf status = %d, want 403", rr.Code)
	}
}

// 权限/认证：CSRF token 不匹配 → 403。
func TestAPI_Edge_PostWithMismatchedCSRFReturns403(t *testing.T) {
	h, _, cookie, _ := setupHandlersWithCSRF(t)
	form := url.Values{
		"date":    {"2026-04-19"},
		"content": {"x"},
		"csrf":    {"not_the_real_token_" + strings.Repeat("x", 32)},
	}
	req := httptest.NewRequest("POST", "/diary/api/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APISave(rr, req)
	if rr.Code != 403 {
		t.Errorf("mismatched-csrf status = %d, want 403", rr.Code)
	}
}

// 非法输入：空 / 路径穿越 / 非数字 / 非法日历日 → 400。
func TestAPI_Edge_InvalidDateReturns400(t *testing.T) {
	h, _, cookie, _ := setupHandlersWithCSRF(t)
	bad := []string{"", "..", "../foo", "2026-13-01", "abc", "2025-02-29"}
	for _, d := range bad {
		req := httptest.NewRequest("GET", "/diary/api/day?date="+url.QueryEscape(d), nil)
		req.Header.Set("User-Agent", "test/ua")
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		h.APIDay(rr, req)
		if rr.Code != 400 {
			t.Errorf("GET day?date=%q → %d, want 400", d, rr.Code)
		}
	}
}

// 边界值：2024-02-29 (闰年) 合法、2025-02-29 被拒。
func TestAPI_Edge_LeapDayBoundary(t *testing.T) {
	h, _, cookie, csrf := setupHandlersWithCSRF(t)

	for _, c := range []struct {
		date   string
		status int
	}{
		{"2024-02-29", 200},
		{"2025-02-29", 400},
	} {
		form := url.Values{"date": {c.date}, "content": {"ok"}, "csrf": {csrf}}
		req := httptest.NewRequest("POST", "/diary/api/save", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "test/ua")
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		h.APISave(rr, req)
		if rr.Code != c.status {
			t.Errorf("date=%s → %d, want %d", c.date, rr.Code, c.status)
		}
	}
}

// 并发：多个 goroutine 同时 POST save 同一日期 → 都 200，无 panic，
// 最终文件内容是其中一份（后写入胜出）。
func TestAPI_Edge_ConcurrentSaveNoPanic(t *testing.T) {
	h, _, cookie, csrf := setupHandlersWithCSRF(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			content := "version_" + string(rune('A'+i))
			form := url.Values{"date": {"2026-04-19"}, "content": {content}, "csrf": {csrf}}
			req := httptest.NewRequest("POST", "/diary/api/save", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("User-Agent", "test/ua")
			req.AddCookie(cookie)
			rr := httptest.NewRecorder()
			h.APISave(rr, req)
			if rr.Code != 200 {
				t.Errorf("concurrent save i=%d status=%d", i, rr.Code)
			}
		}()
	}
	wg.Wait()

	body, exists, err := h.Store.Get("2026-04-19")
	if err != nil || !exists {
		t.Fatalf("post-concurrent: exists=%v err=%v", exists, err)
	}
	if !strings.HasPrefix(body, "version_") {
		t.Errorf("final body not one of the versions: %q", body)
	}
}

// GET /api/day 对不存在的日期返回空 body + 200。
func TestAPI_Edge_GetDayForMissingDateReturnsEmpty(t *testing.T) {
	h, _, cookie, _ := setupHandlersWithCSRF(t)
	req := httptest.NewRequest("GET", "/diary/api/day?date=2026-04-19", nil)
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APIDay(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["body"] != "" {
		t.Errorf("body for missing date = %v, want empty", got["body"])
	}
}

// 错误方法 GET /api/save → 405。
func TestAPI_Edge_SaveWithGETReturns405(t *testing.T) {
	h, _, cookie, _ := setupHandlersWithCSRF(t)
	req := httptest.NewRequest("GET", "/diary/api/save", nil)
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APISave(rr, req)
	if rr.Code != 405 {
		t.Errorf("GET /api/save status = %d, want 405", rr.Code)
	}
}
