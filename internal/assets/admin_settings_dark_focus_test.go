package assets_test

import (
	"strings"
	"testing"
)

// Regression: 暗色模式下 settings 页输入框/文本域聚焦后不能回退成白底。
// 原 bug：亮色规则把 .settings-form input:focus / textarea:focus 的 background
// 写死成 #fff，但暗色覆盖只修了 .admin-card input:focus；settings 页实际容器是
// .admin-section，而且 textarea 也没覆盖，所以点击后会闪成亮色。
func TestTheme_Regression_AdminSettingsDarkFocusOverride(t *testing.T) {
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

	if !strings.Contains(darkBlock, "color-scheme: dark;") {
		t.Fatal("dark-mode block missing color-scheme: dark — browser native form controls may still render with light appearance")
	}

	for _, selector := range []string{
		".settings-form input:focus",
		".settings-form textarea:focus",
	} {
		idx := strings.Index(darkBlock, selector)
		if idx < 0 {
			t.Errorf("dark-mode block does not override %s — settings fields will fall back to white focus background", selector)
			continue
		}
		ruleEnd := strings.Index(darkBlock[idx:], "}")
		if ruleEnd < 0 {
			t.Fatalf("%s dark rule not closed", selector)
		}
		rule := darkBlock[idx : idx+ruleEnd]
		if !strings.Contains(rule, "background") {
			t.Errorf("dark-mode %s override missing background declaration: %q", selector, rule)
		}
	}

	if strings.Contains(darkBlock, ".admin-section .settings-form input:focus") ||
		strings.Contains(darkBlock, ".admin-section .settings-form textarea:focus") {
		t.Error("dark-mode settings focus override must not use descendant selectors — admin-section and settings-form live on the same <form>")
	}
}
