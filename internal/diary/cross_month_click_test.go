package diary_test

import (
	"regexp"
	"strings"
	"testing"
)

// Regression：跨月格（.diary-out-of-month）点击必须能跳到对应月份，
// 而不是被 JS 无声丢弃。现象：用户在 4-5 月交接那周看到 May 1-3 灰
// 色且不可点。
//
// 修复后约定：
//  1. diary.js 不再对 .diary-out-of-month 早退，而是改用
//     window.location.href = '/diary?date=...' 跳转
//  2. CSS 在 .diary-week-mode 下不再把跨月格置灰/禁用光标，
//     让用户视觉上感觉它和本月格一样可点
func TestCrossMonthClick_Regression_OutOfMonthCellIsNavigable(t *testing.T) {
	js := readStatic(t, "js/diary.js")

	// 1. diary.js 不得再对 .diary-out-of-month 做早退
	//    老代码形如：cell.classList.contains('diary-out-of-month')) return
	bad := "classList.contains('diary-out-of-month')) return"
	if strings.Contains(js, bad) {
		t.Errorf("diary.js 仍在 onCellClick 里对 .diary-out-of-month 早退，导致跨月格点击失效")
	}

	// 2. 必须存在跨月跳转分支：点了 out-of-month 格就走 /diary?date=...
	//    允许几种写法：提取 date 后调 location.href = ... ；或者调 navigateWeek-like 函数
	//    最低限度必须出现 "'/diary?date=' +" 的字符串拼接入口
	if !strings.Contains(js, "'/diary?date=' +") && !strings.Contains(js, `"/diary?date=" +`) {
		t.Errorf("diary.js 缺少 /diary?date=... 跳转入口，跨月格点击无法导航")
	}

	// 3. CSS 把 .diary-out-of-month 渲染成可点外观（月视图 / 周视图统一样式）
	css := readTheme(t)
	// 基础规则必须给 .diary-out-of-month 一个 cursor: pointer（不再 default/disabled）
	re := regexp.MustCompile(`(?s)\.diary-out-of-month\s*\{[^}]*cursor:\s*pointer`)
	if !re.MatchString(css) {
		t.Errorf("theme.css 的 .diary-out-of-month 未设为 cursor: pointer，跨月格仍显禁用态")
	}
}
