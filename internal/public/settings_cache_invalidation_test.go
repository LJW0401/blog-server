package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression: admin 保存新 qq_group 后，底部联系卡应立即反映新值。
// 原 bug：public.Handlers.resolveSettings 有 30s 内存缓存，而 admin 的
// SettingsSubmit 写入数据库后没有回调通知公共侧失效，于是底部 QQ 群号会
// 滞后到 30s 才更新。修复：引入 Handlers.InvalidateSettings() 并由 admin
// 在保存成功后调用。
func TestSettings_Regression_FooterQQUpdatesAfterInvalidate(t *testing.T) {
	h := setup(t, nil, nil)
	setTemplatesSettingsFn(t, h)

	// 1. 设置初始值，第一次渲染主页 → 缓存生效，footer 显示 11111111
	if err := h.SettingsDB.Set("qq_group", "11111111"); err != nil {
		t.Fatalf("set initial: %v", err)
	}
	rr1 := httptest.NewRecorder()
	h.Home(rr1, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rr1.Body.String(), "11111111") {
		t.Fatalf("initial qq not rendered; body: %s", rr1.Body.String())
	}

	// 2. 模拟 admin 再次保存，写入新值
	if err := h.SettingsDB.Set("qq_group", "99999999"); err != nil {
		t.Fatalf("set updated: %v", err)
	}

	// 3. 若不调 InvalidateSettings，缓存仍然命中，渲染的仍是旧值 → 这就是 bug
	rr2 := httptest.NewRecorder()
	h.Home(rr2, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rr2.Body.String(), "11111111") {
		t.Errorf("pre-condition check: expected stale cache to still show 11111111; if this fails, the cache design changed and this test needs updating")
	}

	// 4. 显式失效后再次渲染 → 必须是新值，而不是旧值
	h.InvalidateSettings()
	rr3 := httptest.NewRecorder()
	h.Home(rr3, httptest.NewRequest("GET", "/", nil))
	body3 := rr3.Body.String()
	if !strings.Contains(body3, "99999999") {
		t.Errorf("after InvalidateSettings(), footer should show new qq 99999999; body: %s", body3)
	}
	if strings.Contains(body3, "11111111") {
		t.Errorf("after InvalidateSettings(), footer still showed stale qq 11111111")
	}
}
