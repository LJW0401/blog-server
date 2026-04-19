package diary_test

import (
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/assets"
)

func readTemplate(t *testing.T, name string) string {
	t.Helper()
	f, err := assets.Templates().Open(name)
	if err != nil {
		t.Fatalf("open template %s: %v", name, err)
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	return string(b)
}

func readStatic(t *testing.T, path string) string {
	t.Helper()
	f, err := assets.Static().Open(path)
	if err != nil {
		t.Fatalf("open static %s: %v", path, err)
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	return string(b)
}

func readTheme(t *testing.T) string {
	t.Helper()
	return readStatic(t, "css/theme.css")
}

// --- Smoke (WI-2.6) --------------------------------------------------------

// diary.html 必须有 textarea / 保存按钮 / CSRF meta / script 引入 diary.js。
func TestUI_Smoke_DiaryTemplateContract(t *testing.T) {
	src := readTemplate(t, "diary.html")
	for _, want := range []string{
		`class="diary-shell"`,
		`class="diary-calendar"`,
		`class="diary-cell`,
		`class="diary-editor"`,
		`<textarea`,
		`diary-save-btn`,
		`meta name="csrf"`,
		`src="/static/js/diary.js"`,
		`defer`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("diary.html missing fragment %q", want)
		}
	}
}

// diary.js 必须包含关键客户端行为：IIFE、'use strict'、fetch、debounce、Ctrl+S
// 快捷键、日历点击委托。
func TestUI_Smoke_DiaryJSContract(t *testing.T) {
	src := readStatic(t, "js/diary.js")
	for _, want := range []string{
		"(function",                       // IIFE 包裹
		"'use strict'",                    // 严格模式
		"fetch(",                          // AJAX 端点调用
		"DEBOUNCE_MS",                     // debounce 常量
		"addEventListener('click'",        // 日历点击委托
		"addEventListener('input'",        // 输入触发 debounce
		"addEventListener('blur'",         // 离焦 flush
		"addEventListener('beforeunload'", // 卸载 flush
		"/diary/api/day",                  // GET 端点
		"/diary/api/save",                 // POST 端点
		"credentials: 'same-origin'",      // Cookie 跟随
		"csrf",                            // CSRF token 提取
		"(e.ctrlKey || e.metaKey) && e.key === 's'", // 保存快捷键
	} {
		if !strings.Contains(src, want) {
			t.Errorf("diary.js missing fragment %q", want)
		}
	}
}

// theme.css 里必须有 .diary-editor / .diary-textarea / .diary-status 的样式；
// 并且暗色 @media 块里对编辑器也有覆盖。
func TestUI_Smoke_DiaryCSSContract(t *testing.T) {
	css := readTheme(t)
	for _, want := range []string{
		".diary-shell",
		".diary-calendar",
		".diary-cell",
		".diary-editor",
		".diary-textarea",
		".diary-status",
		".diary-dot",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("theme.css missing selector %q", want)
		}
	}
	darkStart := strings.Index(css, "@media (prefers-color-scheme: dark)")
	if darkStart < 0 {
		t.Fatal("dark-mode block missing entirely")
	}
	darkTail := css[darkStart:]
	if !strings.Contains(darkTail, ".diary-cell") || !strings.Contains(darkTail, ".diary-editor") {
		t.Errorf("dark-mode block missing diary overrides")
	}
}

// --- 异常 / 边界 (WI-2.7) --------------------------------------------------

// diary.js 不得把 CSRF token 拼到 URL query（会在 referer / 日志里泄露）。
// 必须走 POST body。
func TestUI_Edge_DiaryJSDoesNotLeakCSRFInURL(t *testing.T) {
	src := readStatic(t, "js/diary.js")
	// "?csrf=" / "&csrf=" 都是在 URL 里拼 CSRF 的典型错误。
	for _, bad := range []string{"?csrf=", "&csrf="} {
		if strings.Contains(src, bad) {
			t.Errorf("diary.js leaks csrf in URL (%q); use POST body instead", bad)
		}
	}
}

// diary.js 的 fetch 调用必须带 credentials（否则 cookie 不发、认证失败）。
func TestUI_Edge_AllFetchesCarryCredentials(t *testing.T) {
	src := readStatic(t, "js/diary.js")
	// 计数：fetch( 出现 N 次，credentials 出现 >= N-1 次（beforeunload 用 sendBeacon 不算）
	fetches := strings.Count(src, "fetch(")
	creds := strings.Count(src, "credentials: 'same-origin'")
	if creds < fetches {
		t.Errorf("fetch calls = %d but credentials occurrences = %d", fetches, creds)
	}
}

// 保存失败反馈（WI-3.10）：diary.js 必须
//  1. catch fetch 错误 → 将 status 设为 error 态
//  2. error 状态粘滞：input 事件不能把 error 切回 editing（除非已 clear）
//  3. 点击 error 状态栏能触发重试
//
// CSS 中必须有 .diary-status 的 error 变体（视觉上要区分）。
func TestUI_Smoke_SaveFailureStickyState(t *testing.T) {
	js := readStatic(t, "js/diary.js")

	// 1. catch → set error
	if !strings.Contains(js, "setStatus('error'") {
		t.Errorf("diary.js 缺少保存失败 → error 状态的分支")
	}
	// 2. input 事件里要先判断当前状态不是 error 才覆盖
	if !strings.Contains(js, "!== 'error'") && !strings.Contains(js, "!==\"error\"") {
		t.Errorf("diary.js 未守卫 error 粘滞（input 事件应检查当前非 error 才能覆盖状态）")
	}
	// 3. 点击 status → 重试 saveDay
	if !strings.Contains(js, "status.addEventListener('click'") {
		t.Errorf("diary.js 缺少 status 点击重试监听")
	}

	// CSS 必须有 error 态样式
	css := readTheme(t)
	if !strings.Contains(css, `diary-status[data-state="error"]`) {
		t.Errorf("theme.css 缺少 .diary-status[data-state=\"error\"] 样式")
	}
}

// debounce 时间应为合理值（[500, 5000]ms 区间），避免写死过激进或过保守。
func TestUI_Edge_DebounceInReasonableRange(t *testing.T) {
	src := readStatic(t, "js/diary.js")
	// 直接寻找 "DEBOUNCE_MS = <N>"
	const marker = "DEBOUNCE_MS = "
	i := strings.Index(src, marker)
	if i < 0 {
		t.Fatal("DEBOUNCE_MS constant missing")
	}
	// 读后面的数字
	rest := src[i+len(marker):]
	end := strings.IndexAny(rest, ";\n,")
	if end < 0 {
		t.Fatal("DEBOUNCE_MS syntax unexpected")
	}
	numStr := strings.TrimSpace(rest[:end])
	n, err := strconv.Atoi(numStr)
	if err != nil {
		t.Fatalf("parse DEBOUNCE_MS %q: %v", numStr, err)
	}
	if n < 500 || n > 5000 {
		t.Errorf("DEBOUNCE_MS = %d, expected in [500,5000]", n)
	}
}
