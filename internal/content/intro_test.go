package content

import (
	"strings"
	"testing"
)

// NOTE: tests live inside package content so we can exercise unexported
// extractIntro directly (it's a pure function; keeping it unexported avoids
// polluting the public API surface).

// --- WI-1.5 Smoke -----------------------------------------------------------

func TestExtractIntro_Smoke_Basic(t *testing.T) {
	body := "<!-- portfolio:intro -->\n一段长简介\n<!-- /portfolio:intro -->\n\n# 详情\n正文\n"
	intro, rest := extractIntro(body)
	if intro != "一段长简介" {
		t.Errorf("intro=%q", intro)
	}
	if strings.Contains(rest, "portfolio:intro") {
		t.Errorf("rest still has marker: %q", rest)
	}
	if !strings.Contains(rest, "# 详情") {
		t.Errorf("rest missing body content: %q", rest)
	}
}

func TestExtractIntro_Smoke_MultiParagraph(t *testing.T) {
	body := `<!-- portfolio:intro -->
第一段。

第二段，**加粗**。
<!-- /portfolio:intro -->

# 标题
`
	intro, _ := extractIntro(body)
	if !strings.Contains(intro, "第一段") || !strings.Contains(intro, "第二段") {
		t.Errorf("intro missing expected paragraphs: %q", intro)
	}
}

func TestExtractIntro_Smoke_NoIntro(t *testing.T) {
	body := "# 只有正文\n没有 intro 块\n"
	intro, rest := extractIntro(body)
	if intro != "" {
		t.Errorf("intro should be empty, got %q", intro)
	}
	if rest != body {
		t.Errorf("rest should equal body verbatim")
	}
}

func TestExtractIntro_Smoke_IntroMidBody(t *testing.T) {
	// Intro markers anywhere in the body are valid; first-match wins.
	body := "# 开头段\n\n<!-- portfolio:intro -->\n中间块\n<!-- /portfolio:intro -->\n\n后续正文\n"
	intro, rest := extractIntro(body)
	if intro != "中间块" {
		t.Errorf("intro=%q", intro)
	}
	if !strings.Contains(rest, "# 开头段") || !strings.Contains(rest, "后续正文") {
		t.Errorf("rest missing surrounding content: %q", rest)
	}
	if strings.Contains(rest, "portfolio:intro") {
		t.Errorf("rest still has marker")
	}
}

// --- WI-1.6 Exception -------------------------------------------------------

func TestExtractIntro_Exception_Cases(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"only open no close", "<!-- portfolio:intro -->\nhanging\n"},
		{"only close no open", "hanging\n<!-- /portfolio:intro -->\n"},
		{"close before open", "<!-- /portfolio:intro -->\nfoo\n<!-- portfolio:intro -->\n"},
		{
			"nested duplicate open",
			"<!-- portfolio:intro --> a <!-- portfolio:intro --> b <!-- /portfolio:intro -->",
		},
		{
			"intro exceeds 4KB",
			"<!-- portfolio:intro -->\n" + strings.Repeat("x", 4*1024+1) + "\n<!-- /portfolio:intro -->",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			intro, rest := extractIntro(c.body)
			if intro != "" {
				t.Errorf("intro should be empty, got %q", intro)
			}
			if rest != c.body {
				t.Errorf("rest should equal body unchanged")
			}
		})
	}
}

func TestExtractIntro_Exception_EmptyIntroContent(t *testing.T) {
	// Empty content between markers is still a valid intro: intro="" but
	// rest has the tag pair removed. This is a deliberate edge: per spec
	// the marker pair is stripped; downstream templates will omit the
	// empty block.
	body := "a\n<!-- portfolio:intro --><!-- /portfolio:intro -->\nb"
	intro, rest := extractIntro(body)
	if intro != "" {
		t.Errorf("intro=%q want empty", intro)
	}
	if strings.Contains(rest, "portfolio:intro") {
		t.Errorf("rest still has marker: %q", rest)
	}
	if !strings.Contains(rest, "a") || !strings.Contains(rest, "b") {
		t.Errorf("surrounding content missing: %q", rest)
	}
}

func TestExtractIntro_Exception_ExactSizeLimit(t *testing.T) {
	// Exactly 4KB of intro content should pass.
	body := "<!-- portfolio:intro -->" + strings.Repeat("y", 4*1024) + "<!-- /portfolio:intro -->"
	intro, _ := extractIntro(body)
	if len(intro) != 4*1024 {
		t.Errorf("expected 4KB intro, got %d bytes", len(intro))
	}
}

func TestExtractIntro_Exception_InjectionAttempts(t *testing.T) {
	// Inside the intro, literal `<script>` and stray non-closing strings are
	// fine — they're just Markdown source; goldmark's safe mode handles the
	// rendering end. extractIntro should not blow up.
	body := "<!-- portfolio:intro -->\n<script>alert(1)</script>\nHTML &amp; entities\n```code```\n<!-- /portfolio:intro -->\n"
	intro, _ := extractIntro(body)
	if !strings.Contains(intro, "<script>") {
		t.Errorf("intro should carry source verbatim; goldmark decides rendering")
	}
}
