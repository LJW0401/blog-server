// update_test.go covers POST /manage/update/check — the dashboard force-check
// endpoint: the happy path (runs the check, 303-redirects back to the version
// bar) and its gates (missing CSRF, GET method, unauthenticated).
package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/admin"
	"github.com/penguin/blog-server/internal/update"
)

// newUpdateHandlers wires an UpdateHandlers onto the crud harness with a given
// release checker.
func newUpdateHandlers(b *crudBundle, c *update.Checker) *admin.UpdateHandlers {
	return &admin.UpdateHandlers{Parent: b.Admin, Checker: c}
}

func TestUpdateCheck_Smoke_RunsAndRedirects(t *testing.T) {
	b := crudSetup(t)
	calls := 0
	c := update.NewChecker("v1.0.0", func(context.Context, string) (string, string, bool, error) {
		calls++
		return "v1.0.0", "e", false, nil
	}, time.Hour, nil)
	uh := newUpdateHandlers(b, c)

	w := b.authedPost(t, "/manage/update/check", url.Values{"csrf": {b.CSRF}}, uh.Check)
	if w.Code != 303 {
		t.Fatalf("check want 303, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/manage#version" {
		t.Errorf("redirect want /manage#version, got %q", loc)
	}
	if calls != 1 {
		t.Errorf("CheckNow should run exactly once, got %d", calls)
	}
}

func TestUpdateCheck_Edge_CSRFMissing(t *testing.T) {
	// 权限/认证：缺 CSRF → 403，且不得触发任何检查
	b := crudSetup(t)
	calls := 0
	c := update.NewChecker("v1.0.0", func(context.Context, string) (string, string, bool, error) {
		calls++
		return "v1.0.0", "e", false, nil
	}, time.Hour, nil)
	uh := newUpdateHandlers(b, c)

	w := b.authedPost(t, "/manage/update/check", url.Values{}, uh.Check)
	if w.Code != 403 {
		t.Fatalf("missing CSRF want 403, got %d", w.Code)
	}
	if calls != 0 {
		t.Errorf("check must not run when CSRF rejected, got %d", calls)
	}
}

func TestUpdateCheck_Edge_GETRejected(t *testing.T) {
	// 非法方法：GET 必须 405，杜绝 GET ...?csrf=<token> 触发检查
	b := crudSetup(t)
	calls := 0
	c := update.NewChecker("v1.0.0", func(context.Context, string) (string, string, bool, error) {
		calls++
		return "v1.0.0", "e", false, nil
	}, time.Hour, nil)
	uh := newUpdateHandlers(b, c)

	// authedGet 携带有效会话 cookie，但方法是 GET → 应在鉴权前就被 405 挡下
	w := b.authedGet(t, "/manage/update/check", uh.Check)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET want 405, got %d", w.Code)
	}
	if calls != 0 {
		t.Errorf("GET must not trigger a check, got %d", calls)
	}
}

func TestUpdateCheck_Edge_Unauthenticated(t *testing.T) {
	// 权限/认证：无会话 cookie 的 POST 必须 401，且不触发检查
	b := crudSetup(t)
	calls := 0
	c := update.NewChecker("v1.0.0", func(context.Context, string) (string, string, bool, error) {
		calls++
		return "v1.0.0", "e", false, nil
	}, time.Hour, nil)
	uh := newUpdateHandlers(b, c)

	req := httptest.NewRequest("POST", "/manage/update/check", strings.NewReader("csrf="+b.CSRF))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	// 故意不 AddCookie → 无会话
	w := httptest.NewRecorder()
	uh.Check(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated want 401, got %d", w.Code)
	}
	if calls != 0 {
		t.Errorf("unauthenticated must not trigger a check, got %d", calls)
	}
}
