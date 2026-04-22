package assets_test

import (
	"regexp"
	"strings"
	"testing"
)

// Regression: 暗色模式下 .portfolio-card / .portfolio-home-card 不再回退成白色。
//
// 原 bug 两层：
//  1. --card-bg 变量从未定义，`var(--card-bg, #fff)` 永远走 #fff 回退
//  2. 试图用 @media 暗色覆盖时放在亮色规则之前，按源码顺序被反向压过
//
// 当前方案：把 --card-bg 提升为全局变量 —— :root 定义亮色值、
// dark @media 覆盖为暗色值。卡片类只需要 `background: var(--card-bg)`。
// 测试为此方案加锁，任何一环缺失都视为回归：
//   - :root 必须定义 --card-bg
//   - 暗色 @media 必须覆盖 --card-bg
//   - 相关卡片类必须引用 var(--card-bg) 而非硬编码白色
func TestTheme_Regression_PortfolioCardDarkModeOverride(t *testing.T) {
	css := readTheme(t)

	// 1) :root 定义 --card-bg
	rootBlock := extractBlockAfter(t, css, ":root {")
	if !regexp.MustCompile(`--card-bg\s*:`).MatchString(rootBlock) {
		t.Error(":root 缺少 --card-bg 变量定义 —— 暗色模式下卡片会走未定义回退")
	}

	// 2) 暗色 @media 至少一处覆盖 --card-bg
	darkBlocks := findDarkMediaBlocks(t, css)
	if len(darkBlocks) == 0 {
		t.Fatal("未找到 @media (prefers-color-scheme: dark) 块")
	}
	darkHasCardBg := false
	for _, b := range darkBlocks {
		if regexp.MustCompile(`--card-bg\s*:`).MatchString(b) {
			darkHasCardBg = true
			break
		}
	}
	if !darkHasCardBg {
		t.Error("暗色 @media 未覆盖 --card-bg —— 卡片暗色下仍会沿用 :root 的亮色值")
	}

	// 3) 卡片类引用 var(--card-bg)，不能是硬编码颜色
	for _, selector := range []string{".portfolio-card", ".portfolio-home-card"} {
		rule := extractFirstRule(css, selector+" {")
		if rule == "" {
			t.Errorf("%s 规则未找到", selector)
			continue
		}
		if !strings.Contains(rule, "var(--card-bg") {
			t.Errorf("%s 应使用 var(--card-bg) 而非硬编码背景：%q", selector, rule)
		}
	}
}

func findDarkMediaBlocks(t *testing.T, css string) []string {
	t.Helper()
	var blocks []string
	cursor := 0
	for {
		at := strings.Index(css[cursor:], "@media (prefers-color-scheme: dark)")
		if at < 0 {
			return blocks
		}
		abs := cursor + at
		openRel := strings.Index(css[abs:], "{")
		if openRel < 0 {
			return blocks
		}
		depth := 0
		endRel := -1
		for i := openRel; i < len(css)-abs; i++ {
			switch css[abs+i] {
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
			return blocks
		}
		blocks = append(blocks, css[abs:abs+endRel+1])
		cursor = abs + endRel + 1
	}
}

func extractBlockAfter(t *testing.T, css, marker string) string {
	t.Helper()
	i := strings.Index(css, marker)
	if i < 0 {
		t.Fatalf("marker %q not found", marker)
	}
	rest := css[i+len(marker):]
	end := strings.Index(rest, "}")
	if end < 0 {
		t.Fatalf("block after %q not closed", marker)
	}
	return rest[:end]
}

func extractFirstRule(css, selectorOpen string) string {
	i := strings.Index(css, selectorOpen)
	if i < 0 {
		return ""
	}
	rest := css[i+len(selectorOpen):]
	end := strings.Index(rest, "}")
	if end < 0 {
		return ""
	}
	return rest[:end]
}
