// dashboard_version_bar_test.go verifies the dashboard's "系统版本" bar renders
// from the update state injected into the request context by WithUpdateBanner.
package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/middleware"
)

// dashboardWith renders the dashboard with the given update state injected into
// the request context (the way WithUpdateBanner does in production).
func dashboardWith(t *testing.T, b *crudBundle, st middleware.UpdateBannerState) string {
	t.Helper()
	h := middleware.WithUpdateBanner(func() middleware.UpdateBannerState { return st })(http.HandlerFunc(b.Admin.Dashboard))
	req := httptest.NewRequest("GET", "/manage", nil)
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(b.Cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("dashboard status %d", w.Code)
	}
	return w.Body.String()
}

func TestDashboardVersionBar_Smoke_Available(t *testing.T) {
	b := crudSetup(t)
	body := dashboardWith(t, b, middleware.UpdateBannerState{
		Available: true, Current: "v1.0.0", Latest: "v2.0.0",
		ReleaseURL: "https://github.com/acme/blog/releases/latest", CheckedAt: time.Now(),
	})
	for _, want := range []string{"系统版本", "v1.0.0", "v2.0.0", "检测新版本", "有新版本",
		"查看发行说明", `href="https://github.com/acme/blog/releases/latest"`} {
		if !strings.Contains(body, want) {
			t.Errorf("version bar missing %q", want)
		}
	}
	// 自助更新已移除：不得再出现一键更新入口
	if strings.Contains(body, "更新到") || strings.Contains(body, "/manage/update\"") {
		t.Error("must not show any one-click self-update action")
	}
}

func TestDashboardVersionBar_AvailableNoReleaseURL(t *testing.T) {
	// 边界：有新版但未配置 update_repo（ReleaseURL 为空）→ 只显示徽章，不显示发行说明链接
	b := crudSetup(t)
	body := dashboardWith(t, b, middleware.UpdateBannerState{
		Available: true, Current: "v1.0.0", Latest: "v2.0.0", ReleaseURL: "", CheckedAt: time.Now(),
	})
	if !strings.Contains(body, "有新版本") {
		t.Error("should still show the new-version badge")
	}
	if strings.Contains(body, "查看发行说明") {
		t.Error("must not show release link when ReleaseURL is empty")
	}
}

func TestDashboardVersionBar_UpToDate(t *testing.T) {
	// 已是最新：显示徽章，不出现发行说明链接
	b := crudSetup(t)
	body := dashboardWith(t, b, middleware.UpdateBannerState{
		Available: false, Current: "v2.0.0", Latest: "v2.0.0",
		ReleaseURL: "https://github.com/acme/blog/releases/latest", CheckedAt: time.Now(),
	})
	if !strings.Contains(body, "已是最新") {
		t.Error("should show up-to-date badge")
	}
	if strings.Contains(body, "查看发行说明") {
		t.Error("no release link should appear when up to date")
	}
	// 检测按钮始终在
	if !strings.Contains(body, "检测新版本") {
		t.Error("check button should always be present")
	}
}
