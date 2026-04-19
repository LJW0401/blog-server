package assets_test

import (
	"strings"
	"testing"
)

// Regression: 暗色模式下 .admin-card / .admin-section 必须有背景覆盖。
// 原 bug：管理后台的两类容器卡片在亮色规则里硬编码 background: #fff
// （admin_card:378, admin_section:402），暗色 @media block 只覆盖了
// 前台卡片（.repo-card/.project-card/.about-card/.doc-item/.proj 等），
// 没管 admin-* 家族，所以系统深色模式下整个后台仍是白底，反差刺眼。
func TestTheme_Regression_AdminCardDarkModeOverride(t *testing.T) {
	css := readTheme(t)

	darkStart := strings.Index(css, "@media (prefers-color-scheme: dark)")
	if darkStart < 0 {
		t.Fatal("dark-mode @media block not found")
	}
	openIdx := strings.Index(css[darkStart:], "{")
	if openIdx < 0 {
		t.Fatal("dark-mode @media opening brace not found")
	}
	depth := 0
	endRel := -1
	for i := openIdx; i < len(css)-darkStart; i++ {
		c := css[darkStart+i]
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				endRel = i
			}
		}
		if endRel >= 0 {
			break
		}
	}
	if endRel < 0 {
		t.Fatal("dark-mode @media block unbalanced")
	}
	darkBlock := css[darkStart : darkStart+endRel+1]

	for _, selector := range []string{".admin-card", ".admin-section"} {
		if !strings.Contains(darkBlock, selector) {
			t.Errorf("dark-mode block does not override %s — admin container will show white #fff in dark mode", selector)
			continue
		}
		// Ensure the override actually sets background (not just border etc.)
		idx := strings.Index(darkBlock, selector)
		ruleEnd := strings.Index(darkBlock[idx:], "}")
		if ruleEnd < 0 {
			t.Errorf("%s dark rule not closed", selector)
			continue
		}
		rule := darkBlock[idx : idx+ruleEnd]
		if !strings.Contains(rule, "background") {
			t.Errorf("dark-mode %s override missing background: %q", selector, rule)
		}
	}
}
