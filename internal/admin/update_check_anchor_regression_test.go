// update_check_anchor_regression_test.go pins the fix for a UI bug: clicking the
// dashboard "检测新版本" button scrolled the page back to the top. The button is a
// full-page form POST whose handler 303-redirects to /manage; a fragment-less
// redirect makes the browser reload at scroll position 0, away from the version
// bar the user was looking at. The fix redirects to /manage#version, with a
// matching anchor on the version-bar section so the browser lands back on it.
package admin_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/middleware"
	"github.com/penguin/blog-server/internal/update"
)

func TestUpdateCheck_Regression_RedirectKeepsScrollAnchor(t *testing.T) {
	// 回归：检测版本的重定向必须带 #version 锚点，否则整页刷新会跳回顶部。
	b := crudSetup(t)
	c := update.NewChecker("v1.0.0", func(context.Context, string) (string, string, bool, error) {
		return "v1.0.0", "e", false, nil
	}, time.Hour, nil)
	uh := newUpdateHandlers(b, c)

	w := b.authedPost(t, "/manage/update/check", url.Values{"csrf": {b.CSRF}}, uh.Check)
	loc := w.Header().Get("Location")
	if !strings.HasSuffix(loc, "#version") {
		t.Fatalf("redirect must carry the #version anchor to preserve scroll, got %q", loc)
	}
}

func TestDashboard_Regression_VersionBarHasAnchorTarget(t *testing.T) {
	// 回归：版本栏必须有 id="version" 作为锚点落点，否则 #version 重定向无处可落。
	b := crudSetup(t)
	body := dashboardWith(t, b, middleware.UpdateBannerState{
		Available: false, Current: "v1.0.0", Latest: "v1.0.0", CheckedAt: time.Now(),
	})
	if !strings.Contains(body, `id="version"`) {
		t.Error(`version bar must expose id="version" so the #version redirect lands on it`)
	}
}
