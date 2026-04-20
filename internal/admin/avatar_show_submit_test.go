package admin_test

import (
	"net/url"
	"testing"
)

// Smoke：SettingsSubmit 支持复选框+同名 hidden 的表单结构——勾选时
// 前后同名字段先后是 ["true", "false"]，Go 的 r.Form.Get 取第一个 "true"；
// 未勾选只剩 hidden "false"，Get 得 "false"。
func TestSettingsSubmit_Smoke_AvatarShowCheckboxChecked(t *testing.T) {
	b := crudSetup(t)
	// 复选框 name 在前、hidden 在后（两者同名 avatar_show）
	form := url.Values{
		"csrf":        {b.CSRF},
		"tagline":     {"x"},
		"avatar_show": {"true", "false"},
	}
	w := b.authedPost(t, "/manage/settings", form, b.Settings.SettingsSubmit)
	if w.Code != 303 {
		t.Fatalf("status = %d", w.Code)
	}
	// 再 GET 看渲染出的 checkbox 是否 checked（说明 DB 里存的是 "true"/"其它非 false" = 开态）
	g := b.authedGet(t, "/manage/settings", b.Settings.SettingsPage)
	// 表现为 checkbox 元素带 `checked`（模板：{{ if ne value "false" }}checked{{ end }}）
	if !hasCheckedCheckbox(g.Body.String(), "avatar_show") {
		t.Errorf("avatar_show 勾选提交后应回显为 checked")
	}
}

// Smoke：未勾选时只提交 hidden = false → 存 false。
func TestSettingsSubmit_Smoke_AvatarShowCheckboxUnchecked(t *testing.T) {
	b := crudSetup(t)
	form := url.Values{
		"csrf":        {b.CSRF},
		"tagline":     {"x"},
		"avatar_show": {"false"},
	}
	w := b.authedPost(t, "/manage/settings", form, b.Settings.SettingsSubmit)
	if w.Code != 303 {
		t.Fatalf("status = %d", w.Code)
	}
	g := b.authedGet(t, "/manage/settings", b.Settings.SettingsPage)
	if hasCheckedCheckbox(g.Body.String(), "avatar_show") {
		t.Errorf("avatar_show 取消勾选后不应回显 checked")
	}
}

// hasCheckedCheckbox 找 `name="<name>"` 且同一标签内含 `checked` 的 input。粗糙
// 但足够 —— 页面里只有一个 avatar_show 的复选框元素（另一处是 hidden，无 checked）。
func hasCheckedCheckbox(body, name string) bool {
	// 简易：找 `type="checkbox" name="NAME" ... checked` 或 `name="NAME" ... type="checkbox" ... checked`
	// 本项目模板里顺序是 type="checkbox" name="avatar_show" value="true" checked
	marker := `name="` + name + `" value="true"`
	idx := indexOf(body, marker)
	if idx < 0 {
		return false
	}
	// checked 通常紧跟在 value="true" 之后
	tail := body[idx : min(idx+120, len(body))]
	return indexOf(tail, "checked") >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
