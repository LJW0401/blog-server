package admin_test

import (
	"net/url"
	"strings"
	"testing"
)

// Regression: SettingsSubmit 成功后必须回调 Invalidate，以便让 public 侧的
// 站点设置缓存立即失效。否则 30s 内访问者看到的仍是旧值（典型症状：添加
// QQ 群号后底部没更新）。
func TestSettingsSubmit_Regression_InvalidateCalledOnSuccess(t *testing.T) {
	b := crudSetup(t)
	var called int
	b.Settings.Invalidate = func() { called++ }

	form := url.Values{
		"csrf":     {b.CSRF},
		"tagline":  {"x"},
		"qq_group": {"12345678"},
	}
	w := b.authedPost(t, "/manage/settings", form, b.Settings.SettingsSubmit)
	if w.Code != 303 {
		t.Fatalf("status %d", w.Code)
	}
	if called != 1 {
		t.Errorf("Invalidate callback not invoked on successful save; called=%d", called)
	}
}

// Edge (非法输入)：校验失败时不应触发 Invalidate，避免白白清空 public 缓存。
func TestSettingsSubmit_Edge_InvalidateSkippedOnValidationError(t *testing.T) {
	b := crudSetup(t)
	var called int
	b.Settings.Invalidate = func() { called++ }

	// tagline 空 → 校验失败路径
	form := url.Values{"csrf": {b.CSRF}, "tagline": {""}}
	w := b.authedPost(t, "/manage/settings", form, b.Settings.SettingsSubmit)
	if w.Code != 303 {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "e=") {
		t.Fatalf("expected error redirect, got %q", w.Header().Get("Location"))
	}
	if called != 0 {
		t.Errorf("Invalidate should not fire on validation error; called=%d", called)
	}
}

// Edge (边界值)：Invalidate 为 nil 时 SettingsSubmit 不应 panic。
func TestSettingsSubmit_Edge_NilInvalidateIsNoOp(t *testing.T) {
	b := crudSetup(t)
	// 不设置 Invalidate；保持 nil
	form := url.Values{
		"csrf":     {b.CSRF},
		"tagline":  {"x"},
		"qq_group": {"12345678"},
	}
	w := b.authedPost(t, "/manage/settings", form, b.Settings.SettingsSubmit)
	if w.Code != 303 {
		t.Fatalf("status %d (nil Invalidate should be safe, not panic)", w.Code)
	}
}
