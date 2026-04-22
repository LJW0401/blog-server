package render

import (
	"strings"
	"testing"
)

func TestReadingMinutes_Smoke_MixedChineseEnglish(t *testing.T) {
	// 400 汉字 ~1 分钟，250 英文词 ~1 分钟，混排应累加为 2。
	body := strings.Repeat("测试", 200) + " " + strings.Repeat("word ", 250)
	if got := readingMinutes(body); got != 2 {
		t.Errorf("mixed 400 汉字 + 250 词 应为 2 分钟，got %d", got)
	}
}

func TestReadingMinutes_Exception_PureChineseNoLongerStuckAtOne(t *testing.T) {
	// 回归：旧实现 strings.Fields 对无空格中文永远返回 1 词。
	// 现在 2000 汉字应接近 5 分钟。
	body := strings.Repeat("中", 2000)
	if got := readingMinutes(body); got < 4 {
		t.Errorf("2000 汉字至少应 ≥4 分钟，got %d（旧实现会返回 1）", got)
	}
}

func TestReadingMinutes_Exception_Edges(t *testing.T) {
	cases := []struct {
		name string
		body string
		min  int // 下界（>=）
		max  int // 上界（<=）
	}{
		{"空字符串最少 1 分钟", "", 1, 1},
		{"单字最少 1 分钟", "a", 1, 1},
		{"代码块不计入", "```\n" + strings.Repeat("code ", 5000) + "\n```\n中文测试", 1, 1},
		{"图片 URL 不计入", strings.Repeat("![alt](https://example.com/very/long/url/path.png) ", 200), 1, 1},
		{"链接保留文字剥掉 URL", strings.Repeat("[词](https://example.com/foo) ", 250), 1, 2},
		{"HTML 标签剥掉", strings.Repeat("<div class=\"foo\">", 500) + strings.Repeat("中", 400), 1, 1},
	}
	for _, c := range cases {
		got := readingMinutes(c.body)
		if got < c.min || got > c.max {
			t.Errorf("%s: got %d, want [%d, %d]", c.name, got, c.min, c.max)
		}
	}
}

func TestReadingMinutes_Smoke_EnglishOnlyStillWorks(t *testing.T) {
	// 250 英文词 → 1 分钟；500 词 → 2 分钟。
	if got := readingMinutes(strings.Repeat("word ", 250)); got != 1 {
		t.Errorf("250 词应为 1 分钟，got %d", got)
	}
	if got := readingMinutes(strings.Repeat("word ", 500)); got != 2 {
		t.Errorf("500 词应为 2 分钟，got %d", got)
	}
}
