package assets_test

import (
	"strings"
	"testing"
)

// Regression: 暗色模式下 .portfolio-card 与 .portfolio-home-card 必须有背景覆盖。
// 原 bug：这两个类在亮色规则里写 `background: var(--card-bg, #fff)`，但
// `--card-bg` 变量从未在任何地方定义过（亮色或暗色都没），所以 UA 永远走
// 回退值 #fff；`.repo-card` / `.about-card` / `.admin-card` 都在 @media
// (prefers-color-scheme: dark) 里显式覆盖了 background，唯独作品集两个
// 卡片类漏掉，导致暗色模式下作品集首页区块和列表页仍是白卡。
func TestTheme_Regression_PortfolioCardDarkModeOverride(t *testing.T) {
	css := readTheme(t)

	// Find ALL dark @media blocks (theme.css has two).
	var darkBlocks []string
	searchFrom := 0
	for {
		start := strings.Index(css[searchFrom:], "@media (prefers-color-scheme: dark)")
		if start < 0 {
			break
		}
		abs := searchFrom + start
		openIdx := strings.Index(css[abs:], "{")
		if openIdx < 0 {
			t.Fatal("dark-mode @media opening brace not found")
		}
		depth := 0
		endRel := -1
		for i := openIdx; i < len(css)-abs; i++ {
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
			t.Fatal("dark-mode @media block unbalanced")
		}
		darkBlocks = append(darkBlocks, css[abs:abs+endRel+1])
		searchFrom = abs + endRel + 1
	}
	if len(darkBlocks) == 0 {
		t.Fatal("no dark-mode @media blocks found")
	}
	joined := strings.Join(darkBlocks, "\n")

	for _, selector := range []string{".portfolio-card", ".portfolio-home-card"} {
		idx := strings.Index(joined, selector)
		if idx < 0 {
			t.Errorf("dark-mode block does not override %s — card will render white #fff in dark mode", selector)
			continue
		}
		// Walk the rule body after the selector token and check for background.
		ruleEnd := strings.Index(joined[idx:], "}")
		if ruleEnd < 0 {
			t.Fatalf("%s dark rule not closed", selector)
		}
		rule := joined[idx : idx+ruleEnd]
		if !strings.Contains(rule, "background") {
			t.Errorf("dark-mode %s override missing background declaration: %q", selector, rule)
		}

		// 关键：源码顺序。@media 不改变 specificity，所以暗色覆盖必须在
		// 亮色规则之后出现；否则会被后者反向压过。
		lightRulePattern := selector + " {"
		lastLight := strings.LastIndex(css, lightRulePattern)
		// Find the dark override's absolute position in css (not joined).
		// Search for selector occurrences inside any dark block.
		darkPositions := []int{}
		start := 0
		for {
			at := strings.Index(css[start:], "@media (prefers-color-scheme: dark)")
			if at < 0 {
				break
			}
			abs := start + at
			blockOpen := strings.Index(css[abs:], "{")
			depth, endRel := 0, -1
			for i := blockOpen; i < len(css)-abs; i++ {
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
				break
			}
			block := css[abs : abs+endRel+1]
			if off := strings.Index(block, selector); off >= 0 {
				darkPositions = append(darkPositions, abs+off)
			}
			start = abs + endRel + 1
		}
		if len(darkPositions) == 0 {
			continue // already reported missing above
		}
		// 最后一处（同选择器最后出现的）必须在亮色规则之后。
		lastDark := darkPositions[len(darkPositions)-1]
		if lastLight >= 0 && lastDark < lastLight {
			t.Errorf("dark-mode %s override at offset %d appears BEFORE light rule at %d — CSS cascade will pick the later light rule and card stays white; move the dark block to end of file",
				selector, lastDark, lastLight)
		}
	}
}
