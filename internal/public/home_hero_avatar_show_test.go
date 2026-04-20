package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Smoke：avatar_url 设了 + avatar_show 未设（默认开）→ 头像正常显示。
func TestHome_Smoke_AvatarShownByDefault(t *testing.T) {
	h := setup(t, nil, nil)
	_ = h.SettingsDB.Set("avatar_url", "/images/avatar.png")
	// 不 Set avatar_show，模拟"老数据 / 新部署从未动过开关"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	if !strings.Contains(rr.Body.String(), `class="hero-avatar"`) {
		t.Errorf("默认应显示头像；body=%s", rr.Body.String())
	}
}

// Smoke：avatar_url 设了 + avatar_show=true → 头像显示。
func TestHome_Smoke_AvatarShownWhenToggleTrue(t *testing.T) {
	h := setup(t, nil, nil)
	_ = h.SettingsDB.Set("avatar_url", "/images/avatar.png")
	_ = h.SettingsDB.Set("avatar_show", "true")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	if !strings.Contains(rr.Body.String(), `class="hero-avatar"`) {
		t.Errorf("avatar_show=true 时应显示头像")
	}
}

// Smoke：avatar_url 设了 + avatar_show=false → 头像隐藏（文件保留但不渲染）。
func TestHome_Smoke_AvatarHiddenWhenToggleFalse(t *testing.T) {
	h := setup(t, nil, nil)
	_ = h.SettingsDB.Set("avatar_url", "/images/avatar.png")
	_ = h.SettingsDB.Set("avatar_show", "false")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	body := rr.Body.String()
	if strings.Contains(body, `class="hero-avatar"`) {
		t.Errorf("avatar_show=false 时不应输出 <img class=hero-avatar>；body=%s", body)
	}
	// URL 本身还在 settings 里（用户只是藏，没删），这条测试也暗示"藏"是可恢复操作
	// 不直接断言，只保证前台不出图
}

// Edge（边界值 / 非常规值）：avatar_show 写了个奇怪的字符串（既不是 true 也不是
// false，比如 "off"、"0"），resolveSettings 的策略是"只有显式 false 才关"，所以
// 这些值都等同于开。锁住这个约定，以后别无意改坏。
func TestHome_Edge_AvatarShowUnknownValueTreatedAsOn(t *testing.T) {
	cases := []string{"", "off", "0", "no", "TRUE", "False"}
	for _, val := range cases {
		t.Run("val="+val, func(t *testing.T) {
			h := setup(t, nil, nil)
			_ = h.SettingsDB.Set("avatar_url", "/images/avatar.png")
			_ = h.SettingsDB.Set("avatar_show", val)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/", nil)
			h.Home(rr, req)
			if !strings.Contains(rr.Body.String(), `class="hero-avatar"`) {
				t.Errorf("avatar_show=%q 不应被当成关；body=%s", val, rr.Body.String())
			}
		})
	}
}

// Edge（权限 / 边界）：没 URL 但 show=true → 仍然不渲染（没源图可用）。
func TestHome_Edge_NoURLEvenWhenShowTrue(t *testing.T) {
	h := setup(t, nil, nil)
	_ = h.SettingsDB.Set("avatar_show", "true")
	// 不 Set avatar_url

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	if strings.Contains(rr.Body.String(), "hero-avatar") {
		t.Errorf("没 URL 时不该渲染 hero-avatar")
	}
}
