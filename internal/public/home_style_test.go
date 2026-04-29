package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Smoke（galaxy 路径）：把 site_settings.home_style = "galaxy" 写进去，
// 重新触发 settings cache，再请求 / 应渲染星系模板（含 #galaxy-app 与
// importmap），并把响应 CSP 放宽到允许 unpkg 与内联脚本。
func TestHome_Smoke_GalaxyStyleRenders(t *testing.T) {
	h := setup(t, nil, nil)
	if err := h.SettingsDB.Set("home_style", "galaxy"); err != nil {
		t.Fatalf("set home_style: %v", err)
	}
	h.InvalidateSettings()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.Home(w, req)
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	body := w.Body.String()
	for _, frag := range []string{
		`id="galaxy-app"`,
		`id="galaxy-config"`,
		`type="importmap"`,
		`unpkg.com/three`,
		`page-home-galaxy`,
	} {
		if !strings.Contains(body, frag) {
			t.Errorf("galaxy 模板缺少 %q", frag)
		}
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "https://unpkg.com") || !strings.Contains(csp, "'unsafe-inline'") {
		t.Errorf("galaxy CSP 未放宽：%q", csp)
	}
	// 回归：buildGalaxyConfig 必须以 template.JS 形式注入，否则 html/template
	// 会把 JSON 当成 JS 字符串字面量再包一层引号 + 反斜杠（JSON.parse 拿到的
	// 是字符串而非对象，整个星系脚本会瞎掉）。任何此类退化都会让下面这段
	// 原样的 sections 数组前缀消失。
	cfgFrag := `<script id="galaxy-config" type="application/json">{"sections":[`
	if !strings.Contains(body, cfgFrag) {
		t.Errorf("galaxy-config 未原样注入；前端 JSON.parse 会失败：缺片段 %q", cfgFrag)
	}
}

// Smoke（默认/简约路径）：未设置或显式设为 minimal 时仍渲染原 home.html
// 模板，且响应不应携带任何 CSP 覆盖（middleware 默认 CSP 由更上层注入，
// handler 自己不应碰 CSP 头）。
func TestHome_Smoke_MinimalStyleStillUsesHomeHTML(t *testing.T) {
	cases := []struct {
		name, value string
	}{
		{"unset", ""},
		{"explicit minimal", "minimal"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			h := setup(t, nil, nil)
			if c.value != "" {
				if err := h.SettingsDB.Set("home_style", c.value); err != nil {
					t.Fatalf("set: %v", err)
				}
				h.InvalidateSettings()
			}
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			h.Home(w, req)
			if w.Code != 200 {
				t.Fatalf("status: %d", w.Code)
			}
			body := w.Body.String()
			if strings.Contains(body, `id="galaxy-app"`) {
				t.Errorf("minimal 路径不应渲染星系模板")
			}
			// 简约风首页有 hero / Recently Active / 联系区域。
			for _, frag := range []string{"Recently Active", "保持联系"} {
				if !strings.Contains(body, frag) {
					t.Errorf("简约首页缺少 %q", frag)
				}
			}
			// handler 不应主动覆盖 CSP；httptest.Recorder 不跑中间件，
			// 所以这里读到空字符串是预期。
			if got := w.Header().Get("Content-Security-Policy"); got != "" {
				t.Errorf("minimal 路径不应主动写 CSP，got %q", got)
			}
		})
	}
}

// Edge（异常输入）：site_settings 里被写入未知值（理论上后台校验会拦下，
// 但万一历史脏数据 / 直接写 DB 绕开了校验）应回退为简约风，不应误触发
// galaxy 模板，否则 CSP 还会被放宽，等于把未知的脏数据当成了授权信号。
func TestHome_Edge_UnknownHomeStyleFallsBackToMinimal(t *testing.T) {
	h := setup(t, nil, nil)
	if err := h.SettingsDB.Set("home_style", "neon"); err != nil {
		t.Fatalf("set: %v", err)
	}
	h.InvalidateSettings()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.Home(w, req)
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, `id="galaxy-app"`) {
		t.Errorf("未知 home_style 不应触发 galaxy 模板")
	}
	if got := w.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("未知 home_style 不应放宽 CSP，got %q", got)
	}
}
