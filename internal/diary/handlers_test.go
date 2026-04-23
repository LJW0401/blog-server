package diary_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/assets"
	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/diary"
	"github.com/penguin/blog-server/internal/render"
	"github.com/penguin/blog-server/internal/storage"
)

// setupHandlers returns a wired-up Handlers + helpers for building
// authenticated / anonymous requests in handler tests. csrf 是当前 session 的
// CSRF token，用于构造 POST 请求。
func setupHandlers(t *testing.T) (h *diary.Handlers, dir string, cookie *http.Cookie) {
	h, dir, cookie, _ = setupHandlersWithCSRF(t)
	return
}

// setupHandlersWithCSRF 和 setupHandlers 一样，但多返一个 CSRF token，给 POST 测试用。
func setupHandlersWithCSRF(t *testing.T) (*diary.Handlers, string, *http.Cookie, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := storage.Open(dir)
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret, _ := auth.LoadOrCreateSecret(st.DB)
	authStore := auth.NewStore(st.DB, secret)

	store, err := diary.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := render.NewTemplates(assets.Templates(), render.NewMarkdown())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	h := diary.New(store, tpl, authStore, logger)
	// 固定 "今天" 为 2026-04-19，便于断言
	h.Now = func() time.Time { return time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC) }

	sess, cookie, err := authStore.IssueSession("admin", "test/ua", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return h, dir, cookie, sess.CSRF
}

// --- Smoke (WI-1.10) -------------------------------------------------------

func TestPage_Smoke_RendersCurrentMonth(t *testing.T) {
	h, dir, cookie := setupHandlers(t)

	// Fixture: 4-19 有日记
	if err := os.WriteFile(filepath.Join(dir, "content", "diary", "2026-04-19.md"),
		[]byte("---\ndate: 2026-04-19\n---\nhi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/diary?year=2026&month=4", nil)
	req.AddCookie(cookie)
	req.Header.Set("User-Agent", "test/ua")
	rr := httptest.NewRecorder()
	h.Page(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d, body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		"2026",
		"日记",             // title
		"diary-calendar", // table class
		"diary-today",    // today 高亮类 — 4-19 是 h.Now，必然出现
		"diary-dot",      // fixture 日 的绿点
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; body:\n%s", want, body)
		}
	}
}

func TestPage_Smoke_DefaultToCurrentMonthWithNoQuery(t *testing.T) {
	h, _, cookie := setupHandlers(t)
	req := httptest.NewRequest("GET", "/diary", nil)
	req.AddCookie(cookie)
	req.Header.Set("User-Agent", "test/ua")
	rr := httptest.NewRecorder()
	h.Page(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	// h.Now 固定到 2026-04，页面应渲染 2026 + 4 月
	body := rr.Body.String()
	if !strings.Contains(body, "2026") || !strings.Contains(body, "4 月") {
		t.Errorf("default view should be 2026-04; body snippet: %s", body[:min(500, len(body))])
	}
}

// --- 异常 / 边界 (WI-1.11) -------------------------------------------------

// 权限/认证：无 session cookie → 302，而且 body 不含任何已写入的日记内容。
func TestPage_Edge_AnonymousRedirectsAndLeaksNoContent(t *testing.T) {
	h, dir, _ := setupHandlers(t)
	const marker = "DIARY_SECRET_XYZ"
	if err := os.WriteFile(filepath.Join(dir, "content", "diary", "2026-04-19.md"),
		[]byte("---\ndate: 2026-04-19\n---\n"+marker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/diary", nil) // 无 cookie
	rr := httptest.NewRecorder()
	h.Page(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("anon should redirect, got status %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/manage/login?next=") {
		t.Errorf("redirect target = %q, want prefix /manage/login?next=", loc)
	}
	if strings.Contains(rr.Body.String(), marker) {
		t.Errorf("302 body leaked diary marker")
	}
}

// 非法输入：非数字 / 超界的 year、month 应回落到当前月，返回 200，不 400。
func TestPage_Edge_InvalidParamsFallbackToCurrentMonth(t *testing.T) {
	h, _, cookie := setupHandlers(t)
	cases := []string{
		"/diary?year=abc&month=xyz",
		"/diary?year=2099&month=13",
		"/diary?year=-1&month=-1",
		"/diary?year=1899&month=0",
		"/diary?month=14",
		"/diary?date=../etc/passwd", // 忽略未知参数，不因 date 出错
	}
	for _, url := range cases {
		req := httptest.NewRequest("GET", url, nil)
		req.AddCookie(cookie)
		req.Header.Set("User-Agent", "test/ua")
		rr := httptest.NewRecorder()
		h.Page(rr, req)
		if rr.Code != 200 {
			t.Errorf("%s: status = %d, want 200 (fallback)", url, rr.Code)
			continue
		}
		body := rr.Body.String()
		if !strings.Contains(body, "2026") || !strings.Contains(body, "4 月") {
			t.Errorf("%s: fallback should render 2026-04; body snippet: %s",
				url, body[:min(300, len(body))])
		}
	}
}

// 边界值：year 合法极限 1900 / 2200 都能渲染。
func TestPage_Edge_YearExtremes(t *testing.T) {
	h, _, cookie := setupHandlers(t)
	for _, url := range []string{"/diary?year=1900&month=1", "/diary?year=2200&month=12"} {
		req := httptest.NewRequest("GET", url, nil)
		req.AddCookie(cookie)
		req.Header.Set("User-Agent", "test/ua")
		rr := httptest.NewRecorder()
		h.Page(rr, req)
		if rr.Code != 200 {
			t.Errorf("%s: status = %d, want 200", url, rr.Code)
		}
	}
}

// 边界值：路径穿越参数 date 原本不是 Page 处理的参数，但如果以后被不小心用到，
// 必须依然不 500。此处仅断言当前实现对未知 query 静默。
func TestPage_Edge_UnknownQueryParamsIgnored(t *testing.T) {
	h, _, cookie := setupHandlers(t)
	req := httptest.NewRequest("GET", "/diary?foo=bar&baz=../etc/passwd", nil)
	req.AddCookie(cookie)
	req.Header.Set("User-Agent", "test/ua")
	rr := httptest.NewRecorder()
	h.Page(rr, req)
	if rr.Code != 200 {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
