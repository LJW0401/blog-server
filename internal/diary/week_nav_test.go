package diary_test

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// --- Smoke --------------------------------------------------------------

// `/diary?date=YYYY-MM-DD` 应把响应月份导向该日期所在月，并在 .diary-shell
// 上带 data-focus-date，客户端 JS 据此自动进入周视图。
func TestPage_Smoke_DateQueryDrivesMonthAndFocus(t *testing.T) {
	h, _, cookie := setupHandlers(t)
	req := httptest.NewRequest("GET", "/diary?date=2026-03-15", nil)
	req.AddCookie(cookie)
	req.Header.Set("User-Agent", "test/ua")
	rr := httptest.NewRecorder()
	h.Page(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "2026") || !strings.Contains(body, "3 月") {
		t.Errorf("page not rendering 2026-03 for date=2026-03-15")
	}
	if !strings.Contains(body, `data-focus-date="2026-03-15"`) {
		t.Errorf("missing data-focus-date=2026-03-15 on shell; body snippet:\n%s",
			body[:min(600, len(body))])
	}
}

// --- 异常 / 边界 --------------------------------------------------------

// 非法 date（路径穿越 / 非法日历日 / 空）→ 回退到 ?year&month 或当月，
// 并且 **不** 输出 data-focus-date（避免客户端误触发）。
func TestPage_Edge_InvalidDateFallsBackNoFocus(t *testing.T) {
	h, _, cookie := setupHandlers(t)
	for _, d := range []string{"../etc/passwd", "2026-13-01", "abc", "2025-02-29"} {
		req := httptest.NewRequest("GET", "/diary?date="+d, nil)
		req.AddCookie(cookie)
		req.Header.Set("User-Agent", "test/ua")
		rr := httptest.NewRecorder()
		h.Page(rr, req)
		if rr.Code != 200 {
			t.Errorf("date=%q status %d, want 200", d, rr.Code)
			continue
		}
		body := rr.Body.String()
		if strings.Contains(body, "data-focus-date=") {
			t.Errorf("date=%q: invalid date should not produce data-focus-date; body contains it", d)
		}
		// 不回显用户输入
		for _, bad := range []string{"passwd", "/etc/"} {
			if strings.Contains(body, bad) {
				t.Errorf("date=%q: body leaked input fragment %q", d, bad)
			}
		}
	}
}

// ?date= 优先于 ?year&month： date 给出合法值时应按 date 的月份渲染，
// 即使 year/month 给了别的值也听 date 的。
func TestPage_Edge_DateQueryOverridesYearMonth(t *testing.T) {
	h, _, cookie := setupHandlers(t)
	req := httptest.NewRequest("GET", "/diary?year=2099&month=1&date=2026-05-20", nil)
	req.AddCookie(cookie)
	req.Header.Set("User-Agent", "test/ua")
	rr := httptest.NewRecorder()
	h.Page(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "2026") || !strings.Contains(body, "5 月") {
		t.Errorf("date should override year/month; body snippet: %s",
			body[:min(500, len(body))])
	}
	if !strings.Contains(body, `data-focus-date="2026-05-20"`) {
		t.Errorf("focus date not applied")
	}
}

// --- UI 契约：JS 里有箭头键导航逻辑 -----------------------------------

// diary.js 必须绑定 keydown 监听并识别 ArrowLeft / ArrowRight 用于切周；
// 跨月必须走 `/diary?date=` 重载，而非本地 DOM 作假。
func TestUI_Smoke_ArrowKeyWeekNavigation(t *testing.T) {
	src := readStatic(t, "js/diary.js")
	for _, frag := range []string{
		"ArrowLeft",
		"ArrowRight",
		"navigateWeek",
		"/diary?date=", // 拼装重载 URL
		"shiftDays",    // 日期运算函数
		"data-focus-date",
	} {
		if !strings.Contains(src, frag) {
			t.Errorf("diary.js missing week-nav fragment %q", frag)
		}
	}

	// textarea 聚焦时不应拦截箭头键（避免破坏光标移动）
	if !strings.Contains(src, "TEXTAREA") {
		t.Errorf("diary.js should bail on ArrowLeft/Right when textarea focused")
	}
}

// 箭头键功能仅在周视图下启用，月视图下应 early-return。
// 静态扫描 diary.js 中存在 "editor.hidden" 的早退守卫。
func TestUI_Edge_ArrowKeysInactiveWhenEditorHidden(t *testing.T) {
	src := readStatic(t, "js/diary.js")

	// 找 keydown handler 里的 `if (editor.hidden) return`
	re := regexp.MustCompile(`keydown[\s\S]{0,500}editor\.hidden`)
	if !re.MatchString(src) {
		t.Errorf("diary.js: keydown 监听未检查 editor.hidden early-return")
	}
}

// 可见按钮：编辑器头上带 ← / → 按钮，点击也能切周（不光键盘）。
// 需求原话是"方向键"——既包括键盘按键，也要有 UI 上可见可点的按钮。
func TestUI_Smoke_VisibleWeekNavButtons(t *testing.T) {
	tpl := readTemplate(t, "diary.html")
	for _, want := range []string{
		`diary-week-prev`,
		`diary-week-next`,
		`aria-label="上一周"`,
		`aria-label="下一周"`,
	} {
		if !strings.Contains(tpl, want) {
			t.Errorf("diary.html missing week-nav fragment %q", want)
		}
	}

	js := readStatic(t, "js/diary.js")
	// JS 必须给两个按钮都绑定 click → navigateWeek
	if !strings.Contains(js, ".diary-week-prev") || !strings.Contains(js, ".diary-week-next") {
		t.Errorf("diary.js 未对 .diary-week-prev / .diary-week-next 绑定 click")
	}
	if !strings.Contains(js, "navigateWeek(-1)") || !strings.Contains(js, "navigateWeek(+1)") {
		t.Errorf("diary.js 未在按钮 click 里调用 navigateWeek(-1) / (+1)")
	}

	css := readTheme(t)
	if !strings.Contains(css, ".diary-week-btn") {
		t.Errorf("theme.css 缺少 .diary-week-btn 样式")
	}
}
