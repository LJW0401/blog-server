package diary_test

import (
	"regexp"
	"strings"
	"testing"
)

// Smoke：theme.css 必须声明 diary 的窄屏 @media 规则
// （桌面 <900px 小窗口 + 手机 <640px），否则所有元素会被原样横向压扁。
func TestDiary_Smoke_NarrowMediaQueriesPresent(t *testing.T) {
	css := readTheme(t)

	// 两级断点都要有
	for _, bp := range []string{"@media (max-width: 900px)", "@media (max-width: 640px)"} {
		if !strings.Contains(css, bp) {
			t.Errorf("theme.css missing diary-breakpoint %q", bp)
		}
	}

	// 900px 级别至少要收紧 shell 的左右 padding（从 40px 到更小）
	re900 := regexp.MustCompile(`(?s)@media \(max-width: 900px\)\s*\{[^}]*\.diary-shell\s*\{[^}]*padding:\s*\d+px\s+(\d+)px`)
	m := re900.FindStringSubmatch(css)
	if m == nil {
		t.Errorf("900px @media block missing .diary-shell padding override")
	} else {
		// 横向 padding 必须 < 40（默认）
		if m[1] == "40" {
			t.Errorf("900px .diary-shell did not reduce horizontal padding (still 40px)")
		}
	}

	// 640px 级别：关键 wrap 规则都要到位，不然横向拥挤依旧
	b640 := extractMediaBlock(css, "@media (max-width: 640px)")
	if b640 == "" {
		t.Fatalf("@media (max-width: 640px) block not found")
	}
	for _, selector := range []string{".diary-shell", ".diary-nav", ".diary-calendar",
		".diary-cell", ".diary-editor", ".diary-textarea", ".diary-editor-actions"} {
		if !strings.Contains(b640, selector) {
			t.Errorf("640px block missing override for %q", selector)
		}
	}
	// flex-wrap 必须在 .diary-nav 和 .diary-editor-actions 至少出现一次
	if !regexp.MustCompile(`\.diary-nav\s*\{[^}]*flex-wrap:\s*wrap`).MatchString(b640) {
		t.Errorf(".diary-nav under 640px should wrap")
	}
	if !regexp.MustCompile(`\.diary-editor-actions\s*\{[^}]*flex-wrap:\s*wrap`).MatchString(b640) {
		t.Errorf(".diary-editor-actions under 640px should wrap")
	}
	// cell 高度要降（默认 72px，此处应 < 72）
	reCell := regexp.MustCompile(`\.diary-cell\s*\{[^}]*height:\s*(\d+)px`)
	cm := reCell.FindStringSubmatch(b640)
	if cm == nil {
		t.Errorf(".diary-cell height override missing in 640px block")
	} else if cm[1] == "72" {
		t.Errorf(".diary-cell height not reduced in 640px (still 72px)")
	}
}

// extractMediaBlock 粗略抠出 `@media (...) { ... }` 的大括号内 body，
// 遇到嵌套不展开——本项目 diary 媒体块没有嵌套，足够用。
func extractMediaBlock(css, header string) string {
	i := strings.Index(css, header)
	if i < 0 {
		return ""
	}
	open := strings.Index(css[i:], "{")
	if open < 0 {
		return ""
	}
	depth := 0
	start := i + open
	for j := start; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[start+1 : j]
			}
		}
	}
	return ""
}
